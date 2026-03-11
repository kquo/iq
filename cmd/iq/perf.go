package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"iq/internal/config"
)

//go:embed bench_corpus.yaml
var benchCorpusYAML string

// ── Benchmark Corpus Data Structures ───────────────────────────────────────

type benchDoc struct {
	ID   string `yaml:"id"`
	Text string `yaml:"text"`
}

type benchQuery struct {
	Query    string   `yaml:"query"`
	Relevant []string `yaml:"relevant"`
}

type benchCueInput struct {
	Text        string `yaml:"text"`
	ExpectedCue string `yaml:"expected_cue"`
}

type benchInferPrompt struct {
	ID   string `yaml:"id"`
	Text string `yaml:"text"`
}

type benchToolPrompt struct {
	ID           string `yaml:"id"`
	Text         string `yaml:"text"`
	ExpectedTool string `yaml:"expected_tool"`
}

type benchCorpus struct {
	KBDocs       []benchDoc         `yaml:"kb_docs"`
	KBQueries    []benchQuery       `yaml:"kb_queries"`
	CueInputs    []benchCueInput    `yaml:"cue_inputs"`
	InferPrompts []benchInferPrompt `yaml:"infer_prompts"`
	ToolPrompts  []benchToolPrompt  `yaml:"tool_prompts"`
}

// loadBenchCorpus parses the embedded bench_corpus.yaml.
func loadBenchCorpus() (*benchCorpus, error) {
	var corpus benchCorpus
	if err := yaml.Unmarshal([]byte(benchCorpusYAML), &corpus); err != nil {
		return nil, fmt.Errorf("failed to parse bench corpus: %w", err)
	}
	return &corpus, nil
}

// ── Benchmark Result Data Structures ───────────────────────────────────────

// HWConfig captures the hardware snapshot at benchmark time.
type HWConfig struct {
	CPUCores int    `json:"cpu_cores"`
	MemGB    int    `json:"mem_gb"`
	OSVer    string `json:"os_version"`
}

// BenchResult holds one complete benchmark run for one model and type.
type BenchResult struct {
	ModelID      string   `json:"model_id"`
	Type         string   `json:"type"`     // "kb", "cue", "infer", "tool"
	BenchAt      string   `json:"bench_at"` // RFC3339 UTC
	HW           HWConfig `json:"hw"`
	SampleCount  int      `json:"sample_count"`
	ThroughputPS float64  `json:"throughput_ps"` // docs/sec, texts/sec, tokens/sec
	P50LatMs     float64  `json:"p50_lat_ms"`
	P95LatMs     float64  `json:"p95_lat_ms"`
	MRR          float64  `json:"mrr,omitempty"`       // kb only
	Accuracy     float64  `json:"accuracy,omitempty"`  // cue only
	AvgScore     float64  `json:"avg_score,omitempty"` // cue only
}

// BenchStore is the JSON file at ~/.config/iq/benchmarks.json.
type BenchStore struct {
	Results []BenchResult `json:"results"`
}

// ── Storage Helpers ───────────────────────────────────────────────────────

// benchStorePath returns ~/.config/iq/benchmarks.json.
func benchStorePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "benchmarks.json"), nil
}

// loadBenchStore reads and parses benchmarks.json; returns empty store if not found.
func loadBenchStore() (*BenchStore, error) {
	path, err := benchStorePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &BenchStore{}, nil
	}
	if err != nil {
		return nil, err
	}
	var bs BenchStore
	if err := json.Unmarshal(data, &bs); err != nil {
		return nil, fmt.Errorf("failed to parse benchmarks.json: %w", err)
	}
	return &bs, nil
}

