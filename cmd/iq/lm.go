package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/embed"
	"iq/internal/lm"
	"iq/internal/sidecar"
)

// ── Help ──────────────────────────────────────────────────────────────────────

func printLmHelp() {
	n := program_name
	fmt.Printf("Work with IQ language models.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s lm <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "search [query|count]", "Search MLX model registry; numeric arg sets result count")
	fmt.Printf("  %-10s %s\n", "get", "Download a model from the registry")
	fmt.Printf("  %-10s %s\n", "list", "List locally available models (alias: ls)")
	fmt.Printf("  %-10s %s\n", "show", "Show details for a model")
	fmt.Printf("  %-10s %s\n\n", "rm", "Remove a model")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("ARGUMENTS"))
	fmt.Printf("  A model name can be supplied as an argument.\n\n")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s lm search\n", n)
	fmt.Printf("  $ %s lm search gemma\n", n)
	fmt.Printf("  $ %s lm get mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s lm list\n", n)
	fmt.Printf("  $ %s lm show mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s lm rm mlx-community/gemma-3-1b-it-4bit\n\n", n)
}

// ── Root lm command ───────────────────────────────────────────────────────────

func newLmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "lm",
		Short:        "Work with IQ language models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printLmHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printLmHelp()
	})

	cmd.AddCommand(
		newLmSearchCmd(),
		newLmGetCmd(),
		newLmListCmd(),
		newLmShowCmd(),
		newLmRmCmd(),
	)
	return cmd
}

// ── search ────────────────────────────────────────────────────────────────────

func newLmSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:          "search [query|count]",
		Short:        "Search MLX model registry on Hugging Face",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				// Pure integer arg → treat as result count, not a search query.
				if n, err := strconv.Atoi(args[0]); err == nil {
					if n > limit {
						limit = n
					}
				} else {
					query = args[0]
				}
			}
			if limit < 20 {
				limit = 20
			}

			models, err := lm.HFSearch(query, limit)
			if err != nil {
				return err
			}
			lm.HFEnrichModels(models)

			fmt.Printf("%-60s  %-24s  %10s  %10s  %12s  %12s\n",
				"MODEL", "TASK", "DISK", "PARAMS", "EST MEM", "DOWNLOADS")
			for _, m := range models {
				disk := m.TotalSize()
				name := m.ID
				if len(name) > 60 {
					name = name[:59] + "…"
				}
				fmt.Printf("%-60s  %s  %10s  %10s  %12s  %12s\n",
					name,
					lm.FormatTaskCol(m.PipelineTag),
					lm.FormatMB(disk),
					lm.ParseParamsM(m.ID),
					lm.EstMemMB(disk),
					lm.FormatInt(m.Downloads),
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Max number of results to return")
	return cmd
}

// ── get ───────────────────────────────────────────────────────────────────────

func newLmGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <model>",
		Short:        "Download a model from the registry",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]

			// Check task type before downloading.
			if m, err := lm.HFFetchModel(model); err == nil && m.PipelineTag != "" && m.PipelineTag != "text-generation" {
				fmt.Fprintf(os.Stderr, "%s\n",
					utl.Yel(fmt.Sprintf("Warning: model task is %q — IQ only supports text-generation", m.PipelineTag)))
			}

			// Run via shell so it inherits the user's full PATH
			// (hf is often installed in a pip user bin dir not visible to exec directly).
			hfCmd := exec.Command("/bin/sh", "-c", "hf download "+shellescape(model))
			hfCmd.Env = os.Environ()

			stdout, err := hfCmd.StdoutPipe()
			if err != nil {
				return err
			}
			hfCmd.Stderr = os.Stderr

			if err := hfCmd.Start(); err != nil {
				return fmt.Errorf("failed to start: %w", err)
			}

			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}

			if err := hfCmd.Wait(); err != nil {
				return fmt.Errorf("get failed (is hf installed? pip install huggingface_hub[cli]): %w", err)
			}

			if err := lm.AddToManifest(model); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: failed to update manifest: "+err.Error()))
			}

			// Cache the pipeline_tag in the manifest.
			if entries, err := lm.LoadManifest(); err == nil {
				for i, e := range entries {
					if e.ID == model && e.Task == "" {
						tag := ""
						if m, err := lm.HFFetchModel(model); err == nil && m.PipelineTag != "" {
							tag = m.PipelineTag
						} else {
							tag = lm.InferTaskFromConfig(model)
						}
						if tag != "" {
							entries[i].Task = tag
							_ = lm.SaveManifest(entries)
						}
						break
					}
				}
			}

			tier := lm.SuggestTier(model)
			fmt.Printf("\nSuggested tier: %s\n", utl.Gre(tier))
			fmt.Printf("%s\n", utl.Gra(
				fmt.Sprintf("  iq tier add %s %s", tier, model)))

			return nil
		},
	}
}

