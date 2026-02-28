package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	ollamaapi "github.com/ollama/ollama/api"
)

const (
	ollamaHost     = "http://localhost:11434"
	embedCacheFile = "cue_embeddings.json"
)

// ── Ollama client helpers ─────────────────────────────────────────────────────

// ollamaClient returns an Ollama API client pointed at OLLAMA_HOST or
// the default localhost:11434.
func ollamaClient() (*ollamaapi.Client, error) {
	return ollamaapi.ClientFromEnvironment()
}

// ollamaRunning returns true if the Ollama daemon is reachable.
func ollamaRunning() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ollamaHost + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ── Embedding call ────────────────────────────────────────────────────────────

// embedTexts calls the Ollama /api/embed endpoint.
// role is "cue" or "kb" — selects which configured model to use.
// task is "query" or "document" — controls instruction prefix for models that require it.
func embedTexts(texts []string, role string, task string) ([][]float32, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	var model string
	if role == "cue" {
		model = cueModel(cfg)
	} else {
		model = kbModel(cfg)
	}

	// Per-model context window limits.
	// mxbai-embed-large: 512 tokens; nomic-embed-*: 8192 tokens.
	maxRunes := 1600
	if strings.HasPrefix(model, "mxbai-embed-large") {
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
	// nomic-embed-*: "search_query:" / "search_document:"
	// mxbai-embed-large: query prefix only
	prefixed := truncated
	switch {
	case strings.HasPrefix(model, "nomic-embed"):
		prefixed = make([]string, len(truncated))
		prefix := "search_document: "
		if task == "query" {
			prefix = "search_query: "
		}
		for i, t := range truncated {
			prefixed[i] = prefix + t
		}
	case strings.HasPrefix(model, "mxbai-embed-large"):
		if task == "query" {
			prefixed = make([]string, len(truncated))
			for i, t := range truncated {
				prefixed[i] = "Represent this sentence for searching relevant passages: " + t
			}
		}
	}

	if os.Getenv("IQ_EMBED_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[embed] role=%s model=%s task=%s inputs=%d first_len=%d\n",
			role, model, task, len(prefixed), len([]rune(prefixed[0])))
	}

	c, err := ollamaClient()
	if err != nil {
		return nil, fmt.Errorf("ollama client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := c.Embed(ctx, &ollamaapi.EmbedRequest{
		Model: model,
		Input: prefixed,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embed(%s): %w", model, err)
	}

	// Convert [][]float64 → [][]float32
	out := make([][]float32, len(resp.Embeddings))
	for i, vec64 := range resp.Embeddings {
		v32 := make([]float32, len(vec64))
		for j, f := range vec64 {
			v32[j] = float32(f)
		}
		out[i] = v32
	}
	return out, nil
}

// ── Cosine similarity ─────────────────────────────────────────────────────────

// cosineSimilarity returns the cosine similarity between two float32 vectors.
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

// ── Cue embedding cache ───────────────────────────────────────────────────────

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
// mutation or cue_model change so the next prompt triggers a rebuild.
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
// result to the cache file. Always uses the "cue" role model.
func refreshCueEmbeddings(cues []Cue, model string) error {
	texts := make([]string, len(cues))
	names := make([]string, len(cues))
	for i, c := range cues {
		// Embed name + description together for richer semantic signal.
		texts[i] = c.Name + ": " + c.Description
		names[i] = c.Name
	}
	embeddings, err := embedTexts(texts, "cue", "document")
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

	inputEmb, err := embedTexts([]string{input}, "cue", "query")
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
