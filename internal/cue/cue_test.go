package cue

import (
	"os"
	"strings"
	"testing"
)

func setHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home+"/.config/iq", 0755); err != nil {
		t.Fatal(err)
	}
}

func TestPath(t *testing.T) {
	setHome(t)
	p, err := Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}
	if !strings.HasSuffix(p, "cues.yaml") {
		t.Errorf("Path() = %q, want suffix cues.yaml", p)
	}
}

func TestLoadDefaults(t *testing.T) {
	cues, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults() error: %v", err)
	}
	if len(cues) == 0 {
		t.Fatal("LoadDefaults() returned empty slice")
	}
	// The default cues must include the "initial" fallback cue.
	_, c := Find(cues, "initial")
	if c == nil {
		t.Error("default cues missing required 'initial' cue")
	}
}

func TestFind(t *testing.T) {
	cues := []Cue{
		{Name: "alpha", Category: "general"},
		{Name: "beta", Category: "code"},
	}

	i, c := Find(cues, "alpha")
	if i != 0 || c == nil || c.Name != "alpha" {
		t.Errorf("Find(alpha) = %d, %v; want 0, non-nil", i, c)
	}

	i, c = Find(cues, "missing")
	if i != -1 || c != nil {
		t.Errorf("Find(missing) = %d, %v; want -1, nil", i, c)
	}
}

func TestSaveAndLoad(t *testing.T) {
	setHome(t)
	want := []Cue{
		{Name: "test-cue", Category: "general", Description: "a test cue", SuggestedTier: "fast"},
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "test-cue" {
		t.Errorf("Load() = %+v, want [{Name:test-cue ...}]", got)
	}
}

func TestLoadSeededFromDefaults(t *testing.T) {
	setHome(t)
	// cues.yaml does not exist yet — Load should seed from defaults.
	cues, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cues) == 0 {
		t.Fatal("Load() returned empty after seeding from defaults")
	}
}

func TestSaveRaw(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cues.yaml"
	data := []byte("- name: raw\n  category: test\n")
	if err := SaveRaw(data, path); err != nil {
		t.Fatalf("SaveRaw() error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("SaveRaw round-trip mismatch: got %q", got)
	}
}

func TestForModel(t *testing.T) {
	setHome(t)
	cues := []Cue{
		{Name: "my-cue", Category: "code", Model: "model-xyz"},
	}
	if err := Save(cues); err != nil {
		t.Fatal(err)
	}

	got := ForModel("model-xyz")
	if got != "my-cue" {
		t.Errorf("ForModel(%q) = %q, want %q", "model-xyz", got, "my-cue")
	}
	got = ForModel("unknown-model")
	if got != "<unassigned>" {
		t.Errorf("ForModel(unknown) = %q, want <unassigned>", got)
	}
}
