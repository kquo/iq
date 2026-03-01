package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// ── OpenAI-compatible types ───────────────────────────────────────────────────

type chatMessage struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

type chatRequest struct {
	Messages          []chatMessage `json:"messages"`
	Stream            bool          `json:"stream"`
	MaxTokens         int           `json:"max_tokens,omitempty"`
	RepetitionPenalty float64       `json:"repetition_penalty,omitempty"`
}

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// ── Session ───────────────────────────────────────────────────────────────────

type session struct {
	ID          string        `yaml:"id"`
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Cue         string        `yaml:"cue"`
	Tier        string        `yaml:"tier"`
	Created     string        `yaml:"created"`
	Updated     string        `yaml:"updated"`
	Messages    []chatMessage `yaml:"messages"`
}

func sessionsDir() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, "sessions")
	return d, os.MkdirAll(d, 0755)
}

func sessionPath(id string) (string, error) {
	d, err := sessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, id+".yaml"), nil
}

func loadSession(id string) (*session, error) {
	path, err := sessionPath(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s session
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveSession(s *session) error {
	s.Updated = time.Now().UTC().Format(time.RFC3339)
	path, err := sessionPath(s.ID)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func newSession(cueN, tierN string) *session {
	id := shortID()
	return &session{
		ID:      id,
		Cue:     cueN,
		Tier:    tierN,
		Created: time.Now().UTC().Format(time.RFC3339),
		Updated: time.Now().UTC().Format(time.RFC3339),
	}
}

func shortID() string {
	return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
}

// ── Classification ────────────────────────────────────────────────────────────
// Embedding-based classification is implemented in embed.go (embedClassify).
// This section is intentionally empty.

// ── Routing ───────────────────────────────────────────────────────────────────

type routeResult struct {
	CueName       string
	Category      string
	SuggestedTier string
	SystemPrompt  string
	Tier          string
	Port          int
	ModelID       string
	TierSource    string // "cue_override", "suggested_tier", "fallback"
}

func resolveRoute(cueName string, cues []Cue) (*routeResult, error) {
	_, cue := findCue(cues, cueName)
	if cue == nil {
		return nil, fmt.Errorf("cue %q not found", cueName)
	}

	// Direct model override on the cue — kept for power users but not
	// actively promoted. Find which tier it belongs to and pick its sidecar.
	if cue.Model != "" {
		tier := tierForModel(cue.Model)
		if tier == "" {
			return nil, fmt.Errorf("cue %q has model %q but it is not in any tier pool", cueName, cue.Model)
		}
		sidecar, err := pickSidecar(tier, false)
		if err != nil {
			return nil, fmt.Errorf("cue model override: %w", err)
		}
		return &routeResult{
			CueName:       cueName,
			Category:      cue.Category,
			SuggestedTier: cue.SuggestedTier,
			SystemPrompt:  cue.SystemPrompt,
			Tier:          tier,
			Port:          sidecar.Port,
			ModelID:       sidecar.Model,
			TierSource:    "cue_override",
		}, nil
	}

	// Use suggested_tier, fall back to "fast".
	tier := cue.SuggestedTier
	tierSource := "suggested_tier"
	if tier != "fast" && tier != "slow" {
		tier = "fast"
		tierSource = "fallback"
	}
	sidecar, err := pickSidecar(tier, false)
	if err != nil {
		// Try the other tier as fallback.
		other := "slow"
		if tier == "slow" {
			other = "fast"
		}
		sidecar, err = pickSidecar(other, false)
		if err != nil {
			return nil, fmt.Errorf("no running sidecars in %s or %s tier — run 'iq svc start'", tier, other)
		}
		tier = other
		tierSource = "fallback"
	}

	return &routeResult{
		CueName:       cueName,
		Category:      cue.Category,
		SuggestedTier: cue.SuggestedTier,
		SystemPrompt:  cue.SystemPrompt,
		Tier:          tier,
		Port:          sidecar.Port,
		ModelID:       sidecar.Model,
		TierSource:    tierSource,
	}, nil
}

// ── Trace output ──────────────────────────────────────────────────────────────

const traceWidth = 100 // wrap width for trace content

// traceStep prints a step header.
func traceStep(n int, label, arrow string) {
	fmt.Fprintf(os.Stderr, "%s\n",
		utl.Gra(fmt.Sprintf("STEP %d  %-20s → %s", n, label, arrow)))
}

// traceField prints "  label  value" with continuation lines indented to match.
func traceField(label, value string) {
	prefix := fmt.Sprintf("  %-12s  ", label)
	indent := strings.Repeat(" ", len(prefix))
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(prefix), utl.Gra(lines[0]))
	for _, l := range lines[1:] {
		fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(indent), utl.Gra(l))
	}
}

// wrapText hard-wraps text at width, preserving existing newlines.
func wrapText(text string, width int) string {
	var out strings.Builder
	for para := range strings.SplitSeq(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out.WriteByte('\n')
			continue
		}
		col := 0
		for i, w := range words {
			if i == 0 {
				out.WriteString(w)
				col = len(w)
			} else if col+1+len(w) > width {
				out.WriteByte('\n')
				out.WriteString(w)
				col = len(w)
			} else {
				out.WriteByte(' ')
				out.WriteString(w)
				col += 1 + len(w)
			}
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

// traceBlock prints a role label then the content indented below it.
// User content is highlighted green when highlightUser=true.
func traceBlock(role, content string, highlightUser bool) {
	const blockIndent = "    "
	colorFn := func(s string) string { return utl.Gra(s) }
	if role == "user" && highlightUser {
		colorFn = func(s string) string { return utl.Gre(s) }
	}
	fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("  [%s]", role)))
	wrapped := wrapText(content, traceWidth-len(blockIndent))
	for l := range strings.SplitSeq(wrapped, "\n") {
		fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), colorFn(l))
	}
}

