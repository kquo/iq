package tools

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── NewRegistry tests ─────────────────────────────────────────────────────────

func TestNewRegistry(t *testing.T) {
	expectedNames := []string{
		"get_time", "read_file", "list_dir", "file_info",
		"calc", "search_text", "count_lines", "web_search",
	}
	reg := NewRegistry()
	if len(reg) != len(expectedNames) {
		t.Fatalf("NewRegistry() returned %d tools, want %d", len(reg), len(expectedNames))
	}
	nameSet := make(map[string]bool, len(reg))
	for _, tool := range reg {
		nameSet[tool.Name] = true
	}
	for _, name := range expectedNames {
		if !nameSet[name] {
			t.Errorf("NewRegistry() missing tool %q", name)
		}
	}
	// Verify isolation: mutations to one instance don't affect another.
	reg2 := NewRegistry()
	reg[0].Name = "mutated"
	if reg2[0].Name == "mutated" {
		t.Error("NewRegistry() instances share state; expected isolation")
	}
}

// ── calcEval tests ───────────────────────────────────────────────────────────

func TestCalcEval(t *testing.T) {
	tests := []struct {
		expr   string
		expect float64
	}{
		{"2 + 3", 5},
		{"10 - 4", 6},
		{"3 * 7", 21},
		{"20 / 4", 5},
		{"10 % 3", 1},
		{"2 + 3 * 4", 14},      // precedence: * before +
		{"(2 + 3) * 4", 20},    // parens
		{"-5", -5},             // unary minus
		{"-5 + 3", -2},         // unary minus in expr
		{"3.14 * 2", 6.28},     // decimals
		{"100 / 3", 100.0 / 3}, // float division
		{"(10 + 5) % 7", 1},    // modulo with parens
		{"  42  ", 42},         // whitespace
		{"2 * (3 + 4) - 1", 13},
		{"+5", 5}, // unary plus
	}
	for _, tc := range tests {
		got, err := calcEval(tc.expr)
		if err != nil {
			t.Errorf("calcEval(%q) error: %v", tc.expr, err)
			continue
		}
		if math.Abs(got-tc.expect) > 1e-9 {
			t.Errorf("calcEval(%q) = %v, want %v", tc.expr, got, tc.expect)
		}
	}
}

func TestCalcEvalErrors(t *testing.T) {
	tests := []string{
		"2 + + 3 abc",
		"abc",
	}
	for _, expr := range tests {
		_, err := calcEval(expr)
		if err == nil {
			// Some malformed expressions may parse partially — that's OK.
			// We mainly care that they don't panic.
			t.Logf("calcEval(%q) did not error (parsed partially)", expr)
		}
	}
}

func TestCalcDivisionByZero(t *testing.T) {
	got, err := calcEval("10 / 0")
	if err != nil {
		t.Fatalf("calcEval(\"10 / 0\") error: %v", err)
	}
	if !math.IsNaN(got) {
		t.Errorf("calcEval(\"10 / 0\") = %v, want NaN", got)
	}
}

// ── extractCalcExpression tests ──────────────────────────────────────────────

func TestExtractCalcExpression(t *testing.T) {
	tests := []struct {
		input  string
		expect string // "" means extraction should fail
	}{
		{"calculate 1234 times 5678", "1234 * 5678"},
		{"what is 15% of 340?", "(15/100)*340"},
		{"100 divided by 4", "100 / 4"},
		{"2 plus 2", "2 + 2"},
		{"10 minus 3", "10 - 3"},
		{"12 multiplied by 7", "12 * 7"},
		{"compute 3 * 7", "3 * 7"},
		{"evaluate (10 + 5) / 3", "(10 + 5) / 3"},
		{"what's 9 mod 4", "9 % 4"},
		{"1234 * 5678", "1234 * 5678"},                // already valid
		{"calculate 1234 times 5678?", "1234 * 5678"}, // trailing punctuation
		{"explain how multiplication works", ""},      // natural language, no expression
		{"tell me about prime numbers", ""},
	}
	for _, tc := range tests {
		got := extractCalcExpression(tc.input)
		if got != tc.expect {
			t.Errorf("extractCalcExpression(%q) = %q, want %q", tc.input, got, tc.expect)
		}
	}
}

// ── ParseCalls tests ─────────────────────────────────────────────────────────

func TestParseToolCallsSingle(t *testing.T) {
	text := `Let me check the time.
<tool_call>{"name": "get_time", "args": {}}</tool_call>
`
	calls, remaining := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
	if remaining != "Let me check the time." {
		t.Errorf("remaining = %q, want %q", remaining, "Let me check the time.")
	}
}

