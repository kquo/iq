package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

const hfAPIBase = "https://huggingface.co/api/models"

// ── HuggingFace API types ────────────────────────────────────────────────────

type hfModel struct {
	ID           string      `json:"id"`
	PipelineTag  string      `json:"pipeline_tag"`
	Downloads    int         `json:"downloads"`
	LastModified string      `json:"lastModified"`
	Siblings     []hfSibling `json:"siblings"`
	UsedStorage  int64       `json:"usedStorage"`
}

type hfSiblingLFS struct {
	Size int64 `json:"size"`
}

type hfSibling struct {
	Rfilename string       `json:"rfilename"`
	Size      int64        `json:"size"` // direct size (small files)
	LFS       hfSiblingLFS `json:"lfs"`  // lfs.size for large files
}

func (s hfSibling) fileSize() int64 {
	if s.LFS.Size > 0 {
		return s.LFS.Size
	}
	return s.Size
}

func (m hfModel) totalSize() int64 {
	if m.UsedStorage > 0 {
		return m.UsedStorage
	}
	var total int64
	for _, s := range m.Siblings {
		total += s.fileSize()
	}
	return total
}

func hfSearch(query string, limit int) ([]hfModel, error) {
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
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface returned status %d", resp.StatusCode)
	}

	var models []hfModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return models, nil
}

// hfFetchModel retrieves full model details (including sibling sizes) from the
// HF individual model endpoint: GET /api/models/{id}
func hfFetchModel(id string) (hfModel, error) {
	url := hfAPIBase + "/" + id
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return hfModel{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return hfModel{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var m hfModel
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return hfModel{}, err
	}
	return m, nil
}

// hfEnrichModels fetches full details for each model in parallel and merges
// sibling sizes back into the original slice.
func hfEnrichModels(models []hfModel) {
	var wg sync.WaitGroup
	enriched := make([]hfModel, len(models))
	for i, m := range models {
		enriched[i] = m // default: keep original
		wg.Add(1)
		go func(idx int, id string) {
			defer wg.Done()
			full, err := hfFetchModel(id)
			if err == nil {
				if full.PipelineTag != "" {
					enriched[idx].PipelineTag = full.PipelineTag
				}
				if full.UsedStorage > 0 {
					enriched[idx].UsedStorage = full.UsedStorage
				}
				if len(full.Siblings) > 0 {
					enriched[idx].Siblings = full.Siblings
				}
			}
		}(i, m.ID)
	}
	wg.Wait()
	copy(models, enriched)
}

// ── Manifest ─────────────────────────────────────────────────────────────────

// iqConfigDir returns ~/.config/iq, creating it if needed.
func iqConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "iq")
	return dir, os.MkdirAll(dir, 0755)
}

func manifestPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "models.json"), nil
}

type manifestEntry struct {
	ID       string `json:"id"`
	PulledAt string `json:"pulled_at"`
	HFCache  string `json:"hf_cache_path"`
	Task     string `json:"task,omitempty"`
}

