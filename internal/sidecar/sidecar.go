package sidecar

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"iq/internal/color"

	"iq/internal/config"
)

//go:embed infer_server.py
var InferServerPy string

// ── Sidecar constants ─────────────────────────────────────────────────────────

const ReadyTimeout = 120 * time.Second
const PollInterval = 500 * time.Millisecond
const PortBase = 27001

// ── Model slug ────────────────────────────────────────────────────────────────

// ModelSlug converts a model ID to a filesystem-safe name for state/log files.
// e.g. "mlx-community/SmolLM2-135M-Instruct-8bit" → "mlx-community--SmolLM2-135M-Instruct-8bit"
func ModelSlug(id string) string {
	return strings.ReplaceAll(id, "/", "--")
}

// ── State file ────────────────────────────────────────────────────────────────

// State represents a running sidecar process.
type State struct {
	Tier    string `json:"tier"`
	Model   string `json:"model"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Started string `json:"started"`
}

// RunDir returns the path to the sidecar state directory.
func RunDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, "run")
	return d, os.MkdirAll(d, 0755)
}

// StatePath returns the state file path for a model ID.
func StatePath(modelID string) (string, error) {
	d, err := RunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ModelSlug(modelID)+".json"), nil
}

// LogPath returns the log file path for a model ID.
func LogPath(modelID string) (string, error) {
	d, err := RunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, ModelSlug(modelID)+".log"), nil
}

// ReadState reads the state file for a model. Returns nil, nil if not found.
func ReadState(modelID string) (*State, error) {
	path, err := StatePath(modelID)
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
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteState writes a state file keyed by the state's Model field.
func WriteState(s *State) error {
	path, err := StatePath(s.Model)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// WriteStateAs writes a state file keyed by an arbitrary slug (e.g. "embed").
func WriteStateAs(slug string, s *State) error {
	d, err := RunDir()
	if err != nil {
		return err
	}
	path := filepath.Join(d, ModelSlug(slug)+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RemoveState deletes the state file for a model.
func RemoveState(modelID string) error {
	path, err := StatePath(modelID)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// AllStates returns all state files in the run directory.
func AllStates() ([]*State, error) {
	d, err := RunDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}
	var states []*State
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(d, e.Name()))
		if err != nil {
			continue
		}
		var s State
		if json.Unmarshal(data, &s) == nil {
			states = append(states, &s)
		}
	}
	return states, nil
}

// AllLiveStates returns only states whose PIDs are still alive.
func AllLiveStates() ([]*State, error) {
	all, err := AllStates()
	if err != nil {
		return nil, err
	}
	var live []*State
	for _, s := range all {
		if PidAlive(s.PID) {
			live = append(live, s)
		}
	}
	return live, nil
}

// NextAvailablePort scans live states and returns the next unused port.
// Dead state files (crashed or stale PIDs) are excluded so their ports
// are immediately available for reuse.
func NextAvailablePort() (int, error) {
	states, err := AllLiveStates()
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, s := range states {
		used[s.Port] = true
	}
	for p := PortBase; p < PortBase+100; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d–%d", PortBase, PortBase+100)
}

// PickSidecar returns a live sidecar for the given tier.
// If preferSmallest is true and diskUsage is provided, returns the smallest.
func PickSidecar(tier string, preferSmallest bool, diskUsage func(string) int64) (*State, error) {
	live, err := AllLiveStates()
	if err != nil {
		return nil, err
	}
	var candidates []*State
	for _, s := range live {
		if s.Tier == tier {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no running sidecar for %q — run 'iq start %s'", tier, tier)
	}
	if preferSmallest && len(candidates) > 1 && diskUsage != nil {
		best := candidates[0]
		bestDisk := diskUsage(best.Model)
		for _, c := range candidates[1:] {
			d := diskUsage(c.Model)
			if d > 0 && (bestDisk == 0 || d < bestDisk) {
				best = c
				bestDisk = d
			}
		}
		return best, nil
	}
	return candidates[0], nil
}

// ── Process helpers ──────────────────────────────────────────────────────────

// PidAlive checks whether a process is still running.
func PidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ProcessRSSKB returns the RSS of a process in kilobytes.
func ProcessRSSKB(pid int) int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	var kb int64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &kb)
	return kb
}

// FormatUptime formats the time since a RFC3339 timestamp as a human-readable string.
func FormatUptime(since string) string {
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

// Endpoint returns the HTTP base URL for a sidecar on the given port.
func Endpoint(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}

// PrintLastLogLines prints the last n lines of a log file to stderr.
func PrintLastLogLines(logFile string, n int) {
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
	fmt.Fprintf(os.Stderr, "\n%s\n", color.Gra("--- last log lines ---"))
	for _, l := range lines {
		fmt.Fprintf(os.Stderr, "  %s\n", l)
	}
	fmt.Fprintf(os.Stderr, "%s\n", color.Gra("--- full log: "+logFile+" ---"))
	fmt.Fprintln(os.Stderr)
}

// IsVisionModel checks a model's config.json for vision-language model indicators.
func IsVisionModel(modelPath string) bool {
	data, err := os.ReadFile(filepath.Join(modelPath, "config.json"))
	if err != nil {
		return false // can't read config — let mlx_lm decide
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return false
	}
	// Check for known VLM indicators in top-level keys.
	for _, key := range []string{"vision_config", "visual", "vision_tower", "image_size"} {
		if _, ok := cfg[key]; ok {
			return true
		}
	}
	// Check model_type for known VLM types.
	if mt, ok := cfg["model_type"].(string); ok {
		vlmTypes := []string{"qwen2_5_vl", "qwen2_vl", "llava", "idefics", "paligemma", "mllama"}
		if slices.Contains(vlmTypes, mt) {
			return true
		}
	}
	return false
}

// StartInfer spawns infer_server.py for the given model.
// modelPath and pythonPath must be pre-resolved by the caller.
// Returns the State on success (the caller may want to register in manifest, etc.).
func StartInfer(modelID, modelPath, pythonPath string) (*State, error) {
	port, err := NextAvailablePort()
	if err != nil {
		return nil, err
	}

	lfPath, err := LogPath(modelID)
	if err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(lfPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Preempt: refuse to start vision-language models (VLMs).
	if IsVisionModel(modelPath) {
		lf.Close()
		return nil, fmt.Errorf("model %s is a vision-language model (VLM) — IQ only supports text-only models", modelID)
	}

	// Write embedded script to config dir. If a dev override already exists
	// there, skip the write so local edits survive without a Go rebuild.
	cfgDir, err := config.Dir()
	if err != nil {
		lf.Close()
		return nil, err
	}
	scriptPath := filepath.Join(cfgDir, "infer_server.py")
	if existing, err := os.ReadFile(scriptPath); err != nil || string(existing) == InferServerPy {
		// Missing or identical to embedded — write (or refresh) the embedded copy.
		if err := os.WriteFile(scriptPath, []byte(InferServerPy), 0755); err != nil {
			lf.Close()
			return nil, fmt.Errorf("failed to write infer script: %w", err)
		}
	} else {
		fi, _ := os.Stat(scriptPath)
		fmt.Fprintf(os.Stderr, "  %s\n", color.Yel(fmt.Sprintf("using %s (%s)", scriptPath, fi.ModTime().Format("2006-01-02 15:04"))))
	}
	cmd := exec.Command(pythonPath, scriptPath, "--model", modelPath, "--port", strconv.Itoa(port))
	cmd.Env = os.Environ()
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		lf.Close()
		return nil, fmt.Errorf("failed to start sidecar: %w", err)
	}
	lf.Close()

	state := &State{
		Tier:    "infer",
		Model:   modelID,
		PID:     cmd.Process.Pid,
		Port:    port,
		Started: time.Now().UTC().Format(time.RFC3339),
	}
	if err := WriteState(state); err != nil {
		return state, fmt.Errorf("started (pid %d) but failed to write state: %w", cmd.Process.Pid, err)
	}

	// Wait for the process in a goroutine so we can detect early crashes.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	fmt.Printf("  %-11s  pid %-7d  %s  ",
		modelID, cmd.Process.Pid, Endpoint(port))
	healthURL := fmt.Sprintf("%s/v1/models", Endpoint(port))
	deadline := time.Now().Add(ReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("%s\n", color.Grn("ready"))
				return state, nil
			}
		}
		select {
		case <-exited:
			fmt.Printf("%s\n", color.Gra("failed"))
			PrintLastLogLines(lfPath, 10)
			RemoveState(state.Model)
			return nil, fmt.Errorf("sidecar process exited unexpectedly")
		default:
		}
		fmt.Print(".")
		time.Sleep(PollInterval)
	}

	fmt.Printf("%s\n", color.Gra("timeout"))
	PrintLastLogLines(lfPath, 10)
	cmd.Process.Kill() //nolint:errcheck // best-effort cleanup
	RemoveState(state.Model)
	return nil, fmt.Errorf("sidecar did not become ready within %s", ReadyTimeout)
}

// Stop sends SIGTERM to the sidecar for a model and removes its state file.
func Stop(modelID string) error {
	state, err := ReadState(modelID)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Printf("  %s  %s\n", modelID, color.Gra("not running"))
		return nil
	}
	if !PidAlive(state.PID) {
		fmt.Printf("  pid %-7d  %s  %s\n", state.PID, modelID, color.Gra("already stopped (stale state removed)"))
		RemoveState(modelID)
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
		if !PidAlive(state.PID) {
			break
		}
	}
	if PidAlive(state.PID) {
		proc.Signal(syscall.SIGKILL)
	}
	RemoveState(modelID)
	fmt.Printf("  pid %-7d  %s  %s\n", state.PID, modelID, color.Gra("stopped"))
	return nil
}

// KillOrphanSidecars finds and kills stale sidecar processes on IQ-managed ports.
func KillOrphanSidecars() {
	patterns := []string{"infer_server.py", "mlx_lm.server", "embed_server.py"}
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
			if PidAlive(pid) {
				proc.Signal(syscall.SIGKILL)
			}
			fmt.Printf("  pid %-7d  %s\n", pid, color.Gra("orphan killed"))
		}
	}
}
