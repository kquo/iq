package embed

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
	"strings"
	"syscall"
	"time"

	"github.com/queone/utl"

	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/sidecar"
)

//go:embed embed_server.py
var EmbedServerPy string

const (
	SlugConst    = "embed"
	PortConst    = 27000
	ReadyTimeout = 60 * time.Second
	CacheFile    = "cue_embeddings.json"

	// ClassifyMinScore is the minimum cosine similarity required to accept a
	// cue match. Below this threshold the classifier falls back to "initial".
	ClassifyMinScore float32 = 0.40
)

// ── Embed sidecar helpers ─────────────────────────────────────────────────────

// SidecarAlive returns true if the embed sidecar is running.
func SidecarAlive() bool {
	state, err := sidecar.ReadState(SlugConst)
	return err == nil && state != nil && sidecar.PidAlive(state.PID)
}

// MlxVenvPython locates the Python interpreter in the same venv as mlx_lm.server.
func MlxVenvPython() (string, error) {
	serverPath, err := exec.LookPath("mlx_lm.server")
	if err != nil {
		return "", fmt.Errorf("mlx_lm.server not found on PATH")
	}
	// Resolve symlink to get the real path inside the pipx venv.
	real, err := filepath.EvalSymlinks(serverPath)
	if err != nil {
		real = serverPath
	}
	binDir := filepath.Dir(real)
	for _, name := range []string{"python3", "python"} {
		candidate := filepath.Join(binDir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no python3/python found in mlx_lm venv (%s) — run: pipx inject mlx-lm mlx-embedding-models", binDir)
}

// StartSidecar extracts embed_server.py, finds the venv Python, and
// spawns the single embedding sidecar. onReady is called after successful start
// (e.g., to register in manifest).
func StartSidecar(modelID string, onReady func(string) error) error {
	slug := SlugConst
	port := PortConst

	existing, _ := sidecar.ReadState(slug)
	if existing != nil && sidecar.PidAlive(existing.PID) {
		fmt.Printf("  %-9s  pid %-7d  %s  %s\n",
			slug, existing.PID, sidecar.Endpoint(existing.Port), utl.Gra("already running"))
		return nil
	}

	// Extract the embedded Python script to a stable temp path.
	scriptPath := filepath.Join(os.TempDir(), "embed_server.py")
	if err := os.WriteFile(scriptPath, []byte(EmbedServerPy), 0755); err != nil {
		return fmt.Errorf("failed to write embed script: %w", err)
	}

	logP, err := sidecar.LogPath(slug)
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(logP, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open embed log: %w", err)
	}

	pyPath, err := MlxVenvPython()
	if err != nil {
		lf.Close()
		return fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}

	cmd := exec.Command(pyPath, scriptPath,
		"--model", modelID,
		"--port", fmt.Sprintf("%d", port),
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

	if err := sidecar.WriteStateAs(slug, &sidecar.State{
		Tier:    "embed",
		Model:   modelID,
		PID:     cmd.Process.Pid,
		Port:    port,
		Started: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("started (pid %d) but failed to write state: %w", cmd.Process.Pid, err)
	}

	if onReady != nil {
		if err := onReady(modelID); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: post-start callback failed: "+err.Error()))
		}
	}

	fmt.Printf("  %-11s  pid %-7d  %s  ", slug, cmd.Process.Pid, sidecar.Endpoint(port))
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	deadline := time.Now().Add(ReadyTimeout)
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
		if !sidecar.PidAlive(cmd.Process.Pid) {
			fmt.Printf("%s\n", utl.Gra("failed"))
			sidecar.PrintLastLogLines(logP, 10)
			return fmt.Errorf("embed sidecar process exited unexpectedly")
		}
		fmt.Print(".")
		time.Sleep(sidecar.PollInterval)
	}
	fmt.Printf("%s\n", utl.Gra("timeout"))
	sidecar.PrintLastLogLines(logP, 10)
	return fmt.Errorf("embed sidecar did not become ready within %s", ReadyTimeout)
}

// ── Embedding call ────────────────────────────────────────────────────────────

// TextsOnPort sends texts to an embed sidecar at the given port, applying
// model-specific truncation and instruction prefixes based on model name.
func TextsOnPort(texts []string, model string, port int, task string) ([][]float32, error) {
	// Per-model context window limits.
	maxRunes := 1600
	if strings.Contains(strings.ToLower(model), "mxbai") {
		maxRunes = 900
	}
	truncated := make([]string, len(texts))
	for i, t := range texts {
		r := []rune(t)
		if len(r) > maxRunes {
			r = r[:maxRunes]
		}
		truncated[i] = string(r)
	}

	// Apply instruction prefix for models that require it.
	prefixed := truncated
	modelLow := strings.ToLower(model)
	switch {
	case strings.Contains(modelLow, "nomic"):
		prefixed = make([]string, len(truncated))
		prefix := "search_document: "
		if task == "query" {
			prefix = "search_query: "
		}
		for i, t := range truncated {
			prefixed[i] = prefix + t
		}
	case strings.Contains(modelLow, "mxbai"):
		if task == "query" {
			prefixed = make([]string, len(truncated))
			for i, t := range truncated {
				prefixed[i] = "Represent this sentence for searching relevant passages: " + t
			}
		}
	}

	if os.Getenv("IQ_EMBED_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[embed] model=%s task=%s port=%d inputs=%d first_len=%d\n",
			model, task, port, len(prefixed), len([]rune(prefixed[0])))
	}

	reqBody := map[string][]string{"texts": prefixed}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("http://localhost:%d/embed", port)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed sidecar at :%d unreachable: %w", port, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed sidecar at :%d error: %s", port, strings.TrimSpace(string(raw)))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse embed response: %w", err)
	}
	return result.Embeddings, nil
}

