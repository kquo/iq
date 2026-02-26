package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

// ── Sidecar constants ─────────────────────────────────────────────────────────

const sidecarReadyTimeout = 120 * time.Second
const sidecarPollInterval = 500 * time.Millisecond
const portBase = 27001

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
			return nil, fmt.Errorf("no models assigned — run 'iq cfg tier add <tier> <model>' first")
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

// ── Help ──────────────────────────────────────────────────────────────────────

func printSvcHelp() {
	n := program_name
	fmt.Printf("Work with IQ service daemon.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s svc <command> [tier|model]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "status", "Show running sidecar status and memory usage")
	fmt.Printf("  %-10s %s\n", "start", "Start sidecars for all, a tier pool, or a specific model")
	fmt.Printf("  %-10s %s\n", "stop", "Stop sidecars for all, a tier pool, or a specific model")
	fmt.Printf("  %-10s %s\n\n", "doc", "Check runtime dependencies and model readiness")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s svc status\n", n)
	fmt.Printf("  $ %s svc start\n", n)
	fmt.Printf("  $ %s svc start fast\n", n)
	fmt.Printf("  $ %s svc start mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s svc stop\n", n)
	fmt.Printf("  $ %s svc stop slow\n", n)
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
	cmd.AddCommand(newSvcStatusCmd(), newSvcStartCmd(), newSvcStopCmd(), newSvcDocCmd())
	return cmd
}

// ── status ────────────────────────────────────────────────────────────────────

func newSvcStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show running sidecar status and memory usage",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			fmt.Printf("%-6s  %-50s  %-28s  %-7s  %-8s  %8s\n",
				"TIER", "MODEL", "ENDPOINT", "PID", "UPTIME", "MEM")

			var totalKB int64
			for _, tier := range tierOrder {
				for _, model := range cfg.Tiers[tier] {
					state, _ := readState(model)
					endpoint := ""
					if state != nil {
						endpoint = sidecarEndpoint(state.Port)
					}
					if state == nil || !pidAlive(state.PID) {
						fmt.Printf("%-6s  %-50s  %-28s  %-7s  %-8s  %8s\n",
							tier, model, endpoint, "—", utl.Gra("stopped"), "—")
						continue
					}
					endpoint = sidecarEndpoint(state.Port)
					rss := processRSSKB(state.PID)
					totalKB += rss
					mem := formatMB(rss * 1024)
					if rss == 0 {
						mem = "?"
					}
					fmt.Printf("%-6s  %-50s  %-28s  %-7d  %-8s  %8s\n",
						tier, model, endpoint,
						state.PID,
						formatUptime(state.Started),
						mem,
					)
				}
			}

			iqRSS := processRSSKB(os.Getpid())
			totalKB += iqRSS
			fmt.Printf("%-20s %s\n", "IQ process mem:", formatMB(iqRSS*1024))
			fmt.Printf("%-20s %s\n", "Total mem:", formatMB(totalKB*1024))
			return nil
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
			return nil
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

			if serverOK {
				helpOut, _ := exec.Command(serverPath, "--help").CombinedOutput()
				modelFlagOK := strings.Contains(string(helpOut), "--model")
				flagDetail := utl.Gra("--model flag supported")
				if !modelFlagOK {
					flagDetail = utl.Gra("--model flag not found — upgrade mlx_lm")
				}
				checks = append(checks, runDocCheck("  --model flag", flagDetail, modelFlagOK, false))
			}

			// ── tier model cache dirs ──
			cfg, cfgErr := loadConfig()
			if cfgErr != nil {
				return cfgErr
			}
			for _, t := range tierOrder {
				for _, model := range cfg.Tiers[t] {
					label := fmt.Sprintf("%-6s %s", t, model)
					cacheDir := hfCacheDir(model)
					_, statErr := os.Stat(cacheDir)
					modelOK := statErr == nil
					if modelOK {
						detail = utl.Gra(cacheDir)
					} else {
						detail = utl.Gra(fmt.Sprintf("cache not found — run: iq lm get %s", model))
					}
					checks = append(checks, runDocCheck(label, detail, modelOK, false))
				}
			}
			if len(cfg.Tiers["fast"]) == 0 && len(cfg.Tiers["slow"]) == 0 {
				checks = append(checks, runDocCheck("tier models", utl.Gra("no models assigned"), true, true))
			}

			// ── print results ──
			fmt.Printf("%-36s  %-6s  %s\n", "CHECK", "STATUS", "DETAIL")
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
				fmt.Printf("%-36s  %s  %s\n", c.label, status, c.detail)
			}

			if !allOK {
				return fmt.Errorf("one or more checks failed — resolve the above before running 'iq svc start'")
			}
			fmt.Printf("%s\n", utl.Gre("All checks passed."))
			return nil
		},
	}
}
