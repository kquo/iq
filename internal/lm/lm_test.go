package lm

import (
	"os"
	"path/filepath"
	"testing"
)

// ── HFSibling / HFModel ───────────────────────────────────────────────────────

func TestHFSiblingFileSize(t *testing.T) {
	cases := []struct {
		s    HFSibling
		want int64
	}{
		{HFSibling{Size: 100, LFS: HFSiblingLFS{Size: 0}}, 100},
		{HFSibling{Size: 100, LFS: HFSiblingLFS{Size: 500}}, 500}, // LFS wins
		{HFSibling{Size: 0, LFS: HFSiblingLFS{Size: 0}}, 0},
	}
	for _, tc := range cases {
		if got := tc.s.FileSize(); got != tc.want {
			t.Errorf("FileSize() = %d, want %d (sibling=%+v)", got, tc.want, tc.s)
		}
	}
}

func TestHFModelTotalSize(t *testing.T) {
	// UsedStorage takes precedence.
	m := HFModel{UsedStorage: 1000, Siblings: []HFSibling{{Size: 200}, {Size: 300}}}
	if got := m.TotalSize(); got != 1000 {
		t.Errorf("TotalSize() = %d, want 1000 (UsedStorage present)", got)
	}
	// Fall back to summing siblings.
	m2 := HFModel{Siblings: []HFSibling{{Size: 200}, {Size: 300}}}
	if got := m2.TotalSize(); got != 500 {
		t.Errorf("TotalSize() = %d, want 500 (sum of siblings)", got)
	}
}

// ── ParseParams ───────────────────────────────────────────────────────────────