func loadManifest() ([]manifestEntry, error) {
	path, err := manifestPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []manifestEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func saveManifest(entries []manifestEntry) error {
	path, err := manifestPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func addToManifest(id string) error {
	entries, err := loadManifest()
	if err != nil {
		return err
	}
	for i, e := range entries {
		if e.ID == id {
			entries[i].PulledAt = time.Now().UTC().Format(time.RFC3339)
			return saveManifest(entries)
		}
	}
	hfName := "models--" + strings.ReplaceAll(id, "/", "--")
	home, _ := os.UserHomeDir()
	hfCache := filepath.Join(home, ".cache", "huggingface", "hub", hfName)
	entries = append(entries, manifestEntry{
		ID:       id,
		PulledAt: time.Now().UTC().Format(time.RFC3339),
		HFCache:  hfCache,
	})
	return saveManifest(entries)
}

// registerInManifest adds id to the manifest only if not already present.
// Unlike addToManifest it does not update the PulledAt timestamp, so it is
// safe to call on every sidecar start without clobbering the download date.
func registerInManifest(id string) error {
	entries, err := loadManifest()
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
	entries = append(entries, manifestEntry{
		ID:       id,
		PulledAt: pulledAt,
		HFCache:  hfCache,
	})
	return saveManifest(entries)
}

func removeFromManifest(id string) (manifestEntry, bool, error) {
	entries, err := loadManifest()
	if err != nil {
		return manifestEntry{}, false, err
	}
	for i, e := range entries {
		if e.ID == id {
			updated := append(entries[:i], entries[i+1:]...)
			return e, true, saveManifest(updated)
		}
	}
	return manifestEntry{}, false, nil
}

// ── HF cache helpers ──────────────────────────────────────────────────────────

// hfCacheDir returns the expected HF cache directory for a model ID.
func hfCacheDir(id string) string {
	home, _ := os.UserHomeDir()
	hfName := "models--" + strings.ReplaceAll(id, "/", "--")
	return filepath.Join(home, ".cache", "huggingface", "hub", hfName)
}

// diskUsage sums the sizes of regular files under dir/blobs/ to avoid
// double-counting symlinks in snapshots/.
func diskUsage(cacheDir string) int64 {
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

// parseParams extracts a human-readable parameter count from a model name.
// e.g. "gemma-3-1b-it-4bit" → "1B", "smollm2-135m" → "135M"
func parseParams(id string) string {
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

// parseQuant extracts quantisation info from a model name.
func parseQuant(id string) string {
	name := strings.ToLower(filepath.Base(id))
	m := reQuant.FindString(name)
	if m == "" {
		return "-"
	}
	return m
}

// suggestTier returns a tier name based on model parameter count heuristic.
// < 500M → tiny, 500M–999M → fast, 1B–2.9B → balanced, 3B+ → quality
func suggestTier(id string) string {
	// Prefer actual disk size if model is already downloaded.
	disk := diskUsage(hfCacheDir(id))
	if disk > 0 {
		if disk < 2*1024*1024*1024 { // < 2GB
			return "fast"
		}
		return "slow"
	}
	// Not yet downloaded — estimate from parameter count in model name.
	raw := parseParams(id)
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

// commatize inserts thousands separators into an integer string.
func commatize(n int64) string {
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

func formatInt(n int) string {
	return commatize(int64(n))
}

// formatMB returns size as rounded integer MB with thousands separator.
func formatMB(b int64) string {
	if b == 0 {
		return "-"
	}
	mb := (b + 512*1024) / (1024 * 1024) // round to nearest MB
	return commatize(mb) + "MB"
}

// estMemMB returns a rough memory estimate (~1.5x disk) as rounded integer MB.
func estMemMB(diskBytes int64) string {
	if diskBytes == 0 {
		return "-"
	}
	mb := int64(float64(diskBytes)*1.5/float64(1024*1024) + 0.5)
	return "~" + commatize(mb) + "MB"
}

// parseParamsM returns parameter count always in M units, commatized.
// e.g. "1B" → "1,000M", "1.5B" → "1,500M", "135M" → "135M"
func parseParamsM(id string) string {
	raw := parseParams(id)
	if raw == "-" {
		return "-"
	}
	upper := strings.ToUpper(raw)
	if strings.HasSuffix(upper, "B") {
		numStr := raw[:len(raw)-1]
		var f float64
		fmt.Sscanf(numStr, "%f", &f)
		m := int64(f * 1000)
		return commatize(m) + "M"
	}
	// Already in M
	if strings.HasSuffix(upper, "M") {
		numStr := raw[:len(raw)-1]
		var f float64
		fmt.Sscanf(numStr, "%f", &f)
		return commatize(int64(f)) + "M"
	}
	return raw
}

// formatTask returns a colored task label for single-line display.
// "text-generation" is green (supported); everything else is red.
func formatTask(tag string) string {
	if tag == "" {
		return utl.Gra("-")
	}
	if tag == "text-generation" {
		return utl.Gre(tag)
	}
	return utl.Red(tag)
}

// formatTaskCol returns a fixed-width (24-char), colored task string for table columns.
func formatTaskCol(tag string) string {
	raw := tag
	if raw == "" {
		raw = "-"
	}
	display := raw
	if len(display) > 24 {
		display = display[:23] + "…"
	}
	padded := fmt.Sprintf("%-24s", display)
	if raw == "text-generation" {
		return utl.Gre(padded)
	}
	if raw != "-" {
		return utl.Red(padded)
	}
	return utl.Gra(padded)
}

// inferTaskFromConfig reads a local model's config.json and infers the
// pipeline_tag from model_type. Returns "" if it cannot determine the task.
func inferTaskFromConfig(modelID string) string {
	cacheDir := hfCacheDir(modelID)
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

// hfFetchTags fetches pipeline_tag for manifest entries that have no Task cached.
// It updates the entries in place and returns true if any were updated.
func hfFetchTags(entries []manifestEntry) bool {
	// Collect indices that need fetching.
	var need []int
	for i, e := range entries {
		if e.Task == "" {
			need = append(need, i)
		}
	}
	if len(need) == 0 {
		return false
	}

	var wg sync.WaitGroup
	type result struct {
		idx int
		tag string
	}
	ch := make(chan result, len(need))
	for _, idx := range need {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			// Try HF API first, fall back to local config.json inference.
			if m, err := hfFetchModel(id); err == nil && m.PipelineTag != "" {
				ch <- result{i, m.PipelineTag}
				return
			}
			if tag := inferTaskFromConfig(id); tag != "" {
				ch <- result{i, tag}
			}
		}(idx, entries[idx].ID)
	}
	wg.Wait()
	close(ch)

	updated := false
	for r := range ch {
		entries[r.idx].Task = r.tag
		updated = true
	}
	return updated
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printLmHelp() {
	n := program_name
	fmt.Printf("Work with IQ language models.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s lm <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "search [query|count]", "Search MLX model registry; numeric arg sets result count")
	fmt.Printf("  %-10s %s\n", "get", "Download a model from the registry")
	fmt.Printf("  %-10s %s\n", "list", "List locally available models (alias: ls)")
	fmt.Printf("  %-10s %s\n", "show", "Show details for a model")
	fmt.Printf("  %-10s %s\n\n", "rm", "Remove a model")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("ARGUMENTS"))
	fmt.Printf("  A model name can be supplied as an argument.\n\n")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s lm search\n", n)
	fmt.Printf("  $ %s lm search gemma\n", n)
	fmt.Printf("  $ %s lm get mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s lm list\n", n)
	fmt.Printf("  $ %s lm show mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s lm rm mlx-community/gemma-3-1b-it-4bit\n\n", n)
}

// ── Root lm command ───────────────────────────────────────────────────────────

func newLmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "lm",
		Short:        "Work with IQ language models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printLmHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printLmHelp()
	})

	cmd.AddCommand(
		newLmSearchCmd(),
		newLmGetCmd(),
		newLmListCmd(),
		newLmShowCmd(),
		newLmRmCmd(),
	)
	return cmd
}

// ── search ────────────────────────────────────────────────────────────────────

func newLmSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:          "search [query|count]",
		Short:        "Search MLX model registry on Hugging Face",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				// Pure integer arg → treat as result count, not a search query.
				if n, err := strconv.Atoi(args[0]); err == nil {
					if n > limit {
						limit = n
					}
				} else {
					query = args[0]
				}
			}
			if limit < 20 {
				limit = 20
			}

			models, err := hfSearch(query, limit)
			if err != nil {
				return err
			}
			hfEnrichModels(models)

			fmt.Printf("%-60s  %-24s  %10s  %10s  %12s  %12s\n",
				"MODEL", "TASK", "DISK", "PARAMS", "EST MEM", "DOWNLOADS")
			for _, m := range models {
				disk := m.totalSize()
				name := m.ID
				if len(name) > 60 {
					name = name[:59] + "…"
				}
				fmt.Printf("%-60s  %s  %10s  %10s  %12s  %12s\n",
					name,
					formatTaskCol(m.PipelineTag),
					formatMB(disk),
					parseParamsM(m.ID),
					estMemMB(disk),
					formatInt(m.Downloads),
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Max number of results to return")
	return cmd
}

// ── get ───────────────────────────────────────────────────────────────────────

func newLmGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <model>",
		Short:        "Download a model from the registry",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]

			// Check task type before downloading.
			if m, err := hfFetchModel(model); err == nil && m.PipelineTag != "" && m.PipelineTag != "text-generation" {
				fmt.Fprintf(os.Stderr, "%s\n",
					utl.Yel(fmt.Sprintf("Warning: model task is %q — IQ only supports text-generation", m.PipelineTag)))
			}

			// Run via shell so it inherits the user's full PATH
			// (hf is often installed in a pip user bin dir not visible to exec directly).
			hfCmd := exec.Command("/bin/sh", "-c", "hf download "+shellescape(model))
			hfCmd.Env = os.Environ()

			stdout, err := hfCmd.StdoutPipe()
			if err != nil {
				return err
			}
			hfCmd.Stderr = os.Stderr

			if err := hfCmd.Start(); err != nil {
				return fmt.Errorf("failed to start: %w", err)
			}

			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}

			if err := hfCmd.Wait(); err != nil {
				return fmt.Errorf("get failed (is hf installed? pip install huggingface_hub[cli]): %w", err)
			}

			if err := addToManifest(model); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: failed to update manifest: "+err.Error()))
			}

			// Cache the pipeline_tag in the manifest.
			if entries, err := loadManifest(); err == nil {
				for i, e := range entries {
					if e.ID == model && e.Task == "" {
						tag := ""
						if m, err := hfFetchModel(model); err == nil && m.PipelineTag != "" {
							tag = m.PipelineTag
						} else {
							tag = inferTaskFromConfig(model)
						}
						if tag != "" {
							entries[i].Task = tag
							_ = saveManifest(entries)
						}
						break
					}
				}
			}

			tier := suggestTier(model)
			fmt.Printf("\nSuggested tier: %s\n", utl.Gre(tier))
			fmt.Printf("%s\n", utl.Gra(
				fmt.Sprintf("  iq svc tier add %s %s", tier, model)))

			return nil
		},
	}
}

