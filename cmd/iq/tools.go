package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/queone/utl"
)

// ── Tool types ───────────────────────────────────────────────────────────────

type toolParam struct {
	Name, Type, Description string
	Required                bool
}

type tool struct {
	Name, Description string
	Params            []toolParam
	Handler           func(args map[string]any) (string, error)
}

type toolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type toolResult struct {
	Call   toolCall
	Output string
	Error  string
}

// ── Registry ─────────────────────────────────────────────────────────────────

var toolRegistry []tool

func init() {
	toolRegistry = []tool{
		toolGetTime(),
		toolReadFile(),
		toolListDir(),
		toolFileInfo(),
		toolCalc(),
		toolSearchText(),
		toolCountLines(),
	}
}

// findTool returns the tool with the given name, or nil.
func findTool(name string) *tool {
	for i := range toolRegistry {
		if toolRegistry[i].Name == name {
			return &toolRegistry[i]
		}
	}
	return nil
}

// ── Path security ────────────────────────────────────────────────────────────

// toolAllowedPaths returns the set of allowed root paths: CWD + config tool_paths.
func toolAllowedPaths() []string {
	roots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		if abs, err := filepath.EvalSymlinks(cwd); err == nil {
			roots = append(roots, abs)
		} else {
			roots = append(roots, cwd)
		}
	}
	cfg, err := loadConfig()
	if err == nil {
		for _, p := range cfg.ToolPaths {
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				roots = append(roots, resolved)
			} else {
				roots = append(roots, abs)
			}
		}
	}
	return roots
}

// validatePath resolves a raw path and checks it falls within allowed roots.
func validatePath(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File may not exist yet — use the abs path for prefix check.
		resolved = abs
	}
	for _, root := range toolAllowedPaths() {
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) || resolved == root {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q is outside allowed directories", raw)
}

// ── Tool handlers ────────────────────────────────────────────────────────────

func toolGetTime() tool {
	return tool{
		Name:        "get_time",
		Description: "Get the current date, time, and timezone",
		Handler: func(args map[string]any) (string, error) {
			now := time.Now()
			return now.Format("2006-01-02 15:04:05 MST (Monday)"), nil
		},
	}
}