// printStep1Classify prints the embedding classification trace.
func printStep1Classify(t *embedClassifyTrace) {
	cacheStr := "rebuilt"
	if t.CacheHit {
		cacheStr = "hit"
	}
	traceStep(1, "CLASSIFY", fmt.Sprintf("embed-cue :%d", embedCuePort))
	traceField("model", t.Model)
	traceField("resolved", t.Resolved)
	traceField("score", fmt.Sprintf("%.4f", t.Score))
	traceField("cache", cacheStr)
	traceField("elapsed", fmt.Sprintf("%dms", t.Elapsed.Milliseconds()))
}

// printStep2Route prints the routing decision.
func printStep2Route(route *routeResult, elapsed time.Duration) {
	traceStep(2, "RESOLVE ROUTE", "Go code")
	traceField("cue", route.CueName)
	traceField("category", route.Category)
	traceField("suggested", route.SuggestedTier)
	traceField("tier", fmt.Sprintf("%s  (source: %s)", route.Tier, route.TierSource))
	traceField("model", route.ModelID)
	traceField("endpoint", fmt.Sprintf("http://localhost:%d", route.Port))
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep3KB prints the knowledge base retrieval trace.
func printStep3KB(results []kbResult, elapsed time.Duration) {
	traceStep(3, "KB RETRIEVE", fmt.Sprintf("kb.json → %d chunks", len(results)))
	for _, r := range results {
		traceField("chunk", fmt.Sprintf("score:%.4f  %s:%d–%d",
			r.Score, r.Chunk.Source, r.Chunk.LineStart, r.Chunk.LineEnd))
	}
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep4EffectivePrompt prints the full message array that will be sent.
func printStep4EffectivePrompt(messages []chatMessage, route *routeResult) {
	traceStep(4, "EFFECTIVE PROMPT", fmt.Sprintf("%s :%d", route.Tier, route.Port))
	for _, m := range messages {
		traceBlock(m.Role, m.Content, true)
	}
}

// printStep6Session prints session persistence info.
func printStep6Session(sess *session, path string, elapsed time.Duration) {
	traceStep(6, "SESSION", "Go code")
	traceField("id", sess.ID)
	traceField("saved", path)
	traceField("turns", fmt.Sprintf("%d", (len(sess.Messages)-1)/2))
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// ── Sidecar HTTP ──────────────────────────────────────────────────────────────

// stripThinkBlocks removes <think>...</think> reasoning blocks emitted by
// models like DeepSeek-R1 before returning the response to the user.
func stripThinkBlocks(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed tag — strip from <think> to end of string.
			s = strings.TrimSpace(s[:start])
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func callSidecar(port int, messages []chatMessage, stream bool, maxTokens int) (string, error) {
	req := chatRequest{Messages: messages, Stream: false, MaxTokens: maxTokens, RepetitionPenalty: 1.3}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq svc start': %w", port, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from sidecar")
	}
	content := result.Choices[0].Message.Content
	return stripThinkBlocks(content), nil
}

func streamSidecar(port int, messages []chatMessage) (string, error) {
	req := chatRequest{Messages: messages, Stream: true, MaxTokens: 8192, RepetitionPenalty: 1.3}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq svc start': %w", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, b)
	}

	// Collect all tokens. If the model uses <think> blocks (e.g. DeepSeek-R1),
	// we suppress streaming output entirely and print the clean result at the end.
	// For non-thinking models, tokens stream normally as they arrive.
	var full strings.Builder
	hasThink := false

	// Use a large scanner buffer — DeepSeek-R1 think blocks can produce
	// SSE lines well in excess of bufio's default 64KB limit.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			token := choice.Delta.Content
			if token == "" {
				continue
			}
			full.WriteString(token)
			if strings.Contains(full.String(), "<think>") {
				hasThink = true
			}
			// Only stream to stdout if we have not encountered a think block.
			if !hasThink {
				fmt.Print(token)
			}
		}
	}
	result := stripThinkBlocks(full.String())
	if hasThink {
		// Print the clean result after stripping think blocks.
		fmt.Print(result)
	}
	fmt.Println()
	return strings.TrimSpace(result), scanner.Err()
}