// ── list / ls ─────────────────────────────────────────────────────────────────

func newLmListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List locally available models",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := loadManifest()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("No models. Use 'iq lm get <model>' to download one.")
				return nil
			}

			// Backfill task tags from HF API for entries that don't have one cached.
			if hfFetchTags(entries) {
				_ = saveManifest(entries)
			}

			fmt.Printf("%-55s  %-24s  %8s  %-10s  %8s  %10s  %s\n",
				"MODEL", "TASK", "DISK", "PULLED", "PARAMS", "EST MEM", "TIER")
			cfg, _ := loadConfig()
			emM := embedModel(cfg)
			for _, e := range entries {
				disk := diskUsage(hfCacheDir(e.ID))
				pulled := ""
				if t, err := time.Parse(time.RFC3339, e.PulledAt); err == nil {
					pulled = t.Format("2006-01-02")
				}
				var tierDisplay string
				if e.ID == emM {
					tierDisplay = utl.Gre(fmt.Sprintf("%-6s", "embed"))
				} else {
					tier := tierForModel(e.ID)
					tierRaw := "<unset>"
					if tier != "" {
						tierRaw = tier
					}
					tierDisplay = utl.Gra(fmt.Sprintf("%-6s", tierRaw))
					if tier != "" {
						tierDisplay = utl.Gre(fmt.Sprintf("%-6s", tierRaw))
					}
				}
				fmt.Printf("%-55s  %s  %8s  %-10s  %8s  %10s  %s\n",
					e.ID,
					formatTaskCol(e.Task),
					formatMB(disk),
					pulled,
					parseParamsM(e.ID),
					estMemMB(disk),
					tierDisplay,
				)
			}
			return nil
		},
	}
}

