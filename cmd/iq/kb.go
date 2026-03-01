package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type KBChunk struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"` // absolute file path
	LineStart int       `json:"line_start"`
	LineEnd   int       `json:"line_end"`
	Label     string    `json:"label"` // symbol/heading/key for metadata prefix
	Text      string    `json:"text"`  // raw chunk text (stored as-is, not prefixed)
	Embedding []float32 `json:"embedding"`
}

type KBSource struct {
	Path       string `json:"path"`
	IngestedAt string `json:"ingested_at"`
	FileCount  int    `json:"file_count"`
	ChunkCount int    `json:"chunk_count"`
}

type KBIndex struct {
	Version int        `json:"version"`
	Sources []KBSource `json:"sources"`
	Chunks  []KBChunk  `json:"chunks"`
}

// ── Storage ───────────────────────────────────────────────────────────────────

func kbPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kb.json"), nil
}

func kbExists() bool {
	path, err := kbPath()
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func loadKB() (*KBIndex, error) {
	path, err := kbPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &KBIndex{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var kb KBIndex
	if err := json.Unmarshal(data, &kb); err != nil {
		return nil, fmt.Errorf("failed to parse kb.json: %w", err)
	}
	return &kb, nil
}

func saveKB(kb *KBIndex) error {
	path, err := kbPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(kb)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ── Chunking ──────────────────────────────────────────────────────────────────

// textExtensions is the set of file extensions treated as plain text.
var textExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".rs": true, ".c": true, ".h": true, ".cpp": true, ".cc": true, ".java": true,
	".rb": true, ".php": true, ".swift": true, ".kt": true, ".sh": true, ".bash": true,
	".zsh": true, ".fish": true, ".yaml": true, ".yml": true, ".toml": true,
	".json": true, ".xml": true, ".html": true, ".css": true, ".scss": true,
	".md": true, ".txt": true, ".rst": true, ".org": true, ".tex": true,
	".sql": true, ".graphql": true, ".proto": true, ".tf": true, ".hcl": true,
	".env": true, ".ini": true, ".cfg": true, ".conf": true,
	"": false,
}

const (
	embedBatch = 20   // chunks per embed request
	kbMinScore = 0.50 // minimum cosine similarity to inject
	kbMaxRunes = 1600 // max runes per chunk text sent to embedder
	kbDefaultK = 5    // default top-K for retrieval
)

// goSymbolRe matches top-level Go declarations that start a new semantic chunk.
var goSymbolRe = regexp.MustCompile(`^(func|type|var|const)\s`)

func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return true
	}
	return slices.Contains(buf[:n], 0)
}

func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "makefile", "dockerfile", "readme", "license", "gitignore", "gitattributes":
		return true
	}
	ok, known := textExtensions[ext]
	if known && !ok {
		return false
	}
	return !isBinary(path)
}

// chunkID returns a stable 8-byte hex hash for a chunk.
func chunkID(source string, start, end int) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d", source, start, end))
	return fmt.Sprintf("%x", h[:8])
}

// truncateRunes truncates s to at most n runes.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// embedText returns the text sent to the embedder: metadata prefix + truncated content.
// The raw Text field is preserved separately for display.
func embedText(root, source, label, text string) string {
	rel, err := filepath.Rel(root, source)
	if err != nil {
		rel = source
	}
	prefix := fmt.Sprintf("File: %s", rel)
	if label != "" {
		prefix += "\n" + label
	}
	return truncateRunes(prefix+"\n\n"+text, kbMaxRunes)
}

// ── Go chunker ────────────────────────────────────────────────────────────────

