package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
	"iq/internal/cache"
	"iq/internal/color"
	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/search"
	"iq/internal/sidecar"
	"iq/internal/tools"
)

// kbTimeout is the maximum time to wait for async KB retrieval before
// proceeding without context. Keeps the prompt pipeline responsive even
// if the embed sidecar is slow or the KB is large.
const kbTimeout = 5 * time.Second

// ── Session ───────────────────────────────────────────────────────────────────

type session struct {
	ID          string           `yaml:"id"`
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Cue         string           `yaml:"cue"`
	Tier        string           `yaml:"tier"`
	Created     string           `yaml:"created"`
	Updated     string           `yaml:"updated"`
	Messages    []config.Message `yaml:"messages"`
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

// acquireLock opens (or creates) a lock file and applies an advisory flock.
// Pass exclusive=true for writes, false for reads. The caller must call
// releaseLock when done.
func acquireLock(path string, exclusive bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// releaseLock releases the advisory flock and closes the lock file.
func releaseLock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	f.Close()
}

func loadSession(id string) (*session, error) {
	path, err := sessionPath(id)
	if err != nil {
		return nil, err
	}
	lf, err := acquireLock(path+".lock", false)
	if err != nil {
		return nil, err
	}
	defer releaseLock(lf)
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
	lf, err := acquireLock(path+".lock", true)
	if err != nil {
		return err
	}
	defer releaseLock(lf)
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
		color.Gra(fmt.Sprintf("STEP %s %s", step, label)))
}

// tracePass prints a pass sub-header within a step: "  PASS N  description".
func tracePass(n int, desc string) {
	fmt.Fprintf(os.Stderr, "  %s  %s\n",
		color.Gra(fmt.Sprintf("%-12s", fmt.Sprintf("PASS %d", n))),
		color.Grn(desc))
}

// traceField prints "  label  value" with continuation lines indented to match.
func traceField(label, value string) {
	prefix := fmt.Sprintf("  %-12s  ", label)
	indent := strings.Repeat(" ", len(prefix))
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(prefix), color.Gra(lines[0]))
	for _, l := range lines[1:] {
		fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(indent), color.Gra(l))
	}
}

// traceBlock prints a role label then the content indented below it.
// For user content: KB separator lines (───) and the actual user input are
// highlighted green; KB chunk text and headers stay gray.
func traceBlock(role, content string, highlightUser bool) {
	const blockIndent = "    "
	fmt.Fprintf(os.Stderr, "%s\n", color.Gra(fmt.Sprintf("  [%s]", role)))
	lines := strings.Split(content, "\n")

	if role == "system" {
		for _, l := range lines {
			if l == "[tools]" {
				fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Grn(l))
			} else {
				fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Gra(l))
			}
		}
		return
	}

	if role != "user" || !highlightUser {
		for _, l := range lines {
			fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Gra(l))
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

	const kbChunkPreviewLines = 4
	inChunk := false
	chunkLines := 0
	skipped := 0
	flushSkipped := func() {
		if skipped > 0 {
			fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent),
				color.Gra(fmt.Sprintf("... %d more lines", skipped)))
			skipped = 0
		}
	}
	for i, l := range lines {
		if strings.HasPrefix(l, "KB Result Chunk ") {
			flushSkipped()
			inChunk = true
			chunkLines = 0
			// KB chunk header → green
			fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Grn(l))
		} else if i >= userStart {
			flushSkipped()
			// User input → green
			fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Grn(l))
		} else if inChunk && chunkLines >= kbChunkPreviewLines {
			chunkLines++
			skipped++
		} else {
			if inChunk {
				chunkLines++
			}
			// KB preamble or chunk preview → gray
			fmt.Fprintf(os.Stderr, "%s%s\n", color.Gra(blockIndent), color.Gra(l))
		}
	}
	flushSkipped()
}

