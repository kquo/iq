package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ── Config file ───────────────────────────────────────────────────────────────

// Config stores pools of models per tier.
// Stored as tiers: { "fast": ["model-a", "model-b"], "slow": ["model-c"] }
type Config struct {
	Tiers      map[string][]string `yaml:"tiers"`
	EmbedModel string              `yaml:"embed_model,omitempty"`
}

var tierOrder = []string{"fast", "slow"}

const defaultEmbedModel = "mlx-community/bge-small-en-v1.5-4bit"

// embedModel returns the configured embed model ID, falling back to the default.
func embedModel(cfg *Config) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return defaultEmbedModel
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

	// Try new format (pools).
	if yamlErr := yaml.Unmarshal(data, cfg); yamlErr == nil && cfg.Tiers != nil {
		// Validate that values are slices — if the old single-string format
		// was loaded, yaml.v3 will have silently ignored the type mismatch
		// and left slices empty. Detect that via an old-format probe.
		var probe struct {
			Tiers map[string]string `yaml:"tiers"`
		}
		if yaml.Unmarshal(data, &probe) == nil {
			_, hasOld := probe.Tiers["tiny"]
			_, hasOldB := probe.Tiers["balanced"]
			_, hasOldC := probe.Tiers["quality"]
			if hasOld || hasOldB || hasOldC {
				// Old four-tier format detected — migrate.
				cfg = migrateOldConfig(probe.Tiers)
				if err := saveConfig(cfg); err == nil {
					fmt.Fprintf(os.Stderr, "%s\n",
						utl.Gra("config.yaml migrated from 4-tier to 2-tier pool format"))
				}
				return cfg, nil
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
	// Old tier → new tier mapping for models we haven't downloaded yet.
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
		// Prefer disk-size classification if model is present.
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

// printModelTable prints a model table for the given model IDs, matching lm list format.
func printModelTable(modelIDs []string) {
	if len(modelIDs) == 0 {
		fmt.Println(utl.Gra("  (no models assigned)"))
		return
	}
	entries, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", err)
		return
	}
	byID := map[string]manifestEntry{}
	for _, e := range entries {
		byID[e.ID] = e
	}

	fmt.Printf("%-55s  %8s  %-10s  %8s  %10s  %s\n",
		"MODEL", "DISK", "PULLED", "PARAMS", "EST MEM", "TIER")
	for _, id := range modelIDs {
		tier := tierForModel(id)
		tierRaw := "<unset>"
		if tier != "" {
			tierRaw = tier
		}
		tierDisplay := utl.Gra(fmt.Sprintf("%-6s", tierRaw))
		if tier != "" {
			tierDisplay = utl.Gre(fmt.Sprintf("%-6s", tierRaw))
		}

		e, ok := byID[id]
		if !ok {
			// Model in config but not in manifest — show minimal row.
			fmt.Printf("%-55s  %8s  %-10s  %8s  %10s  %s\n",
				id, "?", "?", "?", "?", tierDisplay)
			continue
		}
		disk := diskUsage(hfCacheDir(e.ID))
		pulled := ""
		if t, err := time.Parse(time.RFC3339, e.PulledAt); err == nil {
			pulled = t.Format("2006-01-02")
		}
		fmt.Printf("%-55s  %8s  %-10s  %8s  %10s  %s\n",
			e.ID,
			formatMB(disk),
			pulled,
			parseParamsM(e.ID),
			estMemMB(disk),
			tierDisplay,
		)
	}
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printCfgHelp() {
	n := program_name
	fmt.Printf("Work with IQ configuration.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s cfg <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "show", "Show current configuration and assigned models")
	fmt.Printf("  %-10s %s\n\n", "tier", "Manage model tier pool assignments")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s cfg show\n", n)
	fmt.Printf("  $ %s cfg tier show\n", n)
	fmt.Printf("  $ %s cfg tier add fast mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s cfg tier rm fast mlx-community/gemma-3-1b-it-4bit\n\n", n)
}

// ── Root cfg command ──────────────────────────────────────────────────────────

func newCfgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "cfg",
		Short:        "Work with IQ configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printCfgHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printCfgHelp()
	})
	cmd.AddCommand(newCfgShowCmd(), newCfgTierCmd(), newCfgEmbedCmd())
	return cmd
}

// ── cfg show ──────────────────────────────────────────────────────────────────

func newCfgShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show current configuration and assigned models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := configPath()
			fmt.Printf("%-10s %s\n\n", "CONFIG", path)
			printModelTable(allAssignedModels())
			return nil
		},
	}
}

// ── cfg tier ──────────────────────────────────────────────────────────────────