// chunkGo splits a Go source file at top-level declaration boundaries
// (func, type, var, const). Each chunk = one complete declaration.
func chunkGo(path string) ([]KBChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	// Find boundary lines: indices where a new top-level symbol starts.
	var boundaries []int
	for i, line := range lines {
		if goSymbolRe.MatchString(line) {
			boundaries = append(boundaries, i)
		}
	}

	if len(boundaries) == 0 {
		// No top-level symbols (e.g. package + import only) — one chunk for the whole file.
		text := strings.TrimSpace(string(data))
		if text == "" {
			return nil, nil
		}
		return []KBChunk{{
			ID:        chunkID(path, 1, len(lines)),
			Source:    path,
			LineStart: 1,
			LineEnd:   len(lines),
			Label:     "",
			Text:      text,
		}}, nil
	}

	var chunks []KBChunk
	for i, start := range boundaries {
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}
		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if text == "" {
			continue
		}
		// Extract label from the declaration's first line.
		label := extractGoLabel(lines[start])
		chunks = append(chunks, KBChunk{
			ID:        chunkID(path, start+1, end),
			Source:    path,
			LineStart: start + 1,
			LineEnd:   end,
			Label:     label,
			Text:      text,
		})
	}

	// Include any preamble (package + imports) before the first symbol as its own chunk.
	if boundaries[0] > 0 {
		text := strings.TrimSpace(strings.Join(lines[:boundaries[0]], "\n"))
		if text != "" {
			chunks = append([]KBChunk{{
				ID:        chunkID(path, 1, boundaries[0]),
				Source:    path,
				LineStart: 1,
				LineEnd:   boundaries[0],
				Label:     "package imports",
				Text:      text,
			}}, chunks...)
		}
	}

	return chunks, nil
}

func extractGoLabel(line string) string {
	line = strings.TrimSpace(line)
	// func Foo(...) → "func Foo"
	if after, ok := strings.CutPrefix(line, "func "); ok {
		// Strip the body opening brace and everything after the closing paren.
		name := after
		// Find end of name+receiver, before parameters.
		if idx := strings.Index(name, "("); idx > 0 {
			name = "func " + strings.TrimSpace(name[:idx])
		} else {
			name = "func " + name
		}
		return name
	}
	// type Foo struct / type Foo interface / type Foo =
	if strings.HasPrefix(line, "type ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return "type " + parts[1]
		}
	}
	// const / var — return the keyword only; block contents vary.
	if strings.HasPrefix(line, "const") {
		return "const block"
	}
	if strings.HasPrefix(line, "var") {
		return "var block"
	}
	return line
}

// ── Markdown chunker ──────────────────────────────────────────────────────────

