package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"iq/internal/config"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/lm"
	"iq/internal/sidecar"
)

// pickSidecar wraps sidecar.PickSidecar with the lm.DiskUsage resolver.
func pickSidecar(tier string, preferSmallest bool) (*sidecar.State, error) {
	return sidecar.PickSidecar(tier, preferSmallest, func(modelID string) int64 {
		return lm.DiskUsage(lm.HFCacheDir(modelID))
	})
}

// startSidecar resolves model/python paths and delegates to sidecar.StartInfer.
func startSidecar(tier, modelID string) error {
	modelPath, err := lm.SnapshotDir(modelID)
	if err != nil {
		return fmt.Errorf("cannot resolve model path: %w", err)
	}
	pyPath, err := embed.MlxVenvPython()
	if err != nil {
		return fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}
	_, err = sidecar.StartInfer(tier, modelID, modelPath, pyPath)
	return err
}

// startEmbedSidecar resolves config and delegates to embed.StartSidecar.
func startEmbedSidecar() error {
	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}
	return embed.StartSidecar(config.EmbedModel(cfg), func(modelID string) error {
		return lm.RegisterInManifest(modelID)
	})
}

// resolveModels returns the model IDs to act on given an optional arg.
// Arg may be a tier name ("fast"/"slow"), a model ID, or empty (all assigned).
func resolveModels(arg string) ([]string, error) {
	cfg, err := config.Load(nil)
	if err != nil {
		return nil, err
	}
	if arg == "" {
		all := config.AllAssignedModels()
		if len(all) == 0 {
			return nil, fmt.Errorf("no models assigned — run 'iq tier add <tier> <model>' first")
		}
		return all, nil
	}
	// Tier name?
	for _, t := range config.TierOrder {
		if t == arg {
			models := cfg.TierModels(t)
			if len(models) == 0 {
				return nil, fmt.Errorf("tier %q has no models assigned", arg)
			}
			return models, nil
		}
	}
	// Model ID?
	for _, t := range config.TierOrder {
		for _, m := range cfg.TierModels(t) {
			if m == arg {
				return []string{m}, nil
			}
		}
	}
	return nil, fmt.Errorf("%q is not a recognised tier or assigned model", arg)
}

// ── Status (shared logic) ─────────────────────────────────────────────────────