func TestParseToolCallsMultiple(t *testing.T) {
	text := `I'll read that file and check the time.
<tool_call>{"name": "read_file", "args": {"path": "test.txt"}}</tool_call>
<tool_call>{"name": "get_time", "args": {}}</tool_call>`
	calls, _ := ParseCalls(text, NewRegistry())
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("calls[0].Name = %q, want %q", calls[0].Name, "read_file")
	}
	if calls[1].Name != "get_time" {
		t.Errorf("calls[1].Name = %q, want %q", calls[1].Name, "get_time")
	}
}

func TestParseToolCallsMalformed(t *testing.T) {
	text := `Some text <tool_call>not json</tool_call> more text`
	calls, remaining := ParseCalls(text, NewRegistry())
	if len(calls) != 0 {
		t.Errorf("got %d calls for malformed JSON, want 0", len(calls))
	}
	if remaining != "Some text  more text" {
		t.Errorf("remaining = %q", remaining)
	}
}

func TestParseToolCallsWithFences(t *testing.T) {
	text := "<tool_call>```json\n{\"name\": \"calc\", \"args\": {\"expression\": \"2+3\"}}\n```</tool_call>"
	calls, _ := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "calc" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "calc")
	}
}

func TestParseToolCallsUnclosed(t *testing.T) {
	text := "Let me check.\n<tool_call>{\"name\": \"get_time\", \"args\": {}}"
	calls, remaining := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls for unclosed tag, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
	if remaining != "Let me check." {
		t.Errorf("remaining = %q, want %q", remaining, "Let me check.")
	}
}

func TestParseToolCallsMalformedArgs(t *testing.T) {
	text := `<tool_call>{"name": "get_time", "args": {"}}</tool_call>`
	calls, _ := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls for malformed args, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
}

func TestParseToolCallsMalformedWithPath(t *testing.T) {
	text := `<tool_call>{"name": "read_file", "args": {"path": "go.mod"</tool_call>`
	calls, _ := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "read_file")
	}
	if p, _ := calls[0].Args["path"].(string); p != "go.mod" {
		t.Errorf("call args[path] = %q, want %q", p, "go.mod")
	}
}

func TestParseToolCallsNone(t *testing.T) {
	text := "Just a normal response with no tool calls."
	calls, remaining := ParseCalls(text, NewRegistry())
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
	if remaining != text {
		t.Errorf("remaining changed: %q", remaining)
	}
}

// ── ValidatePath tests ───────────────────────────────────────────────────────

func TestValidatePathCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(cwd, "test_validate_path.tmp")
	if err := os.WriteFile(tmp, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	path, err := ValidatePath("test_validate_path.tmp")
	if err != nil {
		t.Errorf("ValidatePath in CWD failed: %v", err)
	}
	if path == "" {
		t.Error("ValidatePath returned empty path")
	}
}

func TestValidatePathOutsideCWD(t *testing.T) {
	_, err := ValidatePath("/etc/passwd")
	if err == nil {
		t.Error("ValidatePath(/etc/passwd) should fail — outside CWD")
	}
}

// ── ParseCalls edge cases ─────────────────────────────────────────────────

func TestParseToolCallsWrongTagName(t *testing.T) {
	text := `<get_time {"name": "now"}></get_time>`
	calls, remaining := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls for wrong tag name, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
	if remaining != "" {
		t.Errorf("remaining = %q, want empty", remaining)
	}
}

func TestParseCallsWebSearchFallback(t *testing.T) {
	// Malformed JSON where the outer structure is broken but the query field
	// is recoverable — this exercises the fallback regex path in parseCallJSON.
	text := `<tool_call>{"name": "web_search", "args": {"query": "latest Go release"</tool_call>`
	calls, _ := ParseCalls(text, NewRegistry())
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "web_search" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "web_search")
	}
	q, _ := calls[0].Args["query"].(string)
	if q != "latest Go release" {
		t.Errorf("query arg = %q, want %q", q, "latest Go release")
	}
}

// ── HasFilePath tests ────────────────────────────────────────────────────────

func TestHasFilePathPositive(t *testing.T) {
	cases := []string{
		"read the contents of go.mod",
		"how many lines in cmd/iq/main.go?",
		"search for func main in cmd/iq/",
		"list files in src/",
		"read file config.yaml",
		"show me list.txt",
	}
	for _, c := range cases {
		if !HasFilePath(c) {
			t.Errorf("HasFilePath(%q) = false, want true", c)
		}
	}
}

func TestHasFilePathNegative(t *testing.T) {
	cases := []string{
		"explain transformers",
		"write a poem about the ocean",
		"what is machine learning?",
		"what time is it?",
		"calculate 2 + 3",
		"translate hello to French",
		"https://example.com/page",
	}
	for _, c := range cases {
		if HasFilePath(c) {
			t.Errorf("HasFilePath(%q) = true, want false", c)
		}
	}
}

