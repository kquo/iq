package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"iq/internal/cache"
	"iq/internal/color"
	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/sidecar"
	"iq/internal/tools"
)

// newConfigCmd returns the `iq config` command with show and validate subcommands.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"cfg"},
		Short:   "Inspect and validate IQ configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printConfigHelp()
	})
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigValidateCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show effective configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Aliases:      []string{"check"},
		Short:        "Validate configuration files",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigValidate()
		},
	}
}

func printConfigHelp() {
	n := programName
	fmt.Printf("Inspect and validate IQ configuration.\n\n")
	fmt.Printf("%s\n", color.Whi2("USAGE"))
	fmt.Printf("  %s config [command]\n\n", n)
	fmt.Printf("%s\n", color.Whi2("COMMANDS"))
	fmt.Printf("  %-16s %s\n", "show", "Show effective configuration (default)")
	fmt.Printf("  %-16s %s\n\n", "validate", "Validate configuration files")
	fmt.Printf("%s\n", color.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s config\n", n)
	fmt.Printf("  $ %s config show\n", n)
	fmt.Printf("  $ %s config validate\n", n)
}

// ── config show ──────────────────────────────────────────────────────────────

func runConfigShow() error {
	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}

	// ── config.yaml ──
	fmt.Printf("%s\n", color.Whi2("CONFIG"))
	cfgField("path", cfgPathStr())
	cfgField("schema_version", fmt.Sprintf("v%d", cfg.Version))

	// Print in config.yaml order: global params, tiers (with resolved params), then extras.
	gp := config.ResolveInferParams(cfg, "")
	cfgField("repetition_penalty", fmt.Sprintf("%.1f", gp.RepetitionPenalty))
	cfgField("temperature", fmt.Sprintf("%.1f", gp.Temperature))
	cfgField("max_tokens", fmt.Sprintf("%d", gp.MaxTokens))
	cfgField("top_p", fmtOptF(gp.TopP, false, "1.0  (mlx_lm default)", "%.2f"))
	cfgField("min_p", fmtOptF(gp.MinP, false, "0.0  (mlx_lm default)", "%.2f"))
	cfgField("top_k", fmtOptI(gp.TopK, false, "0  (mlx_lm default)"))
	cfgField("stop", fmtStop(gp.Stop, false))
	cfgField("seed", fmtOptI(gp.Seed, false, "—  (random)"))
	fmt.Println()

	// Tiers — mirrors config.yaml structure, showing resolved effective values per tier.
	cfgField("tiers:", "")
	for _, t := range config.TierOrder {
		models := cfg.TierModels(t)
		cfgField(fmt.Sprintf("  %s:", t), "")
		if len(models) == 0 {
			cfgField("    models", "(empty)")
		} else {
			for _, m := range models {
				fmt.Printf("      - %s\n", m)
			}
		}
		// Show effective inference params for this tier.
		tp := config.ResolveInferParams(cfg, t)
		tc := cfg.Tiers[t]
		cfgField("    repetition_penalty", fmtParam(tp.RepetitionPenalty, tc != nil && tc.RepetitionPenalty != nil, "%.1f"))
		cfgField("    temperature", fmtParam(tp.Temperature, tc != nil && tc.Temperature != nil, "%.1f"))
		cfgField("    max_tokens", fmtParamInt(tp.MaxTokens, tc != nil && tc.MaxTokens != nil))
		cfgField("    top_p", fmtOptF(tp.TopP, tc != nil && tc.TopP != nil, "1.0  (mlx_lm default)", "%.2f"))
		cfgField("    min_p", fmtOptF(tp.MinP, tc != nil && tc.MinP != nil, "0.0  (mlx_lm default)", "%.2f"))
		cfgField("    top_k", fmtOptI(tp.TopK, tc != nil && tc.TopK != nil, "0  (mlx_lm default)"))
		cfgField("    stop", fmtStop(tp.Stop, tc != nil && len(tc.Stop) > 0))
		cfgField("    seed", fmtOptI(tp.Seed, tc != nil && tc.Seed != nil, "—  (random)"))
	}
	fmt.Println()

	cfgField("embed_model", config.EmbedModel(cfg))
	cfgField("pipeline", cfg.EffectivePipeline())

	if cfg.BraveAPIKey != "" {
		masked := cfg.BraveAPIKey[:4] + strings.Repeat("*", len(cfg.BraveAPIKey)-4)
		cfgField("brave_api_key", masked)
	} else {
		cfgField("brave_api_key", "(not set)")
	}

	if len(cfg.ToolPaths) > 0 {
		cfgField("tool_paths", strings.Join(cfg.ToolPaths, ", "))
	} else {
		cfgField("tool_paths", "(none)")
	}

	// ── Cues ──
	cfgSection("CUES")
	cuePath, _ := cue.Path()
	cfgField("path", cuePath)
	cues, cueErr := cue.Load()
	if cueErr != nil {
		cfgField("status", fmt.Sprintf("error: %s", cueErr))
	} else {
		cfgField("count", fmt.Sprintf("%d", len(cues)))
		cats := map[string]int{}
		for _, c := range cues {
			cats[c.Category]++
		}
		var catParts []string
		for cat, n := range cats {
			catParts = append(catParts, fmt.Sprintf("%s(%d)", cat, n))
		}
		cfgField("categories", strings.Join(catParts, " "))
	}

	// ── Knowledge base ──
	cfgSection("KNOWLEDGE BASE")
	if kb.Exists() {
		idx, kbErr := kb.Load()
		if kbErr != nil {
			cfgField("status", fmt.Sprintf("error: %s", kbErr))
		} else {
			cfgField("sources", fmt.Sprintf("%d", len(idx.Sources)))
			cfgField("chunks", fmt.Sprintf("%d", len(idx.Chunks)))
		}
	} else {
		cfgField("status", "(not initialized)")
	}

	// ── Thresholds & constants ──
	cfgSection("THRESHOLDS")
	cfgField("cue_classify_min", fmt.Sprintf("%.2f", embed.ClassifyMinScore))
	cfgField("keyword_boost", fmt.Sprintf("%.2f", embed.KeywordBoostConst))
	cfgField("tool_classify_min", fmt.Sprintf("%.2f", tools.ClassifyMinScore))
	cfgField("kb_min_score", fmt.Sprintf("%.2f", config.KBMinScore(cfg)))
	cfgField("kb_top_k", fmt.Sprintf("%d", kb.DefaultK))

	cfgSection("RUNTIME")
	cfgField("embed_port", fmt.Sprintf("%d", embed.PortConst))
	cfgField("sidecar_port_base", fmt.Sprintf("%d", sidecar.PortBase))
	cfgField("sidecar_ready_timeout", sidecar.ReadyTimeout.String())
	cfgField("embed_ready_timeout", embed.ReadyTimeout.String())
	cfgField("tool_exec_timeout", tools.ExecuteTimeout.String())
	cfgField("tool_max_output", fmt.Sprintf("%d bytes", tools.MaxOutputBytes))
	cfgField("cache_ttl", cache.TTL.String())
	cfgField("tools_registered", fmt.Sprintf("%d", len(tools.NewRegistry())))
	cfgField("tool_signals", fmt.Sprintf("%d", len(tools.Signals)))

	return nil
}

