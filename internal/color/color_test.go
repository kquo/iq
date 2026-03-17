package color

import (
	"strings"
	"testing"
)

// In test environments stdout is not a TTY, so enabled == false and all
// color functions return the input string unchanged. The tests below verify
// both the no-color path (input preserved) and the wrap helper directly.

func TestColorFunctionsContainInput(t *testing.T) {
	cases := []struct {
		name string
		fn   func(any) string
	}{
		{"Gra", Gra},
		{"Grn", Grn},
		{"GrnR", GrnR},
		{"Yel", Yel},
		{"Blu", Blu},
		{"Cya", Cya},
		{"Red", Red},
		{"Whi", Whi},
		{"Whi2", Whi2},
	}
	for _, tc := range cases {
		got := tc.fn("hello")
		if !strings.Contains(got, "hello") {
			t.Errorf("%s(%q) = %q, does not contain input", tc.name, "hello", got)
		}
		if got == "" {
			t.Errorf("%s(%q) returned empty string", tc.name, "hello")
		}
	}
}

func TestColorFunctionsNoTTY(t *testing.T) {
	// In test environment, enabled is false (stdout is not a char device).
	// Functions must return the bare input string.
	if enabled {
		t.Skip("TTY detected — skipping no-color path test")
	}
	cases := []struct {
		name string
		fn   func(any) string
	}{
		{"Gra", Gra},
		{"Grn", Grn},
		{"GrnR", GrnR},
		{"Yel", Yel},
		{"Blu", Blu},
		{"Cya", Cya},
		{"Red", Red},
		{"Whi", Whi},
		{"Whi2", Whi2},
	}
	for _, tc := range cases {
		got := tc.fn("test")
		if got != "test" {
			t.Errorf("%s(%q) = %q, want %q (no-TTY path)", tc.name, "test", got, "test")
		}
	}
}

func TestWrapEmptyString(t *testing.T) {
	// wrap with an empty input should not panic regardless of TTY state.
	_ = wrap("32", "")
}