func printStatus() error {
	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}

	type statusRow struct {
		tier     string
		model    string
		endpoint string
		pid      int
		uptime   string
		running  bool
		mem      string
	}

	var rows []statusRow
	var totalKB int64

	for _, tier := range config.TierOrder {
		for _, model := range cfg.TierModels(tier) {
			state, _ := sidecar.ReadState(model)
			endpoint := ""
			if state != nil {
				endpoint = sidecar.Endpoint(state.Port)
			}
			if state == nil || !sidecar.PidAlive(state.PID) {
				rows = append(rows, statusRow{tier, model, endpoint, 0, "—", false, "—"})
				continue
			}
			rss := sidecar.ProcessRSSKB(state.PID)
			totalKB += rss
			mem := lm.FormatMB(rss * 1024)
			if rss == 0 {
				mem = "?"
			}
			rows = append(rows, statusRow{tier, model, endpoint, state.PID, sidecar.FormatUptime(state.Started), true, mem})
		}
	}

	// Embed sidecar row.
	{
		slug := embed.SlugConst
		model := config.EmbedModel(cfg)
		eState, _ := sidecar.ReadState(slug)
		endpoint := ""
		if eState != nil {
			endpoint = sidecar.Endpoint(eState.Port)
		}
		if eState == nil || !sidecar.PidAlive(eState.PID) {
			rows = append(rows, statusRow{slug, model, endpoint, 0, "—", false, "—"})
		} else {
			rss := sidecar.ProcessRSSKB(eState.PID)
			totalKB += rss
			mem := lm.FormatMB(rss * 1024)
			if rss == 0 {
				mem = "?"
			}
			rows = append(rows, statusRow{slug, model, endpoint, eState.PID, sidecar.FormatUptime(eState.Started), true, mem})
		}
	}
	// Compute TIER column width dynamically (longest tier name).
	tierW := len("TIER")
	for _, r := range rows {
		if len(r.tier) > tierW {
			tierW = len(r.tier)
		}
	}
	tierW += 2

	// Compute MODEL column width.
	modelW := len("MODEL")
	for _, r := range rows {
		if len(r.model) > modelW {
			modelW = len(r.model)
		}
	}
	modelW += 2

	// CONFIG line aligned with MODEL column.
	cfgPath, _ := config.Path()
	fmt.Printf("%-*s  %-*s\n", tierW, "CONFIG", modelW, cfgPath)

	// Header.
	fmt.Printf("%-*s  %-*s  %-28s  %-7s  %-8s  %-7s  %8s\n",
		tierW, "TIER", modelW, "MODEL", "ENDPOINT", "PID", "UPTIME", "RUNNING", "MEM")

	for _, r := range rows {
		// Pad the raw string to fixed width BEFORE colorizing — ANSI escape codes
		// added by utl.Gre/Gra inflate len() and break %-Ns alignment.
		runRaw := fmt.Sprintf("%-7s", "no")
		runDisplay := utl.Gra(runRaw)
		if r.running {
			runDisplay = utl.Gre(fmt.Sprintf("%-7s", "yes"))
		}
		if !r.running {
			fmt.Printf("%-*s  %-*s  %-28s  %-7s  %-8s  %s  %8s\n",
				tierW, r.tier, modelW, r.model, r.endpoint, "—", r.uptime, runDisplay, r.mem)
		} else {
			fmt.Printf("%-*s  %-*s  %-28s  %-7d  %-8s  %s  %8s\n",
				tierW, r.tier, modelW, r.model, r.endpoint, r.pid, r.uptime, runDisplay, r.mem)
		}
	}

	iqRSS := sidecar.ProcessRSSKB(os.Getpid())
	totalKB += iqRSS
	// Last line: IQ mem left-aligned, total mem right-aligned to MEM column.
	// Column positions: 6+2 + modelW+2 + 28+2 + 7+2 + 8+2 + 7+2 + 8 = left of MEM column
	lineW := tierW + 2 + modelW + 2 + 28 + 2 + 7 + 2 + 8 + 2 + 7 + 2 + 8
	iqLabel := "IQ process mem:"
	iqVal := lm.FormatMB(iqRSS * 1024)
	totLabel := "Total mem:"
	totVal := lm.FormatMB(totalKB * 1024)
	// Build the line: left part + right part padded to lineW.
	left := fmt.Sprintf("%-20s %s", iqLabel, iqVal)
	right := fmt.Sprintf("%s  %8s", totLabel, totVal)
	gap := max(lineW-len(left)-len(right), 2)
	fmt.Printf("%s%s%s\n", left, strings.Repeat(" ", gap), right)
	return nil
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printSvcHelp() {
	n := program_name
	fmt.Printf("Legacy alias — svc commands have moved to the root.\n\n")
	fmt.Printf("%s\n", utl.Whi2("NEW USAGE"))
	fmt.Printf("  %s start [tier|model]\n", n)
	fmt.Printf("  %s stop [tier|model]\n", n)
	fmt.Printf("  %s status\n", n)
	fmt.Printf("  %s tier show|add|rm\n", n)
	fmt.Printf("  %s embed show|set|rm\n", n)
	fmt.Printf("  %s doc\n\n", n)
}

// ── Root svc command ──────────────────────────────────────────────────────────

// newSvcCmd returns a hidden backward-compat alias that delegates to the
// new root-level commands (start, stop, tier, embed, doc).
func newSvcCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "svc",
		Hidden:       true,
		Short:        "Legacy alias — use root-level commands instead",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printSvcHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printSvcHelp()
	})
	cmd.AddCommand(
		newSvcStatusCmd(),
		newStartCmd(),
		newStopCmd(),
		newTierCmd(),
		newEmbedCmd(),
		newDocCmd(),
	)
	return cmd
}

// ── status ────────────────────────────────────────────────────────────────────

func newSvcStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Aliases:      []string{"st"},
		Short:        "Show running sidecar status and memory usage",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printStatus()
		},
	}
}