// saveBenchStore writes BenchStore to benchmarks.json with indentation.
func saveBenchStore(bs *BenchStore) error {
	path, err := benchStorePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// upsertResult replaces the existing result for (ModelID, Type) or appends.
func upsertResult(bs *BenchStore, r BenchResult) {
	for i, existing := range bs.Results {
		if existing.ModelID == r.ModelID && existing.Type == r.Type {
			bs.Results[i] = r
			return
		}
	}
	bs.Results = append(bs.Results, r)
}

// resultsFor returns all BenchResults matching optional modelID and/or benchType filters.
// Empty string means "all".
func resultsFor(bs *BenchStore, modelID, benchType string) []BenchResult {
	var out []BenchResult
	for _, r := range bs.Results {
		if modelID != "" && r.ModelID != modelID {
			continue
		}
		if benchType != "" && r.Type != benchType {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ── Hardware Snapshot ──────────────────────────────────────────────────────

// captureHW returns a HWConfig for the current machine.
func captureHW() HWConfig {
	hw := HWConfig{
		CPUCores: runtime.NumCPU(),
		OSVer:    runtime.GOOS,
	}

	// Try to get memory on Darwin.
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err == nil {
			var bytes int64
			if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &bytes); err == nil {
				hw.MemGB = int(bytes / (1024 * 1024 * 1024))
			}
		}
	}

	// Append uname -r for more detail.
	out, err := exec.Command("uname", "-r").Output()
	if err == nil {
		hw.OSVer += " " + strings.TrimSpace(string(out))
	}

	return hw
}

// ── Percentile Computation ────────────────────────────────────────────────

// percentile returns the p-th percentile (0–100) of a sorted slice of float64.
// Slice must already be sorted ascending.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	index := (p / 100.0) * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))
	if lower == upper {
		return sorted[lower]
	}
	frac := index - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// ── Display Helpers ───────────────────────────────────────────────────────

// unitFor returns the unit label for a benchmark type.
func unitFor(t string) string {
	switch t {
	case "kb":
		return "docs"
	case "cue":
		return "texts"
	case "infer", "tool":
		return "prompts"
	default:
		return ""
	}
}

// qualityStr formats the quality metrics for a BenchResult.
func qualityStr(r BenchResult) string {
	switch r.Type {
	case "kb":
		if r.MRR > 0 {
			return fmt.Sprintf("MRR:%.2f", r.MRR)
		}
	case "cue":
		if r.Accuracy > 0 {
			return fmt.Sprintf("acc:%.0f%% s:%.2f", r.Accuracy*100, r.AvgScore)
		}
	case "tool":
		if r.Accuracy > 0 {
			return fmt.Sprintf("route:%.0f%% exec:%.0f%%", r.Accuracy*100, r.AvgScore*100)
		}
	case "infer":
		return ""
	}
	return ""
}

// formatBenchRow formats one BenchResult as a display string.
func formatBenchRow(r BenchResult) string {
	qual := qualityStr(r)
	if qual != "" {
		return fmt.Sprintf("%s  %d %s  %.1f/s  p50:%.0fms p95:%.0fms  %s",
			r.Type, r.SampleCount, unitFor(r.Type), r.ThroughputPS,
			r.P50LatMs, r.P95LatMs, qual)
	}
	return fmt.Sprintf("%s  %d %s  %.1f/s  p50:%.0fms p95:%.0fms",
		r.Type, r.SampleCount, unitFor(r.Type), r.ThroughputPS,
		r.P50LatMs, r.P95LatMs)
}

// printPerfTable prints the full comparison table for iq perf show.
func printPerfTable(results []BenchResult, _ string) {
	if len(results) == 0 {
		fmt.Println(utl.Gra("no benchmark results"))
		return
	}

	// Group by type if benchType not specified.
	typeMap := make(map[string][]BenchResult)
	for _, r := range results {
		typeMap[r.Type] = append(typeMap[r.Type], r)
	}

	// Print header.
	fmt.Printf("%-6s  %-50s  %-10s  %-11s  %-8s  %-8s  %-20s\n",
		"TYPE", "MODEL", "SAMPLES", "THROUGHPUT", "P50", "P95", "QUALITY")
	fmt.Println(strings.Repeat("─", 115))

	// Print each type's results.
	types := []string{"kb", "cue", "tool", "infer"}
	for _, t := range types {
		rows := typeMap[t]
		if len(rows) == 0 {
			continue
		}
		// Sort by throughput descending (slowest first).
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].ThroughputPS > rows[j].ThroughputPS
		})
		for _, r := range rows {
			qual := qualityStr(r)
			fmt.Printf("%-6s  %-50s  %-10s  %9.1f/s  %6.0fms  %6.0fms  %-20s\n",
				r.Type, r.ModelID,
				fmt.Sprintf("%d %s", r.SampleCount, unitFor(r.Type)),
				r.ThroughputPS, r.P50LatMs, r.P95LatMs, qual)
		}
	}
}

// ── Benchmark Sidecar Management ──────────────────────────────────────────

// benchSidecar holds the state of a temporary embed sidecar spun up for benchmarking.
type benchSidecar struct {
	ModelID string
	Port    int
	PID     int
	Temp    bool // true if we started it, false if reusing an existing one
}