// ── show ──────────────────────────────────────────────────────────────────────

// snapshotDir returns the path to the most recent snapshot directory for a model,
// which is what mlx_lm tools expect as the --model argument.
func snapshotDir(modelID string) (string, error) {
	cacheDir := hfCacheDir(modelID)
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

// snapshotFiles returns the files in the most recent snapshot of an HF cache dir
// with their resolved sizes via the blobs symlinks.
func snapshotFiles(cacheDir string) ([]snapshotFile, error) {
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

	var files []snapshotFile
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
		files = append(files, snapshotFile{Name: rel, Size: size})
		return nil
	})
	return files, nil
}

type snapshotFile struct {
	Name string
	Size int64
}

func newLmShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show <model>",
		Short:        "Show details for a specific model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := loadManifest()
			if err != nil {
				return err
			}

			id := args[0]
			var entry *manifestEntry
			for i := range entries {
				if entries[i].ID == id {
					entry = &entries[i]
					break
				}
			}
			if entry == nil {
				return fmt.Errorf("model %q not found in manifest", id)
			}

			// Backfill task tag if missing — try HF API, then local config.json.
			if entry.Task == "" {
				tag := ""
				if m, err := hfFetchModel(entry.ID); err == nil && m.PipelineTag != "" {
					tag = m.PipelineTag
				} else {
					tag = inferTaskFromConfig(entry.ID)
				}
				if tag != "" {
					entry.Task = tag
					for i := range entries {
						if entries[i].ID == entry.ID {
							entries[i].Task = tag
							break
						}
					}
					_ = saveManifest(entries)
				}
			}

			cacheDir := hfCacheDir(entry.ID)
			disk := diskUsage(cacheDir)
			pulled := ""
			if t, err := time.Parse(time.RFC3339, entry.PulledAt); err == nil {
				pulled = t.Format("2006-01-02")
			}

			fmt.Printf("%-12s %s\n", "MODEL", entry.ID)
			fmt.Printf("%-12s %s\n", "TASK", formatTask(entry.Task))

			// ── PERFORMANCE ───────────────────────────────────────────
			bs, bsErr := loadBenchStore()
			if bsErr == nil && bs != nil {
				results := resultsFor(bs, entry.ID, "")
				if len(results) == 0 {
					fmt.Printf("%-12s %s\n", "PERFORMANCE",
						utl.Gra("<not benchmarked>"))
				} else {
					first := true
					for _, r := range results {
						label := ""
						if first {
							label = "PERFORMANCE"
							first = false
						}
						fmt.Printf("%-12s %s\n", label,
							formatBenchRow(r))
					}
				}
			}

			fmt.Printf("%-12s %s\n", "PARAMS", parseParamsM(entry.ID))
			fmt.Printf("%-12s %s\n", "QUANT", parseQuant(entry.ID))
			fmt.Printf("%-12s %s\n", "DISK", formatMB(disk))
			fmt.Printf("%-12s %s\n", "EST MEM", estMemMB(disk))
			fmt.Printf("%-12s %s\n", "PULLED", pulled)
			fmt.Printf("%-12s %s\n", "CACHE", cacheDir)
			fmt.Printf("%-12s %s\n", "CUE", cueForModel(entry.ID))

			tier := tierForModel(entry.ID)
			if tier == "" {
				suggested := suggestTier(entry.ID)
				fmt.Printf("%-12s %s\n", "TIER", utl.Gra("<unset>"))
				fmt.Printf("%-12s %s\n", "",
					utl.Gra(fmt.Sprintf("iq svc tier add %s %s", suggested, entry.ID)))
			} else {
				fmt.Printf("%-12s %s\n", "TIER", utl.Gre(tier))
			}

			files, ferr := snapshotFiles(cacheDir)
			if ferr == nil && len(files) > 0 {
				fmt.Printf("\n%-44s  %15s\n", "FILES", "SIZE")
				for _, f := range files {
					fmt.Printf("  %-42s  %15s\n", f.Name, commatize(f.Size))
				}
			}
			return nil
		},
	}
}

