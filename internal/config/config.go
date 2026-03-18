package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
	"iq/internal/color"
)

// ── Shared types ────────────────────────────────────────────────────────────

// Message is a role+content pair used across inference, session, and cache.
type Message struct {
	Role    string `json:"role" yaml:"role"`
	Content string `json:"content" yaml:"content"`
}

// ── Schema versioning ────────────────────────────────────────────────────────

// ConfigVersion is the schema version written by Save.
// Version 0 (absent field) represents any pre-versioning format.
const ConfigVersion = 1

// ── Inference parameters ─────────────────────────────────────────────────────

// Hardcoded defaults when nothing is configured.
const (
	DefaultRepetitionPenalty = 1.3
	DefaultTemperature       = 0.7
	DefaultMaxTokens         = 8192
)

// InferParams holds inference parameters that can be set globally or per-tier.
// Pointer types distinguish "not set" (nil) from "set to zero."
type InferParams struct {
	RepetitionPenalty *float64 `yaml:"repetition_penalty,omitempty"`
	Temperature       *float64 `yaml:"temperature,omitempty"`
	MaxTokens         *int     `yaml:"max_tokens,omitempty"`
	TopP              *float64 `yaml:"top_p,omitempty"`
	MinP              *float64 `yaml:"min_p,omitempty"`
	TopK              *int     `yaml:"top_k,omitempty"`
	Stop              []string `yaml:"stop,omitempty"`
	Seed              *int     `yaml:"seed,omitempty"`
}

// ResolvedParams holds the effective inference parameters after resolution.
// The three legacy fields always carry a value (hardcoded default applies).
// The five extended fields are nil/empty when not configured — the sidecar
// omits them from the request and lets mlx_lm use its own defaults.
type ResolvedParams struct {
	RepetitionPenalty float64
	Temperature       float64
	MaxTokens         int
	TopP              *float64
	MinP              *float64
	TopK              *int
	Stop              []string
	Seed              *int
}

// ResolveInferParams returns effective params: per-tier override > global > hardcoded default.
func ResolveInferParams(cfg *Config, tier string) ResolvedParams {
	p := ResolvedParams{
		RepetitionPenalty: DefaultRepetitionPenalty,
		Temperature:       DefaultTemperature,
		MaxTokens:         DefaultMaxTokens,
	}
	// Global overrides.
	if cfg.RepetitionPenalty != nil {
		p.RepetitionPenalty = *cfg.RepetitionPenalty
	}
	if cfg.Temperature != nil {
		p.Temperature = *cfg.Temperature
	}
	if cfg.MaxTokens != nil {
		p.MaxTokens = *cfg.MaxTokens
	}
	// Global overrides for extended params.
	if cfg.TopP != nil {
		p.TopP = cfg.TopP
	}
	if cfg.MinP != nil {
		p.MinP = cfg.MinP
	}
	if cfg.TopK != nil {
		p.TopK = cfg.TopK
	}
	if len(cfg.Stop) > 0 {
		p.Stop = cfg.Stop
	}
	if cfg.Seed != nil {
		p.Seed = cfg.Seed
	}
	// Per-tier overrides.
	if tc, ok := cfg.Tiers[tier]; ok && tc != nil {
		if tc.RepetitionPenalty != nil {
			p.RepetitionPenalty = *tc.RepetitionPenalty
		}
		if tc.Temperature != nil {
			p.Temperature = *tc.Temperature
		}
		if tc.MaxTokens != nil {
			p.MaxTokens = *tc.MaxTokens
		}
		if tc.TopP != nil {
			p.TopP = tc.TopP
		}
		if tc.MinP != nil {
			p.MinP = tc.MinP
		}
		if tc.TopK != nil {
			p.TopK = tc.TopK
		}
		if len(tc.Stop) > 0 {
			p.Stop = tc.Stop
		}
		if tc.Seed != nil {
			p.Seed = tc.Seed
		}
	}
	return p
}

// ── Tier config ──────────────────────────────────────────────────────────────

// TierConfig holds a tier's model pool and optional inference overrides.
type TierConfig struct {
	Models      []string `yaml:"models"`
	InferParams `yaml:",inline"`
}

// ── Config file ──────────────────────────────────────────────────────────────