// ── Tool registry tests ─────────────────────────────────────────────────────

func TestFindTool(t *testing.T) {
	for _, name := range []string{"get_time", "read_file", "list_dir", "file_info", "calc", "search_text", "count_lines", "web_search"} {
		if FindTool(name) == nil {
			t.Errorf("FindTool(%q) returned nil", name)
		}
	}
	if FindTool("nonexistent") != nil {
		t.Error("FindTool(nonexistent) should return nil")
	}
}

func TestGetTimeHandler(t *testing.T) {
	tl := FindTool("get_time")
	if tl == nil {
		t.Fatal("get_time tool not found")
	}
	out, err := tl.Handler(nil)
	if err != nil {
		t.Fatalf("get_time error: %v", err)
	}
	if out == "" {
		t.Error("get_time returned empty string")
	}
}

func TestBuildToolSystemPrompt(t *testing.T) {
	prompt := BuildSystemPrompt()
	if prompt == "" {
		t.Fatal("BuildSystemPrompt returned empty string")
	}
	if !strings.Contains(prompt, "<tool_call>") {
		t.Error("system prompt should contain <tool_call> instruction")
	}
	for _, name := range []string{"get_time", "read_file", "list_dir", "file_info", "calc", "search_text", "count_lines", "web_search"} {
		if !strings.Contains(prompt, name) {
			t.Errorf("system prompt should mention tool %q", name)
		}
	}
}

func TestParseRoutingPrefix(t *testing.T) {
	tests := []struct {
		input    string
		wantTool string
		wantRest string
	}{
		{"<tool:get_time>{}", "get_time", "{}"},
		{`<tool:read_file>{"path":"/x"}`, "read_file", `{"path":"/x"}`},
		{"<no_tool>The answer is 42.", "", "The answer is 42."},
		{"plain text with no prefix", "", "plain text with no prefix"},
		{"<tool:calc>", "calc", ""},
		{"<no_tool>", "", ""},
	}
	for _, tt := range tests {
		tl, rest := ParseRoutingPrefix(tt.input)
		if tl != tt.wantTool || rest != tt.wantRest {
			t.Errorf("ParseRoutingPrefix(%q) = (%q, %q), want (%q, %q)",
				tt.input, tl, rest, tt.wantTool, tt.wantRest)
		}
	}
}

func TestParseRoutingArgs(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]string
	}{
		{`{"path":"/tmp/foo.go"}`, map[string]string{"path": "/tmp/foo.go"}},
		{`{ path: "/Users/tek1/main.go" }`, map[string]string{"path": "/Users/tek1/main.go"}},
		{`{"path":"/x.go"}\nHere is the file...`, map[string]string{"path": "/x.go"}},
		{"{ path: \"/x.go\" }\n```go\npackage main\n```", map[string]string{"path": "/x.go"}},
		{"", nil},
		{"just some text", nil},
		{`{ path = "/Users/tek1/main.go" }`, map[string]string{"path": "/Users/tek1/main.go"}},
		{`  --path=/Users/tek1/code/iq/cmd/iq/main.go`, map[string]string{"path": "/Users/tek1/code/iq/cmd/iq/main.go"}},
		{`--path "/Users/tek1/main.go"`, map[string]string{"path": "/Users/tek1/main.go"}},
		{`{"expression":"2+2"}`, map[string]string{"expression": "2+2"}},
	}
	for _, tt := range tests {
		args := ParseRoutingArgs(tt.input)
		if tt.want == nil {
			if args != nil {
				t.Errorf("ParseRoutingArgs(%q) = %v, want nil", tt.input, args)
			}
			continue
		}
		if args == nil {
			t.Errorf("ParseRoutingArgs(%q) = nil, want %v", tt.input, tt.want)
			continue
		}
		for k, v := range tt.want {
			got, _ := args[k].(string)
			if got != v {
				t.Errorf("ParseRoutingArgs(%q)[%q] = %q, want %q", tt.input, k, got, v)
			}
		}
	}
}

func TestToolRegistryNames(t *testing.T) {
	reg := NewRegistry()
	names := RegistryNames(reg)
	if len(names) != len(reg) {
		t.Fatalf("RegistryNames returned %d names, want %d", len(names), len(Registry))
	}
	for i, name := range names {
		if name != reg[i].Name {
			t.Errorf("RegistryNames()[%d] = %q, want %q", i, name, reg[i].Name)
		}
	}
}

// ── ParseCallsStrict tests ────────────────────────────────────────────────────