// Texts calls the local embed sidecar.
// task is "query" or "document" — controls instruction prefix for models that require it.
func Texts(texts []string, task string) ([][]float32, error) {
	state, err := sidecar.ReadState(SlugConst)
	if err != nil || state == nil || !sidecar.PidAlive(state.PID) {
		return nil, fmt.Errorf("embed sidecar not running — run: iq start")
	}

	cfg, err := config.Load(nil)
	if err != nil {
		return nil, err
	}

	return TextsOnPort(texts, config.EmbedModel(cfg), state.Port, task)
}

// ── Cosine similarity ─────────────────────────────────────────────────────────

// CosineSimilarity returns the cosine similarity between two float32 vectors.
func CosineSimilarity(a, b []float32) float32 {
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

// ── Cue embedding cache ───────────────────────────────────────────────────────

type cueEmbeddingCache struct {
	Model     string               `json:"model"`
	Generated string               `json:"generated"`
	Cues      map[string][]float32 `json:"cues"`
}

func cueEmbeddingsPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, CacheFile), nil
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

// InvalidateCueEmbeddings deletes the embedding cache.
func InvalidateCueEmbeddings() {
	path, err := cueEmbeddingsPath()
	if err == nil {
		os.Remove(path)
	}
}

// CueEmbeddingsStale returns true if the cache needs rebuilding.
func CueEmbeddingsStale(cues []cue.Cue, model string) bool {
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

// RefreshCueEmbeddings embeds all cue name+description strings and writes the cache.
func RefreshCueEmbeddings(cues []cue.Cue, model string) error {
	texts := make([]string, len(cues))
	names := make([]string, len(cues))
	for i, c := range cues {
		texts[i] = c.Name + ": " + c.Description
		names[i] = c.Name
	}
	embeddings, err := Texts(texts, "document")
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

// ClassifyTrace carries the details of an embedding classification call.
type ClassifyTrace struct {
	Model    string
	Resolved string
	Score    float32
	Elapsed  time.Duration
	CacheHit bool
	InputVec []float32
}

// Classify returns the best-matching cue name for the input using
// cosine similarity against pre-computed cue description embeddings.
func Classify(input string, cues []cue.Cue, model string) (string, *ClassifyTrace, error) {
	t0 := time.Now()
	cacheHit := true

	if CueEmbeddingsStale(cues, model) {
		cacheHit = false
		if err := RefreshCueEmbeddings(cues, model); err != nil {
			return "initial", nil, err
		}
	}

	cache, err := loadCueEmbeddings()
	if err != nil || cache == nil {
		return "initial", nil, fmt.Errorf("cue embeddings unavailable")
	}

	inputEmb, err := Texts([]string{input}, "query")
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
		score := CosineSimilarity(vec, cueVec)
		if score > bestScore {
			bestScore = score
			bestName = c.Name
		}
	}
	if bestScore < ClassifyMinScore {
		bestName = "initial"
	}

	trace := &ClassifyTrace{
		Model:    model,
		Resolved: bestName,
		Score:    bestScore,
		Elapsed:  time.Since(t0),
		CacheHit: cacheHit,
		InputVec: vec,
	}
	return bestName, trace, nil
}
