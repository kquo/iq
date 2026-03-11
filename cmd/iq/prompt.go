package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
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
	"iq/internal/cache"
	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/search"
	"iq/internal/tools"
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
	Temperature       float64       `json:"temperature,omitempty"`
	RoutingGrammar    *routeGrammar `json:"routing_grammar,omitempty"`
}

type routeGrammar struct {
	ToolNames []string `json:"tool_names"`
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
	dir, err := config.Dir()
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
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}

// ── Classification ────────────────────────────────────────────────────────────
// Embedding-based classification is implemented in internal/embed (embed.Classify).
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

func resolveRoute(cueName string, cues []cue.Cue) (*routeResult, error) {
	_, c := cue.Find(cues, cueName)
	if c == nil {
		return nil, fmt.Errorf("cue %q not found", cueName)
	}

	// Direct model override on the cue — kept for power users but not
	// actively promoted. Find which tier it belongs to and pick its sidecar.
	if c.Model != "" {
		tier := config.TierForModel(c.Model)
		if tier == "" {
			return nil, fmt.Errorf("cue %q has model %q but it is not in any tier pool", cueName, c.Model)
		}
		sc, err := pickSidecar(tier, false)
		if err != nil {
			return nil, fmt.Errorf("cue model override: %w", err)
		}
		return &routeResult{
			CueName:       cueName,
			Category:      c.Category,
			SuggestedTier: c.SuggestedTier,
			SystemPrompt:  c.SystemPrompt,
			Tier:          tier,
			Port:          sc.Port,
			ModelID:       sc.Model,
			TierSource:    "cue_override",
		}, nil
	}

	// Use suggested_tier, fall back to "fast".
	tier := c.SuggestedTier
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
			return nil, fmt.Errorf("no running sidecars in %s or %s tier — run 'iq start'", tier, other)
		}
		tier = other
		tierSource = "fallback"
	}

	return &routeResult{
		CueName:       cueName,
		Category:      c.Category,
		SuggestedTier: c.SuggestedTier,
		SystemPrompt:  c.SystemPrompt,
		Tier:          tier,
		Port:          sidecar.Port,
		ModelID:       sidecar.Model,
		TierSource:    tierSource,
	}, nil
}

// ── Trace output ──────────────────────────────────────────────────────────────

// traceStep prints a step header.
func traceStep(step, label string) {
	fmt.Fprintf(os.Stderr, "%s\n",
		utl.Gra(fmt.Sprintf("STEP %s %s", step, label)))
}

// tracePass prints a pass sub-header within a step: "  PASS N  description".
func tracePass(n int, desc string) {
	fmt.Fprintf(os.Stderr, "  %s  %s\n",
		utl.Gra(fmt.Sprintf("%-12s", fmt.Sprintf("PASS %d", n))),
		utl.Gre(desc))
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

// traceBlock prints a role label then the content indented below it.
// For user content: KB separator lines (───) and the actual user input are
// highlighted green; KB chunk text and headers stay gray.
func traceBlock(role, content string, highlightUser bool) {
	const blockIndent = "    "
	fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("  [%s]", role)))
	lines := strings.Split(content, "\n")

	if role == "system" {
		for _, l := range lines {
			if l == "[tools]" {
				fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gre(l))
			} else {
				fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gra(l))
			}
		}
		return
	}

	if role != "user" || !highlightUser {
		for _, l := range lines {
			fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gra(l))
		}
		return
	}

	// Find where user input starts (after KB context, if any).
	// KB chunk header lines start with "KB Result Chunk". User input
	// follows after the last chunk text, separated by a blank line.
	userStart := 0 // default: all green (no KB context)
	lastSep := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "KB Result Chunk ") {
			lastSep = i
		}
	}
	if lastSep >= 0 {
		// The KB context and user input are joined by "\n\n", producing
		// two consecutive blank lines when split. Single blank lines can
		// occur within chunk text (e.g. between a code block and prose),
		// so we specifically look for a double-blank boundary.
		for i := lastSep + 1; i+1 < len(lines); i++ {
			if lines[i] == "" && lines[i+1] == "" {
				for j := i + 2; j < len(lines); j++ {
					if lines[j] != "" {
						userStart = j
						break
					}
				}
				break
			}
		}
		if userStart == 0 {
			userStart = len(lines)
		}
	}

	for i, l := range lines {
		if strings.HasPrefix(l, "KB Result Chunk ") {
			// KB chunk header → green
			fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gre(l))
		} else if i >= userStart {
			// User input → green
			fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gre(l))
		} else {
			// KB header/chunk text → gray
			fmt.Fprintf(os.Stderr, "%s%s\n", utl.Gra(blockIndent), utl.Gra(l))
		}
	}
}

