package config

import (
	"os"
	"strings"
	"testing"
)

// ── Migration tests ───────────────────────────────────────────────────────────

func TestMigrateFlatTiers(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantIDs []string // expected model IDs in order (fast first, slow second)
	}{
		{
			"flat list fast and slow",
			"tiers:\n  fast:\n    - model-a\n  slow:\n    - model-b\n",
			[]string{"model-a", "model-b"},
		},
		{
			"flat list fast only",
			"tiers:\n  fast:\n    - model-a\n",
			[]string{"model-a"},
		},
		{
			"flat list slow only",
			"tiers:\n  slow:\n    - model-b\n",
			[]string{"model-b"},
		},
		{
			"flat list multiple models",
			"tiers:\n  fast:\n    - model-a\n    - model-c\n  slow:\n    - model-b\n",
			[]string{"model-a", "model-c", "model-b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := migrateFlatTiers([]byte(tc.yaml), nil)
			if cfg == nil {
				t.Fatal("migrateFlatTiers returned nil")
			}
			got := cfg.AllModels()
			if !stringSliceEqual(got, tc.wantIDs) {
				t.Errorf("models = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestMigrateOldFourTier(t *testing.T) {
	tests := []struct {
		name     string
		old      map[string]string
		diskSize map[string]int64
		wantIDs  []string // expected model IDs in order
	}{
		{
			"quality maps to slow by disk (≥2GB)",
			map[string]string{"quality": "large-model"},
			map[string]int64{"large-model": 3 * 1024 * 1024 * 1024},
			[]string{"large-model"},
		},
		{
			"tiny maps to fast by disk (<2GB)",
			map[string]string{"tiny": "small-model"},
			map[string]int64{"small-model": 500 * 1024 * 1024},
			[]string{"small-model"},
		},
		{
			"no disk info falls back to tier mapping",
			map[string]string{"tiny": "small-model", "quality": "large-model"},
			nil,
			[]string{"small-model", "large-model"}, // fast first, slow second
		},
		{
			"duplicate models deduplicated",
			map[string]string{"tiny": "same-model", "fast": "same-model"},
			nil,
			[]string{"same-model"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diskFn := func(id string) int64 {
				if tc.diskSize == nil {
					return 0
				}
				return tc.diskSize[id]
			}
			cfg := migrateOldFourTier(tc.old, diskFn)
			if cfg == nil {
				t.Fatal("migrateOldFourTier returned nil")
			}
			got := cfg.AllModels()
			if !stringSliceEqual(got, tc.wantIDs) {
				t.Errorf("models = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestLegacyEmbedModelMigration(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// v0 structured tiers with cue_model set, embed_model absent.
	yaml := "tiers:\n  fast:\n    models: []\n  slow:\n    models: []\ncue_model: embed-via-cue\n"
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.EmbedModel != "embed-via-cue" {
		t.Errorf("EmbedModel = %q, want %q", cfg.EmbedModel, "embed-via-cue")
	}
	if cfg.CueModel != "" {
		t.Errorf("CueModel should be cleared after migration, got %q", cfg.CueModel)
	}
}

// stringSliceEqual compares two string slices treating nil and empty as equal.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolveInferParams(t *testing.T) {
	t.Run("defaults only", func(t *testing.T) {
		cfg := &Config{}
		p := ResolveInferParams(cfg, "")
		if p.Temperature != DefaultTemperature {
			t.Errorf("Temperature: got %v, want %v", p.Temperature, DefaultTemperature)
		}
		if p.TopP != nil || p.MinP != nil || p.TopK != nil || p.Stop != nil || p.Seed != nil {
			t.Error("extended params should all be nil when not configured")
		}
	})

	t.Run("global extended params applied", func(t *testing.T) {
		cfg := &Config{
			InferParams: InferParams{
				TopP: new(0.9),
				MinP: new(0.05),
				TopK: new(40),
				Stop: []string{"</s>", "\n\n"},
				Seed: new(42),
			},
		}
		p := ResolveInferParams(cfg, "")
		if p.TopP == nil || *p.TopP != 0.9 {
			t.Errorf("TopP: got %v, want 0.9", p.TopP)
		}
		if p.MinP == nil || *p.MinP != 0.05 {
			t.Errorf("MinP: got %v, want 0.05", p.MinP)
		}
		if p.TopK == nil || *p.TopK != 40 {
			t.Errorf("TopK: got %v, want 40", p.TopK)
		}
		if len(p.Stop) != 2 || p.Stop[0] != "</s>" {
			t.Errorf("Stop: got %v", p.Stop)
		}
		if p.Seed == nil || *p.Seed != 42 {
			t.Errorf("Seed: got %v, want 42", p.Seed)
		}
	})

	t.Run("model overrides global", func(t *testing.T) {
		cfg := &Config{
			Models: []ModelEntry{
				{
					ID: "some-model",
					InferParams: InferParams{
						TopP: new(0.8),
						Seed: new(99),
					},
				},
			},
			InferParams: InferParams{
				TopP: new(0.95),
				Seed: new(1),
			},
		}
		p := ResolveInferParams(cfg, "some-model")
		if p.TopP == nil || *p.TopP != 0.8 {
			t.Errorf("TopP model override: got %v, want 0.8", p.TopP)
		}
		if p.Seed == nil || *p.Seed != 99 {
			t.Errorf("Seed model override: got %v, want 99", p.Seed)
		}
	})

	t.Run("unknown model uses global only", func(t *testing.T) {
		cfg := &Config{
			InferParams: InferParams{
				TopP: new(0.95),
			},
		}
		p := ResolveInferParams(cfg, "not-in-pool")
		if p.TopP == nil || *p.TopP != 0.95 {
			t.Errorf("TopP: got %v, want 0.95 (global)", p.TopP)
		}
	})

	t.Run("model stop overrides global stop", func(t *testing.T) {
		cfg := &Config{
			Models: []ModelEntry{
				{
					ID:          "some-model",
					InferParams: InferParams{Stop: []string{"STOP"}},
				},
			},
			InferParams: InferParams{Stop: []string{"</s>"}},
		}
		p := ResolveInferParams(cfg, "some-model")
		if len(p.Stop) != 1 || p.Stop[0] != "STOP" {
			t.Errorf("Stop model override: got %v", p.Stop)
		}
	})
}

func TestLoadInvalidYAML(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// A leading tab is illegal in YAML and guaranteed to fail go-yaml.
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte("\t: invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	_, err := Load(nil)
	if err == nil {
		t.Fatal("Load with invalid YAML should return an error, got nil")
	}
}

func TestLoadSchemaV1MigratesToV2(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	y := "version: 1\ntiers:\n  fast:\n    models:\n      - model-a\n  slow:\n    models: []\n"
	cfgPath := cfgDir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte(y), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Version != ConfigVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, ConfigVersion)
	}
	models := cfg.AllModels()
	if len(models) != 1 || models[0] != "model-a" {
		t.Errorf("AllModels = %v, want [model-a]", models)
	}
	// Confirm saved file is now v2.
	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "version: 2") {
		t.Errorf("saved config.yaml missing 'version: 2':\n%s", saved)
	}
}

func TestLoadSchemaV1WithPerTierParams(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// v1 with per-tier temperature override on slow tier.
	y := "version: 1\ntiers:\n  fast:\n    models:\n      - fast-model\n  slow:\n    models:\n      - slow-model\n    temperature: 0.5\n"
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte(y), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Version != ConfigVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, ConfigVersion)
	}
	// fast-model should have no per-model override.
	p := ResolveInferParams(cfg, "fast-model")
	if p.Temperature != DefaultTemperature {
		t.Errorf("fast-model temperature = %v, want default %v", p.Temperature, DefaultTemperature)
	}
	// slow-model should inherit the old slow-tier temperature override.
	p2 := ResolveInferParams(cfg, "slow-model")
	if p2.Temperature != 0.5 {
		t.Errorf("slow-model temperature = %v, want 0.5 (migrated from tier override)", p2.Temperature)
	}
}

func TestLoadSchemaV0StampsV2(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Structured format with no version field (v0).
	y := "tiers:\n  fast:\n    models:\n      - model-a\n  slow:\n    models: []\n"
	cfgPath := cfgDir + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte(y), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Version != ConfigVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, ConfigVersion)
	}
	updated, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "version: 2") {
		t.Errorf("saved config.yaml missing 'version: 2':\n%s", updated)
	}
}

func TestLoadSchemaV2Direct(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	y := "version: 2\nmodels:\n  - id: model-a\n  - id: model-b\n    temperature: 0.5\n"
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte(y), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Version != 2 {
		t.Errorf("Version = %d, want 2", cfg.Version)
	}
	models := cfg.AllModels()
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-b" {
		t.Errorf("AllModels = %v, want [model-a model-b]", models)
	}
	// model-b should have per-model temperature override.
	p := ResolveInferParams(cfg, "model-b")
	if p.Temperature != 0.5 {
		t.Errorf("model-b temperature = %v, want 0.5", p.Temperature)
	}
}

func TestLoadFutureSchemaVersionErrors(t *testing.T) {
	home := t.TempDir()
	cfgDir := home + "/.config/iq"
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte("version: 99\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	_, err := Load(nil)
	if err == nil {
		t.Fatal("Load with future schema version should return an error")
	}
}

func TestHasModel(t *testing.T) {
	cfg := &Config{
		Models: []ModelEntry{
			{ID: "org/model-a"},
			{ID: "org/model-b"},
		},
	}
	if !cfg.HasModel("org/model-a") {
		t.Error("HasModel(model-a) should be true")
	}
	if cfg.HasModel("org/model-c") {
		t.Error("HasModel(model-c) should be false")
	}
}

func TestAllModels(t *testing.T) {
	cfg := &Config{
		Models: []ModelEntry{
			{ID: "first"},
			{ID: "second"},
		},
	}
	got := cfg.AllModels()
	want := []string{"first", "second"}
	if !stringSliceEqual(got, want) {
		t.Errorf("AllModels = %v, want %v", got, want)
	}

	// Empty pool.
	empty := &Config{}
	if ids := empty.AllModels(); len(ids) != 0 {
		t.Errorf("empty AllModels = %v, want []", ids)
	}
}