// printStep1Classify prints the embedding classification trace.
func printStep1Classify(t *embed.ClassifyTrace) {
	traceStep("1 ", "CLASSIFY")
	traceField("task", "Hybrid match: cosine-similarity + keyword boost against cue descriptions")
	traceField("call", fmt.Sprintf("model %s @ localhost:%d", t.Model, embed.PortConst))
	scoreDetail := fmt.Sprintf("%s (score: %.4f, method: %s", t.Resolved, t.Score, t.Method)
	if t.KeywordBoost > 0 {
		scoreDetail += fmt.Sprintf(", kw_boost: +%.2f", t.KeywordBoost)
	}
	scoreDetail += ")"
	traceField("resolved_cue", scoreDetail)
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
func printStep3KB(results []kb.Result, model string, minScore float32, elapsed time.Duration) {
	traceStep("3 ", "KB RETRIEVE")
	traceField("task", "Cosine-similarity search user input against KB chunks")
	traceField("call", fmt.Sprintf("model %s @ localhost:%d", model, embed.PortConst))
	traceField("threshold", fmt.Sprintf("%.2f", minScore))
	if len(results) == 0 {
		traceField("chunks", "0 results (all below threshold — KB injection skipped)")
	} else {
		traceField("chunks", fmt.Sprintf("%d results", len(results)))
		for _, r := range results {
			traceField("top", fmt.Sprintf("score:%.4f  %s:%d–%d",
				r.Score, r.Chunk.Source, r.Chunk.LineStart, r.Chunk.LineEnd))
		}
	}
	traceField("elapsed", fmt.Sprintf("%dms", elapsed.Milliseconds()))
}

// printStep4Assemble prints the full message array that will be sent.
func printStep4Assemble(messages []config.Message) {
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
		sc, err := pickSidecar("fast", true)
		if err != nil {
			return
		}
		nameCfg, _ := config.Load(nil)
		nameIP := config.ResolveInferParams(nameCfg, "fast")
		response, err := sidecar.Call(sc.Port, []config.Message{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: excerpt.String()},
		}, 60, nameIP)
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
	cueName    string
	category   string
	tier       string
	sessionID  string
	dryRun     bool
	debug      bool
	noStream   bool
	noKB       bool
	noCache    bool
	toolMode   string // "" = auto, "on", "off"
	dumpPrompt string // file path ("-" for stdout), write assembled messages as JSON
}

