package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

// ── Sidecar constants ─────────────────────────────────────────────────────────

const sidecarReadyTimeout = 120 * time.Second
const sidecarPollInterval = 500 * time.Millisecond
const portBase = 27002

// ── Model slug ────────────────────────────────────────────────────────────────

// modelSlug converts a model ID to a filesystem-safe name for state/log files.
// e.g. "mlx-community/SmolLM2-135M-Instruct-8bit" → "mlx-community--SmolLM2-135M-Instruct-8bit"
func modelSlug(id string) string {
	return strings.ReplaceAll(id, "/", "--")
}

// ── State file ────────────────────────────────────────────────────────────────

type svcState struct {
	Tier    string `json:"tier"`
	Model   string `json:"model"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Started string `json:"started"`
}

func runDir() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, "run")
	return d, os.MkdirAll(d, 0755)
}

func statePath(modelID string) (string, error) {
	d, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, modelSlug(modelID)+".json"), nil
}

func logPath(modelID string) (string, error) {
	d, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, modelSlug(modelID)+".log"), nil
}

func readState(modelID string) (*svcState, error) {
	path, err := statePath(modelID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s svcState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeState(s *svcState) error {
	path, err := statePath(s.Model)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// writeStateAs writes a svcState to the state file keyed by slug rather than
// s.Model. Used for embed sidecars whose Model field holds an HF model ID that
// would produce the wrong filename via modelSlug().
func writeStateAs(slug string, s *svcState) error {
	path, err := statePath(slug)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func removeState(modelID string) error {
	path, err := statePath(modelID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// allStates returns all state files in the run directory.
func allStates() ([]*svcState, error) {
	d, err := runDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	var states []*svcState
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(d, e.Name()))
		if err != nil {
			continue
		}
		var s svcState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, &s)
	}
	return states, nil
}

// allLiveStates returns only states where the process is still alive.
func allLiveStates() ([]*svcState, error) {
	all, err := allStates()
	if err != nil {
		return nil, err
	}
	var live []*svcState
	for _, s := range all {
		if pidAlive(s.PID) {
			live = append(live, s)
		}
	}
	return live, nil
}

// ── Port allocation ───────────────────────────────────────────────────────────

// nextAvailablePort returns the lowest port >= portBase not already used by a
// live sidecar state file.
func nextAvailablePort() (int, error) {
	states, err := allStates()
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, s := range states {
		if pidAlive(s.PID) {
			used[s.Port] = true
		}
	}
	for p := portBase; p < portBase+100; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available port in range %d–%d", portBase, portBase+99)
}

// ── Pool dispatcher ───────────────────────────────────────────────────────────

// pickSidecar returns a live sidecar for the given tier.
// If preferSmallest is true, it prefers the model with the smallest disk footprint
// (used for classification to minimise latency).
func pickSidecar(tier string, preferSmallest bool) (*svcState, error) {
	live, err := allLiveStates()
	if err != nil {
		return nil, err
	}
	var candidates []*svcState
	for _, s := range live {
		if s.Tier == tier {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no running %s-tier sidecar — run 'iq svc start %s'", tier, tier)
	}
	if preferSmallest && len(candidates) > 1 {
		best := candidates[0]
		bestDisk := diskUsage(hfCacheDir(best.Model))
		for _, c := range candidates[1:] {
			d := diskUsage(hfCacheDir(c.Model))
			if d > 0 && (bestDisk == 0 || d < bestDisk) {
				best = c
				bestDisk = d
			}
		}
		return best, nil
	}
	return candidates[0], nil
}

// ── Process helpers ───────────────────────────────────────────────────────────

func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func processRSSKB(pid int) int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	var kb int64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &kb)
	return kb
}

func formatUptime(since string) string {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return "?"
	}
	d := time.Since(t).Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// ── Sidecar lifecycle ─────────────────────────────────────────────────────────

func sidecarEndpoint(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

func printLastLogLines(logFile string, n int) {
	f, err := os.Open(logFile)
	if err != nil {
		return
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s\n", utl.Gra("--- last log lines ---"))
	for _, l := range lines {
		fmt.Fprintf(os.Stderr, "  %s\n", l)
	}
	fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("--- full log: "+logFile+" ---"))
	fmt.Fprintln(os.Stderr)
}

// startSidecar spawns an mlx_lm.server for the given tier/model, assigns a
// dynamic port, writes a state file, then polls /v1/models until ready.
func startSidecar(tier, modelID string) error {
	port, err := nextAvailablePort()
	if err != nil {
		return err
	}

	lf_path, err := logPath(modelID)
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(lf_path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	modelPath, snapErr := snapshotDir(modelID)
	if snapErr != nil {
		return fmt.Errorf("cannot resolve model path: %w", snapErr)
	}

	serverPath, _ := checkCommand("mlx_lm.server", "")
	if serverPath == "" {
		return fmt.Errorf("mlx_lm.server not found — run 'iq svc doc' for details")
	}
	cmd := exec.Command(serverPath, "--model", modelPath, "--port", strconv.Itoa(port))
	cmd.Env = os.Environ()
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("failed to start sidecar: %w", err)
	}
	lf.Close()

	if err := writeState(&svcState{
		Tier:    tier,
		Model:   modelID,
		PID:     cmd.Process.Pid,
		Port:    port,
		Started: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("started (pid %d) but failed to write state: %w", cmd.Process.Pid, err)
	}

	fmt.Printf("  %-6s  pid %-7d  %s  ",
		tier, cmd.Process.Pid, sidecarEndpoint(port))
	healthURL := fmt.Sprintf("%s/v1/models", sidecarEndpoint(port))
	deadline := time.Now().Add(sidecarReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("%s\n", utl.Gre("ready"))
				return nil
			}
		}
		if !pidAlive(cmd.Process.Pid) {
			fmt.Printf("%s\n", utl.Gra("failed"))
			printLastLogLines(lf_path, 10)
			return fmt.Errorf("sidecar process exited unexpectedly")
		}
		fmt.Print(".")
		time.Sleep(sidecarPollInterval)
	}

	fmt.Printf("%s\n", utl.Gra("timeout"))
	printLastLogLines(lf_path, 10)
	return fmt.Errorf("sidecar did not become ready within %s", sidecarReadyTimeout)
}

// stopSidecar sends SIGTERM to the sidecar for a model and removes its state file.
func stopSidecar(modelID string) error {
	state, err := readState(modelID)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Printf("  %s  %s\n", modelID, utl.Gra("not running"))
		return nil
	}
	if !pidAlive(state.PID) {
		fmt.Printf("  pid %-7d  %s  %s\n", state.PID, modelID, utl.Gra("already stopped (stale state removed)"))
		removeState(modelID)
		return nil
	}
	proc, err := os.FindProcess(state.PID)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to pid %d: %w", state.PID, err)
	}
	for range 20 {
		time.Sleep(500 * time.Millisecond)
		if !pidAlive(state.PID) {
			break
		}
	}
	if pidAlive(state.PID) {
		proc.Signal(syscall.SIGKILL)
	}
	removeState(modelID)
	fmt.Printf("  pid %-7d  %s  %s\n", state.PID, modelID, utl.Gra("stopped"))
	return nil
}

// resolveModels returns the model IDs to act on given an optional arg.
// Arg may be a tier name ("fast"/"slow"), a model ID, or empty (all assigned).
func resolveModels(arg string) ([]string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if arg == "" {
		all := allAssignedModels()
		if len(all) == 0 {
			return nil, fmt.Errorf("no models assigned — run 'iq svc tier add <tier> <model>' first")
		}
		return all, nil
	}
	// Tier name?
	for _, t := range tierOrder {
		if t == arg {
			models := cfg.Tiers[t]
			if len(models) == 0 {
				return nil, fmt.Errorf("tier %q has no models assigned", arg)
			}
			return models, nil
		}
	}
	// Model ID?
	for _, t := range tierOrder {
		for _, m := range cfg.Tiers[t] {
			if m == arg {
				return []string{m}, nil
			}
		}
	}
	return nil, fmt.Errorf("%q is not a recognised tier or assigned model", arg)
}

// ── Status (shared logic) ─────────────────────────────────────────────────────

func printStatus() error {
	cfg, err := loadConfig()
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

	for _, tier := range tierOrder {
		for _, model := range cfg.Tiers[tier] {
			state, _ := readState(model)
			endpoint := ""
			if state != nil {
				endpoint = sidecarEndpoint(state.Port)
			}
			if state == nil || !pidAlive(state.PID) {
				rows = append(rows, statusRow{tier, model, endpoint, 0, "—", false, "—"})
				continue
			}
			rss := processRSSKB(state.PID)
			totalKB += rss
			mem := formatMB(rss * 1024)
			if rss == 0 {
				mem = "?"
			}
			rows = append(rows, statusRow{tier, model, endpoint, state.PID, formatUptime(state.Started), true, mem})
		}
	}

	// Embed sidecar rows.
	for _, role := range []string{"cue", "kb"} {
		slug := embedSlug(role)
		var model string
		if role == "kb" {
			model = kbModel(cfg)
		} else {
			model = cueModel(cfg)
		}
		eState, _ := readState(slug)
		endpoint := ""
		if eState != nil {
			endpoint = sidecarEndpoint(eState.Port)
		}
		if eState == nil || !pidAlive(eState.PID) {
			rows = append(rows, statusRow{slug, model, endpoint, 0, "—", false, "—"})
			continue
		}
		rss := processRSSKB(eState.PID)
		totalKB += rss
		mem := formatMB(rss * 1024)
		if rss == 0 {
			mem = "?"
		}
		rows = append(rows, statusRow{slug, model, endpoint, eState.PID, formatUptime(eState.Started), true, mem})
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
	cfgPath, _ := configPath()
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

	iqRSS := processRSSKB(os.Getpid())
	totalKB += iqRSS
	// Last line: IQ mem left-aligned, total mem right-aligned to MEM column.
	// Column positions: 6+2 + modelW+2 + 28+2 + 7+2 + 8+2 + 7+2 + 8 = left of MEM column
	lineW := tierW + 2 + modelW + 2 + 28 + 2 + 7 + 2 + 8 + 2 + 7 + 2 + 8
	iqLabel := "IQ process mem:"
	iqVal := formatMB(iqRSS * 1024)
	totLabel := "Total mem:"
	totVal := formatMB(totalKB * 1024)
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
	fmt.Printf("Work with IQ service daemon.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s svc <command> [tier|model]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "status", "Show running sidecar status and memory usage (alias: st)")
	fmt.Printf("  %-10s %s\n", "start", "Start sidecars for all, a tier pool, or a specific model")
	fmt.Printf("  %-10s %s\n", "stop", "Stop sidecars for all, a tier pool, or a specific model")
	fmt.Printf("  %-10s %s\n", "tier", "Manage model tier pool assignments")
	fmt.Printf("  %-10s %s\n", "embed", "Manage embed sidecar models")
	fmt.Printf("  %-10s %s\n\n", "doc", "Check runtime dependencies and model readiness")
	fmt.Printf("%s\n", utl.Whi2("NOTES"))
	fmt.Printf("  Embed sidecars (cue/kb) are started automatically with 'iq svc start'.\n")
	fmt.Printf("  They require mlx-embedding-models in the mlx-lm venv:\n")
	fmt.Printf("  pipx inject mlx-lm mlx-embedding-models\n\n")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s svc status\n", n)
	fmt.Printf("  $ %s svc start\n", n)
	fmt.Printf("  $ %s svc start fast\n", n)
	fmt.Printf("  $ %s svc stop\n", n)
	fmt.Printf("  $ %s svc tier add fast mlx-community/SmolLM2-135M-Instruct-8bit\n", n)
	fmt.Printf("  $ %s svc embed set nomic-embed-text\n", n)
	fmt.Printf("  $ %s svc doc\n\n", n)
}

// ── Root svc command ──────────────────────────────────────────────────────────

func newSvcCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "svc",
		Short:        "Work with IQ service daemon",
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
		newSvcStartCmd(),
		newSvcStopCmd(),
		newSvcTierCmd(),
		newSvcEmbedCmd(),
		newSvcDocCmd(),
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

func newSvcStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "start [tier|model]",
		Short:        "Start sidecars for all, a tier pool, or a specific model",
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) > 0 {
				arg = args[0]
			}
			// Start embed sidecars first when starting everything (no specific target).
			if arg == "" {
				for _, role := range []string{"cue", "kb"} {
					if err := startEmbedSidecar(role); err != nil {
						fmt.Fprintf(os.Stderr, "  error starting embed-%s: %s\n", role, err.Error())
					}
				}
			}
			models, err := resolveModels(arg)
			if err != nil {
				return err
			}
			for _, modelID := range models {
				tier := tierForModel(modelID)
				state, _ := readState(modelID)
				if state != nil && pidAlive(state.PID) {
					fmt.Printf("  pid %-7d  %s  %s\n",
						state.PID, sidecarEndpoint(state.Port), utl.Gra("already running"))
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

// killOrphanSidecars scans for any mlx_lm.server or embed_server.py
// processes on iq ports and kills them regardless of whether they have a state file.
// This catches processes started during manual testing or left behind by
// interrupted starts where no state file was written.
func killOrphanSidecars() {
	patterns := []string{"mlx_lm.server", "embed_server.py"}
	for _, pattern := range patterns {
		out, err := exec.Command("pgrep", "-f", pattern).Output()
		if err != nil {
			continue // no matching processes
		}
		for pidStr := range strings.FieldsSeq(string(out)) {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			// Confirm this process is on an iq-managed port range (270xx).
			psOut, err := exec.Command("ps", "-p", pidStr, "-o", "command=").Output()
			if err != nil {
				continue
			}
			if !strings.Contains(string(psOut), "--port 270") {
				continue
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				continue
			}
			proc.Signal(syscall.SIGTERM)
			time.Sleep(300 * time.Millisecond)
			if pidAlive(pid) {
				proc.Signal(syscall.SIGKILL)
			}
			fmt.Printf("  pid %-7d  %s\n", pid, utl.Gra("orphan killed"))
		}
	}
}

// ── stop ──────────────────────────────────────────────────────────────────────

func newSvcStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stop [tier|model]",
		Short:        "Stop sidecars for all, a tier pool, or a specific model",
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
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
				if err := stopSidecar(modelID); err != nil {
					fmt.Fprintf(os.Stderr, "  error stopping %s: %s\n", modelID, err.Error())
				}
			}
			// Stop embed sidecars and sweep for orphans when stopping everything.
			if arg == "" {
				for _, role := range []string{"cue", "kb"} {
					if err := stopSidecar(embedSlug(role)); err != nil {
						fmt.Fprintf(os.Stderr, "  error stopping embed-%s: %s\n", role, err.Error())
					}
				}
				killOrphanSidecars()
			}
			return nil
		},
	}
}

// ── tier ──────────────────────────────────────────────────────────────────────

func printSvcTierHelp() {
	n := program_name
	fmt.Printf("Manage model tier pool assignments.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s svc tier <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "show", "Show current tier assignments")
	fmt.Printf("  %-10s %s\n", "add", "Add a model to a tier pool")
	fmt.Printf("  %-10s %s\n\n", "rm", "Remove a model from a tier pool")
	fmt.Printf("%s\n", utl.Whi2("TIERS"))
	fmt.Printf("  %-8s %s\n", "fast", "Sub-2GB models — used for classification and quick tasks")
	fmt.Printf("  %-8s %s\n\n", "slow", "2GB+ models — used for quality inference")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s svc tier show\n", n)
	fmt.Printf("  $ %s svc tier add fast mlx-community/SmolLM2-135M-Instruct-8bit\n", n)
	fmt.Printf("  $ %s svc tier add slow mlx-community/Phi-4-mini-reasoning-4bit\n", n)
	fmt.Printf("  $ %s svc tier rm fast mlx-community/SmolLM2-135M-Instruct-8bit\n\n", n)
}

func newSvcTierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "tier",
		Short:        "Manage model tier pool assignments",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printSvcTierHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printSvcTierHelp()
	})
	cmd.AddCommand(newSvcTierShowCmd(), newSvcTierAddCmd(), newSvcTierRmCmd())
	return cmd
}

func newSvcTierShowCmd() *cobra.Command {
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

func newSvcTierAddCmd() *cobra.Command {
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
			if slices.Contains(cfg.Tiers[tier], modelID) {
				fmt.Printf("%s is already in the %s tier\n", modelID, tier)
				return nil
			}
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

func newSvcTierRmCmd() *cobra.Command {
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

// ── embed ─────────────────────────────────────────────────────────────────────

func printSvcEmbedHelp() {
	n := program_name
	fmt.Printf("Manage MLX embed models for cue classification and KB retrieval.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s svc embed <command>\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-24s %s\n", "show", "Show both configured embed models")
	fmt.Printf("  %-24s %s\n", "set cue <model>", "Set cue classification model and restart its sidecar")
	fmt.Printf("  %-24s %s\n", "set kb <model>", "Set KB indexing/retrieval model and restart its sidecar")
	fmt.Printf("  %-24s %s\n", "rm cue", "Revert cue model to default and restart its sidecar")
	fmt.Printf("  %-24s %s\n\n", "rm kb", "Revert KB model to default and restart its sidecar")
	fmt.Printf("%s\n", utl.Whi2("NOTES"))
	fmt.Printf("  Models are HF model IDs (mlx-community/*). The model must be\n")
	fmt.Printf("  downloaded first with 'iq lm get <model>'.\n")
	fmt.Printf("  Changing kb model invalidates kb.json — re-ingest required.\n\n")
	fmt.Printf("%s\n", utl.Whi2("DEFAULTS"))
	fmt.Printf("  cue  %s\n", utl.Gra(defaultCueModel))
	fmt.Printf("  kb   %s\n\n", utl.Gra(defaultKbModel))
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s svc embed show\n", n)
	fmt.Printf("  $ %s svc embed set cue mlx-community/nomicai-modernbert-embed-base-4bit\n", n)
	fmt.Printf("  $ %s svc embed set kb mlx-community/mxbai-embed-large-v1\n\n", n)
}

func newSvcEmbedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "embed",
		Short:        "Manage embed sidecar models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printSvcEmbedHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printSvcEmbedHelp()
	})
	cmd.AddCommand(newSvcEmbedShowCmd(), newSvcEmbedSetCmd(), newSvcEmbedRmCmd())
	return cmd
}

func newSvcEmbedShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show configured embed models for both roles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			cueSuffix := ""
			if cfg.CueModel == "" {
				cueSuffix = utl.Gra("  (default)")
			}
			kbSuffix := ""
			if cfg.KbModel == "" {
				kbSuffix = utl.Gra("  (default)")
			}
			fmt.Printf("cue_model  %s%s\n", utl.Gre(cueModel(cfg)), cueSuffix)
			fmt.Printf("kb_model   %s%s\n", utl.Gre(kbModel(cfg)), kbSuffix)
			return nil
		},
	}
}

func newSvcEmbedSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "set <cue|kb> <model>",
		Short:        "Set embed model for a role and restart its sidecar",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			role, modelName := args[0], args[1]
			if role != "cue" && role != "kb" {
				return fmt.Errorf("role must be 'cue' or 'kb'")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			switch role {
			case "cue":
				cfg.CueModel = modelName
				if err := saveConfig(cfg); err != nil {
					return err
				}
				invalidateCueEmbeddings()
				fmt.Printf("cue_model  %s\n", utl.Gre(modelName))
			case "kb":
				cfg.KbModel = modelName
				if err := saveConfig(cfg); err != nil {
					return err
				}
				fmt.Printf("kb_model   %s\n", utl.Gre(modelName))
				kbP, _ := kbPath()
				if _, err := os.Stat(kbP); err == nil {
					fmt.Printf("%s\n", utl.Yel("warning: kb_model changed — existing kb.json is stale"))
					fmt.Printf("%s\n", utl.Gra("  run: iq kb clear && iq kb ingest <path>"))
				}
			}
			// Stop old sidecar and start fresh with the new model.
			stopSidecar(embedSlug(role))
			return startEmbedSidecar(role)
		},
	}
}

func newSvcEmbedRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <cue|kb>",
		Short:        "Revert an embed model role to its default and restart its sidecar",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			role := args[0]
			if role != "cue" && role != "kb" {
				return fmt.Errorf("role must be 'cue' or 'kb'")
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			switch role {
			case "cue":
				cfg.CueModel = ""
				if err := saveConfig(cfg); err != nil {
					return err
				}
				invalidateCueEmbeddings()
				fmt.Printf("cue_model  %s\n", utl.Gra("(default) "+defaultCueModel))
			case "kb":
				cfg.KbModel = ""
				if err := saveConfig(cfg); err != nil {
					return err
				}
				fmt.Printf("kb_model   %s\n", utl.Gra("(default) "+defaultKbModel))
				kbP, _ := kbPath()
				if _, err := os.Stat(kbP); err == nil {
					fmt.Printf("%s\n", utl.Yel("warning: kb_model changed — existing kb.json is stale"))
					fmt.Printf("%s\n", utl.Gra("  run: iq kb clear && iq kb ingest <path>"))
				}
			}
			// Stop old sidecar and start fresh with the default model.
			stopSidecar(embedSlug(role))
			return startEmbedSidecar(role)
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

func newSvcDocCmd() *cobra.Command {
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
			venvPy, pyVenvErr := mlxVenvPython()
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

			// ── embed model caches ──
			cfg2, cfgErr2 := loadConfig()
			if cfgErr2 == nil {
				checkEmbedCache := func(label, modelID string) {
					cacheDir := hfCacheDir(modelID)
					_, statErr := os.Stat(cacheDir)
					ok := statErr == nil
					var d string
					if ok {
						parent := filepath.Dir(cacheDir)
						d = utl.Gra(parent+"/") + utl.Whi(filepath.Base(cacheDir))
					} else {
						d = utl.Gra(fmt.Sprintf("cache not found — run: iq lm get %s", modelID))
					}
					checks = append(checks, runDocCheck("  "+label, d, ok, false))
				}
				checkEmbedCache("cue_model", cueModel(cfg2))
				checkEmbedCache("kb_model", kbModel(cfg2))
			}

			// ── tier model cache dirs ──
			cfg, cfgErr := loadConfig()
			if cfgErr != nil {
				return cfgErr
			}
			for _, t := range tierOrder {
				for _, model := range cfg.Tiers[t] {
					cacheDir := hfCacheDir(model)
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
			if len(cfg.Tiers["fast"]) == 0 && len(cfg.Tiers["slow"]) == 0 {
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
				return fmt.Errorf("one or more checks failed — resolve the above before running 'iq svc start'")
			}
			fmt.Printf("%s\n", utl.Gre("All checks passed."))
			return nil
		},
	}
}