// chunkMarkdown splits a markdown file at heading boundaries.
// Each heading + its content body = one chunk.
// The label carries the full heading path, e.g. "Architecture > Retrieval > RAG".
func chunkMarkdown(path string) ([]KBChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.+)`)

	type section struct {
		start int
		label string
		lines []string
	}

	var sections []section
	headingStack := make([]string, 6) // index = heading level - 1
	var current *section

	flush := func(end int) {
		if current != nil && len(current.lines) > 0 {
			sections = append(sections, *current)
		}
	}

	for i, line := range lines {
		m := headingRe.FindStringSubmatch(line)
		if m != nil {
			flush(i)
			level := len(m[1]) - 1
			text := strings.TrimSpace(m[2])
			headingStack[level] = text
			// Clear deeper levels.
			for j := level + 1; j < 6; j++ {
				headingStack[j] = ""
			}
			// Build path from non-empty heading stack entries up to current level.
			var parts []string
			for j := 0; j <= level; j++ {
				if headingStack[j] != "" {
					parts = append(parts, headingStack[j])
				}
			}
			label := strings.Join(parts, " > ")
			current = &section{start: i + 1, label: label, lines: []string{line}}
		} else if current != nil {
			current.lines = append(current.lines, line)
		} else {
			// Content before any heading.
			if current == nil {
				current = &section{start: 1, label: "", lines: []string{}}
			}
			current.lines = append(current.lines, line)
		}
	}
	flush(len(lines))

	var chunks []KBChunk
	for _, s := range sections {
		text := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if text == "" {
			continue
		}
		end := s.start + len(s.lines) - 1
		chunks = append(chunks, KBChunk{
			ID:        chunkID(path, s.start, end),
			Source:    path,
			LineStart: s.start,
			LineEnd:   end,
			Label:     s.label,
			Text:      text,
		})
	}
	return chunks, nil
}

// ── YAML/TOML chunker ─────────────────────────────────────────────────────────

// chunkKeyValue splits YAML/TOML by top-level keys (lines with no leading whitespace
// that look like "key:" or "key ="). Each top-level key + its subtree = one chunk.
func chunkKeyValue(path string) ([]KBChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	topKeyRe := regexp.MustCompile(`^([a-zA-Z0-9_\-]+)\s*[:=]`)

	var boundaries []int
	for i, line := range lines {
		if topKeyRe.MatchString(line) {
			boundaries = append(boundaries, i)
		}
	}

	if len(boundaries) == 0 {
		text := strings.TrimSpace(string(data))
		if text == "" {
			return nil, nil
		}
		return []KBChunk{{
			ID: chunkID(path, 1, len(lines)), Source: path,
			LineStart: 1, LineEnd: len(lines), Text: text,
		}}, nil
	}

	var chunks []KBChunk
	for i, start := range boundaries {
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}
		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if text == "" {
			continue
		}
		m := topKeyRe.FindStringSubmatch(lines[start])
		label := ""
		if m != nil {
			label = "key: " + m[1]
		}
		chunks = append(chunks, KBChunk{
			ID:        chunkID(path, start+1, end),
			Source:    path,
			LineStart: start + 1,
			LineEnd:   end,
			Label:     label,
			Text:      text,
		})
	}
	return chunks, nil
}

// ── Prose chunker (txt, rst, fallback) ───────────────────────────────────────

// chunkProse splits plain text at blank-line paragraph boundaries,
// grouping paragraphs until the rune limit is reached.
func chunkProse(path string) ([]KBChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	// Split into paragraphs first.
	var paragraphs []struct {
		start int
		text  string
	}
	paraStart := 0
	var paraLines []string
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(paraLines) > 0 {
				paragraphs = append(paragraphs, struct {
					start int
					text  string
				}{paraStart + 1, strings.Join(paraLines, "\n")})
			}
			paraStart = i + 1
			paraLines = nil
		} else {
			if len(paraLines) == 0 {
				paraStart = i
			}
			paraLines = append(paraLines, line)
		}
	}
	if len(paraLines) > 0 {
		paragraphs = append(paragraphs, struct {
			start int
			text  string
		}{paraStart + 1, strings.Join(paraLines, "\n")})
	}

	if len(paragraphs) == 0 {
		return nil, nil
	}

	// Group paragraphs into chunks up to kbMaxRunes.
	var chunks []KBChunk
	var groupText strings.Builder
	groupStart := paragraphs[0].start
	groupEnd := groupStart

	flush := func(end int) {
		text := strings.TrimSpace(groupText.String())
		if text == "" {
			return
		}
		chunks = append(chunks, KBChunk{
			ID:        chunkID(path, groupStart, end),
			Source:    path,
			LineStart: groupStart,
			LineEnd:   end,
			Text:      text,
		})
		groupText.Reset()
	}

	for _, p := range paragraphs {
		pRunes := len([]rune(p.text))
		currentRunes := len([]rune(groupText.String()))
		if currentRunes > 0 && currentRunes+pRunes > kbMaxRunes {
			flush(groupEnd)
			groupStart = p.start
		}
		if groupText.Len() > 0 {
			groupText.WriteString("\n\n")
		}
		groupText.WriteString(p.text)
		groupEnd = p.start + strings.Count(p.text, "\n")
	}
	flush(groupEnd)

	return chunks, nil
}

// ── Chunker dispatcher ────────────────────────────────────────────────────────

func chunkFile(path string) ([]KBChunk, error) {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	switch {
	case ext == ".go":
		return chunkGo(path)
	case ext == ".md":
		return chunkMarkdown(path)
	case ext == ".yaml" || ext == ".yml" || ext == ".toml":
		return chunkKeyValue(path)
	case base == "makefile" || base == "dockerfile":
		return chunkProse(path)
	default:
		return chunkProse(path)
	}
}

// ── Search ────────────────────────────────────────────────────────────────────

type kbResult struct {
	Chunk KBChunk
	Score float32
}

// extractKeywords pulls meaningful tokens from a query for keyword boosting.
// Splits on whitespace/punctuation, expands camelCase, keeps tokens >= 4 chars.
// e.g. "how is embedClassify being used?" → ["embedClassify", "embed", "Classify"]
var camelRe = regexp.MustCompile(`[A-Z][a-z]+|[a-z]+|[A-Z]+`)

func extractKeywords(query string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		low := strings.ToLower(s)
		if len(low) >= 4 && !seen[low] {
			seen[low] = true
			out = append(out, low)
		}
	}
	// Split on non-alphanumeric boundaries.
	for _, tok := range regexp.MustCompile(`[^a-zA-Z0-9_]+`).Split(query, -1) {
		if tok == "" {
			continue
		}
		add(tok)
		// Expand camelCase tokens into sub-words.
		parts := camelRe.FindAllString(tok, -1)
		if len(parts) > 1 {
			for _, p := range parts {
				add(p)
			}
		}
	}
	return out
}

// keywordBoost returns a score bonus for a chunk based on exact keyword matches.
// Each keyword found in the chunk text adds kbKeywordBoost to the score.
// Chunks containing a call pattern (keyword followed by `(`) get an extra boost
// to surface callsites that semantic search tends to rank below definitions.
const (
	kbKeywordBoost = 0.05
	kbCallBoost    = 0.12
	kbMaxBoost     = 0.25
)

func keywordBoost(text, label string, keywords []string) float32 {
	low := strings.ToLower(text)
	lowLabel := strings.ToLower(label)
	var bonus float32
	for _, kw := range keywords {
		if strings.Contains(low, kw) {
			bonus += kbKeywordBoost
			// Extra boost if this keyword appears as a function call,
			// but NOT if this chunk IS the definition of that symbol
			// (i.e. the label is "func <keyword>" — skip to avoid
			// boosting the definition above genuine callsites).
			isDefinition := strings.Contains(lowLabel, "func "+kw) ||
				strings.Contains(lowLabel, "type "+kw)
			if !isDefinition && strings.Contains(low, kw+"(") {
				bonus += kbCallBoost
			}
		}
	}
	if bonus > kbMaxBoost {
		bonus = kbMaxBoost
	}
	return bonus
}

// KBSearch embeds the query and returns the top-k most similar chunks.
// Uses hybrid scoring: cosine similarity + keyword boost for exact token matches.
func KBSearch(query string, topK int) ([]kbResult, error) {
	kb, err := loadKB()
	if err != nil {
		return nil, err
	}
	if len(kb.Chunks) == 0 {
		return nil, nil
	}

	vecs, err := embedTexts([]string{query}, "query")
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	qvec := vecs[0]

	keywords := extractKeywords(query)

	results := make([]kbResult, 0, len(kb.Chunks))
	for _, c := range kb.Chunks {
		if len(c.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(qvec, c.Embedding)
		// Boost chunks that contain exact keyword matches from the query.
		// Apply boost to label too so symbol names in headings are matched.
		score += keywordBoost(c.Text, c.Label, keywords)
		results = append(results, kbResult{Chunk: c, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	// Filter by threshold.
	filtered := results[:0]
	for _, r := range results {
		if r.Score >= kbMinScore {
			filtered = append(filtered, r)
		}
	}
	results = filtered
	if topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// KBContext builds a context string from search results for prompt injection.
func KBContext(results []kbResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Relevant context from knowledge base:\n")
	for _, r := range results {
		header := fmt.Sprintf("\n─── %s", r.Chunk.Source)
		if r.Chunk.Label != "" {
			header += " — " + r.Chunk.Label
		}
		header += fmt.Sprintf(" (lines %d–%d) ───\n", r.Chunk.LineStart, r.Chunk.LineEnd)
		sb.WriteString(header)
		sb.WriteString(r.Chunk.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ── Ingest ────────────────────────────────────────────────────────────────────

func kbIngest(root string) error {
	if !embedSidecarAlive() {
		return fmt.Errorf("embed sidecar not running — run: iq svc start")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("%s: %w", abs, err)
	}

	kb, err := loadKB()
	if err != nil {
		return err
	}

	kb = removeSource(kb, abs)

	var files []string
	if info.IsDir() {
		err = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" ||
					name == "vendor" || name == "__pycache__" || name == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if isTextFile(p) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		if !isTextFile(abs) {
			return fmt.Errorf("%s does not appear to be a text file", abs)
		}
		files = []string{abs}
	}

	if len(files) == 0 {
		return fmt.Errorf("no text files found in %s", abs)
	}

	var allChunks []KBChunk
	for _, f := range files {
		chunks, err := chunkFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("  skip %s: %v", f, err)))
			continue
		}
		allChunks = append(allChunks, chunks...)
	}

	if len(allChunks) == 0 {
		return fmt.Errorf("no chunks produced from %s", abs)
	}

	fmt.Printf("  %d files  →  %d chunks  ", len(files), len(allChunks))

	// Embed in batches. Send metadata-prefixed text to the embedder;
	// store raw text in the chunk for display.
	embedded := 0
	for i := 0; i < len(allChunks); i += embedBatch {
		end := min(i+embedBatch, len(allChunks))
		batch := allChunks[i:end]
		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = embedText(abs, c.Source, c.Label, c.Text)
		}
		vecs, err := embedTexts(texts, "document")
		if err != nil {
			fmt.Println()
			return fmt.Errorf("embed batch %d: %w", i/embedBatch, err)
		}
		for j := range batch {
			if j < len(vecs) {
				allChunks[i+j].Embedding = vecs[j]
				embedded++
			}
		}
		fmt.Print(".")
	}
	fmt.Println()

	kb.Chunks = append(kb.Chunks, allChunks...)
	kb.Sources = append(kb.Sources, KBSource{
		Path:       abs,
		IngestedAt: time.Now().UTC().Format(time.RFC3339),
		FileCount:  len(files),
		ChunkCount: embedded,
	})

	if err := saveKB(kb); err != nil {
		return fmt.Errorf("failed to save kb.json: %w", err)
	}
	fmt.Printf("  %s  ingested %s  (%d chunks embedded)\n",
		utl.Gre("ok"), utl.Whi(abs), embedded)
	return nil
}

func removeSource(kb *KBIndex, absPath string) *KBIndex {
	filtered := kb.Chunks[:0]
	for _, c := range kb.Chunks {
		if !strings.HasPrefix(c.Source, absPath) {
			filtered = append(filtered, c)
		}
	}
	kb.Chunks = filtered
	sources := kb.Sources[:0]
	for _, s := range kb.Sources {
		if s.Path != absPath {
			sources = append(sources, s)
		}
	}
	kb.Sources = sources
	return kb
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printKBHelp() {
	n := program_name
	fmt.Printf("Manage the IQ knowledge base for RAG-augmented prompts.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s kb <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-12s %s\n", "ingest, in", "Ingest a file or directory tree into the knowledge base")
	fmt.Printf("  %-12s %s\n", "list", "Show indexed sources")
	fmt.Printf("  %-12s %s\n", "search", "Run a raw similarity search (no inference)")
	fmt.Printf("  %-12s %s\n", "rm", "Remove a source from the knowledge base")
	fmt.Printf("  %-12s %s\n\n", "clear", "Wipe the entire knowledge base")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s kb ingest ~/projects/myapp\n", n)
	fmt.Printf("  $ %s kb ingest ./README.md\n", n)
	fmt.Printf("  $ %s kb list\n", n)
	fmt.Printf("  $ %s kb search \"how does auth work\"\n", n)
	fmt.Printf("  $ %s kb rm ~/projects/myapp\n", n)
	fmt.Printf("  $ %s kb clear\n\n", n)
}

// ── Command ───────────────────────────────────────────────────────────────────

func newKbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "kb",
		Short:        "Manage the IQ knowledge base",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printKBHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printKBHelp()
	})
	cmd.AddCommand(
		newKbIngestCmd(),
		newKbListCmd(),
		newKbSearchCmd(),
		newKbRmCmd(),
		newKbClearCmd(),
	)
	return cmd
}

func newKbIngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "ingest <path>",
		Aliases:      []string{"in"},
		Short:        "Ingest a file or directory into the knowledge base",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return kbIngest(args[0])
		},
	}
}

func newKbListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "Show indexed sources",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			kb, err := loadKB()
			if err != nil {
				return err
			}
			if len(kb.Sources) == 0 {
				fmt.Printf("%s\n", utl.Gra("knowledge base is empty — run: iq kb ingest <path>"))
				return nil
			}
			path, _ := kbPath()
			total := 0
			for _, s := range kb.Sources {
				total += s.ChunkCount
			}
			fmt.Printf("%-12s %s\n\n", "KB", path)
			fmt.Printf("%-50s  %6s  %6s  %s\n", "SOURCE", "FILES", "CHUNKS", "INGESTED")
			for _, s := range kb.Sources {
				t, _ := time.Parse(time.RFC3339, s.IngestedAt)
				ingested := ""
				if !t.IsZero() {
					ingested = t.Format("2006-01-02 15:04")
				}
				fmt.Printf("%-50s  %6d  %6d  %s\n",
					s.Path, s.FileCount, s.ChunkCount, utl.Gra(ingested))
			}
			fmt.Printf("\n%-50s  %6s  %6d\n", "TOTAL", "", total)
			return nil
		},
	}
}

func newKbSearchCmd() *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:          "search <query>",
		Short:        "Run a raw similarity search against the knowledge base",
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !kbExists() {
				return fmt.Errorf("knowledge base is empty — run: iq kb ingest <path>")
			}
			if !embedSidecarAlive() {
				return fmt.Errorf("embed sidecar not running — run: iq svc start")
			}
			query := strings.Join(args, " ")

			// Bypass KBSearch threshold — show all results for diagnostic purposes.
			kb, err := loadKB()
			if err != nil {
				return err
			}
			vecs, err := embedTexts([]string{query}, "query")
			if err != nil {
				return err
			}
			qvec := vecs[0]
			keywords := extractKeywords(query)
			results := make([]kbResult, 0, len(kb.Chunks))
			for _, c := range kb.Chunks {
				if len(c.Embedding) == 0 {
					continue
				}
				score := cosineSimilarity(qvec, c.Embedding)
				score += keywordBoost(c.Text, c.Label, keywords)
				results = append(results, kbResult{Chunk: c, Score: score})
			}
			sort.Slice(results, func(i, j int) bool {
				return results[i].Score > results[j].Score
			})
			if topK < len(results) {
				results = results[:topK]
			}
			if len(results) == 0 {
				fmt.Printf("%s\n", utl.Gra("no results"))
				return nil
			}
			fmt.Printf("%s threshold:%.2f\n\n", utl.Gra("kb search —"), kbMinScore)
			for _, r := range results {
				willInject := r.Score >= kbMinScore
				scoreStr := fmt.Sprintf("score:%.4f", r.Score)
				if !willInject {
					scoreStr = utl.Gra(scoreStr + "  (below threshold — will not inject)")
				}
				labelStr := ""
				if r.Chunk.Label != "" {
					labelStr = "  [" + r.Chunk.Label + "]"
				}
				header := fmt.Sprintf("%s%s  %s  lines %d–%d",
					r.Chunk.Source, labelStr, scoreStr, r.Chunk.LineStart, r.Chunk.LineEnd)
				if willInject {
					fmt.Printf("%s\n", utl.Whi(header))
				} else {
					fmt.Printf("%s\n", utl.Gra(header))
				}
				lines := strings.SplitN(r.Chunk.Text, "\n", 4)
				preview := lines
				if len(lines) > 3 {
					preview = append(lines[:3], "...")
				}
				fmt.Printf("%s\n\n", utl.Gra(strings.Join(preview, "\n")))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&topK, "top", "k", kbDefaultK, "Number of results to return")
	return cmd
}

func newKbRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <path>",
		Short:        "Remove a source from the knowledge base",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			kb, err := loadKB()
			if err != nil {
				return err
			}
			before := len(kb.Chunks)
			kb = removeSource(kb, abs)
			removed := before - len(kb.Chunks)
			if removed == 0 {
				fmt.Printf("%s\n", utl.Gra(fmt.Sprintf("%s not found in knowledge base", abs)))
				return nil
			}
			if err := saveKB(kb); err != nil {
				return err
			}
			fmt.Printf("removed %s  (%d chunks)\n", utl.Whi(abs), removed)
			return nil
		},
	}
}

func newKbClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "clear",
		Short:        "Wipe the entire knowledge base",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := kbPath()
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Printf("%s\n", utl.Gre("knowledge base cleared"))
			return nil
		},
	}
}
