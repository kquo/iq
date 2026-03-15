package config

import (
	"os"
	"testing"
)

func TestResolveInferParams(t *testing.T) {
	t.Run("defaults only", func(t *testing.T) {
		cfg := &Config{Tiers: emptyTiers()}
		p := ResolveInferParams(cfg, "fast")
		if p.Temperature != DefaultTemperature {
			t.Errorf("Temperature: got %v, want %v", p.Temperature, DefaultTemperature)
		}
		if p.TopP != nil || p.MinP != nil || p.TopK != nil || p.Stop != nil || p.Seed != nil {
			t.Error("extended params should all be nil when not configured")
		}
	})

	t.Run("global extended params applied", func(t *testing.T) {
		cfg := &Config{
			Tiers: emptyTiers(),
			InferParams: InferParams{
				TopP: new(0.9),
				MinP: new(0.05),
				TopK: new(40),
				Stop: []string{"</s>", "\n\n"},
				Seed: new(42),
			},
		}
		p := ResolveInferParams(cfg, "fast")
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

	t.Run("tier overrides global", func(t *testing.T) {
		cfg := &Config{
			Tiers: map[string]*TierConfig{
				"slow": {
					Models: []string{"some-model"},
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
		p := ResolveInferParams(cfg, "slow")
		if p.TopP == nil || *p.TopP != 0.8 {
			t.Errorf("TopP tier override: got %v, want 0.8", p.TopP)
		}
		if p.Seed == nil || *p.Seed != 99 {
			t.Errorf("Seed tier override: got %v, want 99", p.Seed)
		}
	})

	t.Run("tier stop overrides global stop", func(t *testing.T) {
		cfg := &Config{
			Tiers: map[string]*TierConfig{
				"fast": {
					Models:      []string{"some-model"},
					InferParams: InferParams{Stop: []string{"STOP"}},
				},
			},
			InferParams: InferParams{Stop: []string{"</s>"}},
		}
		p := ResolveInferParams(cfg, "fast")
		if len(p.Stop) != 1 || p.Stop[0] != "STOP" {
			t.Errorf("Stop tier override: got %v", p.Stop)
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

func TestEffectivePipeline(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *Config
		expect string
	}{
		{"nil config", nil, PipelineTwoTier},
		{"empty pipeline field", &Config{}, PipelineTwoTier},
		{"explicit two_tier", &Config{Pipeline: PipelineTwoTier}, PipelineTwoTier},
		{"unknown mode returned as-is", &Config{Pipeline: "single_pool"}, "single_pool"},
	}
	for _, tc := range tests {
		got := tc.cfg.EffectivePipeline()
		if got != tc.expect {
			t.Errorf("%s: EffectivePipeline() = %q, want %q", tc.name, got, tc.expect)
		}
	}
}
