package lm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"iq/internal/color"

	"iq/internal/config"
)

const hfAPIBase = "https://huggingface.co/api/models"

// ── HuggingFace API types ────────────────────────────────────────────────────

// HFModel represents a model from the HuggingFace API.
type HFModel struct {
	ID           string      `json:"id"`
	PipelineTag  string      `json:"pipeline_tag"`
	Downloads    int         `json:"downloads"`
	LastModified string      `json:"lastModified"`
	Siblings     []HFSibling `json:"siblings"`
	UsedStorage  int64       `json:"usedStorage"`
}

// HFSiblingLFS holds LFS size metadata.
type HFSiblingLFS struct {
	Size int64 `json:"size"`
}

// HFSibling represents a file in a HuggingFace model.
type HFSibling struct {
	Rfilename string       `json:"rfilename"`
	Size      int64        `json:"size"` // direct size (small files)
	LFS       HFSiblingLFS `json:"lfs"`  // lfs.size for large files
}

// FileSize returns the resolved file size (LFS or direct).
func (s HFSibling) FileSize() int64 {
	if s.LFS.Size > 0 {
		return s.LFS.Size
	}
	return s.Size
}

// TotalSize returns the total disk size for a model.
func (m HFModel) TotalSize() int64 {
	if m.UsedStorage > 0 {
		return m.UsedStorage
	}
	var total int64
	for _, s := range m.Siblings {
		total += s.FileSize()
	}
	return total
}

// HFSearch queries the HuggingFace API for MLX models.
func HFSearch(ctx context.Context, query string, limit int) ([]HFModel, error) {
	u, _ := url.Parse(hfAPIBase)
	q := u.Query()
	q.Set("search", query)
	q.Set("filter", "mlx")
	q.Set("full", "true")
	q.Set("sort", "downloads")
	q.Set("direction", "-1")
	q.Set("limit", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface returned status %d", resp.StatusCode)
	}

	var models []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return models, nil
}

