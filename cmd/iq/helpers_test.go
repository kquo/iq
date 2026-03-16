package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"iq/internal/config"
)

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncateCmd(t *testing.T) {
	// Short string — unchanged.
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want %q", got, "hello")
	}
	// Exactly at limit — unchanged (no ellipsis).
	if got := truncate("abcde", 5); got != "abcde" {
		t.Errorf("truncate exact = %q, want %q", got, "abcde")
	}
	// Over limit — truncated with trailing "…".
	got := truncate("abcdef", 3)
	if got != "abc…" {
		t.Errorf("truncate over = %q, want %q", got, "abc…")
	}
	// Multi-byte runes — truncates on rune boundary.
	got2 := truncate("日本語テスト", 3)
	if got2 != "日本語…" {
		t.Errorf("truncate multibyte = %q, want %q", got2, "日本語…")
	}
	// Empty string.
	if got := truncate("", 5); got != "" {
		t.Errorf("truncate empty = %q, want %q", got, "")
	}
}

// ── shortID ───────────────────────────────────────────────────────────────────

func TestShortID(t *testing.T) {
	id := shortID()
	// Should be 8 hex characters.
	if len(id) != 8 {
		t.Errorf("shortID len = %d, want 8 (got %q)", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("shortID contains non-hex char %q in %q", c, id)
		}
	}
	// Two successive calls should (very likely) differ.
	id2 := shortID()
	if id == id2 {
		t.Logf("shortID: two successive calls returned the same ID %q (possible but unlikely)", id)
	}
}

// ── newSession ────────────────────────────────────────────────────────────────

func TestNewSession(t *testing.T) {
	s := newSession("my-cue", "fast")
	if s.Cue != "my-cue" {
		t.Errorf("newSession.Cue = %q, want %q", s.Cue, "my-cue")
	}
	if s.Tier != "fast" {
		t.Errorf("newSession.Tier = %q, want %q", s.Tier, "fast")
	}
	if s.ID == "" {
		t.Error("newSession.ID is empty")
	}
	if s.Created == "" {
		t.Error("newSession.Created is empty")
	}
	if s.Updated == "" {
		t.Error("newSession.Updated is empty")
	}
}

// ── webSearchSynthPrompt ──────────────────────────────────────────────────────

func TestWebSearchSynthPrompt(t *testing.T) {
	got := webSearchSynthPrompt()
	// Must contain today's date in the expected format.
	today := time.Now().Format("January 2, 2006")
	if !strings.Contains(got, today) {
		t.Errorf("webSearchSynthPrompt: missing today's date %q in output %q", today, got)
	}
	// Must instruct the model to answer concisely.
	if !strings.Contains(got, "1-2 sentences") {
		t.Errorf("webSearchSynthPrompt: missing conciseness instruction, got %q", got)
	}
}

// ── runDocCheck ───────────────────────────────────────────────────────────────

func TestRunDocCheck(t *testing.T) {
	c := runDocCheck("test-label", "detail text", true, false)
	if c.label != "test-label" || c.detail != "detail text" || !c.ok || c.warn {
		t.Errorf("runDocCheck = %+v, unexpected fields", c)
	}
	c2 := runDocCheck("warn-label", "warn detail", false, true)
	if c2.ok || !c2.warn {
		t.Errorf("runDocCheck warn = %+v, expected ok=false warn=true", c2)
	}
}

// ── saveSession / loadSession round-trip ─────────────────────────────────────

func TestSessionRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := &session{
		ID:      "test-roundtrip",
		Name:    "my session",
		Cue:     "general",
		Tier:    "fast",
		Created: time.Now().UTC().Format(time.RFC3339),
		Messages: []config.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	if err := saveSession(s); err != nil {
		t.Fatalf("saveSession error: %v", err)
	}

	got, err := loadSession(s.ID)
	if err != nil {
		t.Fatalf("loadSession error: %v", err)
	}
	if got == nil {
		t.Fatal("loadSession returned nil")
	}
	if got.ID != s.ID || got.Cue != s.Cue || got.Tier != s.Tier {
		t.Errorf("round-trip mismatch: got %+v, want ID=%s Cue=%s Tier=%s",
			got, s.ID, s.Cue, s.Tier)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "hello" {
		t.Errorf("round-trip messages mismatch: got %+v", got.Messages)
	}
}

func TestLoadSessionMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Non-existent session — loadSession should return (nil, nil).
	got, err := loadSession("does-not-exist")
	if err != nil {
		t.Fatalf("loadSession(missing) error: %v", err)
	}
	if got != nil {
		t.Errorf("loadSession(missing) = %+v, want nil", got)
	}
}

