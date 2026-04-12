package color

import (
	"io"
	"os"
	"strings"
	"testing"
)

// In test environments stdout is not a TTY, so enabled == false and all
// color functions return the input string unchanged. The tests below verify
// both the no-color path (input preserved) and the wrap helper directly.
//
// No test in this file uses t.Parallel() because several tests mutate the
// package-level enabled and color256 variables. Running any test concurrently
// with those mutations would be a data race.

func TestColorFunctionsContainInput(t *testing.T) {
	cases := []struct {
		name string
		fn   func(any) string
	}{
		{"Gra", Gra},
		{"Grn", Grn},
		{"GrnR", GrnR},
		{"GrnD", GrnD},
		{"Yel", Yel},
		{"Blu", Blu},
		{"Cya", Cya},
		{"Red", Red},
		{"RedR", RedR},
		{"RedD", RedD},
		{"Whi", Whi},
		{"Whi2", Whi2},
		{"BoldW", BoldW},
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
		{"GrnD", GrnD},
		{"Yel", Yel},
		{"Blu", Blu},
		{"Cya", Cya},
		{"Red", Red},
		{"RedR", RedR},
		{"RedD", RedD},
		{"Whi", Whi},
		{"Whi2", Whi2},
		{"BoldW", BoldW},
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

// TestWrapProduces256ColorEscapes verifies wrap() emits the exact ANSI
// 256-color escape format. This test calls wrap() directly so results are
// deterministic regardless of TTY state.
func TestWrapProduces256ColorEscapes(t *testing.T) {
	origEnabled := enabled
	enabled = true
	defer func() { enabled = origEnabled }()

	got := wrap("38;5;2", "ok")
	want := "\033[38;5;2mok\033[0m"
	if got != want {
		t.Fatalf("wrap(\"38;5;2\", \"ok\") = %q, want %q", got, want)
	}
}

// TestColorFunctions256Codes verifies every color function uses the
// documented 256-color escape code when color256 is true.
func TestColorFunctions256Codes(t *testing.T) {
	origEnabled := enabled
	orig256 := color256
	enabled = true
	color256 = true
	defer func() { enabled = origEnabled; color256 = orig256 }()

	cases := []struct {
		name string
		fn   func(any) string
		code string // expected escape code between \033[ and m
	}{
		{"Gra", Gra, "38;5;246"},
		{"Grn", Grn, "38;5;2"},
		{"GrnR", GrnR, "7;38;5;2"},
		{"GrnD", GrnD, "38;5;28"},
		{"Yel", Yel, "38;5;3"},
		{"Blu", Blu, "38;5;12"},
		{"Cya", Cya, "38;5;6"},
		{"Red", Red, "38;5;9"},
		{"RedR", RedR, "38;5;15;48;5;1"},
		{"RedD", RedD, "38;5;124"},
		{"Whi", Whi, "38;5;7"},
		{"Whi2", Whi2, "38;5;15"},
		{"BoldW", BoldW, "1;38;5;15"},
	}
	for _, tc := range cases {
		got := tc.fn("x")
		wantPrefix := "\033[" + tc.code + "m"
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("%s: got %q, want prefix %q", tc.name, got, wantPrefix)
		}
		wantSuffix := "\033[0m"
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("%s: got %q, want suffix %q", tc.name, got, wantSuffix)
		}
	}
}

// TestColorFunctionsBasicCodes verifies every color function falls back to
// basic ANSI codes when color256 is false.
func TestColorFunctionsBasicCodes(t *testing.T) {
	origEnabled := enabled
	orig256 := color256
	enabled = true
	color256 = false
	defer func() { enabled = origEnabled; color256 = orig256 }()

	cases := []struct {
		name string
		fn   func(any) string
		code string
	}{
		{"Gra", Gra, "90"},
		{"Grn", Grn, "32"},
		{"GrnR", GrnR, "7;32"},
		{"GrnD", GrnD, "32"},
		{"Yel", Yel, "33"},
		{"Blu", Blu, "94"},
		{"Cya", Cya, "36"},
		{"Red", Red, "91"},
		{"RedR", RedR, "97;41"},
		{"RedD", RedD, "31"},
		{"Whi", Whi, "37"},
		{"Whi2", Whi2, "97"},
		{"BoldW", BoldW, "1;97"},
	}
	for _, tc := range cases {
		got := tc.fn("x")
		wantPrefix := "\033[" + tc.code + "m"
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("%s: got %q, want prefix %q", tc.name, got, wantPrefix)
		}
		wantSuffix := "\033[0m"
		if !strings.HasSuffix(got, wantSuffix) {
			t.Errorf("%s: got %q, want suffix %q", tc.name, got, wantSuffix)
		}
	}
}

// TestShowPaletteCoversAllFunctions captures ShowPalette output and verifies
// all 13 color function labels are present.
func TestShowPaletteCoversAllFunctions(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	ShowPalette()
	w.Close()
	os.Stdout = oldStdout

	buf, _ := io.ReadAll(r)
	output := string(buf)

	for _, label := range []string{
		"Gra", "Grn", "GrnR", "GrnD",
		"Yel", "Blu", "Cya",
		"Red", "RedR", "RedD",
		"Whi", "Whi2", "BoldW",
	} {
		if !strings.Contains(output, label) {
			t.Errorf("ShowPalette() output missing label %q", label)
		}
	}
}

// TestFormatUsage exercises heading, flag alignment, type-suffix rendering,
// long-flag overflow, and footer newline handling.
func TestFormatUsage(t *testing.T) {
	// Tests run with enabled=false, so color wrappers return plain text.

	t.Run("basic", func(t *testing.T) {
		got := FormatUsage("prog [flags]", []UsageLine{
			{"-v", "verbose output"},
			{"-o string", "output file"},
		}, "")
		if !strings.HasPrefix(got, "Usage: prog [flags]\n") {
			t.Errorf("heading mismatch: %q", got)
		}
		if !strings.Contains(got, "-v") || !strings.Contains(got, "verbose output") {
			t.Errorf("missing flag line: %q", got)
		}
		if !strings.Contains(got, "-o string") || !strings.Contains(got, "output file") {
			t.Errorf("missing type-suffix flag line: %q", got)
		}
		// No footer => no trailing blank line.
		if strings.HasSuffix(got, "\n\n") {
			t.Errorf("unexpected trailing blank line with empty footer: %q", got)
		}
	})

	t.Run("footer_no_newline", func(t *testing.T) {
		got := FormatUsage("prog", nil, "See docs.")
		if !strings.Contains(got, "\nSee docs.\n") {
			t.Errorf("footer missing or not newline-terminated: %q", got)
		}
	})

	t.Run("footer_with_newline", func(t *testing.T) {
		got := FormatUsage("prog", nil, "See docs.\n")
		// Should not double the trailing newline.
		if strings.HasSuffix(got, "docs.\n\n") {
			t.Errorf("footer double-newlined: %q", got)
		}
	})

	t.Run("long_flag", func(t *testing.T) {
		got := FormatUsage("prog", []UsageLine{
			{"--very-long-flag-name-that-exceeds-column string", "desc"},
		}, "")
		// Long flags get 2-space gap instead of padding to column 38.
		if !strings.Contains(got, "string  desc") {
			t.Errorf("long flag alignment wrong: %q", got)
		}
	})
}