// acquireEmbedSidecar returns a sidecar serving modelID.
// If the currently running sidecar already serves that model, it is reused.
// Otherwise a temporary sidecar is started on a dynamic port and must be
// released with releaseBenchSidecar when done.
func acquireEmbedSidecar(modelID string) (*benchSidecar, error) {
	// Check if the live sidecar already serves the requested model.
	state, err := readState(embedSlugConst)
	if err == nil && state != nil && pidAlive(state.PID) && state.Model == modelID {
		return &benchSidecar{
			ModelID: modelID,
			Port:    state.Port,
			PID:     state.PID,
			Temp:    false,
		}, nil
	}

	// Spin up a temporary sidecar on a dynamic port.
	fmt.Fprintf(os.Stderr, "  starting    temporary embed sidecar for %s ...\n", modelID)

	port, err := nextAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("no available port for bench sidecar: %w", err)
	}

	scriptPath := filepath.Join(os.TempDir(), "embed_server.py")
	if err := os.WriteFile(scriptPath, []byte(embedServerPy), 0755); err != nil {
		return nil, fmt.Errorf("failed to write embed script: %w", err)
	}

	pyPath, err := mlxVenvPython()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}

	cmd := exec.Command(pyPath, scriptPath,
		"--model", modelID,
		"--port", fmt.Sprintf("%d", port),
	)
	cmd.Env = os.Environ()

	// Log temp sidecar output for debugging if startup fails.
	benchLogPath := filepath.Join(os.TempDir(), fmt.Sprintf("iq-bench-embed-%d.log", port))
	lf, lfErr := os.OpenFile(benchLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if lfErr == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		if lf != nil {
			lf.Close()
		}
		return nil, fmt.Errorf("failed to start bench sidecar: %w", err)
	}
	if lf != nil {
		lf.Close()
	}

	pid := cmd.Process.Pid
	fmt.Fprintf(os.Stderr, "  temp sidecar pid %d on :%d  log:%s\n", pid, port, benchLogPath)
	fmt.Fprintf(os.Stderr, "  waiting for ready ")

	// Poll health endpoint — same timeout as regular embed sidecars.
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	deadline := time.Now().Add(embedReadyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Fprintf(os.Stderr, " %s\n", utl.Gre("ready"))
				return &benchSidecar{
					ModelID: modelID,
					Port:    port,
					PID:     pid,
					Temp:    true,
				}, nil
			}
		}
		if !pidAlive(pid) {
			fmt.Fprintf(os.Stderr, " %s\n", utl.Gra("failed"))
			printLastLogLines(benchLogPath, 15)
			return nil, fmt.Errorf("bench sidecar process exited unexpectedly (see log above)")
		}
		fmt.Fprint(os.Stderr, ".")
		time.Sleep(sidecarPollInterval)
	}
	// Timed out — kill it and dump log for troubleshooting.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
	fmt.Fprintf(os.Stderr, " %s\n", utl.Gra("timeout"))
	printLastLogLines(benchLogPath, 15)
	return nil, fmt.Errorf("bench sidecar did not become ready within %s (see log above)", embedReadyTimeout)
}

// releaseBenchSidecar stops a temporary sidecar. No-op if it was reused.
func releaseBenchSidecar(sc *benchSidecar) {
	if sc == nil || !sc.Temp {
		return
	}
	fmt.Fprintf(os.Stderr, "  stopping    temporary sidecar pid %d on :%d\n", sc.PID, sc.Port)
	if proc, err := os.FindProcess(sc.PID); err == nil {
		_ = syscall.Kill(-sc.PID, syscall.SIGTERM) // kill process group
		_ = proc.Kill()                            // fallback
	}
}

// ── KB Benchmark ───────────────────────────────────────────────────────────