func TestParseParams(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"mlx-community/gemma-3-1b-it-4bit", "1B"},
		{"mlx-community/smollm2-135m-instruct-4bit", "135M"},
		{"mlx-community/Qwen2.5-7B-Instruct-4bit", "7B"},
		{"mlx-community/llama-3.2-1b-4bit", "1B"},
		{"mlx-community/mistral-7b-v0.1-4bit", "7B"},
		{"mlx-community/unknown-model", "-"},
	}
	for _, tc := range cases {
		got := ParseParams(tc.id)
		if got != tc.want {
			t.Errorf("ParseParams(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// ── ParseQuant ────────────────────────────────────────────────────────────────

func TestParseQuant(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"mlx-community/gemma-3-1b-it-4bit", "4bit"},
		{"mlx-community/bge-small-en-v1.5-bf16", "bf16"},
		{"mlx-community/model-fp16", "fp16"},
		{"mlx-community/no-quant-info", "-"},
	}
	for _, tc := range cases {
		got := ParseQuant(tc.id)
		if got != tc.want {
			t.Errorf("ParseQuant(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// ── Commatize / FormatInt / FormatMB / EstMemMB / ParseParamsM ───────────────

func TestCommatize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
	}
	for _, tc := range cases {
		got := Commatize(tc.n)
		if got != tc.want {
			t.Errorf("Commatize(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFormatInt(t *testing.T) {
	if got := FormatInt(1000); got != "1,000" {
		t.Errorf("FormatInt(1000) = %q, want %q", got, "1,000")
	}
}

func TestFormatMB(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "-"},
		{1024 * 1024, "1MB"},
		{4 * 1024 * 1024 * 1024, "4,096MB"},
	}
	for _, tc := range cases {
		got := FormatMB(tc.b)
		if got != tc.want {
			t.Errorf("FormatMB(%d) = %q, want %q", tc.b, got, tc.want)
		}
	}
}

func TestEstMemMB(t *testing.T) {
	if got := EstMemMB(0); got != "-" {
		t.Errorf("EstMemMB(0) = %q, want \"-\"", got)
	}
	// 1GB disk → ~1536MB estimate
	got := EstMemMB(1024 * 1024 * 1024)
	if got == "-" || got == "" {
		t.Errorf("EstMemMB(1GB) = %q, want non-empty estimate", got)
	}
}

func TestParseParamsM(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"mlx-community/gemma-3-1b-it-4bit", "1,000M"},
		{"mlx-community/smollm2-135m-instruct-4bit", "135M"},
		{"mlx-community/unknown-model", "-"},
	}
	for _, tc := range cases {
		got := ParseParamsM(tc.id)
		if got != tc.want {
			t.Errorf("ParseParamsM(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// ── IsModelNotDownloaded ──────────────────────────────────────────────────────

func TestIsModelNotDownloaded(t *testing.T) {
	// nil is never a "not downloaded" error.
	if IsModelNotDownloaded(nil) {
		t.Error("expected false for nil error")
	}
	// SnapshotDir wraps missing dirs with "no snapshots found".
	_, err := SnapshotDir("org/definitely-not-a-real-model")
	if !IsModelNotDownloaded(err) {
		t.Errorf("expected true for missing model, got false (err=%v)", err)
	}
}

// ── DiskUsage ─────────────────────────────────────────────────────────────────

func TestDiskUsage(t *testing.T) {
	dir := t.TempDir()
	blobsDir := filepath.Join(dir, "blobs")
	if err := os.MkdirAll(blobsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write two files of known sizes.
	if err := os.WriteFile(filepath.Join(blobsDir, "a"), make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobsDir, "b"), make([]byte, 200), 0644); err != nil {
		t.Fatal(err)
	}

	got := DiskUsage(dir)
	if got != 300 {
		t.Errorf("DiskUsage() = %d, want 300", got)
	}
}

func TestDiskUsageNoBlobs(t *testing.T) {
	dir := t.TempDir()
	// No blobs/ subdirectory — should return 0 gracefully.
	got := DiskUsage(dir)
	if got != 0 {
		t.Errorf("DiskUsage(no blobs) = %d, want 0", got)
	}
}

// ── Manifest round-trip ───────────────────────────────────────────────────────

func TestManifestRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.config/iq", 0755); err != nil {
		t.Fatal(err)
	}

	entries := []ManifestEntry{
		{ID: "org/model-a", PulledAt: "2024-01-01T00:00:00Z", HFCache: "/tmp/a"},
		{ID: "org/model-b", PulledAt: "2024-01-02T00:00:00Z", HFCache: "/tmp/b"},
	}
	if err := SaveManifest(entries); err != nil {
		t.Fatalf("SaveManifest() error: %v", err)
	}
	got, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "org/model-a" || got[1].ID != "org/model-b" {
		t.Errorf("manifest round-trip mismatch: %+v", got)
	}
}

func TestLoadManifestEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.config/iq", 0755); err != nil {
		t.Fatal(err)
	}
	// No models.json yet.
	got, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() on missing file error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadManifest() on missing file = %v, want empty slice", got)
	}
}

func TestRemoveFromManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.config/iq", 0755); err != nil {
		t.Fatal(err)
	}

	entries := []ManifestEntry{{ID: "org/keep"}, {ID: "org/remove"}}
	if err := SaveManifest(entries); err != nil {
		t.Fatal(err)
	}

	removed, ok, err := RemoveFromManifest("org/remove")
	if err != nil || !ok || removed.ID != "org/remove" {
		t.Errorf("RemoveFromManifest() = (%+v, %v, %v)", removed, ok, err)
	}

	got, _ := LoadManifest()
	if len(got) != 1 || got[0].ID != "org/keep" {
		t.Errorf("after remove, manifest = %+v, want [{org/keep}]", got)
	}

	// Remove non-existent — ok should be false.
	_, ok, err = RemoveFromManifest("org/ghost")
	if err != nil || ok {
		t.Errorf("RemoveFromManifest(missing) = (_, %v, %v), want (_, false, nil)", ok, err)
	}
}

// ── SuggestTier ───────────────────────────────────────────────────────────────

func TestSuggestTierByName(t *testing.T) {
	// These models are not downloaded, so SuggestTier falls back to param count.
	cases := []struct {
		id   string
		want string
	}{
		{"org/tiny-135m-4bit", "fast"},    // 135M → fast
		{"org/large-7b-instruct", "slow"}, // 7B → slow
		{"org/unknown-model", "slow"},     // unknown → assume large
	}
	for _, tc := range cases {
		got := SuggestTier(tc.id)
		if got != tc.want {
			t.Errorf("SuggestTier(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