// Config stores the model pool, global inference defaults, and the shared embed model.
type Config struct {
	Version     int                    `yaml:"version,omitempty"`
	Tiers       map[string]*TierConfig `yaml:"tiers"`
	InferParams `yaml:",inline"`
	EmbedModel  string   `yaml:"embed_model,omitempty"`
	KbMinScore  float64  `yaml:"kb_min_score,omitempty"`
	ToolPaths   []string `yaml:"tool_paths,omitempty"`
	BraveAPIKey string   `yaml:"brave_api_key,omitempty"` // Brave Search fallback API key
	// Legacy fields — migrated to EmbedModel on load.
	CueModel string `yaml:"cue_model,omitempty"`
	KbModel  string `yaml:"kb_model,omitempty"`
}

// DefaultKbMinScore is the minimum cosine similarity required to inject a KB chunk.
const DefaultKbMinScore float32 = 0.72

// KBMinScore returns the configured KB minimum score, or the package default.
func KBMinScore(cfg *Config) float32 {
	if cfg != nil && cfg.KbMinScore > 0 {
		return float32(cfg.KbMinScore)
	}
	return DefaultKbMinScore
}

// TierModels returns the model list for a tier, or nil.
func (c *Config) TierModels(tier string) []string {
	if tc, ok := c.Tiers[tier]; ok && tc != nil {
		return tc.Models
	}
	return nil
}

// TierOrder defines the canonical tier ordering.
var TierOrder = []string{"fast", "slow"}

// DefaultEmbedModel is the fallback embed model when none is configured.
const DefaultEmbedModel = "mlx-community/bge-small-en-v1.5-bf16"

// EmbedModel returns the configured embed model (shared by cue + KB).
func EmbedModel(cfg *Config) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return DefaultEmbedModel
}

// Dir returns ~/.config/iq, creating it if needed.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "iq")
	return dir, os.MkdirAll(dir, 0755)
}

// Path returns the full path to config.yaml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// DiskUsageFunc is a callback for measuring model disk usage during migration.
// Injected by cmd/iq to avoid pulling HF cache logic into config.
type DiskUsageFunc func(modelID string) int64

// emptyTiers returns a default tier map with empty model pools.
func emptyTiers() map[string]*TierConfig {
	return map[string]*TierConfig{
		"fast": {Models: []string{}},
		"slow": {Models: []string{}},
	}
}

// defaultConfig returns a new Config with all hardcoded defaults populated.
func defaultConfig() *Config {
	rp := float64(DefaultRepetitionPenalty)
	temp := float64(DefaultTemperature)
	mt := DefaultMaxTokens
	return &Config{
		Version: ConfigVersion,
		Tiers:   emptyTiers(),
		InferParams: InferParams{
			RepetitionPenalty: &rp,
			Temperature:       &temp,
			MaxTokens:         &mt,
		},
		EmbedModel: DefaultEmbedModel,
	}
}

// normalizeConfig ensures all canonical tiers exist and have non-nil model slices.
func normalizeConfig(cfg *Config) {
	if cfg.Tiers == nil {
		cfg.Tiers = emptyTiers()
	}
	for _, t := range TierOrder {
		if cfg.Tiers[t] == nil {
			cfg.Tiers[t] = &TierConfig{Models: []string{}}
		}
		if cfg.Tiers[t].Models == nil {
			cfg.Tiers[t].Models = []string{}
		}
	}
}

// migrateV0 converts any pre-versioning config format to the current Config.
// It handles the flat-tier list format, the old 4-tier format, and legacy
// cue_model/kb_model fields. It does not save; the caller stamps the version.
func migrateV0(data []byte, diskUsage DiskUsageFunc) (*Config, error) {
	// Probe: is this the old flat-list format (tiers: {fast: [model-a, ...]})?
	var flatProbe struct {
		Tiers map[string]any `yaml:"tiers"`
	}
	if yaml.Unmarshal(data, &flatProbe) == nil && len(flatProbe.Tiers) > 0 {
		for _, v := range flatProbe.Tiers {
			if _, isList := v.([]any); isList {
				cfg := migrateFlatTiers(data, diskUsage)
				fmt.Fprintf(os.Stderr, "%s\n",
					color.Gra("config.yaml migrated: tiers updated to structured format"))
				return cfg, nil
			}
			break // only need to check one
		}
	}

	// Structured format — unmarshal directly.
	cfg := &Config{Tiers: emptyTiers()}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}

	// Migrate legacy cue_model / kb_model → embed_model.
	if cfg.EmbedModel == "" && (cfg.CueModel != "" || cfg.KbModel != "") {
		if cfg.CueModel != "" {
			cfg.EmbedModel = cfg.CueModel
		} else {
			cfg.EmbedModel = cfg.KbModel
		}
		cfg.CueModel = ""
		cfg.KbModel = ""
		fmt.Fprintf(os.Stderr, "%s\n",
			color.Gra("config.yaml migrated: cue_model/kb_model → embed_model"))
	}

	return cfg, nil
}