// printStep1Classify prints the embedding classification trace.
func printStep1Classify(t *embed.ClassifyTrace) {
	traceStep("1 ", "CLASSIFY")
	traceField("task", "Cosine-similarity match user input against 17 cue descriptions")
	traceField("call", fmt.Sprintf("model %s @ localhost:%d", t.Model, embed.PortConst))
	traceField("resolved_cue", fmt.Sprintf("%s (score: %.4f)", t.Resolved, t.Score))
	if !t.CacheHit {
		traceField("cache", "rebuilt")
	}
	traceField("elapsed", fmt.Sprintf("%dms", t.Elapsed.Milliseconds()))
}

// printStep1bToolDetect prints the tool detection trace.
// webSearchSynthPrompt builds the synthesis instruction sent to the model
// after a web_search tool call. It injects the current date/time so the model
// can reason about recency and avoid contradicting fresh search results with
// stale training data.
func webSearchSynthPrompt() string {
	now := time.Now().Format("January 2, 2006")
	return fmt.Sprintf(
		"You are a concise assistant. Today is %s.\n"+
			"Answer the question in 1-2 sentences using the search results below.\n"+
			"Give the single best answer. Do NOT discuss discrepancies, conflicts, or other sources.\n", now)
}

func printStep1bToolDetect(tt *tools.ClassifyTrace) {
	traceStep("1b", "TOOL DETECT")
	if tt.Reason == "file path" || tt.Reason == "forced" {
		if tt.Enabled {
			traceField("result", fmt.Sprintf("enabled (%s)", tt.Reason))
		} else {
			traceField("result", "disabled (forced)")
		}
		return
	}
	traceField("task", fmt.Sprintf("Cosine-similarity match input vector against %d tool signal descriptions", len(tools.Signals)))
	traceField("best_signal", fmt.Sprintf("%s (score: %.2f)", tt.BestSignal, tt.BestScore))
	if tt.Enabled {
		traceField("result", fmt.Sprintf("enabled (%s)", tt.Reason))
	} else {
		traceField("result", fmt.Sprintf("disabled (best: %.2f)", tt.BestScore))
	}
	traceField("elapsed", fmt.Sprintf("%dms", tt.Elapsed.Milliseconds()))
}

