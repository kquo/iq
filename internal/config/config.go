package config

import (
	"fmt"
	"os"
	"path/filepath"

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
// Version 1 was the two-tier (fast/slow) structured format.
// Version 2 is the flat models list.
const ConfigVersion = 2

// ── Inference parameters ─────────────────────────────────────────────────────

// Hardcoded defaults when nothing is configured.
const (
	DefaultRepetitionPenalty = 1.3
	DefaultTemperature       = 0.7
	DefaultMaxTokens         = 8192
)

// InferParams holds inference parameters that can be set globally or per-model.
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
// ContextWindow is 0 when not configured (trimming disabled).
type ResolvedParams struct {
	RepetitionPenalty float64
	Temperature       float64
	MaxTokens         int
	ContextWindow     int
	TopP              *float64
	MinP              *float64
	TopK              *int
	Stop              []string
	Seed              *int
}

// ── Model entry ──────────────────────────────────────────────────────────────

// ModelEntry is a single inference model in the flat pool with optional
// per-model inference param overrides.
type ModelEntry struct {
	ID            string `yaml:"id"`
	ContextWindow int    `yaml:"context_window,omitempty"`
	InferParams   `yaml:",inline"`
}

// ── Config file ──────────────────────────────────────────────────────────────

// Config stores the model pool, global inference defaults, and the shared embed model.
type Config struct {
	Version     int          `yaml:"version,omitempty"`
	Models      []ModelEntry `yaml:"models,omitempty"`
	InferParams `yaml:",inline"`
	EmbedModel  string   `yaml:"embed_model,omitempty"`
	KbMinScore  float64  `yaml:"kb_min_score,omitempty"`
	ToolPaths   []string `yaml:"tool_paths,omitempty"`
	BraveAPIKey string   `yaml:"brave_api_key,omitempty"` // Brave Search fallback API key
	// Legacy fields — migrated to EmbedModel on load.
	CueModel string `yaml:"cue_model,omitempty"`
	KbModel  string `yaml:"kb_model,omitempty"`
}

// AllModels returns all model IDs in the pool, in order.
func (c *Config) AllModels() []string {
	ids := make([]string, len(c.Models))
	for i, m := range c.Models {
		ids[i] = m.ID
	}
	return ids
}

// HasModel reports whether modelID is in the pool.
func (c *Config) HasModel(id string) bool {
	for _, m := range c.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// ModelEntryFor returns a pointer to the ModelEntry for modelID, or nil.
func (c *Config) ModelEntryFor(id string) *ModelEntry {
	for i := range c.Models {
		if c.Models[i].ID == id {
			return &c.Models[i]
		}
	}
	return nil
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

// DefaultEmbedModel is the fallback embed model when none is configured.
const DefaultEmbedModel = "mlx-community/bge-small-en-v1.5-bf16"

// EmbedModel returns the configured embed model (shared by cue + KB).
func EmbedModel(cfg *Config) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return DefaultEmbedModel
}

// ResolveInferParams returns effective params: per-model override > global > hardcoded default.
// When modelID is empty or not in the pool, only the global layer is applied.
func ResolveInferParams(cfg *Config, modelID string) ResolvedParams {
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
	// Per-model overrides.
	if me := cfg.ModelEntryFor(modelID); me != nil {
		if me.ContextWindow > 0 {
			p.ContextWindow = me.ContextWindow
		}
		if me.RepetitionPenalty != nil {
			p.RepetitionPenalty = *me.RepetitionPenalty
		}
		if me.Temperature != nil {
			p.Temperature = *me.Temperature
		}
		if me.MaxTokens != nil {
			p.MaxTokens = *me.MaxTokens
		}
		if me.TopP != nil {
			p.TopP = me.TopP
		}
		if me.MinP != nil {
			p.MinP = me.MinP
		}
		if me.TopK != nil {
			p.TopK = me.TopK
		}
		if len(me.Stop) > 0 {
			p.Stop = me.Stop
		}
		if me.Seed != nil {
			p.Seed = me.Seed
		}
	}
	return p
}

// ── Directory helpers ─────────────────────────────────────────────────────────

// Dir returns ~/.config/iq, creating it if needed.
func Dir() (string, error) {
	return DirFor("iq")
}

// DirFor returns ~/.config/<name>, creating it if needed.
func DirFor(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", name)
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

func defaultConfig() *Config {
	rp := float64(DefaultRepetitionPenalty)
	temp := float64(DefaultTemperature)
	mt := DefaultMaxTokens
	return &Config{
		Version: ConfigVersion,
		InferParams: InferParams{
			RepetitionPenalty: &rp,
			Temperature:       &temp,
			MaxTokens:         &mt,
		},
		EmbedModel: DefaultEmbedModel,
	}
}

// ── Migration helpers ─────────────────────────────────────────────────────────

// legacyTierConfig is used only during v0/v1 migration.
type legacyTierConfig struct {
	Models      []string `yaml:"models"`
	InferParams `yaml:",inline"`
}

// legacyConfig mirrors the old tier-based schema and is used only for migration.
type legacyConfig struct {
	Tiers       map[string]*legacyTierConfig `yaml:"tiers"`
	InferParams `yaml:",inline"`
	EmbedModel  string   `yaml:"embed_model,omitempty"`
	KbMinScore  float64  `yaml:"kb_min_score,omitempty"`
	ToolPaths   []string `yaml:"tool_paths,omitempty"`
	BraveAPIKey string   `yaml:"brave_api_key,omitempty"`
	CueModel    string   `yaml:"cue_model,omitempty"`
	KbModel     string   `yaml:"kb_model,omitempty"`
}

// legacyToConfig converts a legacyConfig (tier-based) to the new flat Config.
// Tiers are flattened in order: fast first, slow second.
// Per-tier inference param overrides are preserved as per-model overrides.
func legacyToConfig(leg *legacyConfig) *Config {
	cfg := &Config{
		InferParams: leg.InferParams,
		EmbedModel:  leg.EmbedModel,
		KbMinScore:  leg.KbMinScore,
		ToolPaths:   leg.ToolPaths,
		BraveAPIKey: leg.BraveAPIKey,
		CueModel:    leg.CueModel,
		KbModel:     leg.KbModel,
	}
	for _, t := range []string{"fast", "slow"} {
		tc, ok := leg.Tiers[t]
		if !ok || tc == nil {
			continue
		}
		for _, modelID := range tc.Models {
			cfg.Models = append(cfg.Models, ModelEntry{
				ID:          modelID,
				InferParams: tc.InferParams,
			})
		}
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
	return cfg
}

// migrateV1 converts a v1 config (fast/slow tiers) to v2 (flat models list).
func migrateV1(data []byte) (*Config, error) {
	var leg legacyConfig
	if err := yaml.Unmarshal(data, &leg); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}
	cfg := legacyToConfig(&leg)
	fmt.Fprintf(os.Stderr, "%s\n",
		color.Gra("config.yaml migrated: tiers → flat models list (v2)"))
	return cfg, nil
}

// migrateV0 converts any pre-versioning config format to the current Config.
// It handles the flat-tier list format, the old 4-tier format, legacy
// cue_model/kb_model fields, and the structured (v0) format.
func migrateV0(data []byte, diskUsage DiskUsageFunc) (*Config, error) {
	// Probe: is this the old flat-list format (tiers: {fast: [model-a, ...]})
	// or old 4-tier single-string format?
	var flatProbe struct {
		Tiers map[string]any `yaml:"tiers"`
	}
	if yaml.Unmarshal(data, &flatProbe) == nil && len(flatProbe.Tiers) > 0 {
		for _, v := range flatProbe.Tiers {
			if _, isList := v.([]any); isList {
				cfg := migrateFlatTiers(data, diskUsage)
				fmt.Fprintf(os.Stderr, "%s\n",
					color.Gra("config.yaml migrated: legacy tier format → flat models list (v2)"))
				return cfg, nil
			}
			break // only need to check one
		}
	}

	// Structured format (v0 with no version field) — same shape as v1.
	var leg legacyConfig
	if err := yaml.Unmarshal(data, &leg); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}
	return legacyToConfig(&leg), nil
}

// migrateFlatTiers converts old flat-list tiers (and old 4-tier single-string format)
// into the new ModelEntry slice format.
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
	yaml.Unmarshal(data, &flat) //nolint:errcheck // best-effort; callers already validated
	cfg := &Config{
		EmbedModel: flat.EmbedModel,
		ToolPaths:  flat.ToolPaths,
		CueModel:   flat.CueModel,
		KbModel:    flat.KbModel,
	}
	for _, t := range []string{"fast", "slow"} {
		for _, modelID := range flat.Tiers[t] {
			cfg.Models = append(cfg.Models, ModelEntry{ID: modelID})
		}
	}
	return cfg
}

// migrateOldFourTier converts the old tiny/fast/balanced/quality single-model-per-tier
// format to the new flat models list, using the 2GB disk threshold for ordering.
func migrateOldFourTier(old map[string]string, diskUsage DiskUsageFunc) *Config {
	cfg := &Config{}
	seen := map[string]bool{}

	slowTiers := map[string]bool{"quality": true}
	var fast, slow []string
	for _, oldTier := range []string{"tiny", "fast", "balanced", "quality"} {
		id := old[oldTier]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		isSlow := slowTiers[oldTier]
		if diskUsage != nil {
			disk := diskUsage(id)
			if disk > 0 {
				isSlow = disk >= 2*1024*1024*1024
			}
		}
		if isSlow {
			slow = append(slow, id)
		} else {
			fast = append(fast, id)
		}
	}
	for _, id := range fast {
		cfg.Models = append(cfg.Models, ModelEntry{ID: id})
	}
	for _, id := range slow {
		cfg.Models = append(cfg.Models, ModelEntry{ID: id})
	}
	return cfg
}

// Load reads and returns the config. It uses a version field to select the
// load path: version 0 (absent) triggers the legacy migration chain; version 1
// triggers migrateV1; version 2 (current) is loaded directly. On read-only
// filesystems, returns in-memory defaults without error. diskUsage is optional
// and only needed for migrating the old 4-tier format.
// Load reads config from the default ~/.config/iq/config.yaml path.
func Load(diskUsage DiskUsageFunc) (*Config, error) {
	path, err := Path()
	if err != nil {
		// Cannot resolve config dir (e.g. read-only FS) — return defaults.
		return defaultConfig(), nil
	}
	return LoadAt(path, diskUsage)
}

// LoadAt reads config from an explicit file path, applying auto-migration.
func LoadAt(cfgPath string, diskUsage DiskUsageFunc) (*Config, error) {
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) {
		cfg := defaultConfig()
		_ = SaveAt(cfgPath, cfg)
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
	switch vp.Version {
	case 0:
		// Pre-versioning format — run the migration chain and stamp the version.
		var merr error
		if cfg, merr = migrateV0(data, diskUsage); merr != nil {
			return nil, merr
		}
		cfg.Version = ConfigVersion
		_ = SaveAt(cfgPath, cfg) // best-effort; read-only FS is fine
	case 1:
		// v1 tier-based format — migrate to flat models list.
		var merr error
		if cfg, merr = migrateV1(data); merr != nil {
			return nil, merr
		}
		cfg.Version = ConfigVersion
		_ = SaveAt(cfgPath, cfg)
	default:
		// Current schema — unmarshal directly.
		cfg = &Config{}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config.yaml: %w", err)
		}
	}

	return cfg, nil
}

// Save writes the config to the default ~/.config/iq/config.yaml path.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveAt(path, cfg)
}

// SaveAt writes the config to an explicit file path.
func SaveAt(cfgPath string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}
