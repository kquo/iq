package tools

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

	"iq/internal/config"
	"iq/internal/embed"
	"iq/internal/search"
)

// ── Tool types ───────────────────────────────────────────────────────────────

// Param describes a tool parameter.
type Param struct {
	Name, Type, Description string
	Required                bool
}

// Tool describes a callable tool with its handler function.
type Tool struct {
	Name, Description string
	Params            []Param
	ReadOnly          bool // true for all current tools; future write tools set false
	Handler           func(args map[string]any) (string, error)
}

// Call represents a parsed tool invocation from model output.
type Call struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// Result holds the output of a tool execution.
type Result struct {
	Call   Call
	Output string
	Error  string
}

// ── Registry ─────────────────────────────────────────────────────────────────

// NewRegistry returns a fresh slice of all available tools.
// Using a constructor instead of init() allows tests to create isolated
// registries without depending on package-level global state.
func NewRegistry() []Tool {
	return []Tool{
		toolGetTime(),
		toolReadFile(),
		toolListDir(),
		toolFileInfo(),
		toolCalc(),
		toolSearchText(),
		toolCountLines(),
		toolWebSearch(),
	}
}

// Registry is the package-level set of all available tools.
var Registry = NewRegistry()

// FindTool returns the tool with the given name, or nil.
func FindTool(name string) *Tool {
	for i := range Registry {
		if Registry[i].Name == name {
			return &Registry[i]
		}
	}
	return nil
}

// RegistryNames returns the names of all registered tools.
func RegistryNames() []string {
	names := make([]string, len(Registry))
	for i, t := range Registry {
		names[i] = t.Name
	}
	return names
}

// ── Path security ────────────────────────────────────────────────────────────

// AllowedPaths returns the set of allowed root paths: CWD + config tool_paths.
func AllowedPaths() []string {
	roots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		if abs, err := filepath.EvalSymlinks(cwd); err == nil {
			roots = append(roots, abs)
		} else {
			roots = append(roots, cwd)
		}
	}
	cfg, err := config.Load(nil)
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