func toolReadFile() tool {
	return tool{
		Name:        "read_file",
		Description: "Read the contents of a file (max 64KB)",
		Params: []toolParam{
			{Name: "path", Type: "string", Description: "File path to read", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := validatePath(raw)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(path)
			if err != nil {
				return "", fmt.Errorf("cannot access %q: %w", raw, err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("%q is a directory, not a file", raw)
			}
			const maxSize = 64 * 1024
			if info.Size() > maxSize {
				return "", fmt.Errorf("file %q is %d bytes (max %d)", raw, info.Size(), maxSize)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	}
}

func toolListDir() tool {
	return tool{
		Name:        "list_dir",
		Description: "List the entries in a directory",
		Params: []toolParam{
			{Name: "path", Type: "string", Description: "Directory path to list", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := validatePath(raw)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return "", fmt.Errorf("cannot read directory %q: %w", raw, err)
			}
			var b strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name)
				b.WriteByte('\n')
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

func toolFileInfo() tool {
	return tool{
		Name:        "file_info",
		Description: "Get file metadata: size, modification time, permissions",
		Params: []toolParam{
			{Name: "path", Type: "string", Description: "File path", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := validatePath(raw)
			if err != nil {
				return "", err
			}
			info, err := os.Stat(path)
			if err != nil {
				return "", fmt.Errorf("cannot access %q: %w", raw, err)
			}
			kind := "file"
			if info.IsDir() {
				kind = "directory"
			}
			return fmt.Sprintf("name: %s\ntype: %s\nsize: %d bytes\nmodified: %s\npermissions: %s",
				info.Name(), kind, info.Size(),
				info.ModTime().Format("2006-01-02 15:04:05 MST"),
				info.Mode().String()), nil
		},
	}
}

func toolCalc() tool {
	return tool{
		Name:        "calc",
		Description: "Evaluate a math expression (supports +, -, *, /, %, parentheses, decimals)",
		Params: []toolParam{
			{Name: "expression", Type: "string", Description: "Math expression to evaluate", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			expr, _ := args["expression"].(string)
			if expr == "" {
				return "", fmt.Errorf("missing required parameter: expression")
			}
			result, err := calcEval(expr)
			if err != nil {
				return "", err
			}
			// Format nicely: integers without decimal, floats trimmed.
			if result == math.Trunc(result) && math.Abs(result) < 1e15 {
				return strconv.FormatInt(int64(result), 10), nil
			}
			return strconv.FormatFloat(result, 'f', -1, 64), nil
		},
	}
}

func toolSearchText() tool {
	return tool{
		Name:        "search_text",
		Description: "Search for a regex pattern in files under a directory",
		Params: []toolParam{
			{Name: "pattern", Type: "string", Description: "Regex pattern to search for", Required: true},
			{Name: "path", Type: "string", Description: "Directory or file to search (default: current directory)", Required: false},
		},
		Handler: func(args map[string]any) (string, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return "", fmt.Errorf("missing required parameter: pattern")
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex: %w", err)
			}
			raw, _ := args["path"].(string)
			if raw == "" {
				raw = "."
			}
			root, err := validatePath(raw)
			if err != nil {
				return "", err
			}

			const maxMatches = 50
			const maxFileSize = 256 * 1024
			var matches []string

			skipDirs := map[string]bool{
				".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
			}

			err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return nil // skip inaccessible entries
				}
				if info.IsDir() {
					name := info.Name()
					if strings.HasPrefix(name, ".") && name != "." {
						return filepath.SkipDir
					}
					if skipDirs[name] {
						return filepath.SkipDir
					}
					return nil
				}
				if info.Size() > maxFileSize || !isTextFile(path) {
					return nil
				}
				if len(matches) >= maxMatches {
					return filepath.SkipAll
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				lines := strings.Split(string(data), "\n")
				rel, _ := filepath.Rel(root, path)
				if rel == "" {
					rel = path
				}
				for i, line := range lines {
					if len(matches) >= maxMatches {
						break
					}
					if re.MatchString(line) {
						matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, i+1, line))
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return "no matches found", nil
			}
			result := strings.Join(matches, "\n")
			if len(matches) >= maxMatches {
				result += fmt.Sprintf("\n... (limited to %d matches)", maxMatches)
			}
			return result, nil
		},
	}
}

func toolCountLines() tool {
	return tool{
		Name:        "count_lines",
		Description: "Count the number of lines in a file",
		Params: []toolParam{
			{Name: "path", Type: "string", Description: "File path", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := validatePath(raw)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("cannot read %q: %w", raw, err)
			}
			count := strings.Count(string(data), "\n")
			// If the file doesn't end with a newline, the last line still counts.
			if len(data) > 0 && data[len(data)-1] != '\n' {
				count++
			}
			return fmt.Sprintf("%d lines", count), nil
		},
	}
}

// ── System prompt builder ────────────────────────────────────────────────────

// buildToolSystemPrompt generates the tool instruction block appended to the
// system message when tools are active.
func buildToolSystemPrompt() string {
	var b strings.Builder
	b.WriteString("\n[tools]\n")
	b.WriteString("You have access to read-only tools. To call a tool, you MUST use this exact format:\n")
	b.WriteString("<tool_call>{\"name\": \"TOOL_NAME\", \"args\": {}}</tool_call>\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- The tag MUST be <tool_call>. Do NOT use <get_time>, <read_file>, or any other tag name.\n")
	b.WriteString("- Always close with </tool_call>.\n")
	b.WriteString("- Only use args listed below. Tools with no parameters use empty args {}.\n\n")
	b.WriteString("Example — getting the current time:\n")
	b.WriteString("<tool_call>{\"name\": \"get_time\", \"args\": {}}</tool_call>\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range toolRegistry {
		b.WriteString(fmt.Sprintf("\n- %s: %s\n", t.Name, t.Description))
		if len(t.Params) > 0 {
			b.WriteString("  Parameters:\n")
			for _, p := range t.Params {
				req := ""
				if p.Required {
					req = " (required)"
				}
				b.WriteString(fmt.Sprintf("    - %s (%s): %s%s\n", p.Name, p.Type, p.Description, req))
			}
		}
	}
	return b.String()
}

// ── Parser ───────────────────────────────────────────────────────────────────

// parseToolCalls extracts tool_call blocks from model output.
// Returns parsed calls and the remaining text with tool blocks removed.
// Handles both closed (<tool_call>...</tool_call>) and unclosed
// (<tool_call>... at end of response) blocks — small models often
// omit closing tags.
func parseToolCalls(text string) ([]toolCall, string) {
	// Match closed blocks first, then unclosed blocks at end of text.
	// The unclosed pattern matches <tool_call> followed by JSON to EOF.
	reClosed := regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)
	reUnclosed := regexp.MustCompile(`(?s)<tool_call>\s*(\{.*)$`)

	type region struct {
		start, end         int // full match bounds in text
		jsonStart, jsonEnd int // capture group bounds
	}
	var regions []region

	// Collect closed matches.
	for _, m := range reClosed.FindAllStringSubmatchIndex(text, -1) {
		regions = append(regions, region{m[0], m[1], m[2], m[3]})
	}

	// If no closed matches, try unclosed at end of text.
	if len(regions) == 0 {
		if m := reUnclosed.FindStringSubmatchIndex(text); m != nil {
			regions = append(regions, region{m[0], m[1], m[2], m[3]})
		}
	}

	// Fallback: model used <toolname ...> instead of <tool_call>.
	// Match known tool names as XML tags, e.g. <get_time ...>...</get_time>
	// or <get_time ...> (unclosed).
	if len(regions) == 0 {
		for _, t := range toolRegistry {
			// Closed: <get_time ...>...</get_time>
			pat := fmt.Sprintf(`(?s)<%s\b[^>]*>(.*?)</%s>`, regexp.QuoteMeta(t.Name), regexp.QuoteMeta(t.Name))
			if m := regexp.MustCompile(pat).FindStringSubmatchIndex(text); m != nil {
				calls := []toolCall{{Name: t.Name, Args: map[string]any{}}}
				var clean strings.Builder
				clean.WriteString(text[:m[0]])
				clean.WriteString(text[m[1]:])
				return calls, strings.TrimSpace(clean.String())
			}
			// Unclosed: <get_time ...> at end
			pat2 := fmt.Sprintf(`<%s\b[^>]*>\s*$`, regexp.QuoteMeta(t.Name))
			if m := regexp.MustCompile(pat2).FindStringIndex(text); m != nil {
				calls := []toolCall{{Name: t.Name, Args: map[string]any{}}}
				remaining := strings.TrimSpace(text[:m[0]])
				return calls, remaining
			}
		}
	}

	if len(regions) == 0 {
		return nil, text
	}

	var calls []toolCall
	var clean strings.Builder
	prev := 0
	for _, r := range regions {
		clean.WriteString(text[prev:r.start])
		jsonStr := text[r.jsonStart:r.jsonEnd]
		// Strip markdown code fences models sometimes wrap JSON in.
		jsonStr = strings.TrimPrefix(jsonStr, "```json")
		jsonStr = strings.TrimPrefix(jsonStr, "```")
		jsonStr = strings.TrimSuffix(jsonStr, "```")
		jsonStr = strings.TrimSpace(jsonStr)

		tc := parseToolCallJSON(jsonStr)
		if tc.Name != "" {
			calls = append(calls, tc)
		}
		prev = r.end
	}
	clean.WriteString(text[prev:])

	return calls, strings.TrimSpace(clean.String())
}

// parseToolCallJSON tries strict JSON unmarshal first, then falls back to
// regex extraction of the tool name if the JSON is malformed (common with
// small models that produce broken args).
func parseToolCallJSON(jsonStr string) toolCall {
	var tc toolCall
	if err := json.Unmarshal([]byte(jsonStr), &tc); err == nil && tc.Name != "" {
		return tc
	}
	// Fallback: extract "name" field with regex.
	nameRe := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
	m := nameRe.FindStringSubmatch(jsonStr)
	if len(m) < 2 {
		return toolCall{}
	}
	tc.Name = m[1]
	tc.Args = map[string]any{}
	// Try to salvage individual string args from the broken JSON.
	argRe := regexp.MustCompile(`"(path|expression|pattern)"\s*:\s*"([^"]*)"`)
	for _, am := range argRe.FindAllStringSubmatch(jsonStr, -1) {
		tc.Args[am[1]] = am[2]
	}
	return tc
}

// ── Executor ─────────────────────────────────────────────────────────────────

// executeTool dispatches a tool call to its handler.
func executeTool(call toolCall) toolResult {
	t := findTool(call.Name)
	if t == nil {
		return toolResult{Call: call, Error: fmt.Sprintf("unknown tool: %s", call.Name)}
	}
	output, err := t.Handler(call.Args)
	if err != nil {
		return toolResult{Call: call, Error: err.Error()}
	}
	return toolResult{Call: call, Output: output}
}

// formatToolResult formats a tool result for injection into the conversation.
func formatToolResult(r toolResult) string {
	if r.Error != "" {
		return fmt.Sprintf("<tool_result name=%q>ERROR: %s</tool_result>", r.Call.Name, r.Error)
	}
	return fmt.Sprintf("<tool_result name=%q>\n%s\n</tool_result>", r.Call.Name, r.Output)
}

// ── Calc evaluator ───────────────────────────────────────────────────────────
// Recursive descent: expr → term ((+|-) term)*
//                    term → unary ((*|/|%) unary)*
//                    unary → (-|+)? atom
//                    atom → NUMBER | '(' expr ')'

type calcParser struct {
	input string
	pos   int
}

func calcEval(expr string) (float64, error) {
	p := &calcParser{input: strings.TrimSpace(expr)}
	result := p.parseExpr()
	p.skipSpace()
	if p.pos < len(p.input) {
		return 0, fmt.Errorf("unexpected character at position %d: %q", p.pos, string(p.input[p.pos]))
	}
	return result, p.err()
}

func (p *calcParser) parseExpr() float64 {
	left := p.parseTerm()
	for {
		p.skipSpace()
		if p.pos >= len(p.input) {
			return left
		}
		op := p.input[p.pos]
		if op != '+' && op != '-' {
			return left
		}
		p.pos++
		right := p.parseTerm()
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
}

func (p *calcParser) parseTerm() float64 {
	left := p.parseUnary()
	for {
		p.skipSpace()
		if p.pos >= len(p.input) {
			return left
		}
		op := p.input[p.pos]
		if op != '*' && op != '/' && op != '%' {
			return left
		}
		p.pos++
		right := p.parseUnary()
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return math.NaN()
			}
			left /= right
		case '%':
			if right == 0 {
				return math.NaN()
			}
			left = math.Mod(left, right)
		}
	}
}

func (p *calcParser) parseUnary() float64 {
	p.skipSpace()
	if p.pos < len(p.input) {
		if p.input[p.pos] == '-' {
			p.pos++
			return -p.parseAtom()
		}
		if p.input[p.pos] == '+' {
			p.pos++
		}
	}
	return p.parseAtom()
}

func (p *calcParser) parseAtom() float64 {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return 0
	}
	// Parenthesized expression.
	if p.input[p.pos] == '(' {
		p.pos++
		val := p.parseExpr()
		p.skipSpace()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
		}
		return val
	}
	// Number.
	start := p.pos
	for p.pos < len(p.input) && (p.input[p.pos] == '.' || (p.input[p.pos] >= '0' && p.input[p.pos] <= '9')) {
		p.pos++
	}
	if p.pos == start {
		return 0
	}
	val, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return 0
	}
	return val
}

func (p *calcParser) skipSpace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *calcParser) err() error {
	// All errors are handled inline; this is a placeholder for future expansion.
	return nil
}