// ── list / ls ─────────────────────────────────────────────────────────────────

func newLmListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List locally available models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := lm.LoadManifest()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("No models. Use 'iq lm get <model>' to download one.")
				return nil
			}

			// Backfill task tags from HF API for entries that don't have one cached.
			if lm.HFFetchTags(entries) {
				_ = lm.SaveManifest(entries)
			}

			fmt.Printf("%-55s  %-24s  %8s  %-10s  %8s  %10s  %s\n",
				"MODEL", "TASK", "DISK", "PULLED", "PARAMS", "EST MEM", "TIER")
			cfg, _ := config.Load(nil)
			emM := config.EmbedModel(cfg)
			for _, e := range entries {
				disk := lm.DiskUsage(lm.HFCacheDir(e.ID))
				pulled := ""
				if t, err := time.Parse(time.RFC3339, e.PulledAt); err == nil {
					pulled = t.Format("2006-01-02")
				}
				var tierDisplay string
				if e.ID == emM {
					tierDisplay = utl.Gre(fmt.Sprintf("%-6s", "embed"))
				} else {
					tier := config.TierForModel(e.ID)
					tierRaw := "<unset>"
					if tier != "" {
						tierRaw = tier
					}
					tierDisplay = utl.Gra(fmt.Sprintf("%-6s", tierRaw))
					if tier != "" {
						tierDisplay = utl.Gre(fmt.Sprintf("%-6s", tierRaw))
					}
				}
				fmt.Printf("%-55s  %s  %8s  %-10s  %8s  %10s  %s\n",
					e.ID,
					lm.FormatTaskCol(e.Task),
					lm.FormatMB(disk),
					pulled,
					lm.ParseParamsM(e.ID),
					lm.EstMemMB(disk),
					tierDisplay,
				)
			}
			return nil
		},
	}
}

// ── show ──────────────────────────────────────────────────────────────────────

func newLmShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show <model>",
		Short:        "Show details for a specific model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := lm.LoadManifest()
			if err != nil {
				return err
			}

			id := args[0]
			var entry *lm.ManifestEntry
			for i := range entries {
				if entries[i].ID == id {
					entry = &entries[i]
					break
				}
			}
			if entry == nil {
				return fmt.Errorf("model %q not found in manifest", id)
			}

			// Backfill task tag if missing — try HF API, then local config.json.
			if entry.Task == "" {
				tag := ""
				if m, err := lm.HFFetchModel(entry.ID); err == nil && m.PipelineTag != "" {
					tag = m.PipelineTag
				} else {
					tag = lm.InferTaskFromConfig(entry.ID)
				}
				if tag != "" {
					entry.Task = tag
					for i := range entries {
						if entries[i].ID == entry.ID {
							entries[i].Task = tag
							break
						}
					}
					_ = lm.SaveManifest(entries)
				}
			}

			cacheDir := lm.HFCacheDir(entry.ID)
			disk := lm.DiskUsage(cacheDir)
			pulled := ""
			if t, err := time.Parse(time.RFC3339, entry.PulledAt); err == nil {
				pulled = t.Format("2006-01-02")
			}

			fmt.Printf("%-12s %s\n", "MODEL", entry.ID)
			fmt.Printf("%-12s %s\n", "TASK", lm.FormatTask(entry.Task))

			// ── PERFORMANCE ───────────────────────────────────────────
			bs, bsErr := loadBenchStore()
			if bsErr == nil && bs != nil {
				results := resultsFor(bs, entry.ID, "")
				if len(results) == 0 {
					fmt.Printf("%-12s %s\n", "PERFORMANCE",
						utl.Gra("<not benchmarked>"))
				} else {
					first := true
					for _, r := range results {
						label := ""
						if first {
							label = "PERFORMANCE"
							first = false
						}
						fmt.Printf("%-12s %s\n", label,
							formatBenchRow(r))
					}
				}
			}

			fmt.Printf("%-12s %s\n", "PARAMS", lm.ParseParamsM(entry.ID))
			fmt.Printf("%-12s %s\n", "QUANT", lm.ParseQuant(entry.ID))
			fmt.Printf("%-12s %s\n", "DISK", lm.FormatMB(disk))
			fmt.Printf("%-12s %s\n", "EST MEM", lm.EstMemMB(disk))
			fmt.Printf("%-12s %s\n", "PULLED", pulled)
			fmt.Printf("%-12s %s\n", "CACHE", cacheDir)
			fmt.Printf("%-12s %s\n", "CUE", cue.ForModel(entry.ID))

			tier := config.TierForModel(entry.ID)
			if tier == "" {
				suggested := lm.SuggestTier(entry.ID)
				fmt.Printf("%-12s %s\n", "TIER", utl.Gra("<unset>"))
				fmt.Printf("%-12s %s\n", "",
					utl.Gra(fmt.Sprintf("iq tier add %s %s", suggested, entry.ID)))
			} else {
				fmt.Printf("%-12s %s\n", "TIER", utl.Gre(tier))
			}

			files, ferr := lm.SnapshotFiles(cacheDir)
			if ferr == nil && len(files) > 0 {
				fmt.Printf("\n%-44s  %15s\n", "FILES", "SIZE")
				for _, f := range files {
					fmt.Printf("  %-42s  %15s\n", f.Name, lm.Commatize(f.Size))
				}
			}
			return nil
		},
	}
}

