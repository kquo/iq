package cue

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"iq/internal/config"
)

//go:embed cues_default.yaml
var DefaultCuesYAML string

// ── Types ─────────────────────────────────────────────────────────────────────

// Cue represents a named prompt classification target.
type Cue struct {
	Name          string `yaml:"name"`
	Category      string `yaml:"category"`
	Description   string `yaml:"description"`
	SystemPrompt  string `yaml:"system_prompt"`
	SuggestedTier string `yaml:"suggested_tier"`
	Model         string `yaml:"model"`
}

// ── File path ─────────────────────────────────────────────────────────────────

// Path returns the full path to cues.yaml.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cues.yaml"), nil
}

// ── Load / save ───────────────────────────────────────────────────────────────

// Load reads cues from disk, seeding from defaults if the file doesn't exist.
func Load() ([]Cue, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	// Seed from defaults if file does not exist yet.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := SaveRaw([]byte(DefaultCuesYAML), path); err != nil {
			return nil, fmt.Errorf("failed to seed cues file: %w", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cues []Cue
	if err := yaml.Unmarshal(data, &cues); err != nil {
		return nil, fmt.Errorf("failed to parse cues.yaml: %w", err)
	}
	return cues, nil
}

// Save marshals cues and writes to disk.
func Save(cues []Cue) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cues)
	if err != nil {
		return err
	}
	return SaveRaw(data, path)
}

// SaveRaw writes raw YAML bytes to the given path.
func SaveRaw(data []byte, path string) error {
	return os.WriteFile(path, data, 0644)
}

// LoadDefaults parses the embedded default cues.
func LoadDefaults() ([]Cue, error) {
	var cues []Cue
	if err := yaml.Unmarshal([]byte(DefaultCuesYAML), &cues); err != nil {
		return nil, err
	}
	return cues, nil
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

// Find returns the index and pointer to a cue by name, or (-1, nil).
func Find(cues []Cue, name string) (int, *Cue) {
	for i := range cues {
		if cues[i].Name == name {
			return i, &cues[i]
		}
	}
	return -1, nil
}

// ForModel returns the cue name assigned to a model ID, or "<unassigned>".
func ForModel(modelID string) string {
	cues, err := Load()
	if err != nil {
		return "<unassigned>"
	}
	for _, c := range cues {
		if c.Model == modelID {
			return c.Name
		}
	}
	return "<unassigned>"
}