// Load reads and returns the config. It uses a version field to select the
// load path: version 0 (absent) triggers the legacy migration chain; the
// current version is loaded directly. On read-only filesystems, returns
// in-memory defaults without error. diskUsage is optional and only needed
// for migrating the old 4-tier format.
func Load(diskUsage DiskUsageFunc) (*Config, error) {
	path, err := Path()
	if err != nil {
		// Cannot resolve config dir (e.g. read-only FS) — return defaults.
		return defaultConfig(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaultConfig()
		_ = Save(cfg)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	// Peek at the schema version to select the load path.
	var vp struct {
		Version int `yaml:"version"`
	}
	_ = yaml.Unmarshal(data, &vp) // missing field → 0; parse errors caught below

	if vp.Version > ConfigVersion {
		return nil, fmt.Errorf("config.yaml uses schema v%d (this build supports up to v%d); upgrade iq",
			vp.Version, ConfigVersion)
	}

	var cfg *Config
	if vp.Version == 0 {
		// Pre-versioning format — run the migration chain and stamp the version.
		if cfg, err = migrateV0(data, diskUsage); err != nil {
			return nil, err
		}
		cfg.Version = ConfigVersion
		_ = Save(cfg) // best-effort; read-only FS is fine
	} else {
		// Current schema — unmarshal directly.
		cfg = &Config{Tiers: emptyTiers()}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config.yaml: %w", err)
		}
	}

	normalizeConfig(cfg)
	return cfg, nil
}

// migrateFlatTiers converts old flat-list tiers (and old 4-tier single-string format)
// into the new TierConfig struct format.
func migrateFlatTiers(data []byte, diskUsage DiskUsageFunc) *Config {
	// Try old 4-tier single-string format first.
	var singleProbe struct {
		Tiers map[string]string `yaml:"tiers"`
	}
	if yaml.Unmarshal(data, &singleProbe) == nil {
		_, hasOld := singleProbe.Tiers["tiny"]
		_, hasOldB := singleProbe.Tiers["balanced"]
		_, hasOldC := singleProbe.Tiers["quality"]
		if hasOld || hasOldB || hasOldC {
			return migrateOldFourTier(singleProbe.Tiers, diskUsage)
		}
	}

	// Flat-list format: tiers: {fast: [model-a, ...], slow: [model-b, ...]}
	var flat struct {
		Tiers      map[string][]string `yaml:"tiers"`
		EmbedModel string              `yaml:"embed_model,omitempty"`
		ToolPaths  []string            `yaml:"tool_paths,omitempty"`
		CueModel   string              `yaml:"cue_model,omitempty"`
		KbModel    string              `yaml:"kb_model,omitempty"`
	}
	yaml.Unmarshal(data, &flat)

	cfg := &Config{
		Tiers:      emptyTiers(),
		EmbedModel: flat.EmbedModel,
		ToolPaths:  flat.ToolPaths,
		CueModel:   flat.CueModel,
		KbModel:    flat.KbModel,
	}
	for tier, models := range flat.Tiers {
		cfg.Tiers[tier] = &TierConfig{Models: models}
	}
	// Ensure canonical tiers exist.
	for _, t := range TierOrder {
		if cfg.Tiers[t] == nil {
			cfg.Tiers[t] = &TierConfig{Models: []string{}}
		}
	}
	return cfg
}

// migrateOldFourTier converts the old tiny/fast/balanced/quality single-model-per-tier
// format to the new fast/slow pool format using the 2GB disk threshold.
func migrateOldFourTier(old map[string]string, diskUsage DiskUsageFunc) *Config {
	cfg := &Config{Tiers: emptyTiers()}
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
		if diskUsage != nil {
			disk := diskUsage(id)
			if disk > 0 {
				if disk >= 2*1024*1024*1024 {
					newTier = "slow"
				} else {
					newTier = "fast"
				}
			}
		}
		cfg.Tiers[newTier].Models = append(cfg.Tiers[newTier].Models, id)
	}
	return cfg
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// TierForModel returns "fast" or "slow" if the model is in a tier pool, else "".
func TierForModel(modelID string) string {
	cfg, err := Load(nil)
	if err != nil {
		return ""
	}
	for _, t := range TierOrder {
		if slices.Contains(cfg.TierModels(t), modelID) {
			return t
		}
	}
	return ""
}

// AllAssignedModels returns all model IDs assigned to any tier, in tier order.
func AllAssignedModels() []string {
	cfg, err := Load(nil)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range TierOrder {
		out = append(out, cfg.TierModels(t)...)
	}
	return out
}