// runKBBench embeds all kb_docs into an isolated in-memory KBIndex,
// then evaluates each kb_query using MRR.
func runKBBench(modelID string, corpus *benchCorpus) (BenchResult, error) {
	sc, err := acquireEmbedSidecar(modelID)
	if err != nil {
		return BenchResult{}, err
	}
	defer releaseBenchSidecar(sc)

	port := sc.Port
	kind := "live"
	if sc.Temp {
		kind = "temporary"
	}
	fmt.Fprintf(os.Stderr, "  sidecar     %s embed pid %d on :%d\n", kind, sc.PID, port)
	fmt.Fprintf(os.Stderr, "  model       %s\n", modelID)
	fmt.Fprintf(os.Stderr, "  corpus      %d docs, %d queries (bench_corpus.yaml)\n",
		len(corpus.KBDocs), len(corpus.KBQueries))

	// Collect all doc texts.
	var texts []string
	for _, doc := range corpus.KBDocs {
		texts = append(texts, doc.Text)
	}

	// Phase 1: batch-embed all docs for throughput measurement.
	fmt.Fprintf(os.Stderr, "  phase 1/3   batch-embedding %d docs ...", len(texts))
	t0 := time.Now()
	embedVecs, err := embedTextsOnPort(texts, modelID, port, "document")
	if err != nil {
		return BenchResult{}, err
	}
	embedElapsed := time.Since(t0)
	throughputPS := float64(len(texts)) / embedElapsed.Seconds()
	fmt.Fprintf(os.Stderr, " %.1f docs/s (%dms)\n", throughputPS, embedElapsed.Milliseconds())

	// Phase 2: per-doc latency measurement.
	fmt.Fprintf(os.Stderr, "  phase 2/3   measuring per-doc latency ...")
	var latenciesMs []float64
	for _, text := range texts {
		t1 := time.Now()
		_, _ = embedTextsOnPort([]string{text}, modelID, port, "document")
		latenciesMs = append(latenciesMs, float64(time.Since(t1).Milliseconds()))
	}
	sort.Float64s(latenciesMs)
	p50 := percentile(latenciesMs, 50)
	p95 := percentile(latenciesMs, 95)
	fmt.Fprintf(os.Stderr, " p50:%.0fms p95:%.0fms\n", p50, p95)

	// Build in-memory KBIndex (never touches user's kb.json).
	chunks := make([]KBChunk, len(texts))
	for i, doc := range corpus.KBDocs {
		chunks[i] = KBChunk{
			ID:        doc.ID,
			Text:      doc.Text,
			Source:    "bench:" + doc.ID,
			Embedding: embedVecs[i],
		}
	}

	// Phase 3: evaluate queries using MRR.
	fmt.Fprintf(os.Stderr, "  phase 3/3   evaluating %d queries (MRR) ...\n", len(corpus.KBQueries))
	var mrrScores []float64
	for qi, q := range corpus.KBQueries {
		queryVecs, err := embedTextsOnPort([]string{q.Query}, modelID, port, "query")
		if err != nil {
			fmt.Fprintf(os.Stderr, "    query %d/%d  SKIP (embed error)\n", qi+1, len(corpus.KBQueries))
			continue
		}
		queryVec := queryVecs[0]

		// Rank all chunks by cosine similarity.
		type scored struct {
			id    string
			score float32
		}
		var ranked []scored
		for _, chunk := range chunks {
			sim := cosineSimilarity(queryVec, chunk.Embedding)
			ranked = append(ranked, scored{chunk.ID, sim})
		}
		sort.Slice(ranked, func(i, j int) bool {
			return ranked[i].score > ranked[j].score
		})

		// Find rank of first relevant doc.
		rr := 0.0
		for idx, s := range ranked {
			if slices.Contains(q.Relevant, s.id) {
				rr = 1.0 / float64(idx+1)
				break
			}
		}
		mrrScores = append(mrrScores, rr)
		topHit := ranked[0].id
		fmt.Fprintf(os.Stderr, "    query %2d/%d  RR:%.2f  top:%s  %s\n",
			qi+1, len(corpus.KBQueries), rr, topHit,
			utl.Gra(q.Query))
	}

	var mrr float64
	if len(mrrScores) > 0 {
		for _, s := range mrrScores {
			mrr += s
		}
		mrr /= float64(len(mrrScores))
	}
	fmt.Fprintf(os.Stderr, "  MRR         %.4f\n", mrr)

	return BenchResult{
		ModelID:      modelID,
		Type:         "kb",
		BenchAt:      time.Now().UTC().Format(time.RFC3339),
		HW:           captureHW(),
		SampleCount:  len(texts),
		ThroughputPS: throughputPS,
		P50LatMs:     p50,
		P95LatMs:     p95,
		MRR:          mrr,
	}, nil
}

// ── Cue Benchmark ──────────────────────────────────────────────────────────