// ValidatePath resolves a raw path and checks it falls within allowed roots.
func ValidatePath(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	for _, root := range AllowedPaths() {
		if strings.HasPrefix(resolved, root+string(filepath.Separator)) || resolved == root {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q is outside allowed directories", raw)
}

// ── Tool handlers ────────────────────────────────────────────────────────────

func toolGetTime() Tool {
	return Tool{
		Name:        "get_time",
		Description: "Get the current date, time, and timezone",
		ReadOnly:    true,
		Handler: func(args map[string]any) (string, error) {
			now := time.Now()
			return now.Format("2006-01-02 15:04:05 MST (Monday)"), nil
		},
	}
}

func toolReadFile() Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read the contents of a file (max 64KB)",
		ReadOnly:    true,
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path to read", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := ValidatePath(raw)
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

func toolListDir() Tool {
	return Tool{
		Name:        "list_dir",
		Description: "List the entries in a directory",
		ReadOnly:    true,
		Params: []Param{
			{Name: "path", Type: "string", Description: "Directory path to list", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := ValidatePath(raw)
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

func toolFileInfo() Tool {
	return Tool{
		Name:        "file_info",
		Description: "Get file metadata: size, modification time, permissions",
		ReadOnly:    true,
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := ValidatePath(raw)
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

func toolCalc() Tool {
	return Tool{
		Name:        "calc",
		Description: "Evaluate a math expression (supports +, -, *, /, %, parentheses, decimals)",
		ReadOnly:    true,
		Params: []Param{
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
			if result == math.Trunc(result) && math.Abs(result) < 1e15 {
				return strconv.FormatInt(int64(result), 10), nil
			}
			return strconv.FormatFloat(result, 'f', -1, 64), nil
		},
	}
}

func toolSearchText() Tool {
	return Tool{
		Name:        "search_text",
		Description: "Search for a regex pattern in files under a directory",
		ReadOnly:    true,
		Params: []Param{
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
			root, err := ValidatePath(raw)
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
					return nil
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
						matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(matches) == 0 {
				return "No matches found.", nil
			}
			return strings.Join(matches, "\n"), nil
		},
	}
}

// isTextFile is a lightweight check for toolSearchText to skip binary files.
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	textExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".java": true,
		".rb": true, ".php": true, ".swift": true, ".kt": true, ".sh": true,
		".yaml": true, ".yml": true, ".toml": true, ".json": true, ".xml": true,
		".html": true, ".css": true, ".md": true, ".txt": true, ".rst": true,
		".sql": true, ".proto": true, ".tf": true, ".env": true, ".ini": true,
		".cfg": true, ".conf": true,
	}
	return textExts[ext]
}

func toolCountLines() Tool {
	return Tool{
		Name:        "count_lines",
		Description: "Count lines in a file",
		ReadOnly:    true,
		Params: []Param{
			{Name: "path", Type: "string", Description: "File path to count lines in", Required: true},
		},
		Handler: func(args map[string]any) (string, error) {
			raw, _ := args["path"].(string)
			if raw == "" {
				return "", fmt.Errorf("missing required parameter: path")
			}
			path, err := ValidatePath(raw)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("cannot read %q: %w", raw, err)
			}
			n := strings.Count(string(data), "\n")
			if len(data) > 0 && data[len(data)-1] != '\n' {
				n++
			}
			return fmt.Sprintf("%d lines", n), nil
		},
	}
}

func toolWebSearch() Tool {
	return Tool{
		Name:        "web_search",
		Description: "Search the web for current information using DuckDuckGo",
		ReadOnly:    true,
		Params: []Param{
			{Name: "query", Type: "string", Description: "Search query", Required: true},
			{Name: "count", Type: "number", Description: "Max results to return (default: 3)", Required: false},
		},
		Handler: func(args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			if query == "" {
				return "", fmt.Errorf("missing required parameter: query")
			}
			maxResults := 3
			if n, ok := args["count"].(float64); ok && n > 0 {
				maxResults = min(int(n), 20)
			}
			param, err := search.NewParam(query)
			if err != nil {
				return "", err
			}
			sc := searchClient
			if sc == nil {
				sc = search.NewClient("")
			}
			results, err := sc.Search(param, maxResults)
			if err != nil {
				return "", fmt.Errorf("web search failed: %w", err)
			}
			if results == nil || len(*results) == 0 {
				return "No results found.", nil
			}
			var b strings.Builder
			for i, r := range *results {
				if i >= maxResults {
					break
				}
				fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.Link, r.Snippet)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

// ── System prompt builders ──────────────────────────────────────────────────

// BuildSystemPrompt generates the tool instruction block for the standard tool_call format.
func BuildSystemPrompt() string {
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
	for _, t := range Registry {
		fmt.Fprintf(&b, "\n- %s: %s\n", t.Name, t.Description)
		if len(t.Params) > 0 {
			b.WriteString("  Parameters:\n")
			for _, p := range t.Params {
				req := ""
				if p.Required {
					req = " (required)"
				}
				fmt.Fprintf(&b, "    - %s (%s): %s%s\n", p.Name, p.Type, p.Description, req)
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&b, "\nCurrent working directory: %s\n", cwd)
		fmt.Fprintf(&b, "Use absolute paths based on this directory for file operations.\n")
	}
	return b.String()
}

// BuildRoutingPrompt returns a system prompt for routing-grammar-aware inference.
func BuildRoutingPrompt() string {
	var b strings.Builder
	b.WriteString("\n[tools]\n")
	b.WriteString("You have access to read-only tools. When a question can be answered by calling a tool, you MUST call the tool — never guess or fabricate the answer.\n\n")
	b.WriteString("Your first output MUST be one of:\n")
	b.WriteString("  <tool:TOOL_NAME>  — to call a tool, followed by JSON arguments\n")
	b.WriteString("  <no_tool>         — to respond without using a tool, followed by your answer\n\n")
	b.WriteString("Use <no_tool> ONLY for questions that no tool can answer (general knowledge, explanations, etc.).\n")
	b.WriteString("Do not produce any text before the routing prefix.\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range Registry {
		fmt.Fprintf(&b, "\n- %s: %s\n", t.Name, t.Description)
		if len(t.Params) > 0 {
			b.WriteString("  Parameters:\n")
			for _, p := range t.Params {
				req := ""
				if p.Required {
					req = " (required)"
				}
				fmt.Fprintf(&b, "    - %s (%s): %s%s\n", p.Name, p.Type, p.Description, req)
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&b, "\nCurrent working directory: %s\n", cwd)
		fmt.Fprintf(&b, "Use absolute paths based on this directory for file operations.\n")
	}
	return b.String()
}

// ── Search client ────────────────────────────────────────────────────────────

var searchClient *search.Client

// SetSearchClient configures the search client used by the web_search tool.
// Called once at prompt startup with the Brave API key from config.
func SetSearchClient(c *search.Client) { searchClient = c }

// ── Confirm mode ────────────────────────────────────────────────────────────

var confirmMode bool

// SetConfirmMode enables or disables confirmation for non-read-only tools.
func SetConfirmMode(v bool) { confirmMode = v }

// ── Executor ─────────────────────────────────────────────────────────────────

const (
	// ExecuteTimeout is the maximum time a tool handler may run.
	ExecuteTimeout = 30 * time.Second
	// MaxOutputBytes caps tool output injected into the conversation context.
	MaxOutputBytes = 32 * 1024
)

// Execute dispatches a tool call to its handler with timeout and output limits.
func Execute(call Call) Result {
	t := FindTool(call.Name)
	if t == nil {
		return Result{Call: call, Error: fmt.Sprintf("unknown tool: %s", call.Name)}
	}
	if !t.ReadOnly && !confirmMode {
		return Result{Call: call, Error: fmt.Sprintf("tool %q requires --confirm (write operation)", call.Name)}
	}

	type handlerResult struct {
		output string
		err    error
	}
	ch := make(chan handlerResult, 1)
	go func() {
		output, err := t.Handler(call.Args)
		ch <- handlerResult{output, err}
	}()

	select {
	case hr := <-ch:
		if hr.err != nil {
			return Result{Call: call, Error: hr.err.Error()}
		}
		output := hr.output
		if len(output) > MaxOutputBytes {
			output = output[:MaxOutputBytes] + "\n... (output truncated)"
		}
		return Result{Call: call, Output: output}
	case <-time.After(ExecuteTimeout):
		return Result{Call: call, Error: fmt.Sprintf("tool %q timed out after %s", call.Name, ExecuteTimeout)}
	}
}

// FormatResult formats a tool result for injection into the conversation.
func FormatResult(r Result) string {
	if r.Error != "" {
		return fmt.Sprintf("<tool_result name=%q>ERROR: %s</tool_result>", r.Call.Name, r.Error)
	}
	return fmt.Sprintf("<tool_result name=%q>\n%s\n</tool_result>", r.Call.Name, r.Output)
}

// extractCalcExpression tries to extract a valid math expression from natural
// language input (e.g. "calculate 1234 times 5678" → "1234 * 5678").
// Returns "" if the input cannot be reduced to a clean math expression.
func extractCalcExpression(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))

	// Strip leading noise words.
	for _, prefix := range []string{
		"what's ", "what is ", "whats ", "how much is ",
		"calculate ", "compute ", "evaluate ", "solve ", "find ",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			break
		}
	}
	s = strings.TrimRight(s, "?. ")

	// Replace word operators with symbols.
	for _, r := range []struct{ word, sym string }{
		{"divided by", "/"},
		{"multiplied by", "*"},
		{"times", "*"},
		{"plus", "+"},
		{"minus", "-"},
		{"modulo", "%"},
		{"mod", "%"},
	} {
		s = strings.ReplaceAll(s, r.word, r.sym)
	}

	// Handle "X% of Y" → "(X/100)*Y".
	s = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%\s*of\s*(\d+(?:\.\d+)?)`).ReplaceAllString(s, "($1/100)*$2")

	s = strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))

	// Accept only if the result contains only math-safe characters.
	if !regexp.MustCompile(`^[\d\s+\-*/%().]+$`).MatchString(s) {
		return ""
	}
	return s
}

// GuardArgs builds a default arg map for a guard direct-call.
// For the calc tool, it attempts to extract a valid math expression from
// natural language before falling back to the raw input.
func GuardArgs(toolName, input string) map[string]any {
	if toolName == "calc" {
		if expr := extractCalcExpression(input); expr != "" {
			return map[string]any{"expression": expr}
		}
		return nil // caller falls back to direct inference
	}
	t := FindTool(toolName)
	if t == nil {
		return nil
	}
	for _, p := range t.Params {
		if p.Required {
			return map[string]any{p.Name: input}
		}
	}
	return nil
}

// ── Schema validation ────────────────────────────────────────────────────────

// ValidateCall checks that a Call conforms to the tool's parameter schema:
// tool must exist, required params present, no unknown params, correct types.
func ValidateCall(call Call) error {
	t := FindTool(call.Name)
	if t == nil {
		return fmt.Errorf("unknown tool: %s", call.Name)
	}
	// Check required params present.
	for _, p := range t.Params {
		if p.Required {
			v, ok := call.Args[p.Name]
			if !ok || v == nil {
				return fmt.Errorf("missing required parameter %q", p.Name)
			}
		}
	}
	// Check no unknown params.
	known := make(map[string]bool, len(t.Params))
	for _, p := range t.Params {
		known[p.Name] = true
	}
	for k := range call.Args {
		if !known[k] {
			return fmt.Errorf("unknown parameter %q", k)
		}
	}
	// Check param types.
	for k, v := range call.Args {
		for _, p := range t.Params {
			if p.Name == k {
				if err := checkParamType(v, p.Type); err != nil {
					return fmt.Errorf("parameter %q: %w", k, err)
				}
				break
			}
		}
	}
	return nil
}

func checkParamType(v any, expected string) error {
	switch expected {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected string, got %T", v)
		}
	case "number":
		switch v.(type) {
		case float64, int:
			// OK
		default:
			return fmt.Errorf("expected number, got %T", v)
		}
	}
	return nil
}

// ParseCallsStrict wraps ParseCalls and validates each call against the
// tool registry schema. Valid calls are returned; invalid ones produce errors.
func ParseCallsStrict(text string) ([]Call, string, []error) {
	calls, remaining := ParseCalls(text)
	var errs []error
	var valid []Call
	for _, c := range calls {
		if err := ValidateCall(c); err != nil {
			errs = append(errs, fmt.Errorf("tool %q: %w", c.Name, err))
		} else {
			valid = append(valid, c)
		}
	}
	return valid, remaining, errs
}

// ── Parsing ──────────────────────────────────────────────────────────────────

// ParseCalls extracts tool_call blocks from model output.
// Returns parsed calls and the remaining text with tool blocks removed.
func ParseCalls(text string) ([]Call, string) {
	reClosed := regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)
	reUnclosed := regexp.MustCompile(`(?s)<tool_call>\s*(\{.*)$`)

	type region struct {
		start, end         int
		jsonStart, jsonEnd int
	}
	var regions []region

	for _, m := range reClosed.FindAllStringSubmatchIndex(text, -1) {
		regions = append(regions, region{m[0], m[1], m[2], m[3]})
	}

	if len(regions) == 0 {
		if m := reUnclosed.FindStringSubmatchIndex(text); m != nil {
			regions = append(regions, region{m[0], m[1], m[2], m[3]})
		}
	}

	// Fallback: model used <toolname ...> instead of <tool_call>.
	if len(regions) == 0 {
		for _, t := range Registry {
			pat := fmt.Sprintf(`(?s)<%s\b[^>]*>(.*?)</%s>`, regexp.QuoteMeta(t.Name), regexp.QuoteMeta(t.Name))
			if m := regexp.MustCompile(pat).FindStringSubmatchIndex(text); m != nil {
				calls := []Call{{Name: t.Name, Args: map[string]any{}}}
				var clean strings.Builder
				clean.WriteString(text[:m[0]])
				clean.WriteString(text[m[1]:])
				return calls, strings.TrimSpace(clean.String())
			}
			pat2 := fmt.Sprintf(`<%s\b[^>]*>\s*$`, regexp.QuoteMeta(t.Name))
			if m := regexp.MustCompile(pat2).FindStringIndex(text); m != nil {
				calls := []Call{{Name: t.Name, Args: map[string]any{}}}
				remaining := strings.TrimSpace(text[:m[0]])
				return calls, remaining
			}
		}
	}

	if len(regions) == 0 {
		return nil, text
	}

	var calls []Call
	var clean strings.Builder
	prev := 0
	for _, r := range regions {
		clean.WriteString(text[prev:r.start])
		jsonStr := text[r.jsonStart:r.jsonEnd]
		jsonStr = strings.TrimPrefix(jsonStr, "```json")
		jsonStr = strings.TrimPrefix(jsonStr, "```")
		jsonStr = strings.TrimSuffix(jsonStr, "```")
		jsonStr = strings.TrimSpace(jsonStr)

		tc := parseCallJSON(jsonStr)
		if tc.Name != "" {
			calls = append(calls, tc)
		}
		prev = r.end
	}
	clean.WriteString(text[prev:])

	return calls, strings.TrimSpace(clean.String())
}

func parseCallJSON(jsonStr string) Call {
	var tc Call
	if err := json.Unmarshal([]byte(jsonStr), &tc); err == nil && tc.Name != "" {
		return tc
	}
	nameRe := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
	m := nameRe.FindStringSubmatch(jsonStr)
	if len(m) < 2 {
		return Call{}
	}
	tc.Name = m[1]
	tc.Args = map[string]any{}
	argRe := regexp.MustCompile(`"(path|expression|pattern)"\s*:\s*"([^"]*)"`)
	for _, am := range argRe.FindAllStringSubmatch(jsonStr, -1) {
		tc.Args[am[1]] = am[2]
	}
	return tc
}

// ParseRoutingPrefix extracts the routing decision from a grammar-constrained response.
func ParseRoutingPrefix(text string) (toolName string, rest string) {
	if after, ok := strings.CutPrefix(text, "<no_tool>"); ok {
		return "", after
	}
	re := regexp.MustCompile(`^<tool:(\w+)>`)
	m := re.FindStringSubmatch(text)
	if m != nil {
		return m[1], text[len(m[0]):]
	}
	return "", text
}

// ParseRoutingArgs extracts tool arguments from the text after a routing prefix.
func ParseRoutingArgs(text string) map[string]any {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil
	}

	flagRe := regexp.MustCompile(`--(\w+)[= ]"?([^\s"]+)"?`)
	if flagMatches := flagRe.FindAllStringSubmatch(s, -1); len(flagMatches) > 0 {
		args := make(map[string]any)
		for _, m := range flagMatches {
			args[m[1]] = m[2]
		}
		return args
	}

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil
	}
	depth := 0
	end := -1
outer:
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				break outer
			}
		}
	}
	if end < 0 {
		end = len(s)
	}
	jsonStr := s[start:end]

	var args map[string]any
	if json.Unmarshal([]byte(jsonStr), &args) == nil && len(args) > 0 {
		return args
	}

	argRe := regexp.MustCompile(`(?:"?)(\w+)(?:"?)\s*[:=]\s*"([^"]*)"`)
	matches := argRe.FindAllStringSubmatch(jsonStr, -1)
	if len(matches) == 0 {
		return nil
	}
	args = make(map[string]any)
	for _, m := range matches {
		args[m[1]] = m[2]
	}
	return args
}

// ── Calc evaluator ──────────────────────────────────────────────────────────

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
	if p.input[p.pos] == '(' {
		p.pos++
		val := p.parseExpr()
		p.skipSpace()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
		}
		return val
	}
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

func (p *calcParser) err() error { return nil }

// ── Tool signal types ───────────────────────────────────────────────────────

// Signal maps a semantic signal to one or more tools.
type Signal struct {
	Name        string
	Description string
	Tools       []string
}

// Signals defines the embed-based tool detection signals.
var Signals = []Signal{
	{
		Name:        "time_date",
		Description: "What time is it, today's date, day of the week, tell me the time, what is the time",
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
	{
		Name:        "web_search",
		Description: "Search the web for current information, look up latest news, who is the current president, what is the current population, recent events and results, who won the game, current price of stock, weather forecast, up to date facts",
		Tools:       []string{"web_search"},
	},
}

const (
	toolCacheFile            = "tool_embeddings.json"
	ClassifyMinScore float32 = 0.60
)

// SignalToolNames returns the tool names associated with a signal, or nil.
func SignalToolNames(signal string) []string {
	for _, s := range Signals {
		if s.Name == signal {
			return s.Tools
		}
	}
	return nil
}

// ── Tool embedding cache ────────────────────────────────────────────────────

type toolEmbeddingCache struct {
	Model     string               `json:"model"`
	Version   uint32               `json:"version"`
	Generated string               `json:"generated"`
	Signals   map[string][]float32 `json:"signals"`
}

func signalsVersion() uint32 {
	h := fnv.New32a()
	for _, s := range Signals {
		h.Write([]byte(s.Name))
		h.Write([]byte(s.Description))
	}
	return h.Sum32()
}

func toolEmbeddingsPath() (string, error) {
	dir, err := config.Dir()
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
	var c toolEmbeddingCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveToolEmbeddings(c *toolEmbeddingCache) error {
	path, err := toolEmbeddingsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func toolEmbeddingsStale(model string) bool {
	c, err := loadToolEmbeddings()
	if err != nil || c == nil {
		return true
	}
	if c.Model != model || c.Version != signalsVersion() {
		return true
	}
	for _, s := range Signals {
		if _, ok := c.Signals[s.Name]; !ok {
			return true
		}
	}
	return false
}

func refreshToolEmbeddings(model string) error {
	texts := make([]string, len(Signals))
	names := make([]string, len(Signals))
	for i, s := range Signals {
		texts[i] = s.Name + ": " + s.Description
		names[i] = s.Name
	}
	embeddings, err := embed.Texts(texts, "document")
	if err != nil {
		return fmt.Errorf("failed to embed tool signal descriptions: %w", err)
	}
	c := &toolEmbeddingCache{
		Model:     model,
		Version:   signalsVersion(),
		Generated: time.Now().UTC().Format(time.RFC3339),
		Signals:   make(map[string][]float32, len(Signals)),
	}
	for i, name := range names {
		if i < len(embeddings) {
			c.Signals[name] = embeddings[i]
		}
	}
	return saveToolEmbeddings(c)
}

// ── Tool classify ───────────────────────────────────────────────────────────

// ClassifyTrace carries the details of a tool detection decision.
type ClassifyTrace struct {
	BestSignal string
	BestScore  float32
	Enabled    bool
	Reason     string // "embed", "file path", "forced"
	Elapsed    time.Duration
}

// Classify compares the pre-computed input vector against tool signal
// embeddings and returns whether tools should be enabled.
func Classify(inputVec []float32, model string) (bool, *ClassifyTrace) {
	t0 := time.Now()

	if toolEmbeddingsStale(model) {
		if err := refreshToolEmbeddings(model); err != nil {
			return false, &ClassifyTrace{Elapsed: time.Since(t0)}
		}
	}

	c, err := loadToolEmbeddings()
	if err != nil || c == nil {
		return false, &ClassifyTrace{Elapsed: time.Since(t0)}
	}

	var bestName string
	var bestScore float32 = -2
	for _, s := range Signals {
		sigVec, ok := c.Signals[s.Name]
		if !ok {
			continue
		}
		score := embed.CosineSimilarity(inputVec, sigVec)
		if score > bestScore {
			bestScore = score
			bestName = s.Name
		}
	}

	enabled := bestScore >= ClassifyMinScore
	trace := &ClassifyTrace{
		BestSignal: bestName,
		BestScore:  bestScore,
		Enabled:    enabled,
		Reason:     "embed",
		Elapsed:    time.Since(t0),
	}
	return enabled, trace
}

// ── File-path heuristic ─────────────────────────────────────────────────────

// HasFilePath returns true if the input contains a file path.
func HasFilePath(input string) bool {
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
		if strings.Contains(w, "/") && !strings.HasPrefix(strings.ToLower(w), "http") {
			return true
		}
		ext := strings.ToLower(filepath.Ext(w))
		if ext != "" && knownExts[ext] {
			return true
		}
	}
	return false
}

// ── Trace helpers (for cmd/iq to call) ──────────────────────────────────────

// PrintStatus prints a short tool-use indicator to stderr.
func PrintStatus(call Call) {
	fmt.Fprintf(os.Stderr, "\033[90m[tool: %s]\033[0m\n", call.Name)
}
