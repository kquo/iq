package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/queone/utl"
)

//go:embed embed_server.py
var embedServerPy string

const (
	embedPortFixed    = 27000
	embedReadyTimeout = 60 * time.Second
	embedCacheFile    = "cue_embeddings.json"
)

// ── Embed sidecar state ───────────────────────────────────────────────────────

type embedSvcState struct {
	Model   string `json:"model"`
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Started string `json:"started"`
}

func embedStatePath() (string, error) {
	d, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "embed.json"), nil
}

func embedLogPath() (string, error) {
	d, err := runDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "embed.log"), nil
}

func readEmbedState() (*embedSvcState, error) {
	path, err := embedStatePath()
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
	var s embedSvcState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeEmbedState(s *embedSvcState) error {
	path, err := embedStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func removeEmbedState() {
	path, err := embedStatePath()
	if err == nil {
		os.Remove(path)
	}
}

func embedSidecarRunning() bool {
	s, err := readEmbedState()
	if err != nil || s == nil {
		return false
	}
	return pidAlive(s.PID)
}

// ── Embed sidecar lifecycle ───────────────────────────────────────────────────

func startEmbedSidecar(model string) error {
	// Extract the embedded Python script to a stable temp path.
	scriptPath := filepath.Join(os.TempDir(), "iq_embed_server.py")
	if err := os.WriteFile(scriptPath, []byte(embedServerPy), 0755); err != nil {
		return fmt.Errorf("failed to write embed server script: %w", err)
	}

	logP, err := embedLogPath()
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(logP, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open embed log: %w", err)
	}

	modelPath := model // mlx_embeddings.load() takes the HF model ID, not a snapshot path

	pyPath, err := mlxVenvPython()
	if err != nil {
		lf.Close()
		return fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}

	cmd := exec.Command(pyPath, scriptPath,
		"--model", modelPath,
		"--port", fmt.Sprintf("%d", embedPortFixed),
	)
	cmd.Env = os.Environ()
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("failed to start embed sidecar: %w", err)
	}
	lf.Close()

	if err := writeEmbedState(&embedSvcState{
		Model:   model,
		PID:     cmd.Process.Pid,
		Port:    embedPortFixed,
		Started: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("started embed sidecar (pid %d) but failed to write state: %w", cmd.Process.Pid, err)
	}

	fmt.Printf("  embed  pid %-7d  http://localhost:%d  ", cmd.Process.Pid, embedPortFixed)
	healthURL := fmt.Sprintf("http://localhost:%d/health", embedPortFixed)
	deadline := time.Now().Add(embedReadyTimeout)
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
			printLastLogLines(logP, 10)
			return fmt.Errorf("embed sidecar process exited unexpectedly")
		}
		fmt.Print(".")
		time.Sleep(sidecarPollInterval)
	}
	fmt.Printf("%s\n", utl.Gra("timeout"))
	return fmt.Errorf("embed sidecar did not become ready within %s", embedReadyTimeout)
}

func stopEmbedSidecar() error {
	state, err := readEmbedState()
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Printf("  embed  %s\n", utl.Gra("not running"))
		return nil
	}
	if !pidAlive(state.PID) {
		fmt.Printf("  embed  pid %-7d  %s\n", state.PID, utl.Gra("already stopped (stale state removed)"))
		removeEmbedState()
		return nil
	}
	proc, err := os.FindProcess(state.PID)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to embed sidecar pid %d: %w", state.PID, err)
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
	removeEmbedState()
	fmt.Printf("  embed  pid %-7d  %s\n", state.PID, utl.Gra("stopped"))
	return nil
}

// ── Embedding HTTP client ─────────────────────────────────────────────────────

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

func embedTexts(texts []string) ([][]float32, error) {
	state, err := readEmbedState()
	if err != nil || state == nil {
		return nil, fmt.Errorf("embed sidecar not running — run 'iq svc start'")
	}
	body, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("http://localhost:%d/embed", state.Port)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed sidecar at :%d unreachable: %w", state.Port, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result embedResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse embed response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("embed sidecar error: %s", result.Error)
	}
	return result.Embeddings, nil
}

// ── Cosine similarity ─────────────────────────────────────────────────────────

// cosineSimilarity returns the cosine similarity between two float32 vectors.
// Vectors from the embed sidecar are L2-normalised, so this is equivalent to
// a dot product — kept explicit for clarity.
func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// ── Embedding cache ───────────────────────────────────────────────────────────

type cueEmbeddingCache struct {
	Model     string               `json:"model"`
	Generated string               `json:"generated"`
	Cues      map[string][]float32 `json:"cues"`
}

func cueEmbeddingsPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, embedCacheFile), nil
}