func executePrompt(input string, opts promptOpts, sess *session) (*session, error) {
	reg := tools.NewRegistry()
	cfg, err := config.Load(nil)
	if err != nil {
		return sess, fmt.Errorf("loading config: %w", err)
	}
	if p := cfg.EffectivePipeline(); p != config.PipelineTwoTier {
		return sess, fmt.Errorf("unknown pipeline mode %q", p)
	}

	trace := opts.dryRun || opts.debug || opts.dumpPrompt != ""
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
			fmt.Fprintf(os.Stderr, "%s\n", color.Gra("embed sidecar not running — falling back to initial cue (run: iq start)"))
			cueName = "initial"
		} else {
			em := config.EmbedModel(cfg)
			cueName, et, err = embed.Classify(input, candidates, em)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", color.Gra("classification error: "+err.Error()+", falling back to initial"))
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

	// ── Step 3: KB PREFETCH (async) ──
	// Launch KB retrieval early so it runs concurrently with Steps 1b and 2.
	// The result is collected before Step 4 ASSEMBLE with a timeout; on
	// failure or timeout the prompt proceeds without KB context.
	type kbResult struct {
		results []kb.Result
		err     error
	}
	var kbCh chan kbResult
	kbEnabled := kb.Exists() && !opts.noKB && embed.SidecarAlive() && sess == nil
	kbMinScore := config.KBMinScore(cfg)
	if kbEnabled {
		kbCh = make(chan kbResult, 1)
		go func() {
			results, kbErr := kb.Search(input, kb.DefaultK, kbMinScore)
			kbCh <- kbResult{results, kbErr}
		}()
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
	ip := config.ResolveInferParams(cfg, route.Tier)

	// Wire search client with Brave fallback key if configured.
	tools.SetSearchClient(search.NewClient(cfg.BraveAPIKey))

	// ── Step 3: KB COLLECT ──
	// Collect the async KB prefetch result with a timeout.
	var kbContext string
	if kbCh != nil {
		t3 := time.Now()
		select {
		case kr := <-kbCh:
			if kr.err == nil {
				if len(kr.results) > 0 {
					kbContext = kb.Context(kr.results)
				}
				if trace {
					printStep3KB(kr.results, config.EmbedModel(cfg), kbMinScore, time.Since(t3))
				}
			} else {
				fmt.Fprintf(os.Stderr, "%s\n", color.Gra("kb search error: "+kr.err.Error()))
			}
		case <-time.After(kbTimeout):
			fmt.Fprintf(os.Stderr, "%s\n", color.Gra("kb search timed out — skipping context"))
			if trace {
				traceStep("3 ", "KB RETRIEVE")
				traceField("result", fmt.Sprintf("timeout after %s", kbTimeout))
			}
		}
	}

	// ── Step 4: ASSEMBLE ──

	var messages []config.Message
	if sess != nil && len(sess.Messages) > 0 {
		messages = append(messages, sess.Messages...)
		// If tools active, inject tool prompt into existing system message.
		if useTools && len(messages) > 0 && messages[0].Role == "system" {
			messages[0].Content += tools.BuildRoutingPrompt(reg)
		}
		messages = append(messages, config.Message{Role: "user", Content: input})
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
			sysprompt += tools.BuildRoutingPrompt(reg)
		}
		userContent := kbContext + "\n\n" + input
		messages = append(messages, config.Message{Role: "system", Content: sysprompt})
		messages = append(messages, config.Message{Role: "user", Content: userContent})
	} else {
		sysprompt := route.SystemPrompt
		if useTools {
			if sysprompt == "" {
				sysprompt = "You are a helpful assistant."
			}
			sysprompt += tools.BuildRoutingPrompt(reg)
		}
		if sysprompt != "" {
			messages = append(messages, config.Message{Role: "system", Content: sysprompt})
		}
		messages = append(messages, config.Message{Role: "user", Content: input})
	}
	if trace {
		printStep4Assemble(messages)
	}

	// ── Dump prompt ──
	if opts.dumpPrompt != "" {
		data, jErr := json.MarshalIndent(messages, "", "  ")
		if jErr != nil {
			return sess, fmt.Errorf("dump-prompt: %w", jErr)
		}
		if opts.dumpPrompt == "-" {
			fmt.Println(string(data))
		} else {
			if jErr := os.WriteFile(opts.dumpPrompt, append(data, '\n'), 0644); jErr != nil {
				return sess, fmt.Errorf("dump-prompt: %w", jErr)
			}
			fmt.Fprintf(os.Stderr, "prompt written to %s\n", opts.dumpPrompt)
		}
		return sess, nil
	}

	// ── Step 4b: CACHE CHECK ──
	useCache := !opts.noCache && sess == nil && !useTools
	var cacheK string
	cacheHit := false
	var response string

	if useCache {
		cacheK = cache.Key(messages, route.ModelID)
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

			// ── Embed short-circuit ──
			// When the embed signal confidently identifies a tool, skip the routing
			// grammar pass entirely and execute the tool directly. The grammar pass
			// is reserved for cases where tool identity can't be determined from the
			// embed signal alone (file path detection, forced tool mode).
			if tt != nil && tt.Reason == "embed" {
				var tPass time.Time
				if trace {
					traceField("mode", fmt.Sprintf("embed short-circuit: %s (skipping routing grammar)", tt.BestSignal))
					tPass = time.Now()
				}
				if tt.BestSignal == "web_search" {
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
						synthMessages := []config.Message{
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
						response, err = sidecar.Call(route.Port, synthMessages, ip.MaxTokens, ip)
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
						response, err = sidecar.Call(route.Port, messages, ip.MaxTokens, ip)
						if err != nil {
							return sess, err
						}
					}
					fmt.Println(response)
				} else {
					// Non-web_search embed signal: execute the identified tool directly.
					expected := tools.SignalToolNames(tt.BestSignal)
					if len(expected) > 0 {
						signalTool := tools.SelectTool(tt.BestSignal, input)
						call := tools.Call{Name: signalTool, Args: tools.GuardArgs(signalTool, input)}
						if trace {
							printToolCallTrace(call)
						} else {
							printToolStatus(call)
						}
						r := tools.Execute(call)
						if trace {
							printToolResultTrace(r)
						}
						// read_file output is raw bytes — synthesize a model response so
						// the model can answer questions about the content, not just dump it.
						// Other tools (get_time, calc, list_dir, file_info, etc.) are
						// self-contained: their output directly answers the question.
						if r.Error == "" && r.Output != "" {
							if signalTool == "read_file" {
								var synthMsgs []config.Message
								if route.SystemPrompt != "" {
									synthMsgs = append(synthMsgs, config.Message{Role: "system", Content: route.SystemPrompt})
								}
								synthMsgs = append(synthMsgs,
									config.Message{Role: "user", Content: input},
									config.Message{Role: "assistant", Content: "I've read the file."},
									config.Message{Role: "user", Content: tools.FormatResult(r) + "\n\nUsing the content above, answer my question or complete my request."},
								)
								if trace {
									tracePass(1, "synthesize file content")
									traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
									tPass = time.Now()
								}
								response, err = sidecar.Call(route.Port, synthMsgs, ip.MaxTokens, ip)
								if err != nil {
									return sess, err
								}
								if trace {
									traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
									traceField("latency", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
								}
								fmt.Println(response)
							} else {
								fmt.Println(r.Output)
								response = r.Output
							}
						} else {
							// Strip tool instructions from the system prompt so the model
							// answers directly rather than trying to call a tool again.
							fallbackMsgs := make([]config.Message, len(messages))
							copy(fallbackMsgs, messages)
							if len(fallbackMsgs) > 0 && fallbackMsgs[0].Role == "system" {
								if route.SystemPrompt != "" {
									fallbackMsgs[0].Content = route.SystemPrompt
								} else {
									fallbackMsgs = fallbackMsgs[1:]
								}
							}
							if trace {
								traceField("tool_fallback", fmt.Sprintf("%s failed — direct inference", signalTool))
								traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
								tPass = time.Now()
							}
							response, err = sidecar.Call(route.Port, fallbackMsgs, ip.MaxTokens, ip)
							if err != nil {
								return sess, err
							}
							if trace {
								traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
								traceField("latency", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
							}
							fmt.Println(response)
						}
					} else {
						// No tool mapped to this signal — fall back to direct inference.
						if trace {
							traceField("fallback", fmt.Sprintf("no tool for signal %s — direct inference", tt.BestSignal))
						}
						response, err = sidecar.Call(route.Port, messages, ip.MaxTokens, ip)
						if err != nil {
							return sess, err
						}
						fmt.Println(response)
					}
				}
			} else {

				// ── Pass 1: routing-grammar-constrained inference ──
				// The grammar forces the model to emit <tool:NAME> or <no_tool>
				// as its very first tokens, then generates freely.
				grammar := &sidecar.RouteGrammar{ToolNames: tools.RegistryNames(reg)}
				var tPass time.Time
				if trace {
					tracePass(1, "routing grammar")
					traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
					tPass = time.Now()
				}
				response, err = sidecar.CallWithGrammar(route.Port, messages, ip.MaxTokens, grammar, ip)
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
						messages = append(messages, config.Message{Role: "assistant", Content: response})
						messages = append(messages, config.Message{
							Role:    "user",
							Content: "Tool result below. Explain the result or error briefly.\n\n" + tools.FormatResult(r),
						})
						if trace {
							tracePass(2, "explain tool result")
							traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
							tPass = time.Now()
						}
						response, err = sidecar.Call(route.Port, messages, ip.MaxTokens, ip)
						if err != nil {
							return sess, err
						}
						if trace {
							traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
							traceField("latency 2", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
						}
					}
				} else {
					// <no_tool> or no prefix.
					if trace {
						traceField("route", "<no_tool>")
						traceField("latency 1", fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}
					response = routeRest // strip the <no_tool> prefix
				}
				// ── Passes 2+: standard tool loop for multi-tool chains ──
				// Skip if tool output was already printed directly.
				const maxToolIter = 5
				for iter := range maxToolIter {
					if toolDone {
						break
					}
					calls, remaining := tools.ParseCalls(response, reg)

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
					messages = append(messages, config.Message{Role: "assistant", Content: response})

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
					messages = append(messages, config.Message{
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
					response, err = sidecar.Call(route.Port, messages, ip.MaxTokens, ip)
					if err != nil {
						return sess, err
					}
					if trace {
						traceField("raw_resp", fmt.Sprintf("%q", truncate(response, 200)))
						traceField(fmt.Sprintf("latency %d", passN), fmt.Sprintf("%dms", time.Since(tPass).Milliseconds()))
					}

					// On the last iteration, strip any remaining tool calls and print.
					if iter == maxToolIter-1 {
						_, remaining = tools.ParseCalls(response, reg)
						fmt.Println(remaining)
						response = remaining
					}
				}
			} // end embed short-circuit else
		} else {
			// Non-tool path. Trace mode forces non-streaming to prevent stdout/stderr
			// interleaving from corrupting the debug output.
			if opts.noStream || trace {
				if trace {
					traceField("mode", "non-streaming")
					traceField("call", fmt.Sprintf("POST localhost:%d/v1/chat/completions", route.Port))
				}
				response, err = sidecar.Call(route.Port, messages, ip.MaxTokens, ip)
				if err != nil {
					return sess, err
				}
				fmt.Println(response)
			} else {
				response, err = sidecar.Stream(route.Port, messages, ip)
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
				sess.Messages = append(sess.Messages, config.Message{Role: "system", Content: route.SystemPrompt})
			}
		}
		sess.Messages = append(sess.Messages,
			config.Message{Role: "user", Content: input},
			config.Message{Role: "assistant", Content: response},
		)
		if err := saveSession(sess); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", color.Gra("warning: failed to save session: "+err.Error()))
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
	fmt.Fprintf(os.Stderr, "%s  %s\n", color.Whi2("IQ"), color.Gra("type /help for commands, /quit to exit"))

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(color.Grn("> "))
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
				fmt.Println(color.Gra("no active session"))
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
			fmt.Println(color.Gra("session cleared"))
			continue
		case "/dry-run":
			dryRun = !dryRun
			debug = false
			state := "off"
			if dryRun {
				state = "on"
			}
			fmt.Printf("%s %s\n", color.Gra("dry-run:"), state)
			continue
		case "/debug":
			debug = !debug
			dryRun = false
			state := "off"
			if debug {
				state = "on"
			}
			fmt.Printf("%s %s\n", color.Gra("debug:"), state)
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
			fmt.Printf("%s %s\n", color.Gra("tools:"), label)
			continue
		}

		if strings.HasPrefix(input, "/cue") {
			parts := strings.Fields(input)
			if len(parts) == 1 {
				fmt.Printf("current cue:  %s\n", color.Grn(opts.cueName))
			} else {
				opts.cueName = parts[1]
				fmt.Printf("cue set to:  %s\n", color.Grn(opts.cueName))
			}
			continue
		}

		turnOpts := opts
		turnOpts.dryRun = dryRun
		turnOpts.debug = debug
		sess, err = executePrompt(input, turnOpts, sess)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", color.Gra("error: "+err.Error()))
		}
	}
	return nil
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printPromptHelp() {
	n := programName
	fmt.Printf("Start the interactive REPL or send a prompt. For one-shot prompts, '%s <message>' works too.\n\n", n)
	fmt.Printf("%s\n", color.Whi2("USAGE"))
	fmt.Printf("  %s ask [flags] [message]\n\n", n)
	fmt.Printf("%s\n", color.Whi2("FLAGS"))
	fmt.Printf("  %-32s %s\n", "-r, --cue <n>", "Skip classification, use this cue")
	fmt.Printf("  %-32s %s\n", "-c, --category <n>", "Classify within a category only")
	fmt.Printf("  %-32s %s\n", "    --tier <n>", "Override tier directly, bypass cue system")
	fmt.Printf("  %-32s %s\n", "-s, --session <id>", "Load/continue a session by ID")
	fmt.Printf("  %-32s %s\n", "-n, --dry-run", "Trace steps 1–4, skip inference")
	fmt.Printf("  %-32s %s\n", "    --dump-prompt <f>", "Write assembled messages as JSON (- for stdout), skip inference")
	fmt.Printf("  %-32s %s\n", "-d, --debug", "Trace all steps including inference")
	fmt.Printf("  %-32s %s\n", "-K, --no-kb", "Disable knowledge base retrieval for this prompt")
	fmt.Printf("  %-32s %s\n", "    --no-cache", "Disable response cache")
	fmt.Printf("  %-32s %s\n", "-T, --tools", "Force enable read-only tool use")
	fmt.Printf("  %-32s %s\n", "    --no-tools", "Disable tool use")
	fmt.Printf("  %-32s %s\n\n", "    --no-stream", "Collect full response before printing")
	fmt.Printf("%s\n", color.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-32s %s\n\n", "-h, -?, --help", "Show help for command")
	fmt.Printf("%s\n", color.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s ask \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask -n \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask -d \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask --cue math \"solve x^2 + 3x - 4\"\n", n)
	fmt.Printf("  $ %s ask --category code \"write a binary search in Go\"\n", n)
	fmt.Printf("  $ %s ask --session abc123 \"continue from before\"\n", n)
	fmt.Printf("  $ %s ask\n", n)
	fmt.Printf("  $ echo \"translate to French: hello\" | %s ask\n", n)
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
	cmd.Flags().StringVar(&opts.dumpPrompt, "dump-prompt", "", "Write assembled message array as JSON to file (- for stdout), skip inference")
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