// ── Tool signal types ────────────────────────────────────────────────────────

// toolSignal maps a semantic signal to one or more tools.
type toolSignal struct {
	Name        string
	Description string
	Tools       []string
}

// toolSignals defines the embed-based tool detection signals.
// Descriptions are keyword-rich for bge-small-en-v1.5 cosine matching.
var toolSignals = []toolSignal{
	{
		Name:        "time_date",
		Description: "What time is it, today's date, current day of the week, tell me the time, right now",
		Tools:       []string{"get_time"},
	},
	{
		Name:        "file_access",
		Description: "Read file contents, show me a file, list files in directory, file size, file metadata",
		Tools:       []string{"read_file", "list_dir", "file_info"},
	},
	{
		Name:        "file_search",
		Description: "Search for text in files, find pattern in code, grep, count lines, how many lines",
		Tools:       []string{"search_text", "count_lines"},
	},
	{
		Name:        "calculation",
		Description: "Calculate expression, compute result, evaluate math, what is X percent of Y, arithmetic",
		Tools:       []string{"calc"},
	},
}

const (
	toolCacheFile         = "tool_embeddings.json"
	toolMinScore  float32 = 0.72
)

// ── Tool embedding cache ─────────────────────────────────────────────────────

type toolEmbeddingCache struct {
	Model     string               `json:"model"`
	Version   uint32               `json:"version"`
	Generated string               `json:"generated"`
	Signals   map[string][]float32 `json:"signals"`
}