// ── rm ────────────────────────────────────────────────────────────────────────

func newLmRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "rm <model>",
		Short:        "Remove a model",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			cacheDir := hfCacheDir(model)

			// Refuse to remove a model assigned as the embed model.
			cfg, _ := loadConfig()
			if cfg != nil && model == embedModel(cfg) {
				s, _ := readState(embedSlugConst)
				if s != nil && pidAlive(s.PID) {
					return fmt.Errorf("%s is the embed model and its sidecar is running\n"+
						"  Run 'iq svc stop' first", model)
				}
				return fmt.Errorf("%s is the embed model\n"+
					"  Run 'iq svc embed rm' to revert it before removing", model)
			}

			// Refuse to remove a model that is assigned to a tier.
			if t := tierForModel(model); t != "" {
				// Also check if the sidecar is running.
				state, _ := readState(model)
				if state != nil && pidAlive(state.PID) {
					return fmt.Errorf("%s is in the %s tier and its sidecar is running\n"+
						"  Run 'iq svc stop %s' then 'iq svc tier rm %s %s' before removing", model, t, model, t, model)
				}
				return fmt.Errorf("%s is in the %s tier\n"+
					"  Run 'iq svc tier rm %s %s' before removing", model, t, t, model)
			}

			if !force {
				disk := diskUsage(cacheDir)
				fmt.Printf("Remove %s (%s)? [y/N] ", model, formatMB(disk))
				var resp string
				fmt.Scanln(&resp)
				if strings.ToLower(strings.TrimSpace(resp)) != "y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			entry, found, err := removeFromManifest(model)
			if err != nil {
				return fmt.Errorf("failed to update manifest: %w", err)
			}
			if !found {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: model not found in manifest"))
			}

			// Use entry's recorded cache path if available, fall back to derived path.
			dir := cacheDir
			if entry.HFCache != "" {
				dir = entry.HFCache
			}

			if _, err := os.Stat(dir); os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("warning: cache directory not found: "+dir))
				return nil
			}

			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("failed to remove cache: %w", err)
			}

			fmt.Printf("Removed %s\n", model)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

// ── shellescape ───────────────────────────────────────────────────────────────

// shellescape single-quotes a string for safe shell interpolation.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