func loadCueEmbeddings() (*cueEmbeddingCache, error) {
	path, err := cueEmbeddingsPath()
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
	var cache cueEmbeddingCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func saveCueEmbeddings(cache *cueEmbeddingCache) error {
	path, err := cueEmbeddingsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// invalidateCueEmbeddings deletes the embedding cache. Called after any cue
// mutation so the next prompt triggers a rebuild.
func invalidateCueEmbeddings() {
	path, err := cueEmbeddingsPath()
	if err == nil {
		os.Remove(path)
	}
}

// cueEmbeddingsStale returns true if the cache is missing, uses a different
// model, or is missing any cue present in the current set.
func cueEmbeddingsStale(cues []Cue, model string) bool {
	cache, err := loadCueEmbeddings()
	if err != nil || cache == nil {
		return true
	}
	if cache.Model != model {
		return true
	}
	for _, c := range cues {
		if _, ok := cache.Cues[c.Name]; !ok {
			return true
		}
	}
	return false
}

// refreshCueEmbeddings embeds all cue name+description strings and writes the
// result to the cache file. Takes ~1s for 56 cues on a Mac mini M-series.
func refreshCueEmbeddings(cues []Cue, model string) error {
	texts := make([]string, len(cues))
	names := make([]string, len(cues))
	for i, c := range cues {
		// Embed name + description together for richer semantic signal.
		texts[i] = c.Name + ": " + c.Description
		names[i] = c.Name
	}
	embeddings, err := embedTexts(texts)
	if err != nil {
		return fmt.Errorf("failed to embed cue descriptions: %w", err)
	}
	cache := &cueEmbeddingCache{
		Model:     model,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Cues:      make(map[string][]float32, len(cues)),
	}
	for i, name := range names {
		if i < len(embeddings) {
			cache.Cues[name] = embeddings[i]
		}
	}
	return saveCueEmbeddings(cache)
}

// ── Embed-based classifier ────────────────────────────────────────────────────

// embedClassifyTrace carries the details of an embedding classification call.
type embedClassifyTrace struct {
	Model    string
	Resolved string
	Score    float32
	Elapsed  time.Duration
	CacheHit bool
}

// embedClassify returns the best-matching cue name for the input using
// cosine similarity against pre-computed cue description embeddings.
// Falls back to "initial" on any error.
func embedClassify(input string, cues []Cue, model string) (string, *embedClassifyTrace, error) {
	t0 := time.Now()
	cacheHit := true

	if cueEmbeddingsStale(cues, model) {
		cacheHit = false
		if err := refreshCueEmbeddings(cues, model); err != nil {
			return "initial", nil, err
		}
	}

	cache, err := loadCueEmbeddings()
	if err != nil || cache == nil {
		return "initial", nil, fmt.Errorf("cue embeddings unavailable")
	}

	inputEmb, err := embedTexts([]string{input})
	if err != nil {
		return "initial", nil, err
	}
	if len(inputEmb) == 0 {
		return "initial", nil, fmt.Errorf("empty embedding response")
	}
	vec := inputEmb[0]

	bestName := "initial"
	var bestScore float32 = -2
	for _, c := range cues {
		cueVec, ok := cache.Cues[c.Name]
		if !ok {
			continue
		}
		score := cosineSimilarity(vec, cueVec)
		if score > bestScore {
			bestScore = score
			bestName = c.Name
		}
	}

	trace := &embedClassifyTrace{
		Model:    model,
		Resolved: bestName,
		Score:    bestScore,
		Elapsed:  time.Since(t0),
		CacheHit: cacheHit,
	}
	return bestName, trace, nil
}

// ── Python interpreter resolution ─────────────────────────────────────────────

// mlxVenvPython returns the Python interpreter that shares a venv with
// mlx_lm.server. pipx installs mlx_lm.server as a symlink:
//
//	~/.local/bin/mlx_lm.server → ~/.local/pipx/venvs/mlx-lm/bin/mlx_lm.server
//
// We resolve the symlink, then look for python3 (or python) as a sibling in
// the same bin/ directory. This ensures mlx_embeddings — installed via
// `pipx inject mlx-lm mlx-embeddings` — is importable.
func mlxVenvPython() (string, error) {
	serverPath, _ := checkCommand("mlx_lm.server", "")
	if serverPath == "" {
		return "", fmt.Errorf("mlx_lm.server not found on PATH")
	}

	// Resolve symlink to get the real path inside the pipx venv.
	real, err := filepath.EvalSymlinks(serverPath)
	if err != nil {
		real = serverPath // not a symlink, use as-is
	}

	binDir := filepath.Dir(real)

	for _, name := range []string{"python3", "python"} {
		candidate := filepath.Join(binDir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no python3/python found in mlx_lm venv (%s) — run: pipx inject mlx-lm mlx-embeddings", binDir)
}