// ── rm ────────────────────────────────────────────────────────────────────────

func newLmRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "rm <model>",
		Short:        "Remove a model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			cacheDir := lm.HFCacheDir(model)

			// Warn and auto-clear if model is the embed model.
			cfg, _ := config.Load(nil)
			if cfg != nil && model == config.EmbedModel(cfg) {
				s, _ := sidecar.ReadState(embed.SlugConst)
				if s != nil && sidecar.PidAlive(s.PID) {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Yel("warning: stopping embed sidecar"))
					if err := sidecar.Stop(embed.SlugConst); err != nil {
						return fmt.Errorf("failed to stop embed sidecar: %w", err)
					}
				}
				fmt.Fprintf(os.Stderr, "%s\n", utl.Yel("warning: clearing embed_model assignment"))
				cfg.EmbedModel = ""
				if err := config.Save(cfg); err != nil {
					return fmt.Errorf("failed to update config: %w", err)
				}
			}

			// Warn and auto-clear if model is assigned to a tier.
			if t := config.TierForModel(model); t != "" {
				state, _ := sidecar.ReadState(model)
				if state != nil && sidecar.PidAlive(state.PID) {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Yel("warning: stopping "+model+" sidecar"))
					if err := sidecar.Stop(model); err != nil {
						return fmt.Errorf("failed to stop sidecar: %w", err)
					}
				}
				fmt.Fprintf(os.Stderr, "%s\n", utl.Yel("warning: removing "+model+" from "+t+" tier"))
				// Reload config in case it was modified above.
				cfg, _ = config.Load(nil)
				if cfg != nil {
					for i, m := range cfg.TierModels(t) {
						if m == model {
							cfg.Tiers[t].Models = append(cfg.Tiers[t].Models[:i], cfg.Tiers[t].Models[i+1:]...)
							break
						}
					}
					if err := config.Save(cfg); err != nil {
						return fmt.Errorf("failed to update config: %w", err)
					}
				}
			}

			if !force {
				disk := lm.DiskUsage(cacheDir)
				fmt.Printf("%s [y/N] ", utl.Yel(fmt.Sprintf("Remove %s (%s)?", model, lm.FormatMB(disk))))
				var resp string
				fmt.Scanln(&resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			entry, found, err := lm.RemoveFromManifest(model)
			if err != nil {
				return fmt.Errorf("failed to update manifest: %w", err)
			}
			if !found {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: model not found in manifest"))
			}

			// Use entry's recorded cache path if available, fall back to derived path.
			dir := cacheDir
			if entry.HFCache != "" {
				dir = entry.HFCache
			}

			if _, err := os.Stat(dir); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: cache directory not found: "+dir))
				return nil
			}

			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("failed to remove cache: %w", err)
			}

			fmt.Printf("Removed %s\n", model)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

// ── shellescape ───────────────────────────────────────────────────────────────

// shellescape single-quotes a string for safe shell interpolation.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