// ── start ─────────────────────────────────────────────────────────────────────

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "start [tier|model]",
		Short:        "Start sidecars for all, a tier pool, or a specific model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) > 0 {
				arg = args[0]
			}
			// Start embed sidecar first when starting everything (no specific target).
			if arg == "" {
				// First-run hint: no tier models and embed model not downloaded.
				if len(config.AllAssignedModels()) == 0 {
					cfg, err := config.Load(nil)
					if err != nil {
						return err
					}
					emDir := lm.HFCacheDir(config.EmbedModel(cfg))
					if _, err := os.Stat(emDir); err != nil {
						fmt.Println("No models configured. Recommended setup:")
						fmt.Println()
						fmt.Println("  iq lm get mlx-community/bge-small-en-v1.5-bf16")
						fmt.Println("  iq lm get mlx-community/Llama-3.2-3B-Instruct-4bit")
						fmt.Println("  iq lm get mlx-community/Qwen2.5-7B-Instruct-4bit")
						fmt.Println()
						fmt.Println("  iq embed set mlx-community/bge-small-en-v1.5-bf16")
						fmt.Println("  iq tier add fast mlx-community/Llama-3.2-3B-Instruct-4bit")
						fmt.Println("  iq tier add slow mlx-community/Qwen2.5-7B-Instruct-4bit")
						fmt.Println()
						fmt.Println("Then run 'iq start' again.")
						return nil
					}
				}
				if err := startEmbedSidecar(); err != nil {
					fmt.Fprintf(os.Stderr, "  error starting embed: %s\n", err.Error())
				}
				// Hint when embed started but no tier models configured yet.
				if len(config.AllAssignedModels()) == 0 {
					fmt.Println("No models configured. Recommended setup:")
					fmt.Println()
					fmt.Println("  iq lm get mlx-community/Llama-3.2-3B-Instruct-4bit")
					fmt.Println("  iq lm get mlx-community/Qwen2.5-7B-Instruct-4bit")
					fmt.Println()
					fmt.Println("  iq tier add fast mlx-community/Llama-3.2-3B-Instruct-4bit")
					fmt.Println("  iq tier add slow mlx-community/Qwen2.5-7B-Instruct-4bit")
					fmt.Println()
					fmt.Println("Then run 'iq start' again.")
					return nil
				}
			}
			models, err := resolveModels(arg)
			if err != nil {
				return err
			}
			for _, modelID := range models {
				tier := config.TierForModel(modelID)
				state, _ := sidecar.ReadState(modelID)
				if state != nil && sidecar.PidAlive(state.PID) {
					fmt.Printf("  pid %-7d  %s  %s\n",
						state.PID, sidecar.Endpoint(state.Port), utl.Gra("already running"))
					continue
				}
				if err := startSidecar(tier, modelID); err != nil {
					fmt.Fprintf(os.Stderr, "  error starting %s: %s\n", modelID, err.Error())
				}
			}
			return nil
		},
	}
}

// ── stop ──────────────────────────────────────────────────────────────────────

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stop [tier|model]",
		Short:        "Stop sidecars for all, a tier pool, or a specific model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) > 0 {
				arg = args[0]
			}
			models, err := resolveModels(arg)
			if err != nil {
				return err
			}
			for _, modelID := range models {
				if err := sidecar.Stop(modelID); err != nil {
					fmt.Fprintf(os.Stderr, "  error stopping %s: %s\n", modelID, err.Error())
				}
			}
			// Stop embed sidecar and sweep for orphans when stopping everything.
			if arg == "" {
				if err := sidecar.Stop(embed.SlugConst); err != nil {
					fmt.Fprintf(os.Stderr, "  error stopping embed: %s\n", err.Error())
				}
				sidecar.KillOrphanSidecars()
			}
			return nil
		},
	}
}

// ── tier ──────────────────────────────────────────────────────────────────────