// ── resolveModels ─────────────────────────────────────────────────────────────

// writeCfgYAML writes a minimal config.yaml with the given tier models.
func writeCfgYAML(t *testing.T, home, fastModel, slowModel string) {
	t.Helper()
	cfgDir := filepath.Join(home, ".config", "iq")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	fast := "[]"
	if fastModel != "" {
		fast = fmt.Sprintf("\n      - %s", fastModel)
	}
	slow := "[]"
	if slowModel != "" {
		slow = fmt.Sprintf("\n      - %s", slowModel)
	}
	yaml := fmt.Sprintf("version: 1\ntiers:\n  fast:\n    models: %s\n  slow:\n    models: %s\n", fast, slow)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0644)
}

func TestResolveModels(t *testing.T) {
	t.Run("tier name fast", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeCfgYAML(t, home, "org/fast-model", "")
		got, err := resolveModels("fast")
		if err != nil {
			t.Fatalf("resolveModels(fast): %v", err)
		}
		if len(got) != 1 || got[0] != "org/fast-model" {
			t.Errorf("resolveModels(fast) = %v, want [org/fast-model]", got)
		}
	})

	t.Run("tier name empty", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeCfgYAML(t, home, "", "")
		_, err := resolveModels("fast")
		if err == nil || !strings.Contains(err.Error(), "no models") {
			t.Errorf("resolveModels(empty tier): want 'no models' error, got %v", err)
		}
	})

	t.Run("model ID", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeCfgYAML(t, home, "org/my-model", "")
		got, err := resolveModels("org/my-model")
		if err != nil {
			t.Fatalf("resolveModels(model ID): %v", err)
		}
		if len(got) != 1 || got[0] != "org/my-model" {
			t.Errorf("resolveModels(model ID) = %v, want [org/my-model]", got)
		}
	})

	t.Run("unknown arg", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeCfgYAML(t, home, "org/fast-model", "")
		_, err := resolveModels("not-a-tier-or-model")
		if err == nil {
			t.Error("resolveModels(unknown): expected error, got nil")
		}
	})
}

// ── checkCommand ──────────────────────────────────────────────────────────────

func TestCheckCommandKnownBinary(t *testing.T) {
	// "echo" and "ls" are always present on macOS/Linux.
	path, _ := checkCommand("ls", "")
	if path == "" {
		t.Skip("ls not found in PATH — skipping checkCommand test")
	}
	if !strings.Contains(path, "ls") {
		t.Errorf("checkCommand(ls) path = %q, want something containing 'ls'", path)
	}
}

func TestCheckCommandMissing(t *testing.T) {
	path, version := checkCommand("definitely-not-a-real-binary-xyz123", "")
	if path != "" || version != "" {
		t.Errorf("checkCommand(missing) = (%q, %q), want (\"\", \"\")", path, version)
	}
}

// ── argsUsage ─────────────────────────────────────────────────────────────────

func TestArgsUsageError(t *testing.T) {
	// argsUsage wraps a validator; when the inner validator fails it prints a
	// yellow error message and returns errSilent.
	wrapped := argsUsage(cobra.ExactArgs(2))
	cmd := &cobra.Command{}
	// Discard help output — we only care about the error type.
	devNull, _ := os.Open(os.DevNull)
	old := os.Stdout
	os.Stdout = devNull
	err := wrapped(cmd, []string{"only-one-arg"})
	os.Stdout = old
	devNull.Close()

	if err == nil {
		t.Fatal("argsUsage: expected error for wrong arg count, got nil")
	}
	// Should return errSilent (already printed).
	if !isErrSilent(err) {
		t.Errorf("argsUsage error = %v (%T), want errSilent", err, err)
	}
}

// isErrSilent checks whether err is the package-level errSilent value.
func isErrSilent(err error) bool {
	_, ok := err.(silentErr)
	return ok
}

func TestArgsUsageNoError(t *testing.T) {
	wrapped := argsUsage(cobra.ExactArgs(1))
	cmd := &cobra.Command{}
	if err := wrapped(cmd, []string{"one-arg"}); err != nil {
		t.Errorf("argsUsage: unexpected error for correct arg count: %v", err)
	}
}
