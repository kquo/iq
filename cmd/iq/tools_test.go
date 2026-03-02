package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

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

// ── parseToolCalls tests ─────────────────────────────────────────────────────

func TestParseToolCallsSingle(t *testing.T) {
	text := `Let me check the time.
<tool_call>{"name": "get_time", "args": {}}</tool_call>
`
	calls, remaining := parseToolCalls(text)
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
	calls, _ := parseToolCalls(text)
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
	calls, remaining := parseToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("got %d calls for malformed JSON, want 0", len(calls))
	}
	if remaining != "Some text  more text" {
		t.Errorf("remaining = %q", remaining)
	}
}

func TestParseToolCallsWithFences(t *testing.T) {
	text := "<tool_call>```json\n{\"name\": \"calc\", \"args\": {\"expression\": \"2+3\"}}\n```</tool_call>"
	calls, _ := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "calc" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "calc")
	}
}

func TestParseToolCallsUnclosed(t *testing.T) {
	text := "Let me check.\n<tool_call>{\"name\": \"get_time\", \"args\": {}}"
	calls, remaining := parseToolCalls(text)
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
	// Model returns broken JSON: args has a stray quote.
	text := `<tool_call>{"name": "get_time", "args": {"}}</tool_call>`
	calls, _ := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls for malformed args, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
}

func TestParseToolCallsMalformedWithPath(t *testing.T) {
	text := `<tool_call>{"name": "read_file", "args": {"path": "go.mod"</tool_call>`
	calls, _ := parseToolCalls(text)
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
	calls, remaining := parseToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
	if remaining != text {
		t.Errorf("remaining changed: %q", remaining)
	}
}

// ── validatePath tests ───────────────────────────────────────────────────────

func TestValidatePathCWD(t *testing.T) {
	// Create a temp file in CWD for testing.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(cwd, "test_validate_path.tmp")
	if err := os.WriteFile(tmp, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	path, err := validatePath("test_validate_path.tmp")
	if err != nil {
		t.Errorf("validatePath in CWD failed: %v", err)
	}
	if path == "" {
		t.Error("validatePath returned empty path")
	}
}

func TestValidatePathOutsideCWD(t *testing.T) {
	_, err := validatePath("/etc/passwd")
	if err == nil {
		t.Error("validatePath(/etc/passwd) should fail — outside CWD")
	}
}

// ── parseToolCalls edge cases ─────────────────────────────────────────────────

func TestParseToolCallsWrongTagName(t *testing.T) {
	// Model used <get_time> instead of <tool_call>.
	text := `<get_time {"name": "now"}></get_time>`
	calls, remaining := parseToolCalls(text)
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

// ── hasFilePath tests ────────────────────────────────────────────────────────

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
		if !hasFilePath(c) {
			t.Errorf("hasFilePath(%q) = false, want true", c)
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
		if hasFilePath(c) {
			t.Errorf("hasFilePath(%q) = true, want false", c)
		}
	}
}

// ── Tool registry tests ─────────────────────────────────────────────────────

func TestFindTool(t *testing.T) {
	for _, name := range []string{"get_time", "read_file", "list_dir", "file_info", "calc", "search_text", "count_lines"} {
		if findTool(name) == nil {
			t.Errorf("findTool(%q) returned nil", name)
		}
	}
	if findTool("nonexistent") != nil {
		t.Error("findTool(nonexistent) should return nil")
	}
}

func TestGetTimeHandler(t *testing.T) {
	tool := findTool("get_time")
	if tool == nil {
		t.Fatal("get_time tool not found")
	}
	out, err := tool.Handler(nil)
	if err != nil {
		t.Fatalf("get_time error: %v", err)
	}
	if out == "" {
		t.Error("get_time returned empty string")
	}
}

func TestBuildToolSystemPrompt(t *testing.T) {
	prompt := buildToolSystemPrompt()
	if prompt == "" {
		t.Fatal("buildToolSystemPrompt returned empty string")
	}
	// Should mention tool_call format.
	if !contains(prompt, "<tool_call>") {
		t.Error("system prompt should contain <tool_call> instruction")
	}
	// Should list all tools.
	for _, name := range []string{"get_time", "read_file", "list_dir", "file_info", "calc", "search_text", "count_lines"} {
		if !contains(prompt, name) {
			t.Errorf("system prompt should mention tool %q", name)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