// ── Auto-name session (background) ───────────────────────────────────────────

func autoNameSession(s *session) {
	go func() {
		if len(s.Messages) < 2 {
			return
		}
		var excerpt strings.Builder
		for _, m := range s.Messages {
			if m.Role == "system" {
				continue
			}
			fmt.Fprintf(&excerpt, "%s: %s\n", m.Role, truncate(m.Content, 200))
		}
		systemMsg := `Given this conversation excerpt, return a JSON object with exactly two fields:
"name" (max 5 words, title case) and "description" (max 15 words).
Return only valid JSON, nothing else.`
		sidecar, err := pickSidecar("fast", true)
		if err != nil {
			return
		}
		response, err := callSidecar(sidecar.Port, []chatMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: excerpt.String()},
		}, false, 60)
		if err != nil {
			return
		}
		clean := strings.TrimSpace(response)
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)

		var result struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal([]byte(clean), &result); err != nil {
			return
		}
		if result.Name != "" {
			s.Name = result.Name
		}
		if result.Description != "" {
			s.Description = result.Description
		}
		saveSession(s)
	}()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ── Core prompt execution ─────────────────────────────────────────────────────

type promptOpts struct {
	cueName   string
	category  string
	tier      string
	sessionID string
	dryRun    bool
	debug     bool
	noStream  bool
	noKB      bool
}

