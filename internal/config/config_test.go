package config

import "testing"

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
