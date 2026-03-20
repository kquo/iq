package main

import (
	"strings"
	"testing"

	"iq/internal/config"
	"iq/internal/kb"
)

// ── estimateTokens ────────────────────────────────────────────────────────────

func TestEstimateTokens(t *testing.T) {
	msgs := []config.Message{
		{Role: "system", Content: strings.Repeat("a", 400)}, // 100 tokens
		{Role: "user", Content: strings.Repeat("b", 800)},   // 200 tokens
	}
	got := estimateTokens(msgs)
	if got != 300 {
		t.Errorf("estimateTokens = %d, want 300", got)
	}
}

func TestEstimateTokensEmpty(t *testing.T) {
	if got := estimateTokens(nil); got != 0 {
		t.Errorf("estimateTokens(nil) = %d, want 0", got)
	}
}

// ── dropOldestSessionTurns ────────────────────────────────────────────────────

func TestDropOldestSessionTurns_Basic(t *testing.T) {
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old-user-1"},
		{Role: "assistant", Content: "old-asst-1"},
		{Role: "user", Content: "old-user-2"},
		{Role: "assistant", Content: "old-asst-2"},
		{Role: "user", Content: "current"},
	}
	got, dropped := dropOldestSessionTurns(msgs, 1)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if len(got) != 4 {
		t.Errorf("len = %d, want 4", len(got))
	}
	if got[0].Content != "sys" {
		t.Errorf("messages[0] should be system, got %q", got[0].Content)
	}
	if got[len(got)-1].Content != "current" {
		t.Errorf("last message should be current user input, got %q", got[len(got)-1].Content)
	}
	// oldest pair (old-user-1, old-asst-1) should be gone
	for _, m := range got {
		if m.Content == "old-user-1" || m.Content == "old-asst-1" {
			t.Errorf("oldest pair should have been dropped, still found %q", m.Content)
		}
	}
}

func TestDropOldestSessionTurns_TooFewMessages(t *testing.T) {
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "current"},
	}
	got, dropped := dropOldestSessionTurns(msgs, 1)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (nothing to drop)", dropped)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (unchanged)", len(got))
	}
}

func TestDropOldestSessionTurns_MultiDrop(t *testing.T) {
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "current"},
	}
	got, dropped := dropOldestSessionTurns(msgs, 2)
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (system + current)", len(got))
	}
	if got[0].Content != "sys" || got[1].Content != "current" {
		t.Errorf("remaining messages wrong: %v", got)
	}
}

// ── trimToContextBudget ───────────────────────────────────────────────────────

func TestTrimContextBudget_NoTrim(t *testing.T) {
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
	}
	out, _, ctrim := trimToContextBudget(msgs, nil, "hi", 8192, 1024)
	if ctrim.KBChunksDropped != 0 || ctrim.SessionTurnsDropped != 0 {
		t.Errorf("expected no trim, got %+v", ctrim)
	}
	if len(out) != 2 {
		t.Errorf("messages unchanged, got len=%d", len(out))
	}
}

func TestTrimContextBudget_DisabledWhenZero(t *testing.T) {
	// contextWindow=0 → trimming disabled even for huge content.
	big := strings.Repeat("x", 100000)
	msgs := []config.Message{
		{Role: "system", Content: big},
		{Role: "user", Content: big},
	}
	out, _, ctrim := trimToContextBudget(msgs, nil, big, 0, 1024)
	if ctrim.KBChunksDropped != 0 || ctrim.SessionTurnsDropped != 0 {
		t.Errorf("expected no trim when contextWindow=0, got %+v", ctrim)
	}
	if len(out) != 2 {
		t.Errorf("messages should be unchanged")
	}
}

func TestTrimContextBudget_KBChunksDrop(t *testing.T) {
	// Each chunk is 400 chars = 100 tokens.
	chunkText := strings.Repeat("k", 400)
	chunks := []kb.Result{
		{Chunk: kb.Chunk{Text: chunkText, Source: "a.md"}},
		{Chunk: kb.Chunk{Text: chunkText, Source: "b.md"}},
		{Chunk: kb.Chunk{Text: chunkText, Source: "c.md"}},
	}
	kbCtx := kb.Context(chunks)
	input := "question?"
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: kbCtx + "\n\n" + input},
	}
	// contextWindow=512 - maxTokens=256 → budget=256 tokens
	// 3 chunks ≈ 300 tokens of KB + system + input → over budget
	out, outChunks, ctrim := trimToContextBudget(msgs, chunks, input, 512, 256)
	if ctrim.KBChunksDropped == 0 {
		t.Error("expected KB chunks to be dropped")
	}
	if len(outChunks) >= len(chunks) {
		t.Errorf("kbResults should have fewer chunks: got %d, had %d", len(outChunks), len(chunks))
	}
	// system prompt must survive
	if out[0].Content != "sys" {
		t.Errorf("system prompt was modified")
	}
	// user input must still be in last message
	if !strings.HasSuffix(out[len(out)-1].Content, input) {
		t.Errorf("user input missing from last message: %q", out[len(out)-1].Content)
	}
}

func TestTrimContextBudget_SessionTurnsDrop(t *testing.T) {
	// Build a session with many turns, each turn = 1000 chars ≈ 250 tokens.
	turn := strings.Repeat("t", 1000)
	msgs := []config.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: turn},
		{Role: "assistant", Content: turn},
		{Role: "user", Content: turn},
		{Role: "assistant", Content: turn},
		{Role: "user", Content: "current"},
	}
	// contextWindow=1000, maxTokens=256 → budget=744 tokens
	// 5 turns × 250 ≈ 1250 > 744 → trim needed
	out, _, ctrim := trimToContextBudget(msgs, nil, "current", 1000, 256)
	if ctrim.SessionTurnsDropped == 0 {
		t.Error("expected session turns to be dropped")
	}
	// system must survive
	if out[0].Content != "sys" {
		t.Errorf("system prompt was modified")
	}
	// current user input must survive
	if out[len(out)-1].Content != "current" {
		t.Errorf("current user input was removed")
	}
}

// ── trimWarning ───────────────────────────────────────────────────────────────

func TestTrimWarning_BothDropped(t *testing.T) {
	w := trimWarning(contextTrim{KBChunksDropped: 2, SessionTurnsDropped: 3}, 8192)
	if !strings.Contains(w, "2 KB chunk(s)") || !strings.Contains(w, "3 session turn(s)") {
		t.Errorf("unexpected warning: %q", w)
	}
	if !strings.Contains(w, "8192") {
		t.Errorf("warning should mention context window size: %q", w)
	}
}

func TestTrimWarning_NothingDropped(t *testing.T) {
	w := trimWarning(contextTrim{}, 8192)
	if w != "" {
		t.Errorf("expected empty warning, got %q", w)
	}
}

func TestTrimWarning_OnlyKB(t *testing.T) {
	w := trimWarning(contextTrim{KBChunksDropped: 1}, 4096)
	if strings.Contains(w, "session") {
		t.Errorf("should not mention session turns when none dropped: %q", w)
	}
}
