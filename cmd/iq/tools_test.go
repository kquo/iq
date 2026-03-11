package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iq/internal/tools"
)

// ── ParseCalls tests ─────────────────────────────────────────────────────────

func TestParseToolCallsSingle(t *testing.T) {
	text := `Let me check the time.
<tool_call>{"name": "get_time", "args": {}}</tool_call>
`
	calls, remaining := tools.ParseCalls(text)
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
	calls, _ := tools.ParseCalls(text)
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
	calls, remaining := tools.ParseCalls(text)
	if len(calls) != 0 {
		t.Errorf("got %d calls for malformed JSON, want 0", len(calls))
	}
	if remaining != "Some text  more text" {
		t.Errorf("remaining = %q", remaining)
	}
}

func TestParseToolCallsWithFences(t *testing.T) {
	text := "<tool_call>```json\n{\"name\": \"calc\", \"args\": {\"expression\": \"2+3\"}}\n```</tool_call>"
	calls, _ := tools.ParseCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "calc" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "calc")
	}
}

func TestParseToolCallsUnclosed(t *testing.T) {
	text := "Let me check.\n<tool_call>{\"name\": \"get_time\", \"args\": {}}"
	calls, remaining := tools.ParseCalls(text)
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
	calls, _ := tools.ParseCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls for malformed args, want 1", len(calls))
	}
	if calls[0].Name != "get_time" {
		t.Errorf("call name = %q, want %q", calls[0].Name, "get_time")
	}
}

func TestParseToolCallsMalformedWithPath(t *testing.T) {
	text := `<tool_call>{"name": "read_file", "args": {"path": "go.mod"</tool_call>`
	calls, _ := tools.ParseCalls(text)
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
	calls, remaining := tools.ParseCalls(text)
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0", len(calls))
	}
	if remaining != text {
		t.Errorf("remaining changed: %q", remaining)
	}
}

// ── ValidatePath tests ───────────────────────────────────────────────────────

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

	path, err := tools.ValidatePath("test_validate_path.tmp")
	if err != nil {
		t.Errorf("ValidatePath in CWD failed: %v", err)
	}
	if path == "" {
		t.Error("ValidatePath returned empty path")
	}
}

func TestValidatePathOutsideCWD(t *testing.T) {
	_, err := tools.ValidatePath("/etc/passwd")
	if err == nil {
		t.Error("ValidatePath(/etc/passwd) should fail — outside CWD")
	}
}

// ── ParseCalls edge cases ─────────────────────────────────────────────────

func TestParseToolCallsWrongTagName(t *testing.T) {
	// Model used <get_time> instead of <tool_call>.
	text := `<get_time {"name": "now"}></get_time>`
	calls, remaining := tools.ParseCalls(text)
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
		if !tools.HasFilePath(c) {
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
		if tools.HasFilePath(c) {
			t.Errorf("HasFilePath(%q) = true, want false", c)
		}
	}
}

// ── Tool registry tests ─────────────────────────────────────────────────────

func TestFindTool(t *testing.T) {
	for _, name := range []string{"get_time", "read_file", "list_dir", "file_info", "calc", "search_text", "count_lines", "web_search"} {
		if tools.FindTool(name) == nil {
			t.Errorf("FindTool(%q) returned nil", name)
		}
	}
	if tools.FindTool("nonexistent") != nil {
		t.Error("FindTool(nonexistent) should return nil")
	}
}

func TestGetTimeHandler(t *testing.T) {
	tl := tools.FindTool("get_time")
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
	prompt := tools.BuildSystemPrompt()
	if prompt == "" {
		t.Fatal("BuildSystemPrompt returned empty string")
	}
	// Should mention tool_call format.
	if !strings.Contains(prompt, "<tool_call>") {
		t.Error("system prompt should contain <tool_call> instruction")
	}
	// Should list all tools.
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
		tl, rest := tools.ParseRoutingPrefix(tt.input)
		if tl != tt.wantTool || rest != tt.wantRest {
			t.Errorf("ParseRoutingPrefix(%q) = (%q, %q), want (%q, %q)",
				tt.input, tl, rest, tt.wantTool, tt.wantRest)
		}
	}
}

func TestParseRoutingArgs(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]string // check these key=value pairs exist
	}{
		// Valid JSON.
		{`{"path":"/tmp/foo.go"}`, map[string]string{"path": "/tmp/foo.go"}},
		// Unquoted keys (common from small models).
		{`{ path: "/Users/tek1/main.go" }`, map[string]string{"path": "/Users/tek1/main.go"}},
		// JSON with trailing garbage.
		{`{"path":"/x.go"}\nHere is the file...`, map[string]string{"path": "/x.go"}},
		// Unquoted keys with trailing garbage.
		{"{ path: \"/x.go\" }\n```go\npackage main\n```", map[string]string{"path": "/x.go"}},
		// Empty / no JSON.
		{"", nil},
		{"just some text", nil},
		// Equals separator instead of colon.
		{`{ path = "/Users/tek1/main.go" }`, map[string]string{"path": "/Users/tek1/main.go"}},
		// CLI flag format.
		{`  --path=/Users/tek1/code/iq/cmd/iq/main.go`, map[string]string{"path": "/Users/tek1/code/iq/cmd/iq/main.go"}},
		// CLI flag with quoted value.
		{`--path "/Users/tek1/main.go"`, map[string]string{"path": "/Users/tek1/main.go"}},
		// Expression arg.
		{`{"expression":"2+2"}`, map[string]string{"expression": "2+2"}},
	}
	for _, tt := range tests {
		args := tools.ParseRoutingArgs(tt.input)
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
	names := tools.RegistryNames()
	if len(names) != len(tools.Registry) {
		t.Fatalf("RegistryNames returned %d names, want %d", len(names), len(tools.Registry))
	}
	for i, name := range names {
		if name != tools.Registry[i].Name {
			t.Errorf("RegistryNames()[%d] = %q, want %q", i, name, tools.Registry[i].Name)
		}
	}
}