// printStep2Route prints the routing decision.
func printStep2Route(route *routeResult, elapsed time.Duration) {
	traceStep("2 ", "RESOLVE ROUTE")
	traceField("task", "Map resolved cue to model tier and running sidecar")
	traceField("model", fmt.Sprintf("%s @ localhost:%d", route.ModelID, route.Port))
	traceField("cue", fmt.Sprintf("%s → %s/%s", route.CueName, route.Category, route.Tier))
	traceField("tier_source", route.TierSource)
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep3KB prints the knowledge base retrieval trace.
func printStep3KB(results []kb.Result, model string, elapsed time.Duration) {
	traceStep("3 ", "KB RETRIEVE")
	traceField("task", "Cosine-similarity search user input against KB chunks")
	traceField("call", fmt.Sprintf("model %s @ localhost:%d", model, embed.PortConst))
	traceField("chunks", fmt.Sprintf("%d results", len(results)))
	for _, r := range results {
		traceField("top", fmt.Sprintf("score:%.4f  %s:%d–%d",
			r.Score, r.Chunk.Source, r.Chunk.LineStart, r.Chunk.LineEnd))
	}
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep4Assemble prints the full message array that will be sent.
func printStep4Assemble(messages []chatMessage) {
	traceStep("4 ", "ASSEMBLE")
	traceField("task", "Combine system prompt, session history, and user message into message array")
	for _, m := range messages {
		traceBlock(m.Role, m.Content, true)
	}
}

// printStep4bCacheCheck prints the cache lookup trace.
func printStep4bCacheCheck(r *cache.HitResult) {
	traceStep("4b", "CACHE CHECK")
	traceField("task", "Hash messages and check response cache")
	traceField("key", r.Key)
	if r.Hit {
		traceField("result", fmt.Sprintf("hit (age: %s, model: %s)", cache.FormatAge(r.Age), r.Model))
	} else {
		traceField("result", "miss")
	}
	traceField("elapsed", fmt.Sprintf("%dms", r.Elapsed.Milliseconds()))
}

// printStep5bCacheWrite prints the cache write trace.
func printStep5bCacheWrite(key string, elapsed time.Duration) {
	traceStep("5b", "CACHE WRITE")
	traceField("task", "Store response in cache")
	traceField("key", key)
	traceField("ttl", fmt.Sprintf("%dm", int(cache.TTL.Minutes())))
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep6Session prints session persistence info.
func printStep6Session(sess *session, path string, elapsed time.Duration) {
	traceStep("6 ", "SESSION")
	traceField("task", "Persist conversation to disk")
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

func doSidecarCall(port int, req chatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
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

func callSidecar(port int, messages []chatMessage, stream bool, maxTokens int, ip config.ResolvedParams) (string, error) {
	req := chatRequest{Messages: messages, Stream: false, MaxTokens: maxTokens, RepetitionPenalty: ip.RepetitionPenalty, Temperature: ip.Temperature}
	return doSidecarCall(port, req)
}

// callSidecarWithGrammar sends an inference request with an optional routing grammar.
func callSidecarWithGrammar(port int, messages []chatMessage, maxTokens int, grammar *routeGrammar, ip config.ResolvedParams) (string, error) {
	req := chatRequest{Messages: messages, Stream: false, MaxTokens: maxTokens, RepetitionPenalty: ip.RepetitionPenalty, Temperature: ip.Temperature, RoutingGrammar: grammar}
	return doSidecarCall(port, req)
}

func streamSidecar(port int, messages []chatMessage, ip config.ResolvedParams) (string, error) {
	req := chatRequest{Messages: messages, Stream: true, MaxTokens: ip.MaxTokens, RepetitionPenalty: ip.RepetitionPenalty, Temperature: ip.Temperature}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("sidecar at :%d unreachable — run 'iq start': %w", port, err)
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
		nameCfg, _ := config.Load(nil)
		nameIP := config.ResolveInferParams(nameCfg, "fast")
		response, err := callSidecar(sidecar.Port, []chatMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: excerpt.String()},
		}, false, 60, nameIP)
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
	noCache   bool
	toolMode  string // "" = auto, "on", "off"
}

func executePrompt(input string, opts promptOpts, sess *session) (*session, error) {
	trace := opts.dryRun || opts.debug
	cues, err := cue.Load()
	if err != nil {
		return sess, err
	}

	// ── Step 1: CLASSIFY ──
	cueName := opts.cueName
	var et *embed.ClassifyTrace

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
		if !embed.SidecarAlive() {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("embed sidecar not running — falling back to initial cue (run: iq start)"))
			cueName = "initial"
		} else {
			cfg2, cfgErr := config.Load(nil)
			em := config.DefaultEmbedModel
			if cfgErr == nil {
				em = config.EmbedModel(cfg2)
			}
			cueName, et, err = embed.Classify(input, candidates, em)
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

	// ── Step 1b: TOOL DETECT ──
	useTools := false
	var tt *tools.ClassifyTrace
	switch opts.toolMode {
	case "on":
		useTools = true
		tt = &tools.ClassifyTrace{Enabled: true, Reason: "forced"}
	case "off":
		useTools = false
		tt = &tools.ClassifyTrace{Enabled: false, Reason: "forced"}
	default:
		useTools = tools.HasFilePath(input)
		if useTools {
			tt = &tools.ClassifyTrace{Enabled: true, Reason: "file path"}
		} else if et != nil && len(et.InputVec) > 0 {
			useTools, tt = tools.Classify(et.InputVec, et.Model)
		}
	}
	if trace && tt != nil {
		printStep1bToolDetect(tt)
	}

	// ── Step 2: RESOLVE ROUTE ──
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

	// Resolve inference parameters: per-tier > global > hardcoded default.
	inferCfg, _ := config.Load(nil)
	ip := config.ResolveInferParams(inferCfg, route.Tier)

	// Wire Brave Search fallback key if configured.
	if inferCfg.BraveAPIKey != "" {
		search.SetBraveAPIKey(inferCfg.BraveAPIKey)
	}

	// ── Step 3: KB RETRIEVE ──
	var kbContext string
	if kb.Exists() && !opts.noKB && embed.SidecarAlive() {
		t3 := time.Now()
		results, kbErr := kb.Search(input, kb.DefaultK)
		if kbErr == nil && len(results) > 0 {
			kbContext = kb.Context(results)
			if trace {
				em := config.DefaultEmbedModel
				if cfg2, cfgErr := config.Load(nil); cfgErr == nil {
					em = config.EmbedModel(cfg2)
				}
				printStep3KB(results, em, time.Since(t3))
			}
		} else if kbErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("kb search error: "+kbErr.Error()))
		}
	}

	// ── Step 4: ASSEMBLE ──

	var messages []chatMessage
	if sess != nil && len(sess.Messages) > 0 {
		messages = append(messages, sess.Messages...)
		// If tools active, inject tool prompt into existing system message.
		if useTools && len(messages) > 0 && messages[0].Role == "system" {
			messages[0].Content += tools.BuildRoutingPrompt()
		}
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
		if useTools {
			sysprompt += tools.BuildRoutingPrompt()
		}
		userContent := kbContext + "\n\n" + input
		messages = append(messages, chatMessage{Role: "system", Content: sysprompt})
		messages = append(messages, chatMessage{Role: "user", Content: userContent})
	} else {
		sysprompt := route.SystemPrompt
		if useTools {
			if sysprompt == "" {
				sysprompt = "You are a helpful assistant."
			}
			sysprompt += tools.BuildRoutingPrompt()
		}
		if sysprompt != "" {
			messages = append(messages, chatMessage{Role: "system", Content: sysprompt})
		}
		messages = append(messages, chatMessage{Role: "user", Content: input})
	}
	if trace {
		printStep4Assemble(messages)
	}

	// ── Step 4b: CACHE CHECK ──
	useCache := !opts.noCache && sess == nil && !useTools
	var cacheK string
	cacheHit := false
	var response string

	if useCache {
		cm := make([]cache.Message, len(messages))
		for i, m := range messages {
			cm[i] = cache.Message{Role: m.Role, Content: m.Content}
		}
		cacheK = cache.Key(cm, route.ModelID)
		resp, hitInfo := cache.Check(cacheK)
		if trace {
			printStep4bCacheCheck(hitInfo)
		}
		if hitInfo.Hit {
			cacheHit = true
			response = resp
		}
	}

	// Dry-run stops here.
	if opts.dryRun {
		return sess, nil
	}

	if cacheHit {
		fmt.Println(response)
	}

	if !cacheHit {
		// ── Step 5: INFERENCE LOOP ──
		var t5 time.Time
		if trace {
			traceStep("5 ", "INFERENCE LOOP")
			traceField("task", "Send assembled messages to model sidecar for generation")
			t5 = time.Now()
		}

		if useTools {
			// Tool-enabled path always uses non-streaming so we can intercept
			// tool_call blocks before they reach the user's terminal.

			// ── Web search short-circuit ──
			// When the embed signal is web_search, skip the routing grammar
			// pass entirely — the model never calls web_search on its own,
			// so Pass 1 would be wasted latency.
			if tt != nil && tt.BestSignal == "web_search" && tt.Reason == "embed" {
				var tPass time.Time
				if trace {
					traceField("mode", "web_search short-circuit (skipping routing grammar)")
					tPass = time.Now()
				}
				call := tools.Call{Name: "web_search", Args: tools.GuardArgs("web_search", input)}
				if trace {
					printToolCallTrace(call)
				} else {
					printToolStatus(call)
				}
				r := tools.Execute(call)
				if trace {
					printToolResultTrace(r)
				}
				if r.Error == "" && r.Output != "" {
					// Replace the cue system prompt with a neutral web-search
					// synthesis prompt so the model doesn't hedge or role-play.
					synthMessages := []chatMessage{
						{Role: "system", Content: webSearchSynthPrompt()},
						{Role: "user", Content: input},
						{Role: "assistant", Content: "Let me search for that."},
						{Role: "user", Content: "Search results:\n\n" + tools.FormatResult(r)},
					}
					if trace {
						tracePass(1, "synthesize web search")
						traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
						tPass = time.Now()
					}
					response, err = callSidecar(route.Port, synthMessages, false, ip.MaxTokens, ip)
					if err != nil {
						return sess, err
					}
					if trace {
						traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
						traceField("latency", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}
				} else {
					// Search failed — fall through to normal inference.
					if trace {
						traceField("search_error", r.Error)
					}
					response, err = callSidecar(route.Port, messages, false, ip.MaxTokens, ip)
					if err != nil {
						return sess, err
					}
				}
				// short-circuit done — print response
				fmt.Println(response)
			} else {

				// ── Pass 1: routing-grammar-constrained inference ──
				// The grammar forces the model to emit <tool:NAME> or <no_tool>
				// as its very first tokens, then generates freely.
				grammar := &routeGrammar{ToolNames: tools.RegistryNames()}
				var tPass time.Time
				if trace {
					tracePass(1, "routing grammar")
					traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
					tPass = time.Now()
				}
				response, err = callSidecarWithGrammar(route.Port, messages, ip.MaxTokens, grammar, ip)
				if err != nil {
					return sess, err
				}
				if trace {
					traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
				}

				// Parse the routing prefix from the grammar-constrained response.
				routedTool, routeRest := tools.ParseRoutingPrefix(response)

				// If the grammar routed to a tool, construct a toolCall and execute it.
				toolDone := false
				if routedTool != "" {
					if trace {
						traceField("route", fmt.Sprintf("<tool:%s>", routedTool))
					}

					// Try to parse JSON args from the text after the prefix.
					args := tools.ParseRoutingArgs(routeRest)
					call := tools.Call{Name: routedTool, Args: args}

					if trace {
						printToolCallTrace(call)
					} else {
						printToolStatus(call)
					}
					r := tools.Execute(call)
					if trace {
						printToolResultTrace(r)
						traceField("latency 1", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}

					// If the tool succeeded, print output directly — don't ask the
					// model to reproduce it (small models hallucinate on long output).
					if r.Error == "" && r.Output != "" {
						fmt.Println(r.Output)
						response = r.Output
						toolDone = true
					} else {
						// Tool failed or returned empty — let the model explain.
						messages = append(messages, chatMessage{Role: "assistant", Content: response})
						messages = append(messages, chatMessage{
							Role:    "user",
							Content: "Tool result below. Explain the result or error briefly.\n\n" + tools.FormatResult(r),
						})
						if trace {
							tracePass(2, "explain tool result")
							traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
							tPass = time.Now()
						}
						response, err = callSidecar(route.Port, messages, false, ip.MaxTokens, ip)
						if err != nil {
							return sess, err
						}
						if trace {
							traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
							traceField("latency 2", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
						}
					}
				} else {
					// <no_tool> or no prefix — use tool guard as fallback.
					if trace {
						traceField("route", "<no_tool>")
						traceField("latency 1", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}
					response = routeRest // strip the <no_tool> prefix

					// Tool guard: if Step 1b detected a tool signal via embedding
					// but the grammar chose <no_tool>, directly execute the first
					// expected tool. Small models can't be reprompted into compliance,
					// so we trust the embed signal and call the tool ourselves.
					if tt != nil && tt.Reason == "embed" {
						expected := tools.SignalToolNames(tt.BestSignal)
						if len(expected) > 0 {
							guardTool := expected[0]
							if trace {
								fmt.Fprintf(os.Stderr, "  %s  %s\n",
									utl.Gra(fmt.Sprintf("%-12s", "GUARD")),
									utl.Gre(fmt.Sprintf("<no_tool> but signal=%s — direct-calling %s", tt.BestSignal, guardTool)))
							}
							// Build default args: populate the first required param
							// with the user input so tools like web_search get a query.
							guardArgs := tools.GuardArgs(guardTool, input)
							call := tools.Call{Name: guardTool, Args: guardArgs}
							if trace {
								printToolCallTrace(call)
							} else {
								printToolStatus(call)
							}
							r := tools.Execute(call)
							if trace {
								printToolResultTrace(r)
							}
							// Tools like get_time produce a self-contained answer;
							// web_search returns raw snippets that need synthesis.
							needsSynth := call.Name == "web_search"
							if !needsSynth && r.Error == "" && r.Output != "" {
								fmt.Println(r.Output)
								response = r.Output
								toolDone = true
							} else {
								messages = append(messages, chatMessage{Role: "assistant", Content: "Let me check that for you."})
								synthPrompt := "Tool result below. Explain the result or error briefly.\n\n"
								if needsSynth && r.Error == "" {
									synthPrompt = webSearchSynthPrompt()
								}
								messages = append(messages, chatMessage{
									Role:    "user",
									Content: synthPrompt + tools.FormatResult(r),
								})
								if trace {
									tracePass(2, "synthesize guard tool result")
									traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
									tPass = time.Now()
								}
								response, err = callSidecar(route.Port, messages, false, ip.MaxTokens, ip)
								if err != nil {
									return sess, err
								}
								if trace {
									traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
									traceField("latency 2", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
								}
							}
						}
					}
				}

				// ── Passes 2+: standard tool loop for multi-tool chains ──
				// Skip if tool output was already printed directly.
				const maxToolIter = 5
				for iter := range maxToolIter {
					if toolDone {
						break
					}
					calls, remaining := tools.ParseCalls(response)

					// Fallback: model may reuse <tool:NAME> routing prefix on
					// follow-up passes instead of <tool_call> blocks.
					if len(calls) == 0 {
						if rTool, rRest := tools.ParseRoutingPrefix(response); rTool != "" {
							args := tools.ParseRoutingArgs(rRest)
							calls = []tools.Call{{Name: rTool, Args: args}}
							remaining = ""
						}
					}

					if len(calls) == 0 {
						// No tool calls — final answer.
						fmt.Println(remaining)
						response = remaining
						break
					}

					// Print any text the model emitted before the tool calls.
					if remaining != "" {
						fmt.Println(remaining)
					}

					// Append assistant message (raw, with tool_call blocks).
					messages = append(messages, chatMessage{Role: "assistant", Content: response})

					// Execute each tool and collect results.
					var resultBlock strings.Builder
					allOK := true
					hasWebSearch := false
					for _, call := range calls {
						if trace {
							printToolCallTrace(call)
						} else {
							printToolStatus(call)
						}
						r := tools.Execute(call)
						if trace {
							printToolResultTrace(r)
						}
						if call.Name == "web_search" {
							hasWebSearch = true
						}
						if r.Error == "" && r.Output != "" && !hasWebSearch {
							fmt.Println(r.Output)
						} else if r.Error != "" {
							allOK = false
						}
						resultBlock.WriteString(tools.FormatResult(r))
						resultBlock.WriteByte('\n')
					}

					// If all tools succeeded and none need synthesis, done.
					if allOK && !hasWebSearch {
						response = strings.TrimSpace(resultBlock.String())
						break
					}

					// Send results back to model for synthesis.
					synthPrompt := "Tool result below. Explain the result or error briefly.\n\n"
					if hasWebSearch && allOK {
						synthPrompt = webSearchSynthPrompt()
					}
					messages = append(messages, chatMessage{
						Role:    "user",
						Content: synthPrompt + strings.TrimSpace(resultBlock.String()),
					})

					// Continue inference with tool results.
					passN := iter + 2
					if trace {
						traceField(fmt.Sprintf("latency %d", passN-1), fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
						tracePass(passN, "explain tool error")
						traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
						tPass = time.Now()
					}
					response, err = callSidecar(route.Port, messages, false, ip.MaxTokens, ip)
					if err != nil {
						return sess, err
					}
					if trace {
						traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
						traceField(fmt.Sprintf("latency %d", passN), fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}

					// On the last iteration, strip any remaining tool calls and print.
					if iter == maxToolIter-1 {
						_, remaining = tools.ParseCalls(response)
						fmt.Println(remaining)
						response = remaining
					}
				}
			} // end web_search else
		} else {
			// Non-tool path.
			if opts.noStream {
				if trace {
					traceField("mode", "non-streaming")
					traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
				}
				response, err = callSidecar(route.Port, messages, false, ip.MaxTokens, ip)
				if err != nil {
					return sess, err
				}
				fmt.Println(response)
			} else {
				if trace {
					traceField("mode", "streaming")
					traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
				}
				response, err = streamSidecar(route.Port, messages, ip)
				if err != nil {
					return sess, err
				}
			}
		}
		if trace {
			traceField("elapsed", fmt.Sprintf("%dms", time.Since(t5).Milliseconds()))
		}

		// ── Step 5b: CACHE WRITE ──
		if useCache && response != "" {
			t5b := time.Now()
			cache.Write(cacheK, response, route.ModelID, route.CueName)
			if trace {
				printStep5bCacheWrite(cacheK, time.Since(t5b))
			}
		}
	}

	// ── Step 6: SESSION ──
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
	"/cue":     "show or set current cue  (e.g. /cue math)",
	"/session": "show current session info",
	"/clear":   "clear session history (start fresh)",
	"/dry-run": "toggle dry-run mode",
	"/debug":   "toggle debug trace mode",
	"/tools":   "cycle tool mode: auto → on → off → auto",
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
		case "/tools":
			switch opts.toolMode {
			case "":
				opts.toolMode = "on"
			case "on":
				opts.toolMode = "off"
			case "off":
				opts.toolMode = ""
			}
			label := "auto"
			if opts.toolMode != "" {
				label = opts.toolMode
			}
			fmt.Printf("%s %s\n", utl.Gra("tools:"), label)
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
	fmt.Printf("Start the interactive REPL or send a prompt. For one-shot prompts, '%s <message>' works too.\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s ask [flags] [message]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  %-32s %s\n", "-r, --cue <n>", "Skip classification, use this cue")
	fmt.Printf("  %-32s %s\n", "-c, --category <n>", "Classify within a category only")
	fmt.Printf("  %-32s %s\n", "    --tier <n>", "Override tier directly, bypass cue system")
	fmt.Printf("  %-32s %s\n", "-s, --session <id>", "Load/continue a session by ID")
	fmt.Printf("  %-32s %s\n", "-n, --dry-run", "Trace steps 1–4, skip inference")
	fmt.Printf("  %-32s %s\n", "-d, --debug", "Trace all steps including inference")
	fmt.Printf("  %-32s %s\n", "-K, --no-kb", "Disable knowledge base retrieval for this prompt")
	fmt.Printf("  %-32s %s\n", "    --no-cache", "Disable response cache")
	fmt.Printf("  %-32s %s\n", "-T, --tools", "Force enable read-only tool use")
	fmt.Printf("  %-32s %s\n", "    --no-tools", "Disable tool use")
	fmt.Printf("  %-32s %s\n\n", "    --no-stream", "Collect full response before printing")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-32s %s\n\n", "-h, -?, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s ask \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask -n \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask -d \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask --cue math \"solve x^2 + 3x - 4\"\n", n)
	fmt.Printf("  $ %s ask --category code \"write a binary search in Go\"\n", n)
	fmt.Printf("  $ %s ask --session abc123 \"continue from before\"\n", n)
	fmt.Printf("  $ %s ask\n", n)
	fmt.Printf("  $ echo \"translate to French: hello\" | %s ask\n\n", n)
}

// ── Shared flag wiring ────────────────────────────────────────────────────────

// addPromptFlags registers prompt flags on cmd, bound to opts.
func addPromptFlags(cmd *cobra.Command, opts *promptOpts) {
	var toolsOn, noTools bool
	cmd.Flags().StringVarP(&opts.cueName, "cue", "r", "", "Skip classification, use this cue")
	cmd.Flags().StringVarP(&opts.category, "category", "c", "", "Classify within a category only")
	cmd.Flags().StringVar(&opts.tier, "tier", "", "Override tier directly")
	cmd.Flags().StringVarP(&opts.sessionID, "session", "s", "", "Load/continue a session by ID")
	cmd.Flags().BoolVarP(&opts.dryRun, "dry-run", "n", false, "Trace steps 1-4, skip inference")
	cmd.Flags().BoolVarP(&opts.debug, "debug", "d", false, "Trace all steps including inference")
	cmd.Flags().BoolVarP(&opts.noKB, "no-kb", "K", false, "Disable knowledge base retrieval")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable response cache")
	cmd.Flags().BoolVar(&opts.noStream, "no-stream", false, "Collect full response before printing")
	cmd.Flags().BoolVarP(&toolsOn, "tools", "T", false, "Force enable read-only tool use")
	cmd.Flags().BoolVar(&noTools, "no-tools", false, "Disable tool use")

	// Resolve toolMode after flags are parsed.
	old := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if old != nil {
			if err := old(cmd, args); err != nil {
				return err
			}
		}
		if toolsOn {
			opts.toolMode = "on"
		} else if noTools {
			opts.toolMode = "off"
		}
		// else "" = auto
		return nil
	}
}

// ── Command ───────────────────────────────────────────────────────────────────

func newPromptCmd() *cobra.Command {
	var opts promptOpts

	cmd := &cobra.Command{
		Use:          "ask [flags] [message]",
		Aliases:      []string{"prompt"},
		Short:        "Start the interactive REPL (or send a prompt)",
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

	addPromptFlags(cmd, &opts)

	return cmd
}