func executePrompt(input string, opts promptOpts, sess *session) (*session, error) {
	trace := opts.dryRun || opts.debug
	cues, err := loadCues()
	if err != nil {
		return sess, err
	}

	// ── Step 1: Classify ──
	cueName := opts.cueName
	var et *embedClassifyTrace

	if cueName == "" && opts.tier == "" {
		candidates := cues
		if opts.category != "" {
			candidates = nil
			for _, r := range cues {
				if r.Category == opts.category {
					candidates = append(candidates, r)
				}
			}
			if len(candidates) == 0 {
				return sess, fmt.Errorf("no cues in category %q", opts.category)
			}
		}
		if !embedSidecarAlive("cue") {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("embed-cue sidecar not running — falling back to initial cue (run: iq svc start)"))
			cueName = "initial"
		} else {
			cfg2, cfgErr := loadConfig()
			em := defaultCueModel
			if cfgErr == nil {
				em = cueModel(cfg2)
			}
			cueName, et, err = embedClassify(input, candidates, em)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("classification error: "+err.Error()+", falling back to initial"))
				cueName = "initial"
			}
		}
	}
	if cueName == "" {
		cueName = "initial"
	}
	if trace && et != nil {
		printStep1Classify(et)
	}

	// ── Step 2: Resolve route ──
	var route *routeResult
	t2 := time.Now()
	if opts.tier != "" {
		sidecar, sErr := pickSidecar(opts.tier, false)
		if sErr != nil {
			return sess, fmt.Errorf("--tier %s: %w", opts.tier, sErr)
		}
		route = &routeResult{
			CueName:      "none",
			TierSource:   "flag_override",
			SystemPrompt: "",
			Tier:         opts.tier,
			Port:         sidecar.Port,
			ModelID:      sidecar.Model,
		}
	} else {
		route, err = resolveRoute(cueName, cues)
		if err != nil {
			return sess, err
		}
	}
	if trace {
		printStep2Route(route, time.Since(t2))
	}

	// ── Step 3: KB retrieval ──
	var kbContext string
	if kbExists() && !opts.noKB && embedSidecarAlive("kb") {
		t3 := time.Now()
		results, kbErr := KBSearch(input, 5)
		if kbErr == nil && len(results) > 0 {
			kbContext = KBContext(results)
			if trace {
				printStep3KB(results, time.Since(t3))
			}
		} else if kbErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("kb search error: "+kbErr.Error()))
		}
	}

	// ── Step 4: Build messages + effective prompt ──
	var messages []chatMessage
	if sess != nil && len(sess.Messages) > 0 {
		messages = append(messages, sess.Messages...)
		messages = append(messages, chatMessage{Role: "user", Content: input})
	} else if kbContext != "" {
		// Use the cue's system prompt (or a generic fallback) and inject KB
		// context as a prefix in the user message, immediately before the
		// question. Avoids overriding the cue prompt and the hard "only use
		// the text above" constraint that was tuned for tiny models.
		sysprompt := route.SystemPrompt
		if sysprompt == "" {
			sysprompt = "You are a helpful assistant."
		}
		userContent := kbContext + "\n\n" + input
		messages = append(messages, chatMessage{Role: "system", Content: sysprompt})
		messages = append(messages, chatMessage{Role: "user", Content: userContent})
	} else {
		if route.SystemPrompt != "" {
			messages = append(messages, chatMessage{Role: "system", Content: route.SystemPrompt})
		}
		messages = append(messages, chatMessage{Role: "user", Content: input})
	}
	if trace {
		printStep4EffectivePrompt(messages, route)
	}

	// Dry-run stops here.
	if opts.dryRun {
		return sess, nil
	}

	// ── Step 5: Infer ──
	var t5 time.Time
	if trace {
		traceStep(5, "INFER", fmt.Sprintf("%s :%d", route.Tier, route.Port))
		t5 = time.Now()
	}

	var response string
	if opts.noStream {
		response, err = callSidecar(route.Port, messages, false, 8192)
		if err != nil {
			return sess, err
		}
		fmt.Println(response)
	} else {
		response, err = streamSidecar(route.Port, messages)
		if err != nil {
			return sess, err
		}
	}
	if trace {
		traceField("elapsed", fmt.Sprintf("%dms", time.Since(t5).Milliseconds()))
	}

	// ── Step 6: Persist session ──
	if opts.sessionID != "" || sess != nil {
		if sess == nil {
			sess = newSession(route.CueName, route.Tier)
			if opts.sessionID != "" {
				sess.ID = opts.sessionID
			}
			if route.SystemPrompt != "" {
				sess.Messages = append(sess.Messages, chatMessage{Role: "system", Content: route.SystemPrompt})
			}
		}
		sess.Messages = append(sess.Messages,
			chatMessage{Role: "user", Content: input},
			chatMessage{Role: "assistant", Content: response},
		)
		if err := saveSession(sess); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: failed to save session: "+err.Error()))
		}
		if trace {
			t6 := time.Now()
			sp, _ := sessionPath(sess.ID)
			printStep6Session(sess, sp, time.Since(t6))
		}
		if len(sess.Messages) <= 3 {
			autoNameSession(sess)
		}
	}

	return sess, nil
}

// ── REPL ──────────────────────────────────────────────────────────────────────

var replCommands = map[string]string{
	"/cue":     "show or set current cue  (e.g. /cue math_reasoning)",
	"/session": "show current session info",
	"/clear":   "clear session history (start fresh)",
	"/dry-run": "toggle dry-run mode",
	"/debug":   "toggle debug trace mode",
	"/quit":    "exit the REPL",
	"/help":    "show REPL commands",
}