func printTierHelp() {
	n := program_name
	fmt.Printf("Manage model tier pool assignments.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s tier <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "show", "Show current tier assignments")
	fmt.Printf("  %-10s %s\n", "add", "Add a model to a tier pool")
	fmt.Printf("  %-10s %s\n\n", "rm", "Remove a model from a tier pool")
	fmt.Printf("%s\n", utl.Whi2("TIERS"))
	fmt.Printf("  %-8s %s\n", "fast", "Sub-2GB models — used for classification and quick tasks")
	fmt.Printf("  %-8s %s\n\n", "slow", "2GB+ models — used for quality inference")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s tier show\n", n)
	fmt.Printf("  $ %s tier add fast mlx-community/SmolLM2-135M-Instruct-8bit\n", n)
	fmt.Printf("  $ %s tier add slow mlx-community/Phi-4-mini-reasoning-4bit\n", n)
	fmt.Printf("  $ %s tier rm fast mlx-community/SmolLM2-135M-Instruct-8bit\n\n", n)
}

func newTierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "tier",
		Short:        "Manage model tier pool assignments",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printTierHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printTierHelp()
	})
	cmd.AddCommand(newTierShowCmd(), newTierAddCmd(), newTierRmCmd())
	return cmd
}

func newTierShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show current tier assignments",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			for _, t := range config.TierOrder {
				models := cfg.TierModels(t)
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

func newTierAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add <tier> <model>",
		Short:        "Add a model to a tier pool",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			tier, modelID := args[0], args[1]
			if tier != "fast" && tier != "slow" {
				return fmt.Errorf("unknown tier %q — valid tiers: fast, slow", tier)
			}
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			if slices.Contains(cfg.TierModels(tier), modelID) {
				fmt.Printf("%s is already in the %s tier\n", modelID, tier)
				return nil
			}
			other := "slow"
			if tier == "slow" {
				other = "fast"
			}
			for i, m := range cfg.TierModels(other) {
				if m == modelID {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(
						fmt.Sprintf("warning: %s moved from %s to %s", modelID, other, tier)))
					cfg.Tiers[other].Models = append(cfg.Tiers[other].Models[:i], cfg.Tiers[other].Models[i+1:]...)
					break
				}
			}
			cfg.Tiers[tier].Models = append(cfg.Tiers[tier].Models, modelID)
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("%-6s  %s\n", tier, utl.Gre(modelID))
			return nil
		},
	}
}

func newTierRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <tier> <model>",
		Short:        "Remove a model from a tier pool",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			tier, modelID := args[0], args[1]
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			found := false
			for i, m := range cfg.TierModels(tier) {
				if m == modelID {
					cfg.Tiers[tier].Models = append(cfg.Tiers[tier].Models[:i], cfg.Tiers[tier].Models[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s is not in the %s tier", modelID, tier)
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("removed %s from %s tier\n", modelID, tier)
			return nil
		},
	}
}

// ── embed ─────────────────────────────────────────────────────────────────────

func printEmbedHelp() {
	n := program_name
	fmt.Printf("Manage the MLX embed model for cue classification and KB retrieval.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s embed <command>\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-24s %s\n", "show", "Show the configured embed model")
	fmt.Printf("  %-24s %s\n", "set <model>", "Set embed model and restart sidecar")
	fmt.Printf("  %-24s %s\n\n", "rm", "Revert embed model to default and restart sidecar")
	fmt.Printf("%s\n", utl.Whi2("NOTES"))
	fmt.Printf("  Models are HF model IDs (mlx-community/*). The model must be\n")
	fmt.Printf("  downloaded first with 'iq lm get <model>'.\n")
	fmt.Printf("  Changing embed model invalidates kb.json — re-ingest required.\n\n")
	fmt.Printf("%s\n", utl.Whi2("DEFAULT"))
	fmt.Printf("  %s\n\n", utl.Gra(config.DefaultEmbedModel))
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s embed show\n", n)
	fmt.Printf("  $ %s embed set mlx-community/bge-small-en-v1.5-bf16\n\n", n)
}

func newEmbedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "embed",
		Short:        "Manage embed sidecar models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printEmbedHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printEmbedHelp()
	})
	cmd.AddCommand(newEmbedShowCmd(), newEmbedSetCmd(), newEmbedRmCmd())
	return cmd
}

func newEmbedShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show configured embed model",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			suffix := ""
			if cfg.EmbedModel == "" {
				suffix = utl.Gra("  (default)")
			}
			fmt.Printf("embed_model  %s%s\n", utl.Gre(config.EmbedModel(cfg)), suffix)
			return nil
		},
	}
}

func newEmbedSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set <model>",
		Short:        "Set embed model and restart sidecar",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			modelName := args[0]
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			cfg.EmbedModel = modelName
			if err := config.Save(cfg); err != nil {
				return err
			}
			embed.InvalidateCueEmbeddings()
			fmt.Printf("embed_model  %s\n", utl.Gre(modelName))
			kbP, _ := kb.Path()
			if _, err := os.Stat(kbP); err == nil {
				fmt.Printf("%s\n", utl.Yel("warning: embed_model changed — existing kb.json is stale"))
				fmt.Printf("%s\n", utl.Gra("  run: iq kb clear && iq kb ingest <path>"))
			}
			// Stop old sidecar and start fresh with the new model.
			sidecar.Stop(embed.SlugConst)
			return startEmbedSidecar()
		},
	}
}

func newEmbedRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm",
		Short:        "Revert embed model to default and restart sidecar",
		SilenceUsage: true,
		Args:         argsUsage(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}
			cfg.EmbedModel = ""
			if err := config.Save(cfg); err != nil {
				return err
			}
			embed.InvalidateCueEmbeddings()
			fmt.Printf("embed_model  %s\n", utl.Gra("(default) "+config.DefaultEmbedModel))
			kbP, _ := kb.Path()
			if _, err := os.Stat(kbP); err == nil {
				fmt.Printf("%s\n", utl.Yel("warning: embed_model changed — existing kb.json is stale"))
				fmt.Printf("%s\n", utl.Gra("  run: iq kb clear && iq kb ingest <path>"))
			}
			// Stop old sidecar and start fresh with the default model.
			sidecar.Stop(embed.SlugConst)
			return startEmbedSidecar()
		},
	}
}

// ── doc ───────────────────────────────────────────────────────────────────────

type docCheck struct {
	label  string
	detail string
	ok     bool
	warn   bool
}

func runDocCheck(label, detail string, ok bool, warn bool) docCheck {
	return docCheck{label: label, detail: detail, ok: ok, warn: warn}
}

// checkCommand resolves an executable by searching Go's inherited PATH plus
// common user install locations that shell rc files normally add.
func checkCommand(name string, versionFlag string) (path, version string) {
	home, _ := os.UserHomeDir()
	extraDirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	}
	augmented := os.Getenv("PATH")
	for _, d := range extraDirs {
		if !strings.Contains(augmented, d) {
			augmented = d + ":" + augmented
		}
	}
	for _, dir := range filepath.SplitList(augmented) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			path = candidate
			break
		}
	}
	if path == "" {
		return "", ""
	}
	if versionFlag != "" {
		out, err := exec.Command(path, versionFlag).Output()
		if err != nil {
			out, _ = exec.Command(path, versionFlag).CombinedOutput()
		}
		version = strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	}
	return path, version
}

func newDocCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doc",
		Short:        "Check runtime dependencies and model readiness",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []docCheck
			allOK := true

			// ── python3 ──
			pyPath, pyVer := checkCommand("python3", "--version")
			pyOK := pyPath != ""
			detail := utl.Gra("not found")
			if pyOK {
				detail = fmt.Sprintf("%s  %s", pyPath, utl.Gra(pyVer))
			}
			checks = append(checks, runDocCheck("python3", detail, pyOK, false))

			// ── mlx_lm.server ──
			serverPath, _ := checkCommand("mlx_lm.server", "")
			serverOK := serverPath != ""
			var serverDetail string
			if serverOK {
				serverDetail = utl.Gra(serverPath)
			} else {
				serverDetail = utl.Gra("not found — install with: pipx install mlx-lm")
			}
			checks = append(checks, runDocCheck("mlx_lm.server", serverDetail, serverOK, false))

			// ── slow subprocess checks — run concurrently ──
			venvPy, pyVenvErr := embed.MlxVenvPython()
			var (
				wg                     sync.WaitGroup
				flagCheck, embPkgCheck docCheck
			)
			if serverOK {
				wg.Go(func() {
					helpOut, _ := exec.Command(serverPath, "--help").CombinedOutput()
					ok := strings.Contains(string(helpOut), "--model")
					d := utl.Gra("--model flag supported")
					if !ok {
						d = utl.Gra("--model flag not found — upgrade mlx_lm")
					}
					flagCheck = runDocCheck("  --model flag", d, ok, false)
				})
			}
			wg.Go(func() {
				if pyVenvErr != nil {
					embPkgCheck = runDocCheck("mlx-embedding-models pkg", utl.Gra("cannot check — "+pyVenvErr.Error()), false, false)
					return
				}
				out, err := exec.Command(venvPy, "-c", "import mlx_embedding_models").CombinedOutput()
				ok := err == nil
				d := utl.Gra("ok")
				if !ok {
					d = utl.Gra("not found — run: pipx inject mlx-lm mlx-embedding-models\n" + strings.TrimSpace(string(out)))
				}
				embPkgCheck = runDocCheck("mlx-embedding-models pkg", d, ok, false)
			})
			wg.Wait()
			if serverOK {
				checks = append(checks, flagCheck)
			}
			checks = append(checks, embPkgCheck)

			// ── embed model cache ──
			cfg2, cfgErr2 := config.Load(nil)
			if cfgErr2 == nil {
				emID := config.EmbedModel(cfg2)
				cacheDir := lm.HFCacheDir(emID)
				_, statErr := os.Stat(cacheDir)
				ok := statErr == nil
				var d string
				if ok {
					parent := filepath.Dir(cacheDir)
					d = utl.Gra(parent+"/") + utl.Whi(filepath.Base(cacheDir))
				} else {
					d = utl.Gra(fmt.Sprintf("cache not found — run: iq lm get %s", emID))
				}
				checks = append(checks, runDocCheck("  embed_model", d, ok, false))
			}

			// ── tier model cache dirs ──
			cfg, cfgErr := config.Load(nil)
			if cfgErr != nil {
				return cfgErr
			}
			for _, t := range config.TierOrder {
				for _, model := range cfg.TierModels(t) {
					cacheDir := lm.HFCacheDir(model)
					_, statErr := os.Stat(cacheDir)
					modelOK := statErr == nil
					if modelOK {
						parent := filepath.Dir(cacheDir)
						detail = utl.Gra(parent+"/") + utl.Whi(filepath.Base(cacheDir))
					} else {
						detail = utl.Gra(fmt.Sprintf("cache not found — run: iq lm get %s", model))
					}
					checks = append(checks, runDocCheck(t, detail, modelOK, false))
				}
			}
			if len(cfg.TierModels("fast")) == 0 && len(cfg.TierModels("slow")) == 0 {
				checks = append(checks, runDocCheck("tier models", utl.Gra("no models assigned"), true, true))
			}

			// ── print results ──
			colW := len("CHECK")
			for _, c := range checks {
				if len(c.label) > colW {
					colW = len(c.label)
				}
			}
			colW += 2
			fmt.Printf("%-*s  %-6s  %s\n", colW, "CHECK", "STATUS", "DETAIL")
			for _, c := range checks {
				var statusRaw, status string
				switch {
				case c.ok:
					statusRaw = "ok"
					status = utl.Gre(fmt.Sprintf("%-6s", statusRaw))
				case c.warn:
					statusRaw = "warn"
					status = utl.Gra(fmt.Sprintf("%-6s", statusRaw))
				default:
					statusRaw = "FAIL"
					status = utl.Gra(fmt.Sprintf("%-6s", statusRaw))
					allOK = false
				}
				fmt.Printf("%-*s  %s  %s\n", colW, c.label, status, c.detail)
			}

			if !allOK {
				return fmt.Errorf("one or more checks failed — resolve the above before running 'iq start'")
			}
			fmt.Printf("%s\n", utl.Gre("All checks passed."))
			return nil
		},
	}
}
