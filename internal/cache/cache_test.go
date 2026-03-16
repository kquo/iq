package cache

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// setHome redirects config.Dir() to a temp directory for test isolation.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.config/iq", 0755); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestKey(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hello"}}
	k1 := Key(msgs, "model-a")
	k2 := Key(msgs, "model-a")
	if k1 != k2 {
		t.Errorf("Key is not deterministic: %q vs %q", k1, k2)
	}
	k3 := Key(msgs, "model-b")
	if k1 == k3 {
		t.Error("different models should produce different keys")
	}
	k4 := Key([]Message{{Role: "user", Content: "world"}}, "model-a")
	if k1 == k4 {
		t.Error("different messages should produce different keys")
	}
	if len(k1) != 16 {
		t.Errorf("Key length = %d, want 16 hex chars", len(k1))
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{61 * time.Minute, "1h1m"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
	}
	for _, tc := range cases {
		got := FormatAge(tc.d)
		if got != tc.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestCheckMiss(t *testing.T) {
	setHome(t)
	resp, hit := Check("nonexistent-key")
	if resp != "" || hit.Hit {
		t.Errorf("expected miss, got hit=%v resp=%q", hit.Hit, resp)
	}
}

func TestWriteAndCheck(t *testing.T) {
	setHome(t)
	msgs := []Message{{Role: "user", Content: "what is 2+2"}}
	key := Key(msgs, "test-model")

	Write(key, "four", "test-model", "math")

	resp, hit := Check(key)
	if !hit.Hit {
		t.Fatal("expected cache hit after Write")
	}
	if resp != "four" {
		t.Errorf("cached response = %q, want %q", resp, "four")
	}
	if hit.Model != "test-model" {
		t.Errorf("hit.Model = %q, want %q", hit.Model, "test-model")
	}
}

func TestCheckExpired(t *testing.T) {
	home := setHome(t)
	key := "expired-key"

	// Write a cache entry with a timestamp older than TTL.
	past := time.Now().Add(-(TTL + time.Minute)).UTC().Format(time.RFC3339)
	c := map[string]Entry{
		key: {Response: "stale", Model: "m", Cue: "c", Timestamp: past},
	}
	data, _ := json.Marshal(c)
	cachePath := home + "/.config/iq/response_cache.json"
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	resp, hit := Check(key)
	if hit.Hit || resp != "" {
		t.Errorf("expected expired entry to be a miss, got hit=%v resp=%q", hit.Hit, resp)
	}
}

func TestWritePrunesExpired(t *testing.T) {
	home := setHome(t)

	// Seed the cache with an expired entry.
	past := time.Now().Add(-(TTL + time.Minute)).UTC().Format(time.RFC3339)
	oldKey := "old-key"
	c := map[string]Entry{
		oldKey: {Response: "stale", Model: "m", Cue: "c", Timestamp: past},
	}
	data, _ := json.Marshal(c)
	cachePath := home + "/.config/iq/response_cache.json"
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Write a new entry — this should prune the expired one.
	Write("new-key", "fresh", "model", "cue")

	loaded, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := loaded[oldKey]; exists {
		t.Error("expired entry should have been pruned by Write")
	}
	if _, exists := loaded["new-key"]; !exists {
		t.Error("new entry should exist after Write")
	}
}