// HFFetchModel retrieves full model details (including sibling sizes) from the
// HF individual model endpoint: GET /api/models/{id}
func HFFetchModel(ctx context.Context, id string) (HFModel, error) {
	rawURL := hfAPIBase + "/" + id
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return HFModel{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return HFModel{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return HFModel{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var m HFModel
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return HFModel{}, err
	}
	return m, nil
}

// HFEnrichModels fetches full details for each model in parallel and merges
// sibling sizes back into the original slice. The first fetch error cancels
// remaining fetches; enrichment errors are non-fatal (original data is kept).
func HFEnrichModels(ctx context.Context, models []HFModel) error {
	enriched := make([]HFModel, len(models))
	copy(enriched, models) // default: keep originals
	g, gctx := errgroup.WithContext(ctx)
	for i, m := range models {
		g.Go(func() error {
			full, err := HFFetchModel(gctx, m.ID)
			if err != nil {
				return err
			}
			if full.PipelineTag != "" {
				enriched[i].PipelineTag = full.PipelineTag
			}
			if full.UsedStorage > 0 {
				enriched[i].UsedStorage = full.UsedStorage
			}
			if len(full.Siblings) > 0 {
				enriched[i].Siblings = full.Siblings
			}
			return nil
		})
	}
	err := g.Wait()
	copy(models, enriched)
	return err
}

// ── Manifest ─────────────────────────────────────────────────────────────────

// ManifestEntry represents a locally available model.
type ManifestEntry struct {
	ID       string `json:"id"`
	PulledAt string `json:"pulled_at"`
	HFCache  string `json:"hf_cache_path"`
	Task     string `json:"task,omitempty"`
}

// ManifestPath returns the path to ~/.config/iq/models.json.
func ManifestPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

// LoadManifest reads and parses the model manifest.
func LoadManifest() ([]ManifestEntry, error) {
	path, err := ManifestPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ManifestEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []ManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveManifest writes the manifest to disk.
func SaveManifest(entries []ManifestEntry) error {
	path, err := ManifestPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AddToManifest adds or updates a model in the manifest with a fresh timestamp.
func AddToManifest(id string) error {
	entries, err := LoadManifest()
	if err != nil {
		return err
	}
	for i, e := range entries {
		if e.ID == id {
			entries[i].PulledAt = time.Now().UTC().Format(time.RFC3339)
			return SaveManifest(entries)
		}
	}
	hfName := "models--" + strings.ReplaceAll(id, "/", "--")
	home, _ := os.UserHomeDir()
	hfCache := filepath.Join(home, ".cache", "huggingface", "hub", hfName)
	entries = append(entries, ManifestEntry{
		ID:       id,
		PulledAt: time.Now().UTC().Format(time.RFC3339),
		HFCache:  hfCache,
	})
	return SaveManifest(entries)
}

// RegisterInManifest adds id to the manifest only if not already present.
// Unlike AddToManifest it does not update the PulledAt timestamp, so it is
// safe to call on every sidecar start without clobbering the download date.
func RegisterInManifest(id string) error {
	entries, err := LoadManifest()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.ID == id {
			return nil // already registered
		}
	}
	hfName := "models--" + strings.ReplaceAll(id, "/", "--")
	home, _ := os.UserHomeDir()
	hfCache := filepath.Join(home, ".cache", "huggingface", "hub", hfName)
	// Use the mtime of the cache dir as a proxy for when the model was pulled.
	pulledAt := time.Now().UTC().Format(time.RFC3339)
	if info, err := os.Stat(hfCache); err == nil {
		pulledAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	entries = append(entries, ManifestEntry{
		ID:       id,
		PulledAt: pulledAt,
		HFCache:  hfCache,
	})
	return SaveManifest(entries)
}

// RemoveFromManifest removes a model from the manifest.
func RemoveFromManifest(id string) (ManifestEntry, bool, error) {
	entries, err := LoadManifest()
	if err != nil {
		return ManifestEntry{}, false, err
	}
	for i, e := range entries {
		if e.ID == id {
			updated := append(entries[:i], entries[i+1:]...)
			return e, true, SaveManifest(updated)
		}
	}
	return ManifestEntry{}, false, nil
}

// ── HF cache helpers ──────────────────────────────────────────────────────────

// HFCacheDir returns the expected HF cache directory for a model ID.
func HFCacheDir(id string) string {
	home, _ := os.UserHomeDir()
	hfName := "models--" + strings.ReplaceAll(id, "/", "--")
	return filepath.Join(home, ".cache", "huggingface", "hub", hfName)
}

// DiskUsage sums the sizes of regular files under dir/blobs/ to avoid
// double-counting symlinks in snapshots/.
func DiskUsage(cacheDir string) int64 {
	blobsDir := filepath.Join(cacheDir, "blobs")
	var total int64
	filepath.Walk(blobsDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// ── Model name parsing ────────────────────────────────────────────────────────

var reParams = regexp.MustCompile(`(?i)[^a-z](\d+\.?\d*)(b|m)(?:[^a-z]|$)`)

// ParseParams extracts a human-readable parameter count from a model name.
// e.g. "gemma-3-1b-it-4bit" → "1B", "smollm2-135m" → "135M"
func ParseParams(id string) string {
	name := strings.ToLower(filepath.Base(id))
	m := reParams.FindStringSubmatch("-" + name)
	if m == nil {
		return "-"
	}
	val := m[1]
	unit := strings.ToUpper(m[2])
	// Normalise: drop trailing ".0" only for whole numbers shown as floats
	if strings.Contains(val, ".") {
		var f float64
		fmt.Sscanf(val, "%f", &f)
		if f == math.Trunc(f) {
			return fmt.Sprintf("%.0f%s", f, unit)
		}
		return fmt.Sprintf("%s%s", val, unit)
	}
	return val + unit
}

var reQuant = regexp.MustCompile(`(?i)(qat[-_]?\d+bit|\d+bit|bf16|f16|fp16)`)

// ParseQuant extracts quantisation info from a model name.
func ParseQuant(id string) string {
	name := strings.ToLower(filepath.Base(id))
	m := reQuant.FindString(name)
	if m == "" {
		return "-"
	}
	return m
}

// SuggestTier returns a tier name based on model parameter count heuristic.
func SuggestTier(id string) string {
	// Prefer actual disk size if model is already downloaded.
	disk := DiskUsage(HFCacheDir(id))
	if disk > 0 {
		if disk < 2*1024*1024*1024 { // < 2GB
			return "fast"
		}
		return "slow"
	}
	// Not yet downloaded — estimate from parameter count in model name.
	raw := ParseParams(id)
	if raw == "-" {
		return "slow" // unknown → assume large
	}
	upper := strings.ToUpper(raw)
	var mb int64
	if strings.HasSuffix(upper, "B") {
		var f float64
		fmt.Sscanf(raw[:len(raw)-1], "%f", &f)
		mb = int64(f * 1000)
	} else if strings.HasSuffix(upper, "M") {
		var f float64
		fmt.Sscanf(raw[:len(raw)-1], "%f", &f)
		mb = int64(f)
	} else {
		return "slow"
	}
	// Rough heuristic: 4-bit quant ~0.5 bytes/param + overhead.
	// 2GB disk ≈ ~3B params at 4-bit.
	if mb < 3000 {
		return "fast"
	}
	return "slow"
}

// ── Formatting helpers ────────────────────────────────────────────────────────

// Commatize inserts thousands separators into an integer string.
func Commatize(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteString(",")
		}
		result.WriteString(string(c))
	}
	return result.String()
}

// FormatInt formats an int with thousands separators.
func FormatInt(n int) string {
	return Commatize(int64(n))
}

// FormatMB returns size as rounded integer MB with thousands separator.
func FormatMB(b int64) string {
	if b == 0 {
		return "-"
	}
	mb := (b + 512*1024) / (1024 * 1024) // round to nearest MB
	return Commatize(mb) + "MB"
}

// EstMemMB returns a rough memory estimate (~1.5x disk) as rounded integer MB.
func EstMemMB(diskBytes int64) string {
	if diskBytes == 0 {
		return "-"
	}
	mb := int64(float64(diskBytes)*1.5/float64(1024*1024) + 0.5)
	return "~" + Commatize(mb) + "MB"
}

// ParseParamsM returns parameter count always in M units, commatized.
// e.g. "1B" → "1,000M", "1.5B" → "1,500M", "135M" → "135M"
func ParseParamsM(id string) string {
	raw := ParseParams(id)
	if raw == "-" {
		return "-"
	}
	upper := strings.ToUpper(raw)
	if strings.HasSuffix(upper, "B") {
		numStr := raw[:len(raw)-1]
		var f float64
		fmt.Sscanf(numStr, "%f", &f)
		m := int64(f * 1000)
		return Commatize(m) + "M"
	}
	// Already in M
	if strings.HasSuffix(upper, "M") {
		numStr := raw[:len(raw)-1]
		var f float64
		fmt.Sscanf(numStr, "%f", &f)
		return Commatize(int64(f)) + "M"
	}
	return raw
}

// FormatTask returns a colored task label for single-line display.
func FormatTask(tag string) string {
	if tag == "" {
		return color.Gra("-")
	}
	if tag == "text-generation" {
		return color.Grn(tag)
	}
	if tag == "feature-extraction" {
		return color.Grn("embedding")
	}
	return color.Red(tag)
}

// FormatTaskCol returns a fixed-width (24-char), colored task string for table columns.
func FormatTaskCol(tag string) string {
	raw := tag
	if raw == "" {
		raw = "-"
	}
	display := raw
	if raw == "feature-extraction" {
		display = "embedding"
	}
	if len(display) > 24 {
		display = display[:23] + "…"
	}
	padded := fmt.Sprintf("%-24s", display)
	if raw == "text-generation" || raw == "feature-extraction" {
		return color.Grn(padded)
	}
	if raw != "-" {
		return color.Red(padded)
	}
	return color.Gra(padded)
}

// InferTaskFromConfig reads a local model's config.json and infers the
// pipeline_tag from model_type. Returns "" if it cannot determine the task.
func InferTaskFromConfig(modelID string) string {
	cacheDir := HFCacheDir(modelID)
	snapshotsDir := filepath.Join(cacheDir, "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return ""
	}
	var snapDir string
	for _, e := range entries {
		if e.IsDir() {
			snapDir = filepath.Join(snapshotsDir, e.Name())
		}
	}
	if snapDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(snapDir, "config.json"))
	if err != nil {
		return ""
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}

	// VLM indicators → image-text-to-text
	for _, key := range []string{"vision_config", "visual", "vision_tower", "image_size"} {
		if _, ok := cfg[key]; ok {
			return "image-text-to-text"
		}
	}

	mt, _ := cfg["model_type"].(string)
	if mt == "" {
		return ""
	}

	// Known VLM model_type values
	if slices.Contains([]string{
		"qwen2_5_vl", "qwen2_vl", "llava", "idefics", "paligemma", "mllama",
	}, mt) {
		return "image-text-to-text"
	}

	// Known text-generation model_type values
	if slices.Contains([]string{
		"gemma", "gemma2", "gemma3",
		"llama", "mistral", "mixtral",
		"phi", "phi3", "phimoe",
		"qwen2", "qwen3",
		"starcoder2", "codellama",
		"deepseek_v2", "deepseek_v3",
		"cohere", "cohere2",
		"stablelm",
	}, mt) {
		return "text-generation"
	}

	return ""
}

// HFFetchTags fetches pipeline_tag for manifest entries that have no Task cached.
// It updates the entries in place and returns true if any were updated. The first
// fetch error cancels remaining fetches; tag-fetch errors are non-fatal.
func HFFetchTags(ctx context.Context, entries []ManifestEntry) (bool, error) {
	// Collect indices that need fetching.
	var need []int
	for i, e := range entries {
		if e.Task == "" {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return false, nil
	}

	type result struct {
		idx int
		tag string
	}
	ch := make(chan result, len(need))
	g, gctx := errgroup.WithContext(ctx)
	for _, idx := range need {
		g.Go(func() error {
			id := entries[idx].ID
			// Try HF API first, fall back to local config.json inference.
			if m, err := HFFetchModel(gctx, id); err == nil && m.PipelineTag != "" {
				ch <- result{idx, m.PipelineTag}
				return nil
			}
			if tag := InferTaskFromConfig(id); tag != "" {
				ch <- result{idx, tag}
			}
			return nil
		})
	}
	err := g.Wait()
	close(ch)

	updated := false
	for r := range ch {
		entries[r.idx].Task = r.tag
		updated = true
	}
	return updated, err
}

// ── Snapshot helpers ──────────────────────────────────────────────────────────

// SnapshotFile holds display info for a file in a model snapshot.
type SnapshotFile struct {
	Name string
	Size int64
}

// IsModelNotDownloaded reports whether err indicates a model has not been
// downloaded to the local HuggingFace cache yet.
func IsModelNotDownloaded(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no snapshots found")
}

// SnapshotDir returns the path to the most recent snapshot directory for a model,
// which is what mlx_lm tools expect as the --model argument.
func SnapshotDir(modelID string) (string, error) {
	cacheDir := HFCacheDir(modelID)
	snapshotsDir := filepath.Join(cacheDir, "snapshots")
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return "", fmt.Errorf("no snapshots found for %s: %w", modelID, err)
	}
	var snapDir string
	for _, e := range entries {
		if e.IsDir() {
			snapDir = filepath.Join(snapshotsDir, e.Name())
		}
	}
	if snapDir == "" {
		return "", fmt.Errorf("no snapshots found for %s", modelID)
	}
	return snapDir, nil
}

// SnapshotFiles returns the files in the most recent snapshot of an HF cache dir
// with their resolved sizes via the blobs symlinks.
func SnapshotFiles(cacheDir string) ([]SnapshotFile, error) {
	snapshotsDir := filepath.Join(cacheDir, "snapshots")
	dirEntries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		return nil, err
	}
	// Pick the last (most recent) snapshot directory.
	var snapDir string
	for _, e := range dirEntries {
		if e.IsDir() {
			snapDir = filepath.Join(snapshotsDir, e.Name())
		}
	}
	if snapDir == "" {
		return nil, fmt.Errorf("no snapshots found")
	}

	var files []SnapshotFile
	filepath.Walk(snapDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(snapDir, path)
		var size int64
		// Symlinks point into blobs/; resolve to get real size.
		resolved, rerr := filepath.EvalSymlinks(path)
		if rerr == nil {
			if rinfo, serr := os.Stat(resolved); serr == nil {
				size = rinfo.Size()
			}
		} else {
			size = info.Size()
		}
		files = append(files, SnapshotFile{Name: rel, Size: size})
		return nil
	})
	return files, nil
}