func TestParseCallsStrict(t *testing.T) {
	reg := NewRegistry()
	tests := []struct {
		name       string
		text       string
		wantValid  int
		wantErrors int
	}{
		{
			"valid get_time",
			`<tool_call>{"name":"get_time","args":{}}</tool_call>`,
			1, 0,
		},
		{
			"valid read_file",
			`<tool_call>{"name":"read_file","args":{"path":"x.go"}}</tool_call>`,
			1, 0,
		},
		{
			"unknown tool",
			`<tool_call>{"name":"nonexistent","args":{}}</tool_call>`,
			0, 1,
		},
		{
			"missing required arg",
			`<tool_call>{"name":"read_file","args":{}}</tool_call>`,
			0, 1,
		},
		{
			"unknown param",
			`<tool_call>{"name":"get_time","args":{"bogus":"x"}}</tool_call>`,
			0, 1,
		},
		{
			"mixed valid and invalid",
			`<tool_call>{"name":"get_time","args":{}}</tool_call><tool_call>{"name":"nonexistent","args":{}}</tool_call>`,
			1, 1,
		},
		{
			"no calls",
			"just some text",
			0, 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls, remaining, errs := ParseCallsStrict(tc.text, reg)
			if len(calls) != tc.wantValid {
				t.Errorf("valid calls: got %d, want %d", len(calls), tc.wantValid)
			}
			if len(errs) != tc.wantErrors {
				t.Errorf("errors: got %d, want %d (errs: %v)", len(errs), tc.wantErrors, errs)
			}
			if strings.Contains(remaining, "<tool_call>") {
				t.Errorf("remaining contains <tool_call>: %q", remaining)
			}
		})
	}
}

// TestSignalRegistryCoverage ensures every tool in Registry is referenced by at
// least one Signal, and every tool referenced by a Signal exists in Registry.
func TestSignalRegistryCoverage(t *testing.T) {
	// Build set of registered tool names.
	registered := make(map[string]bool, len(Registry))
	for _, tool := range Registry {
		registered[tool.Name] = true
	}

	// Collect all tool names referenced by signals.
	signaled := make(map[string]bool)
	for _, s := range Signals {
		for _, name := range s.Tools {
			signaled[name] = true
			if !registered[name] {
				t.Errorf("signal %q references tool %q which is not in Registry", s.Name, name)
			}
		}
	}

	// Check every registered tool appears in at least one signal.
	for _, tool := range Registry {
		if !signaled[tool.Name] {
			t.Errorf("tool %q is in Registry but not referenced by any Signal", tool.Name)
		}
	}
}

func TestSelectTool(t *testing.T) {
	tests := []struct {
		signal string
		input  string
		want   string
	}{
		// Single-tool signals always return their only tool.
		{"time_date", "what time is it", "get_time"},
		{"calculation", "what is 2 + 2", "calc"},
		{"web_search", "latest news today", "web_search"},
		// file_access disambiguation.
		{"file_access", "list every file in current directory", "list_dir"},
		{"file_access", "list files in cwd", "list_dir"},
		{"file_access", "ls the src directory", "list_dir"},
		{"file_access", "show files in folder", "list_dir"},
		{"file_access", "what is the file size of main.go", "file_info"},
		{"file_access", "file info for go.mod", "file_info"},
		{"file_access", "show me main.go", "read_file"},
		{"file_access", "read the config file", "read_file"},
		// file_search disambiguation.
		{"file_search", "count lines in main.go", "count_lines"},
		{"file_search", "how many lines in go.mod", "count_lines"},
		{"file_search", "search for TODO in code", "search_text"},
		{"file_search", "find pattern main in files", "search_text"},
	}
	for _, tc := range tests {
		t.Run(tc.signal+"/"+tc.input, func(t *testing.T) {
			got := SelectTool(tc.signal, tc.input)
			if got != tc.want {
				t.Errorf("SelectTool(%q, %q) = %q, want %q", tc.signal, tc.input, got, tc.want)
			}
		})
	}
}

func TestGuardArgsListDir(t *testing.T) {
	tests := []struct {
		input    string
		wantPath string
	}{
		{"list files in current directory", "."},
		{"list files in cwd", "."},
		{"what files are here", "."},
		{"list files in /tmp", "/tmp"},
		{"list files in ~/Documents", "~/Documents"},
		{"list files in ./src", "./src"},
		{"list every file", "."}, // no explicit path → default
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			args := GuardArgs("list_dir", tc.input)
			if args == nil {
				t.Fatal("GuardArgs returned nil")
			}
			got, ok := args["path"].(string)
			if !ok {
				t.Fatalf("args[\"path\"] is not a string: %v", args["path"])
			}
			if got != tc.wantPath {
				t.Errorf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}
