package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"iq/internal/color"
	"iq/internal/config"
	iembed "iq/internal/embed"
	"iq/internal/lm"
	"iq/internal/sidecar"
)

// pickAnySidecar returns the first live inference sidecar (non-embed).
func pickAnySidecar() (*sidecar.State, error) {
	live, err := sidecar.AllLiveStates()
	if err != nil {
		return nil, err
	}
	for _, sc := range live {
		if sc.Tier != "embed" {
			return sc, nil
		}
	}
	return nil, fmt.Errorf("no running inference sidecars — run 'kb start <model>'")
}

// startSidecar resolves model/python paths and delegates to sidecar.StartInfer.
func startSidecar(modelID string) error {
	modelPath, err := lm.SnapshotDir(modelID)
	if err != nil {
		return fmt.Errorf("cannot resolve model path: %w", err)
	}
	pyPath, err := iembed.MlxVenvPython()
	if err != nil {
		return fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}
	_, err = sidecar.StartInfer(modelID, modelPath, pyPath)
	return err
}

// startEmbedSidecar resolves config and delegates to embed.StartSidecar.
func startEmbedSidecar() error {
	cfg, err := loadKBConfig()
	if err != nil {
		return err
	}
	return iembed.StartSidecar(config.EmbedModel(cfg), func(modelID string) error {
		return lm.RegisterInManifest(modelID)
	})
}

// ── Status ────────────────────────────────────────────────────────────────────

func printStatus() error {
	cfg, err := loadKBConfig()
	if err != nil {
		return err
	}

	type statusRow struct {
		role     string
		model    string
		endpoint string
		pid      int
		uptime   string
		running  bool
		mem      string
	}

	var rows []statusRow
	var totalKB int64

	// Inference pool models from kb config.
	for _, model := range cfg.AllModels() {
		state, _ := sidecar.ReadState(model)
		endpoint := ""
		if state != nil {
			endpoint = sidecar.Endpoint(state.Port)
		}
		if state == nil || !sidecar.PidAlive(state.PID) {
			rows = append(rows, statusRow{"infer", model, endpoint, 0, "—", false, "—"})
			continue
		}
		rss := sidecar.ProcessRSSKB(state.PID)
		totalKB += rss
		mem := lm.FormatMB(rss * 1024)
		if rss == 0 {
			mem = "?"
		}
		rows = append(rows, statusRow{"infer", model, endpoint, state.PID, sidecar.FormatUptime(state.Started), true, mem})
	}

	// Embed sidecar row.
	{
		slug := iembed.SlugConst
		model := config.EmbedModel(cfg)
		eState, _ := sidecar.ReadState(slug)
		endpoint := ""
		if eState != nil {
			endpoint = sidecar.Endpoint(eState.Port)
		}
		if eState == nil || !sidecar.PidAlive(eState.PID) {
			rows = append(rows, statusRow{"embed", model, endpoint, 0, "—", false, "—"})
		} else {
			rss := sidecar.ProcessRSSKB(eState.PID)
			totalKB += rss
			mem := lm.FormatMB(rss * 1024)
			if rss == 0 {
				mem = "?"
			}
			rows = append(rows, statusRow{"embed", model, endpoint, eState.PID, sidecar.FormatUptime(eState.Started), true, mem})
		}
	}

	// Compute MODEL column width.
	modelW := len("MODEL")
	for _, r := range rows {
		if len(r.model) > modelW {
			modelW = len(r.model)
		}
	}
	modelW += 2

	// CONFIG line.
	dir, _ := kbDir()
	fmt.Printf("CONFIG  %s\n", dir)

	// Header.
	fmt.Printf("%-*s  %-28s  %-7s  %-8s  %-7s  %8s\n",
		modelW, "MODEL", "ENDPOINT", "PID", "UPTIME", "RUNNING", "MEM")

	for _, r := range rows {
		runRaw := fmt.Sprintf("%-7s", "no")
		runDisplay := color.Gra(runRaw)
		if r.running {
			runDisplay = color.Grn(fmt.Sprintf("%-7s", "yes"))
		}
		if !r.running {
			fmt.Printf("%-*s  %-28s  %-7s  %-8s  %s  %8s\n",
				modelW, r.model, r.endpoint, "—", r.uptime, runDisplay, r.mem)
		} else {
			fmt.Printf("%-*s  %-28s  %-7d  %-8s  %s  %8s\n",
				modelW, r.model, r.endpoint, r.pid, r.uptime, runDisplay, r.mem)
		}
	}

	kbRSS := sidecar.ProcessRSSKB(os.Getpid())
	totalKB += kbRSS
	lineW := modelW + 2 + 28 + 2 + 7 + 2 + 8 + 2 + 7 + 2 + 8
	kbLabel := "KB process mem:"
	kbVal := lm.FormatMB(kbRSS * 1024)
	totLabel := "Total mem:"
	totVal := lm.FormatMB(totalKB * 1024)
	left := fmt.Sprintf("%-20s %s", kbLabel, kbVal)
	right := fmt.Sprintf("%s  %8s", totLabel, totVal)
	gap := max(lineW-len(left)-len(right), 2)
	fmt.Printf("%s%s%s\n", left, strings.Repeat(" ", gap), right)
	return nil
}

// ── start ─────────────────────────────────────────────────────────────────────

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "start [model]",
		Short:        "Start embed sidecar, or a specific inference model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// Start embed only.
				return startEmbedSidecar()
			}
			// Start a specific inference model.
			modelID := args[0]
			state, _ := sidecar.ReadState(modelID)
			if state != nil && sidecar.PidAlive(state.PID) {
				fmt.Printf("  pid %-7d  %s  %s\n",
					state.PID, sidecar.Endpoint(state.Port), color.Gra("already running"))
				return nil
			}
			return startSidecar(modelID)
		},
	}
}

// ── stop ──────────────────────────────────────────────────────────────────────

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stop [model]",
		Short:        "Stop sidecars",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return sidecar.Stop(args[0])
			}
			// Stop embed and all live sidecars, then sweep orphans.
			if err := sidecar.Stop(iembed.SlugConst); err != nil {
				fmt.Fprintf(os.Stderr, "  error stopping embed: %s\n", err.Error())
			}
			live, _ := sidecar.AllLiveStates()
			for _, sc := range live {
				if sc.Tier != "embed" {
					if err := sidecar.Stop(sc.Model); err != nil {
						fmt.Fprintf(os.Stderr, "  error stopping %s: %s\n", sc.Model, err.Error())
					}
				}
			}
			sidecar.KillOrphanSidecars()
			return nil
		},
	}
}

// ── restart ───────────────────────────────────────────────────────────────────

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "restart [model]",
		Short:        "Restart sidecars (stop then start)",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				modelID := args[0]
				if err := sidecar.Stop(modelID); err != nil {
					fmt.Fprintf(os.Stderr, "  error stopping %s: %s\n", modelID, err.Error())
				}
				return startSidecar(modelID)
			}
			// Restart everything: stop all, then restart embed.
			if err := sidecar.Stop(iembed.SlugConst); err != nil {
				fmt.Fprintf(os.Stderr, "  error stopping embed: %s\n", err.Error())
			}
			live, _ := sidecar.AllLiveStates()
			for _, sc := range live {
				if sc.Tier != "embed" {
					if err := sidecar.Stop(sc.Model); err != nil {
						fmt.Fprintf(os.Stderr, "  error stopping %s: %s\n", sc.Model, err.Error())
					}
				}
			}
			sidecar.KillOrphanSidecars()
			return startEmbedSidecar()
		},
	}
}