// runCueBench classifies each cue_input and measures accuracy and throughput.
// For cue benchmarking we need to embed the input text AND compare it against
// the cue embedding cache, so we implement classification inline using the
// benchmark sidecar's port directly.
func runCueBench(modelID string, corpus *benchCorpus) (BenchResult, error) {
	sc, err := acquireEmbedSidecar(modelID)
	if err != nil {
		return BenchResult{}, err
	}
	defer releaseBenchSidecar(sc)

	port := sc.Port
	kind := "live"
	if sc.Temp {
		kind = "temporary"
	}
	fmt.Fprintf(os.Stderr, "  sidecar     %s embed pid %d on :%d\n", kind, sc.PID, port)
	fmt.Fprintf(os.Stderr, "  model       %s\n", modelID)
	fmt.Fprintf(os.Stderr, "  corpus      %d cue inputs (bench_corpus.yaml)\n", len(corpus.CueInputs))

	cues, err := loadCues()
	if err != nil {
		return BenchResult{}, err
	}
	fmt.Fprintf(os.Stderr, "  cue set     %d cues loaded\n", len(cues))
	fmt.Fprintf(os.Stderr, "  threshold   %.2f\n", classifyMinScore)

	// Build cue embeddings using the benchmark sidecar.
	fmt.Fprintf(os.Stderr, "  embedding   %d cue descriptions ...", len(cues))
	var cueTexts []string
	for _, c := range cues {
		cueTexts = append(cueTexts, c.Name+": "+c.Description)
	}
	cueVecs, err := embedTextsOnPort(cueTexts, modelID, port, "document")
	if err != nil {
		return BenchResult{}, fmt.Errorf("failed to embed cue descriptions: %w", err)
	}
	fmt.Fprintf(os.Stderr, " done\n")

	var latenciesMs []float64
	correct := 0
	var scoreSum float64

	fmt.Fprintf(os.Stderr, "  classifying %d inputs ...\n", len(corpus.CueInputs))
	t0 := time.Now()
	for ci, input := range corpus.CueInputs {
		t1 := time.Now()

		// Embed the input text.
		inputVecs, err := embedTextsOnPort([]string{input.Text}, modelID, port, "query")
		elapsed := time.Since(t1)
		latenciesMs = append(latenciesMs, float64(elapsed.Milliseconds()))

		if err != nil {
			fmt.Fprintf(os.Stderr, "    %2d/%d  %4dms  %-5s  expect:%-25s %s\n",
				ci+1, len(corpus.CueInputs), elapsed.Milliseconds(),
				utl.Gra("ERR"), input.ExpectedCue,
				utl.Gra(input.Text[:min(60, len(input.Text))]))
			continue
		}

		// Find best matching cue by cosine similarity.
		inputVec := inputVecs[0]
		bestIdx := 0
		var bestScore float32
		for j, cv := range cueVecs {
			sim := cosineSimilarity(inputVec, cv)
			if sim > bestScore {
				bestScore = sim
				bestIdx = j
			}
		}

		resolved := "initial"
		if bestScore >= classifyMinScore {
			resolved = cues[bestIdx].Name
		}

		// The raw top match (before threshold) helps diagnose ranking vs threshold issues.
		rawTop := cues[bestIdx].Name

		match := ""
		if resolved == input.ExpectedCue {
			correct++
			scoreSum += float64(bestScore)
			match = utl.Gre("OK")
		} else {
			match = "MISS"
		}
		fmt.Fprintf(os.Stderr, "    %2d/%d  %4dms  %.2f  %-4s  top:%-28s expect:%-25s %s\n",
			ci+1, len(corpus.CueInputs), elapsed.Milliseconds(),
			bestScore, match, rawTop, input.ExpectedCue,
			utl.Gra(input.Text[:min(60, len(input.Text))]))
	}
	totalSec := time.Since(t0).Seconds()

	sort.Float64s(latenciesMs)
	p50 := percentile(latenciesMs, 50)
	p95 := percentile(latenciesMs, 95)
	throughputPS := float64(len(corpus.CueInputs)) / totalSec

	accuracy := 0.0
	avgScore := 0.0
	if len(corpus.CueInputs) > 0 {
		accuracy = float64(correct) / float64(len(corpus.CueInputs))
	}
	if correct > 0 {
		avgScore = scoreSum / float64(correct)
	}

	fmt.Fprintf(os.Stderr, "  accuracy    %d/%d (%.0f%%)  avg score:%.4f\n",
		correct, len(corpus.CueInputs), accuracy*100, avgScore)

	return BenchResult{
		ModelID:      modelID,
		Type:         "cue",
		BenchAt:      time.Now().UTC().Format(time.RFC3339),
		HW:           captureHW(),
		SampleCount:  len(corpus.CueInputs),
		ThroughputPS: throughputPS,
		P50LatMs:     p50,
		P95LatMs:     p95,
		Accuracy:     accuracy,
		AvgScore:     avgScore,
	}, nil
}

// ── Infer Benchmark ───────────────────────────────────────────────────────

