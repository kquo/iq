package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"time"

	"iq/internal/config"
)

const (
	responseCacheFile = "response_cache.json"
	responseCacheTTL  = 1 * time.Hour
)

// responseCacheEntry stores a cached inference response.
type responseCacheEntry struct {
	Response  string `json:"response"`
	Model     string `json:"model"`
	Cue       string `json:"cue"`
	Timestamp string `json:"timestamp"`
	Hits      int    `json:"hits"`
}

// cacheHitResult carries details of a cache lookup for trace output.
type cacheHitResult struct {
	Key     string
	Hit     bool
	Age     time.Duration
	Model   string
	Elapsed time.Duration
}

func responseCachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, responseCacheFile), nil
}

func loadResponseCache() (map[string]responseCacheEntry, error) {
	path, err := responseCachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]responseCacheEntry), nil
	}
	if err != nil {
		return nil, err
	}
	var cache map[string]responseCacheEntry
	if err := json.Unmarshal(data, &cache); err != nil {
		return make(map[string]responseCacheEntry), nil
	}
	return cache, nil
}

func saveResponseCache(cache map[string]responseCacheEntry) error {
	path, err := responseCachePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// cacheKey computes an FNV64a hash over the message array and model ID.
func cacheKey(messages []chatMessage, model string) string {
	h := fnv.New64a()
	for _, m := range messages {
		h.Write([]byte(m.Role))
		h.Write([]byte{0})
		h.Write([]byte(m.Content))
		h.Write([]byte{0})
	}
	h.Write([]byte(model))
	return fmt.Sprintf("%016x", h.Sum64())
}

// cacheCheck looks up a response in the cache. Returns the cached response
// (empty on miss) and a result struct for trace output.
func cacheCheck(key string) (string, *cacheHitResult) {
	t0 := time.Now()
	cache, err := loadResponseCache()
	if err != nil {
		return "", &cacheHitResult{Key: key, Elapsed: time.Since(t0)}
	}
	entry, ok := cache[key]
	if !ok {
		return "", &cacheHitResult{Key: key, Elapsed: time.Since(t0)}
	}
	ts, err := time.Parse(time.RFC3339, entry.Timestamp)
	if err != nil {
		return "", &cacheHitResult{Key: key, Elapsed: time.Since(t0)}
	}
	age := time.Since(ts)
	if age > responseCacheTTL {
		delete(cache, key)
		saveResponseCache(cache)
		return "", &cacheHitResult{Key: key, Elapsed: time.Since(t0)}
	}
	entry.Hits++
	cache[key] = entry
	saveResponseCache(cache)
	return entry.Response, &cacheHitResult{
		Key:     key,
		Hit:     true,
		Age:     age,
		Model:   entry.Model,
		Elapsed: time.Since(t0),
	}
}

// cacheWrite stores a response in the cache, pruning expired entries.
func cacheWrite(key, response, model, cueName string) {
	cache, err := loadResponseCache()
	if err != nil {
		return
	}
	now := time.Now()
	for k, e := range cache {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil || now.Sub(ts) > responseCacheTTL {
			delete(cache, k)
		}
	}
	cache[key] = responseCacheEntry{
		Response:  response,
		Model:     model,
		Cue:       cueName,
		Timestamp: now.UTC().Format(time.RFC3339),
	}
	saveResponseCache(cache)
}

// formatAge formats a duration as a human-readable age string.
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