func runREPL(opts promptOpts) error {
	var sess *session
	if opts.sessionID != "" {
		var err error
		sess, err = loadSession(opts.sessionID)
		if err != nil {
			return err
		}
	}

	dryRun := opts.dryRun
	debug := opts.debug
	fmt.Fprintf(os.Stderr, "%s  %s\n", utl.Whi2("IQ"), utl.Gra("type /help for commands, /quit to exit"))

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(utl.Gre("> "))
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			break
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		switch input {
		case "/quit", "/q":
			return nil
		case "/help":
			for cmd, desc := range replCommands {
				fmt.Printf("  %-12s %s\n", cmd, desc)
			}
			continue
		case "/session":
			if sess == nil {
				fmt.Println(utl.Gra("no active session"))
			} else {
				fmt.Printf("id:          %s\n", sess.ID)
				fmt.Printf("name:        %s\n", sess.Name)
				fmt.Printf("description: %s\n", sess.Description)
				fmt.Printf("cue:         %s\n", sess.Cue)
				fmt.Printf("tier:        %s\n", sess.Tier)
				fmt.Printf("turns:       %d\n", len(sess.Messages))
			}
			continue
		case "/clear":
			sess = nil
			if opts.sessionID != "" {
				sess = newSession(opts.cueName, opts.tier)
				sess.ID = opts.sessionID
			}
			fmt.Println(utl.Gra("session cleared"))
			continue
		case "/dry-run":
			dryRun = !dryRun
			debug = false
			state := "off"
			if dryRun {
				state = "on"
			}
			fmt.Printf("%s %s\n", utl.Gra("dry-run:"), state)
			continue
		case "/debug":
			debug = !debug
			dryRun = false
			state := "off"
			if debug {
				state = "on"
			}
			fmt.Printf("%s %s\n", utl.Gra("debug:"), state)
			continue
		}

		if strings.HasPrefix(input, "/cue") {
			parts := strings.Fields(input)
			if len(parts) == 1 {
				fmt.Printf("current cue:  %s\n", utl.Gre(opts.cueName))
			} else {
				opts.cueName = parts[1]
				fmt.Printf("cue set to:  %s\n", utl.Gre(opts.cueName))
			}
			continue
		}

		turnOpts := opts
		turnOpts.dryRun = dryRun
		turnOpts.debug = debug
		sess, err = executePrompt(input, turnOpts, sess)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("error: "+err.Error()))
		}
	}
	return nil
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printPromptHelp() {
	n := program_name
	fmt.Printf("Send a prompt to a local IQ model.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s prompt [flags] [message]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  %-32s %s\n", "-r, --cue <n>", "Skip classification, use this cue")
	fmt.Printf("  %-32s %s\n", "-c, --category <n>", "Classify within a category only")
	fmt.Printf("  %-32s %s\n", "    --tier <n>", "Override tier directly, bypass cue system")
	fmt.Printf("  %-32s %s\n", "-s, --session <id>", "Load/continue a session by ID")
	fmt.Printf("  %-32s %s\n", "-n, --dry-run", "Trace steps 1–4, skip inference")
	fmt.Printf("  %-32s %s\n", "-d, --debug", "Trace all steps including inference")
	fmt.Printf("  %-32s %s\n", "-K, --no-kb", "Disable knowledge base retrieval for this prompt")
	fmt.Printf("  %-32s %s\n\n", "    --no-stream", "Collect full response before printing")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-32s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s prompt \"explain transformers\"\n", n)
	fmt.Printf("  $ %s prompt -n \"explain transformers\"\n", n)
	fmt.Printf("  $ %s prompt -d \"explain transformers\"\n", n)
	fmt.Printf("  $ %s prompt --cue math_reasoning \"solve x^2 + 3x - 4\"\n", n)
	fmt.Printf("  $ %s prompt --category code \"write a binary search in Go\"\n", n)
	fmt.Printf("  $ %s prompt --session abc123 \"continue from before\"\n", n)
	fmt.Printf("  $ %s prompt\n", n)
	fmt.Printf("  $ echo \"translate to French: hello\" | %s prompt\n\n", n)
}

// ── Command ───────────────────────────────────────────────────────────────────

func newPromptCmd() *cobra.Command {
	var opts promptOpts

	cmd := &cobra.Command{
		Use:          "prompt [flags] [message]",
		Short:        "Send a prompt to a local IQ model",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var sess *session
			if opts.sessionID != "" {
				var err error
				sess, err = loadSession(opts.sessionID)
				if err != nil {
					return err
				}
			}

			if len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
				return runREPL(opts)
			}

			var input string
			if len(args) > 0 {
				input = strings.Join(args, " ")
			} else {
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				input = strings.TrimSpace(string(data))
			}
			if input == "" {
				printPromptHelp()
				return nil
			}

			_, err := executePrompt(input, opts, sess)
			return err
		},
	}

	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printPromptHelp()
	})

	cmd.Flags().StringVarP(&opts.cueName, "cue", "r", "", "Skip classification, use this cue")
	cmd.Flags().StringVarP(&opts.category, "category", "c", "", "Classify within a category only")
	cmd.Flags().StringVar(&opts.tier, "tier", "", "Override tier directly")
	cmd.Flags().StringVarP(&opts.sessionID, "session", "s", "", "Load/continue a session by ID")
	cmd.Flags().BoolVarP(&opts.dryRun, "dry-run", "n", false, "Trace steps 1–4, skip inference")
	cmd.Flags().BoolVarP(&opts.debug, "debug", "d", false, "Trace all steps including inference")
	cmd.Flags().BoolVarP(&opts.noKB, "no-kb", "K", false, "Disable knowledge base retrieval")
	cmd.Flags().BoolVar(&opts.noStream, "no-stream", false, "Collect full response before printing")

	return cmd
}