// runInferBench sends prompts to a model's sidecar and measures latency and throughput.
func runInferBench(modelID string, corpus *benchCorpus) (BenchResult, error) {
	state, err := readState(modelID)
	if err != nil || state == nil || !pidAlive(state.PID) {
		return BenchResult{}, fmt.Errorf("model %q sidecar not running — run: iq start %q", modelID, modelID)
	}

	fmt.Fprintf(os.Stderr, "  sidecar     %s pid %d on :%d (tier:%s)\n",
		modelID, state.PID, state.Port, state.Tier)
	fmt.Fprintf(os.Stderr, "  corpus      %d prompts (bench_corpus.yaml)\n", len(corpus.InferPrompts))
	fmt.Fprintf(os.Stderr, "  max_tokens  512\n")

	var latenciesMs []float64
	var tokenCounts []float64

	t0 := time.Now()
	for pi, prompt := range corpus.InferPrompts {
		messages := []chatMessage{
			{Role: "user", Content: prompt.Text},
		}
		fmt.Fprintf(os.Stderr, "    %d/%d  %-25s ...",
			pi+1, len(corpus.InferPrompts), prompt.ID)
		t1 := time.Now()
		response, err := callSidecar(state.Port, messages, false, 512)
		elapsed := time.Since(t1)
		latenciesMs = append(latenciesMs, float64(elapsed.Milliseconds()))

		if err == nil {
			tokens := float64(len(strings.Fields(response)))
			tokenCounts = append(tokenCounts, tokens)
			tps := tokens / elapsed.Seconds()
			fmt.Fprintf(os.Stderr, " %dms  ~%.0f words  %.1f tok/s\n",
				elapsed.Milliseconds(), tokens, tps)
		} else {
			fmt.Fprintf(os.Stderr, " %dms  %s\n", elapsed.Milliseconds(), utl.Gra("error"))
		}
	}
	totalSec := time.Since(t0).Seconds()

	var totalTokens float64
	for _, t := range tokenCounts {
		totalTokens += t
	}

	throughputPS := 0.0
	if totalSec > 0 {
		throughputPS = totalTokens / totalSec
	}

	sort.Float64s(latenciesMs)
	p50 := percentile(latenciesMs, 50)
	p95 := percentile(latenciesMs, 95)

	fmt.Fprintf(os.Stderr, "  throughput   %.1f tok/s  (~%.0f words across %d prompts in %.1fs)\n",
		throughputPS, totalTokens, len(corpus.InferPrompts), totalSec)

	return BenchResult{
		ModelID:      modelID,
		Type:         "infer",
		BenchAt:      time.Now().UTC().Format(time.RFC3339),
		HW:           captureHW(),
		SampleCount:  len(corpus.InferPrompts),
		ThroughputPS: throughputPS,
		P50LatMs:     p50,
		P95LatMs:     p95,
	}, nil
}

// ── Tool Benchmark ────────────────────────────────────────────────────────