func cfgPathStr() string {
	p, _ := config.Path()
	return p
}

func cfgSection(name string) {
	fmt.Printf("\n%s\n", color.Whi2(name))
}

func cfgField(label, value string) {
	fmt.Printf("  %-24s %s\n", label, value)
}

// fmtParam formats a float64 param, annotating whether it's a tier override.
func fmtParam(effective float64, isOverride bool, verb string) string {
	val := fmt.Sprintf(verb, effective)
	if isOverride {
		return val + "  (tier override)"
	}
	return val
}

// fmtParamInt formats an int param, annotating whether it's a tier override.
func fmtParamInt(effective int, isOverride bool) string {
	val := fmt.Sprintf("%d", effective)
	if isOverride {
		return val + "  (tier override)"
	}
	return val
}

// fmtOptF formats an optional *float64. nilLabel is shown when the pointer is
// nil (e.g. "1.0  (mlx_lm default)"). isOverride annotates tier-level overrides.
func fmtOptF(p *float64, isOverride bool, nilLabel, verb string) string {
	if p == nil {
		return nilLabel
	}
	val := fmt.Sprintf(verb, *p)
	if isOverride {
		return val + "  (tier override)"
	}
	return val
}

// fmtOptI formats an optional *int. nilLabel is shown when the pointer is nil.
func fmtOptI(p *int, isOverride bool, nilLabel string) string {
	if p == nil {
		return nilLabel
	}
	val := fmt.Sprintf("%d", *p)
	if isOverride {
		return val + "  (tier override)"
	}
	return val
}

