package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/queone/utl"
	"gopkg.in/yaml.v3"
)

// ── Config file ───────────────────────────────────────────────────────────────

// Config stores pools of models per tier and named embed model roles.
type Config struct {
	Tiers    map[string][]string `yaml:"tiers"`
	CueModel string              `yaml:"cue_model,omitempty"` // embed model for cue classification
	KbModel  string              `yaml:"kb_model,omitempty"`  // embed model for KB indexing/retrieval
	// Legacy field — migrated to KbModel on load.
	EmbedModel string `yaml:"embed_model,omitempty"`
}

var tierOrder = []string{"fast", "slow"}

const (
	defaultCueModel = "mlx-community/nomicai-modernbert-embed-base-4bit"
	defaultKbModel  = "mlx-community/mxbai-embed-large-v1"
)

// cueModel returns the configured cue classification embed model.
func cueModel(cfg *Config) string {
	if cfg.CueModel != "" {
		return cfg.CueModel
	}
	return defaultCueModel
}

// kbModel returns the configured KB embed model.
func kbModel(cfg *Config) string {
	if cfg.KbModel != "" {
		return cfg.KbModel
	}
	return defaultKbModel
}

func configPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func loadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	cfg := &Config{Tiers: map[string][]string{"fast": {}, "slow": {}}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if yamlErr := yaml.Unmarshal(data, cfg); yamlErr == nil && cfg.Tiers != nil {
		// Detect and migrate old 4-tier single-string format.
		var probe struct {
			Tiers map[string]string `yaml:"tiers"`
		}
		if yaml.Unmarshal(data, &probe) == nil {
			_, hasOld := probe.Tiers["tiny"]
			_, hasOldB := probe.Tiers["balanced"]
			_, hasOldC := probe.Tiers["quality"]
			if hasOld || hasOldB || hasOldC {
				cfg = migrateOldConfig(probe.Tiers)
				if err := saveConfig(cfg); err == nil {
					fmt.Fprintf(os.Stderr, "%s\n",
						utl.Gra("config.yaml migrated from 4-tier to 2-tier pool format"))
				}
				return cfg, nil
			}
		}
		// Migrate legacy embed_model → kb_model.
		if cfg.EmbedModel != "" && cfg.KbModel == "" {
			cfg.KbModel = cfg.EmbedModel
			cfg.EmbedModel = ""
			if err := saveConfig(cfg); err == nil {
				fmt.Fprintf(os.Stderr, "%s\n",
					utl.Gra("config.yaml migrated: embed_model → kb_model"))
			}
		}
		if cfg.Tiers == nil {
			cfg.Tiers = map[string][]string{"fast": {}, "slow": {}}
		}
		for _, t := range tierOrder {
			if cfg.Tiers[t] == nil {
				cfg.Tiers[t] = []string{}
			}
		}
		return cfg, nil
	}

	return cfg, nil
}

// migrateOldConfig converts the old tiny/fast/balanced/quality single-model-per-tier
// format to the new fast/slow pool format using the 2GB disk threshold.
func migrateOldConfig(old map[string]string) *Config {
	cfg := &Config{Tiers: map[string][]string{"fast": {}, "slow": {}}}
	oldToNew := map[string]string{
		"tiny":     "fast",
		"fast":     "fast",
		"balanced": "fast",
		"quality":  "slow",
	}
	seen := map[string]bool{}
	for _, oldTier := range []string{"tiny", "fast", "balanced", "quality"} {
		id := old[oldTier]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		newTier := oldToNew[oldTier]
		disk := diskUsage(hfCacheDir(id))
		if disk > 0 {
			if disk >= 2*1024*1024*1024 {
				newTier = "slow"
			} else {
				newTier = "fast"
			}
		}
		cfg.Tiers[newTier] = append(cfg.Tiers[newTier], id)
	}
	return cfg
}

func saveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// tierForModel returns "fast" or "slow" if the model is in a tier pool, else "".
func tierForModel(modelID string) string {
	cfg, err := loadConfig()
	if err != nil {
		return ""
	}
	for _, t := range tierOrder {
		if slices.Contains(cfg.Tiers[t], modelID) {
			return t
		}
	}
	return ""
}

// allAssignedModels returns all model IDs assigned to any tier, in tier order.
func allAssignedModels() []string {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range tierOrder {
		out = append(out, cfg.Tiers[t]...)
	}
	return out
}