// runToolBench sends each tool_prompt through the routing grammar pipeline
// and checks that the model routes to the expected tool and that execution succeeds.
func runToolBench(modelID string, corpus *benchCorpus, verbose bool) (BenchResult, error) {
	state, err := readState(modelID)
	if err != nil || state == nil || !pidAlive(state.PID) {
		return BenchResult{}, fmt.Errorf("model %q sidecar not running — run: iq start %q", modelID, modelID)
	}

	fmt.Fprintf(os.Stderr, "  sidecar     %s pid %d on :%d (tier:%s)\n",
		modelID, state.PID, state.Port, state.Tier)
	fmt.Fprintf(os.Stderr, "  corpus      %d tool prompts (bench_corpus.yaml)\n", len(corpus.ToolPrompts))

	// Build the system prompt with tool instructions.
	sysprompt := "You are a helpful assistant.\n" + buildRoutingToolPrompt()
	grammar := &routeGrammar{ToolNames: toolRegistryNames()}

	var latenciesMs []float64
	routeCorrect := 0
	execOK := 0

	t0 := time.Now()
	for pi, tp := range corpus.ToolPrompts {
		messages := []chatMessage{
			{Role: "system", Content: sysprompt},
			{Role: "user", Content: tp.Text},
		}

		fmt.Fprintf(os.Stderr, "    %2d/%d  %-20s  expect:%-14s",
			pi+1, len(corpus.ToolPrompts), tp.ID, tp.ExpectedTool)

		t1 := time.Now()
		response, err := callSidecarWithGrammar(state.Port, messages, 8192, grammar)
		elapsed := time.Since(t1)
		latenciesMs = append(latenciesMs, float64(elapsed.Milliseconds()))

		if err != nil {
			fmt.Fprintf(os.Stderr, "  %4dms  %s\n", elapsed.Milliseconds(), utl.Gra("sidecar error"))
			continue
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "      %s  %s\n", utl.Gra("prompt"), utl.Gra(tp.Text))
			fmt.Fprintf(os.Stderr, "      %s  %s\n", utl.Gra("raw_resp"), utl.Gra(fmt.Sprintf("%q", truncate(response, 200))))
		}

		// Parse routing prefix.
		routedTool, routeRest := parseRoutingPrefix(response)
		routeMatch := routedTool == tp.ExpectedTool
		if routeMatch {
			routeCorrect++
		}

		// Try to execute the tool.
		var execResult string
		if routedTool != "" {
			args := parseRoutingArgs(routeRest)
			call := toolCall{Name: routedTool, Args: args}

			if verbose {
				argsJSON, _ := json.Marshal(args)
				fmt.Fprintf(os.Stderr, "      %s  %s(%s)\n", utl.Gra("tool_call"), call.Name, string(argsJSON))
			}

			r := executeTool(call)
			if r.Error == "" {
				execOK++
				execResult = utl.Gre("OK")
				if verbose {
					fmt.Fprintf(os.Stderr, "      %s  %s\n", utl.Gra("tool_result"), utl.Gra(truncate(r.Output, 120)))
				}
			} else {
				execResult = utl.Yel("err: " + truncate(r.Error, 40))
				if verbose {
					fmt.Fprintf(os.Stderr, "      %s  %s\n", utl.Gra("tool_error"), utl.Yel(r.Error))
				}
			}
		} else {
			execResult = utl.Gra("<no_tool>")
		}

		routeLabel := utl.Gre("OK")
		if !routeMatch {
			routeLabel = fmt.Sprintf("MISS→%s", routedTool)
		}

		fmt.Fprintf(os.Stderr, "  %4dms  route:%-16s exec:%s\n",
			elapsed.Milliseconds(), routeLabel, execResult)
	}
	totalSec := time.Since(t0).Seconds()

	sort.Float64s(latenciesMs)
	p50 := percentile(latenciesMs, 50)
	p95 := percentile(latenciesMs, 95)
	throughputPS := float64(len(corpus.ToolPrompts)) / totalSec

	routeAcc := 0.0
	execAcc := 0.0
	if len(corpus.ToolPrompts) > 0 {
		routeAcc = float64(routeCorrect) / float64(len(corpus.ToolPrompts))
		execAcc = float64(execOK) / float64(len(corpus.ToolPrompts))
	}

	fmt.Fprintf(os.Stderr, "  routing     %d/%d (%.0f%%)\n", routeCorrect, len(corpus.ToolPrompts), routeAcc*100)
	fmt.Fprintf(os.Stderr, "  execution   %d/%d (%.0f%%)\n", execOK, len(corpus.ToolPrompts), execAcc*100)

	return BenchResult{
		ModelID:      modelID,
		Type:         "tool",
		BenchAt:      time.Now().UTC().Format(time.RFC3339),
		HW:           captureHW(),
		SampleCount:  len(corpus.ToolPrompts),
		ThroughputPS: throughputPS,
		P50LatMs:     p50,
		P95LatMs:     p95,
		Accuracy:     routeAcc,
		AvgScore:     execAcc, // repurpose: execution success rate
	}, nil
}

// ── Cobra Commands ────────────────────────────────────────────────────────

func printPerfHelp() {
	fmt.Printf("Benchmark IQ model performance\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  iq perf <subcommand> [flags]\n\n")
	fmt.Printf("%s\n", utl.Whi2("SUBCOMMANDS"))
	fmt.Printf("  %-15s %s\n", "bench", "Run benchmark for a model")
	fmt.Printf("  %-15s %s\n", "show", "Show benchmark comparison table")
	fmt.Printf("  %-15s %s\n\n", "clear", "Remove benchmark results")
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  --type kb|cue|tool|infer  Benchmark type (default: all applicable)\n")
	fmt.Printf("  --model <id>              Model ID to benchmark (required for infer/tool)\n")
	fmt.Printf("  -v, --verbose             Show debug detail for each prompt (tool bench)\n\n")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  iq perf bench --type kb\n")
	fmt.Printf("  iq perf bench --type cue\n")
	fmt.Printf("  iq perf bench --type infer --model mlx-community/gemma-3-1b-it-4bit\n")
	fmt.Printf("  iq perf bench --type tool --model mlx-community/Meta-Llama-3.1-8B-Instruct-4bit\n")
	fmt.Printf("  iq perf show\n")
	fmt.Printf("  iq perf show --type kb\n")
	fmt.Printf("  iq perf clear --model <id>\n")
}

func newPerfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "perf",
		Short:        "Benchmark IQ model performance",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printPerfHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printPerfHelp()
	})
	cmd.AddCommand(newPerfBenchCmd(), newPerfShowCmd(), newPerfClearCmd())
	return cmd
}

func newPerfBenchCmd() *cobra.Command {
	var benchType string
	var modelID string
	var verbose bool

	cmd := &cobra.Command{
		Use:          "bench",
		Short:        "Run benchmark for a model",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			corpus, err := loadBenchCorpus()
			if err != nil {
				return err
			}

			bs, err := loadBenchStore()
			if err != nil {
				return err
			}

			cfg, err := config.Load(nil)
			if err != nil {
				return err
			}

			// Determine which types to run.
			var types []string
			if benchType != "" {
				types = []string{benchType}
			} else {
				types = []string{"kb", "cue"}
				if modelID != "" {
					types = append(types, "infer")
				}
			}

			for _, t := range types {
				var result BenchResult
				var rerr error
				switch t {
				case "kb":
					mid := modelID
					if mid == "" {
						mid = config.EmbedModel(cfg)
					}
					fmt.Printf("benchmarking kb  model:%s ...\n", mid)
					result, rerr = runKBBench(mid, corpus)
				case "cue":
					mid := modelID
					if mid == "" {
						mid = config.EmbedModel(cfg)
					}
					fmt.Printf("benchmarking cue  model:%s ...\n", mid)
					result, rerr = runCueBench(mid, corpus)
				case "tool":
					if modelID == "" {
						return fmt.Errorf("--model required for tool benchmark")
					}
					fmt.Printf("benchmarking tool  model:%s ...\n", modelID)
					result, rerr = runToolBench(modelID, corpus, verbose)
				case "infer":
					if modelID == "" {
						return fmt.Errorf("--model required for infer benchmark")
					}
					fmt.Printf("benchmarking infer  model:%s ...\n", modelID)
					result, rerr = runInferBench(modelID, corpus)
				}

				if rerr != nil {
					fmt.Fprintf(os.Stderr, "  error: %v\n", rerr)
					continue
				}
				upsertResult(bs, result)
				fmt.Printf("  %s\n", formatBenchRow(result))
			}

			return saveBenchStore(bs)
		},
	}

	cmd.Flags().StringVar(&benchType, "type", "", "Benchmark type: cue, kb, tool, infer")
	cmd.Flags().StringVar(&modelID, "model", "", "Model ID to benchmark")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show debug detail for each prompt (tool bench)")
	return cmd
}

func newPerfShowCmd() *cobra.Command {
	var modelID string
	var benchType string

	cmd := &cobra.Command{
		Use:          "show",
		Short:        "Show benchmark comparison table",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bs, err := loadBenchStore()
			if err != nil {
				return err
			}
			results := resultsFor(bs, modelID, benchType)
			if len(results) == 0 {
				fmt.Println(utl.Gra("no benchmark results — run: iq perf bench"))
				return nil
			}
			printPerfTable(results, benchType)
			return nil
		},
	}

	cmd.Flags().StringVar(&modelID, "model", "", "Filter by model ID")
	cmd.Flags().StringVar(&benchType, "type", "", "Filter by type: cue, kb, infer")
	return cmd
}

func newPerfClearCmd() *cobra.Command {
	var modelID string

	cmd := &cobra.Command{
		Use:          "clear",
		Short:        "Remove benchmark results",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bs, err := loadBenchStore()
			if err != nil {
				return err
			}

			if modelID == "" {
				// Clear all.
				bs.Results = nil
			} else {
				// Keep results not matching modelID.
				filtered := bs.Results[:0]
				for _, r := range bs.Results {
					if r.ModelID != modelID {
						filtered = append(filtered, r)
					}
				}
				bs.Results = filtered
			}

			if err := saveBenchStore(bs); err != nil {
				return err
			}

			if modelID == "" {
				fmt.Println(utl.Gra("cleared all benchmark results"))
			} else {
				fmt.Printf(utl.Gra("cleared benchmark results for %s\n"), modelID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&modelID, "model", "", "Model ID to clear (clear all if unset)")
	return cmd
}