// fmtStop formats a stop sequence list. Empty slice renders as "—".
func fmtStop(stop []string, isOverride bool) string {
	if len(stop) == 0 {
		return "—"
	}
	val := strings.Join(stop, ", ")
	if isOverride {
		return val + "  (tier override)"
	}
	return val
}

// ── config validate ──────────────────────────────────────────────────────────

func runConfigValidate() error {
	var warnings, errors []string
	warn := func(msg string) { warnings = append(warnings, msg) }
	fail := func(msg string) { errors = append(errors, msg) }

	// ── config.yaml ──
	cfgPath, err := config.Path()
	if err != nil {
		fail("cannot resolve config path: " + err.Error())
	} else {
		data, err := os.ReadFile(cfgPath)
		if os.IsNotExist(err) {
			warn("config.yaml does not exist (will use defaults)")
		} else if err != nil {
			fail("cannot read config.yaml: " + err.Error())
		} else {
			var probe map[string]any
			if err := yaml.Unmarshal(data, &probe); err != nil {
				fail("config.yaml parse error: " + err.Error())
			}
		}
	}

	cfg, err := config.Load(nil)
	if err != nil {
		fail("config load error: " + err.Error())
	} else {
		// Check tiers have models.
		emptyTiers := 0
		for _, t := range config.TierOrder {
			if len(cfg.TierModels(t)) == 0 {
				emptyTiers++
			}
		}
		if emptyTiers == len(config.TierOrder) {
			warn("no models assigned to any tier — run: iq tier add <tier> <model>")
		}

		// Check embed model.
		if cfg.EmbedModel == "" {
			warn("embed_model not set (using default: " + config.DefaultEmbedModel + ")")
		}

		// Check deprecated fields.
		if cfg.CueModel != "" {
			warn("deprecated field cue_model found — should auto-migrate to embed_model")
		}
		if cfg.KbModel != "" {
			warn("deprecated field kb_model found — should auto-migrate to embed_model")
		}

		// Check tool_paths exist.
		for _, p := range cfg.ToolPaths {
			if _, err := os.Stat(p); err != nil {
				warn(fmt.Sprintf("tool_path %q does not exist", p))
			}
		}

		// Validate inference params.
		gp := config.ResolveInferParams(cfg, "")
		if gp.Temperature < 0 || gp.Temperature > 2 {
			fail(fmt.Sprintf("temperature %.1f is out of range [0, 2]", gp.Temperature))
		}
		if gp.RepetitionPenalty < 1 || gp.RepetitionPenalty > 3 {
			warn(fmt.Sprintf("repetition_penalty %.1f is unusual (typical: 1.0–2.0)", gp.RepetitionPenalty))
		}
		if gp.MaxTokens < 1 {
			fail("max_tokens must be positive")
		}
	}

	// ── cues.yaml ──
	cues, cueErr := cue.Load()
	if cueErr != nil {
		fail("cues.yaml error: " + cueErr.Error())
	} else {
		if len(cues) == 0 {
			fail("cues.yaml is empty — no cues defined")
		}
		names := map[string]bool{}
		for _, c := range cues {
			if names[c.Name] {
				fail(fmt.Sprintf("duplicate cue name: %q", c.Name))
			}
			names[c.Name] = true
			if c.Category == "" {
				warn(fmt.Sprintf("cue %q has no category", c.Name))
			}
		}
		if !names["initial"] {
			warn("no 'initial' cue (fallback catch-all) defined")
		}
	}

	// ── KB ──
	if kb.Exists() {
		_, kbErr := kb.Load()
		if kbErr != nil {
			fail("kb.json error: " + kbErr.Error())
		}
	}

	// ── Print results ──
	for _, e := range errors {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", color.Red("ERROR"), e)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", color.Yel("WARN "), w)
	}
	if len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("  %s  configuration is valid\n", color.Grn("OK"))
	} else if len(errors) == 0 {
		fmt.Printf("\n  %s  %d warning(s), no errors\n", color.Grn("OK"), len(warnings))
	} else {
		fmt.Fprintf(os.Stderr, "\n  %s  %d error(s), %d warning(s)\n",
			color.Red("FAIL"), len(errors), len(warnings))
		return fmt.Errorf("config validation failed")
	}
	return nil
}
