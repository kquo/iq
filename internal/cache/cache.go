package cache

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
	responseFile = "response_cache.json"
	TTL          = 1 * time.Hour
)

// Entry stores a cached inference response.
type Entry struct {
	Response  string `json:"response"`
	Model     string `json:"model"`
	Cue       string `json:"cue"`
	Timestamp string `json:"timestamp"`
	Hits      int    `json:"hits"`
}

// HitResult carries details of a cache lookup for trace output.
type HitResult struct {
	Key     string
	Hit     bool
	Age     time.Duration
	Model   string
	Elapsed time.Duration
}

// Message is an alias for config.Message for cache key computation.
type Message = config.Message

func responsePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, responseFile), nil
}

func load() (map[string]Entry, error) {
	path, err := responsePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]Entry), nil
	}
	if err != nil {
		return nil, err
	}
	var c map[string]Entry
	if err := json.Unmarshal(data, &c); err != nil {
		return make(map[string]Entry), nil
	}
	return c, nil
}

func save(c map[string]Entry) error {
	path, err := responsePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Key computes an FNV64a hash over the message array and model ID.
func Key(messages []Message, model string) string {
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

// Check looks up a response in the cache. Returns the cached response
// (empty on miss) and a result struct for trace output.
func Check(key string) (string, *HitResult) {
	t0 := time.Now()
	c, err := load()
	if err != nil {
		return "", &HitResult{Key: key, Elapsed: time.Since(t0)}
	}
	entry, ok := c[key]
	if !ok {
		return "", &HitResult{Key: key, Elapsed: time.Since(t0)}
	}
	ts, err := time.Parse(time.RFC3339, entry.Timestamp)
	if err != nil {
		return "", &HitResult{Key: key, Elapsed: time.Since(t0)}
	}
	age := time.Since(ts)
	if age > TTL {
		delete(c, key)
		_ = save(c) // best-effort: prune expired entry
		return "", &HitResult{Key: key, Elapsed: time.Since(t0)}
	}
	entry.Hits++
	c[key] = entry
	_ = save(c) // best-effort: persist hit count
	return entry.Response, &HitResult{
		Key:     key,
		Hit:     true,
		Age:     age,
		Model:   entry.Model,
		Elapsed: time.Since(t0),
	}
}

// Write stores a response in the cache, pruning expired entries.
func Write(key, response, model, cueName string) {
	c, err := load()
	if err != nil {
		return
	}
	now := time.Now()
	for k, e := range c {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil || now.Sub(ts) > TTL {
			delete(c, k)
		}
	}
	c[key] = Entry{
		Response:  response,
		Model:     model,
		Cue:       cueName,
		Timestamp: now.UTC().Format(time.RFC3339),
	}
	_ = save(c) // best-effort: cache write failures are non-fatal
}

// FormatAge formats a duration as a human-readable age string.
func FormatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