// toolSignalsVersion returns an FNV32a hash over signal names+descriptions
// so we can detect when the signal set changes.
func toolSignalsVersion() uint32 {
	h := fnv.New32a()
	for _, s := range toolSignals {
		h.Write([]byte(s.Name))
		h.Write([]byte(s.Description))
	}
	return h.Sum32()
}

func toolEmbeddingsPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, toolCacheFile), nil
}

func loadToolEmbeddings() (*toolEmbeddingCache, error) {
	path, err := toolEmbeddingsPath()
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
	var cache toolEmbeddingCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveToolEmbeddings(cache *toolEmbeddingCache) error {
	path, err := toolEmbeddingsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func toolEmbeddingsStale(model string) bool {
	cache, err := loadToolEmbeddings()
	if err != nil || cache == nil {
		return true
	}
	if cache.Model != model || cache.Version != toolSignalsVersion() {
		return true
	}
	for _, s := range toolSignals {
		if _, ok := cache.Signals[s.Name]; !ok {
			return true
		}
	}
	return false
}

func refreshToolEmbeddings(model string) error {
	texts := make([]string, len(toolSignals))
	names := make([]string, len(toolSignals))
	for i, s := range toolSignals {
		texts[i] = s.Name + ": " + s.Description
		names[i] = s.Name
	}
	embeddings, err := embedTexts(texts, "document")
	if err != nil {
		return fmt.Errorf("failed to embed tool signal descriptions: %w", err)
	}
	cache := &toolEmbeddingCache{
		Model:     model,
		Version:   toolSignalsVersion(),
		Generated: time.Now().UTC().Format(time.RFC3339),
		Signals:   make(map[string][]float32, len(toolSignals)),
	}
	for i, name := range names {
		if i < len(embeddings) {
			cache.Signals[name] = embeddings[i]
		}
	}
	return saveToolEmbeddings(cache)
}

// ── Tool classify ────────────────────────────────────────────────────────────

// toolClassifyTrace carries the details of a tool detection decision.
type toolClassifyTrace struct {
	BestSignal string
	BestScore  float32
	Enabled    bool
	Reason     string // "embed", "file path", "forced"
	Elapsed    time.Duration
}

// toolClassify compares the pre-computed input vector against tool signal
// embeddings and returns whether tools should be enabled.
func toolClassify(inputVec []float32, model string) (bool, *toolClassifyTrace) {
	t0 := time.Now()

	if toolEmbeddingsStale(model) {
		if err := refreshToolEmbeddings(model); err != nil {
			return false, &toolClassifyTrace{Elapsed: time.Since(t0)}
		}
	}

	cache, err := loadToolEmbeddings()
	if err != nil || cache == nil {
		return false, &toolClassifyTrace{Elapsed: time.Since(t0)}
	}

	var bestName string
	var bestScore float32 = -2
	for _, s := range toolSignals {
		sigVec, ok := cache.Signals[s.Name]
		if !ok {
			continue
		}
		score := cosineSimilarity(inputVec, sigVec)
		if score > bestScore {
			bestScore = score
			bestName = s.Name
		}
	}

	enabled := bestScore >= toolMinScore
	trace := &toolClassifyTrace{
		BestSignal: bestName,
		BestScore:  bestScore,
		Enabled:    enabled,
		Reason:     "embed",
		Elapsed:    time.Since(t0),
	}
	return enabled, trace
}

// ── File-path heuristic ──────────────────────────────────────────────────────

// hasFilePath returns true if the input contains a file path (slash-separated
// non-URL token or a word ending in a known source-code extension).
func hasFilePath(input string) bool {
	knownExts := map[string]bool{
		".txt": true, ".go": true, ".py": true, ".md": true, ".json": true,
		".yaml": true, ".yml": true, ".csv": true, ".log": true, ".sh": true,
		".js": true, ".ts": true, ".html": true, ".css": true, ".xml": true,
		".toml": true, ".cfg": true, ".conf": true, ".ini": true, ".sql": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".java": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true, ".r": true,
		".mod": true, ".sum": true, ".lock": true, ".env": true, ".makefile": true,
	}

	words := strings.FieldsSeq(input)
	for w := range words {
		// Path with slash (but not URLs).
		if strings.Contains(w, "/") && !strings.HasPrefix(strings.ToLower(w), "http") {
			return true
		}
		// Known file extension.
		ext := strings.ToLower(filepath.Ext(w))
		if ext != "" && knownExts[ext] {
			return true
		}
	}
	return false
}

// ── Trace helpers ────────────────────────────────────────────────────────────

func printToolCallTrace(call toolCall) {
	argsJSON, _ := json.Marshal(call.Args)
	traceField("tool_call", fmt.Sprintf("%s(%s)", call.Name, string(argsJSON)))
}

func printToolResultTrace(r toolResult) {
	if r.Error != "" {
		traceField("tool_error", truncate(r.Error, 200))
	} else {
		traceField("tool_result", truncate(r.Output, 200))
	}
}

// printToolStatus prints a short tool-use indicator to stderr.
func printToolStatus(call toolCall) {
	fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("[tool: %s]", call.Name)))
}