func printCfgTierHelp() {
	n := program_name
	fmt.Printf("Manage model tier pool assignments.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s cfg tier <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "show", "Show current tier assignments")
	fmt.Printf("  %-10s %s\n", "add", "Add a model to a tier pool")
	fmt.Printf("  %-10s %s\n\n", "rm", "Remove a model from a tier pool")
	fmt.Printf("%s\n", utl.Whi2("TIERS"))
	fmt.Printf("  %-8s %s\n", "fast", "Sub-2GB models — used for classification and quick tasks")
	fmt.Printf("  %-8s %s\n\n", "slow", "2GB+ models — used for quality inference")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s cfg tier show\n", n)
	fmt.Printf("  $ %s cfg tier add fast mlx-community/SmolLM2-135M-Instruct-8bit\n", n)
	fmt.Printf("  $ %s cfg tier add slow mlx-community/Phi-4-mini-reasoning-4bit\n", n)
	fmt.Printf("  $ %s cfg tier rm fast mlx-community/SmolLM2-135M-Instruct-8bit\n\n", n)
}

func newCfgTierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "tier",
		Short:        "Manage model tier pool assignments",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printCfgTierHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printCfgTierHelp()
	})
	cmd.AddCommand(newCfgTierShowCmd(), newCfgTierAddCmd(), newCfgTierRmCmd())
	return cmd
}

func newCfgTierShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show current tier assignments",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			for _, t := range tierOrder {
				models := cfg.Tiers[t]
				if len(models) == 0 {
					fmt.Printf("%-6s  %s\n", t, utl.Gra("<empty>"))
				} else {
					for _, m := range models {
						fmt.Printf("%-6s  %s\n", t, utl.Gre(m))
					}
				}
			}
			return nil
		},
	}
}

func newCfgTierAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add <tier> <model>",
		Short:        "Add a model to a tier pool",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			tier, modelID := args[0], args[1]
			if tier != "fast" && tier != "slow" {
				return fmt.Errorf("unknown tier %q — valid tiers: fast, slow", tier)
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// Check not already in this tier.
			if slices.Contains(cfg.Tiers[tier], modelID) {
				fmt.Printf("%s is already in the %s tier\n", modelID, tier)
				return nil
			}
			// Warn if in the other tier.
			other := "slow"
			if tier == "slow" {
				other = "fast"
			}
			for i, m := range cfg.Tiers[other] {
				if m == modelID {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(
						fmt.Sprintf("warning: %s moved from %s to %s", modelID, other, tier)))
					cfg.Tiers[other] = append(cfg.Tiers[other][:i], cfg.Tiers[other][i+1:]...)
					break
				}
			}
			cfg.Tiers[tier] = append(cfg.Tiers[tier], modelID)
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("%-6s  %s\n", tier, utl.Gre(modelID))
			return nil
		},
	}
}

func newCfgTierRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <tier> <model>",
		Short:        "Remove a model from a tier pool",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			tier, modelID := args[0], args[1]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			found := false
			for i, m := range cfg.Tiers[tier] {
				if m == modelID {
					cfg.Tiers[tier] = append(cfg.Tiers[tier][:i], cfg.Tiers[tier][i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s is not in the %s tier", modelID, tier)
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Printf("removed %s from %s tier\n", modelID, tier)
			return nil
		},
	}
}

// ── cfg embed ─────────────────────────────────────────────────────────────────

func printCfgEmbedHelp() {
	n := program_name
	fmt.Printf("Set the embedding model used for cue classification.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s cfg embed <command>\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "show", "Show current embed model")
	fmt.Printf("  %-10s %s\n", "set", "Set the embed model")
	fmt.Printf("  %-10s %s\n\n", "rm", "Clear the embed model (revert to default)")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s cfg embed show\n", n)
	fmt.Printf("  $ %s cfg embed set mlx-community/bge-small-en-v1.5-mlx\n\n", n)
}

func newCfgEmbedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "embed",
		Short:        "Set the embedding model for cue classification",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printCfgEmbedHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printCfgEmbedHelp()
	})
	cmd.AddCommand(newCfgEmbedShowCmd(), newCfgEmbedSetCmd(), newCfgEmbedRmCmd())
	return cmd
}

func newCfgEmbedShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show current embed model",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			model := embedModel(cfg)
			suffix := ""
			if cfg.EmbedModel == "" {
				suffix = utl.Gra("  (default)")
			}
			fmt.Printf("embed_model  %s%s\n", utl.Gre(model), suffix)
			return nil
		},
	}
}

func newCfgEmbedSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set <model>",
		Short:        "Set the embed model",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			cfg.EmbedModel = args[0]
			if err := saveConfig(cfg); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("embed_model  %s\n", utl.Gre(args[0]))
			return nil
		},
	}
}

func newCfgEmbedRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm",
		Short:        "Clear embed model (revert to default)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			cfg.EmbedModel = ""
			if err := saveConfig(cfg); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("embed_model  %s\n", utl.Gra("(default) "+defaultEmbedModel))
			return nil
		},
	}
}
