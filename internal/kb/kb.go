package kb

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

	"iq/internal/config"
	"iq/internal/embed"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Chunk represents a segment of a source file in the knowledge base.
type Chunk struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"` // absolute file path
	LineStart int       `json:"line_start"`
	LineEnd   int       `json:"line_end"`
	Label     string    `json:"label"` // symbol/heading/key for metadata prefix
	Text      string    `json:"text"`  // raw chunk text (stored as-is, not prefixed)
	Embedding []float32 `json:"embedding"`
}

// Source tracks an ingested source path.
type Source struct {
	Path       string `json:"path"`
	IngestedAt string `json:"ingested_at"`
	FileCount  int    `json:"file_count"`
	ChunkCount int    `json:"chunk_count"`
}

// Index is the top-level knowledge base structure.
type Index struct {
	Version int      `json:"version"`
	Sources []Source `json:"sources"`
	Chunks  []Chunk  `json:"chunks"`
}

// Result holds a chunk and its similarity score.
type Result struct {
	Chunk Chunk
	Score float32
}

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	EmbedBatch = 20   // chunks per embed request
	MinScore   = 0.72 // minimum cosine similarity to inject
	MaxRunes   = 1600 // max runes per chunk text sent to embedder
	DefaultK   = 3    // default top-K for retrieval
)

const (
	keywordBoostVal = 0.05
	callBoostVal    = 0.12
	maxBoostVal     = 0.25
)

// ── Text extensions ──────────────────────────────────────────────────────────

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

// goSymbolRe matches top-level Go declarations that start a new semantic chunk.
var goSymbolRe = regexp.MustCompile(`^(func|type|var|const)\s`)

// ── Storage ───────────────────────────────────────────────────────────────────

// Path returns the full path to kb.json.
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kb.json"), nil
}

// Exists returns true if a non-empty kb.json exists.
func Exists() bool {
	path, err := Path()
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// Load reads the knowledge base from disk.
func Load() (*Index, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Index{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("failed to parse kb.json: %w", err)
	}
	return &idx, nil
}

// Save writes the knowledge base to disk.
func Save(idx *Index) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ── Text file detection ──────────────────────────────────────────────────────

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

// IsTextFile returns true if the file at path appears to be a text file.
func IsTextFile(path string) bool {
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

// ── Chunking helpers ─────────────────────────────────────────────────────────

func chunkID(source string, start, end int) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d", source, start, end))
	return fmt.Sprintf("%x", h[:8])
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// EmbedText returns the text sent to the embedder: metadata prefix + truncated content.
func EmbedText(root, source, label, text string) string {
	rel, err := filepath.Rel(root, source)
	if err != nil {
		rel = source
	}
	prefix := fmt.Sprintf("File: %s", rel)
	if label != "" {
		prefix += "\n" + label
	}
	return truncateRunes(prefix+"\n\n"+text, MaxRunes)
}

// ── Go chunker ───────────────────────────────────────────────────────────────

func chunkGo(path string) ([]Chunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	var boundaries []int
	for i, line := range lines {
		if goSymbolRe.MatchString(line) {
			boundaries = append(boundaries, i)
		}
	}

	if len(boundaries) == 0 {
		text := strings.TrimSpace(string(data))
		if text == "" {
			return nil, nil
		}
		return []Chunk{{
			ID: chunkID(path, 1, len(lines)), Source: path,
			LineStart: 1, LineEnd: len(lines), Text: text,
		}}, nil
	}

	var chunks []Chunk
	for i, start := range boundaries {
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1]
		}
		text := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if text == "" {
			continue
		}
		label := extractGoLabel(lines[start])
		chunks = append(chunks, Chunk{
			ID: chunkID(path, start+1, end), Source: path,
			LineStart: start + 1, LineEnd: end, Label: label, Text: text,
		})
	}

	if boundaries[0] > 0 {
		text := strings.TrimSpace(strings.Join(lines[:boundaries[0]], "\n"))
		if text != "" {
			chunks = append([]Chunk{{
				ID: chunkID(path, 1, boundaries[0]), Source: path,
				LineStart: 1, LineEnd: boundaries[0], Label: "package imports", Text: text,
			}}, chunks...)
		}
	}

	return chunks, nil
}

func extractGoLabel(line string) string {
	line = strings.TrimSpace(line)
	if after, ok := strings.CutPrefix(line, "func "); ok {
		name := after
		if idx := strings.Index(name, "("); idx > 0 {
			name = "func " + strings.TrimSpace(name[:idx])
		} else {
			name = "func " + name
		}
		return name
	}
	if strings.HasPrefix(line, "type ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			return "type " + parts[1]
		}
	}
	if strings.HasPrefix(line, "const") {
		return "const block"
	}
	if strings.HasPrefix(line, "var") {
		return "var block"
	}
	return line
}

// ── Markdown chunker ─────────────────────────────────────────────────────────

func chunkMarkdown(path string) ([]Chunk, error) {
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
	headingStack := make([]string, 6)
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
			for j := level + 1; j < 6; j++ {
				headingStack[j] = ""
			}
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
			if current == nil {
				current = &section{start: 1, label: "", lines: []string{}}
			}
			current.lines = append(current.lines, line)
		}
	}
	flush(len(lines))

	var chunks []Chunk
	for _, s := range sections {
		text := strings.TrimSpace(strings.Join(s.lines, "\n"))
		if text == "" {
			continue
		}
		end := s.start + len(s.lines) - 1
		chunks = append(chunks, Chunk{
			ID: chunkID(path, s.start, end), Source: path,
			LineStart: s.start, LineEnd: end, Label: s.label, Text: text,
		})
	}
	return chunks, nil
}

// ── YAML/TOML chunker ────────────────────────────────────────────────────────

func chunkKeyValue(path string) ([]Chunk, error) {
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
		return []Chunk{{
			ID: chunkID(path, 1, len(lines)), Source: path,
			LineStart: 1, LineEnd: len(lines), Text: text,
		}}, nil
	}

	var chunks []Chunk
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
		chunks = append(chunks, Chunk{
			ID: chunkID(path, start+1, end), Source: path,
			LineStart: start + 1, LineEnd: end, Label: label, Text: text,
		})
	}
	return chunks, nil
}

// ── Prose chunker ────────────────────────────────────────────────────────────

func chunkProse(path string) ([]Chunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	type para struct {
		start int
		text  string
	}
	var paragraphs []para
	paraStart := 0
	var paraLines []string
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(paraLines) > 0 {
				paragraphs = append(paragraphs, para{paraStart + 1, strings.Join(paraLines, "\n")})
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
		paragraphs = append(paragraphs, para{paraStart + 1, strings.Join(paraLines, "\n")})
	}
	if len(paragraphs) == 0 {
		return nil, nil
	}

	var chunks []Chunk
	var groupText strings.Builder
	groupStart := paragraphs[0].start
	groupEnd := groupStart

	flushGroup := func(end int) {
		text := strings.TrimSpace(groupText.String())
		if text == "" {
			return
		}
		chunks = append(chunks, Chunk{
			ID: chunkID(path, groupStart, end), Source: path,
			LineStart: groupStart, LineEnd: end, Text: text,
		})
		groupText.Reset()
	}

	for _, p := range paragraphs {
		pRunes := len([]rune(p.text))
		currentRunes := len([]rune(groupText.String()))
		if currentRunes > 0 && currentRunes+pRunes > MaxRunes {
			flushGroup(groupEnd)
			groupStart = p.start
		}
		if groupText.Len() > 0 {
			groupText.WriteString("\n\n")
		}
		groupText.WriteString(p.text)
		groupEnd = p.start + strings.Count(p.text, "\n")
	}
	flushGroup(groupEnd)

	return chunks, nil
}

// ── Chunker dispatcher ───────────────────────────────────────────────────────

// ChunkFile splits a file into semantic chunks based on its type.
func ChunkFile(path string) ([]Chunk, error) {
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

// ── Keyword extraction + boosting ────────────────────────────────────────────

var camelRe = regexp.MustCompile(`[A-Z][a-z]+|[a-z]+|[A-Z]+`)

// ExtractKeywords pulls meaningful tokens from a query for keyword boosting.
func ExtractKeywords(query string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		low := strings.ToLower(s)
		if len(low) >= 4 && !seen[low] {
			seen[low] = true
			out = append(out, low)
		}
	}
	for _, tok := range regexp.MustCompile(`[^a-zA-Z0-9_]+`).Split(query, -1) {
		if tok == "" {
			continue
		}
		add(tok)
		parts := camelRe.FindAllString(tok, -1)
		if len(parts) > 1 {
			for _, p := range parts {
				add(p)
			}
		}
	}
	return out
}

// KeywordBoost returns a score bonus for a chunk based on exact keyword matches.
func KeywordBoost(text, label string, keywords []string) float32 {
	low := strings.ToLower(text)
	lowLabel := strings.ToLower(label)
	var bonus float32
	for _, kw := range keywords {
		if strings.Contains(low, kw) {
			bonus += keywordBoostVal
			isDefinition := strings.Contains(lowLabel, "func "+kw) ||
				strings.Contains(lowLabel, "type "+kw)
			if !isDefinition && strings.Contains(low, kw+"(") {
				bonus += callBoostVal
			}
		}
	}
	if bonus > maxBoostVal {
		bonus = maxBoostVal
	}
	return bonus
}

// ── Search ───────────────────────────────────────────────────────────────────

// Search embeds the query and returns the top-k most similar chunks above minScore.
func Search(query string, topK int, minScore float32) ([]Result, error) {
	idx, err := Load()
	if err != nil {
		return nil, err
	}
	if len(idx.Chunks) == 0 {
		return nil, nil
	}

	vecs, err := embed.Texts([]string{query}, "query")
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	qvec := vecs[0]

	keywords := ExtractKeywords(query)

	results := make([]Result, 0, len(idx.Chunks))
	for _, c := range idx.Chunks {
		if len(c.Embedding) == 0 {
			continue
		}
		score := embed.CosineSimilarity(qvec, c.Embedding)
		score += KeywordBoost(c.Text, c.Label, keywords)
		results = append(results, Result{Chunk: c, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	filtered := results[:0]
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}
	results = filtered
	if topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// Context builds a context string from search results for prompt injection.
func Context(results []Result) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Relevant context from knowledge base:\n")
	for i, r := range results {
		header := fmt.Sprintf("\nKB Result Chunk %02d: %s (lines %d–%d)\n",
			i+1, r.Chunk.Source, r.Chunk.LineStart, r.Chunk.LineEnd)
		sb.WriteString(header)
		sb.WriteString(r.Chunk.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ── Ingest ───────────────────────────────────────────────────────────────────

// RemoveSource removes all chunks and the source entry for absPath.
func RemoveSource(idx *Index, absPath string) *Index {
	filtered := idx.Chunks[:0]
	for _, c := range idx.Chunks {
		if !strings.HasPrefix(c.Source, absPath) {
			filtered = append(filtered, c)
		}
	}
	idx.Chunks = filtered
	sources := idx.Sources[:0]
	for _, s := range idx.Sources {
		if s.Path != absPath {
			sources = append(sources, s)
		}
	}
	idx.Sources = sources
	return idx
}

// Ingest walks root, chunks all text files, embeds them, and adds to the index.
func Ingest(root string) error {
	if !embed.SidecarAlive() {
		return fmt.Errorf("embed sidecar not running — run: iq start")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("%s: %w", abs, err)
	}

	idx, err := Load()
	if err != nil {
		return err
	}

	idx = RemoveSource(idx, abs)

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
			if IsTextFile(p) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		if !IsTextFile(abs) {
			return fmt.Errorf("%s does not appear to be a text file", abs)
		}
		files = []string{abs}
	}

	if len(files) == 0 {
		return fmt.Errorf("no text files found in %s", abs)
	}

	var allChunks []Chunk
	for _, f := range files {
		chunks, err := ChunkFile(f)
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

	embedded := 0
	for i := 0; i < len(allChunks); i += EmbedBatch {
		end := min(i+EmbedBatch, len(allChunks))
		batch := allChunks[i:end]
		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = EmbedText(abs, c.Source, c.Label, c.Text)
		}
		vecs, err := embed.Texts(texts, "document")
		if err != nil {
			fmt.Println()
			return fmt.Errorf("embed batch %d: %w", i/EmbedBatch, err)
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

	idx.Chunks = append(idx.Chunks, allChunks...)
	idx.Sources = append(idx.Sources, Source{
		Path:       abs,
		IngestedAt: time.Now().UTC().Format(time.RFC3339),
		FileCount:  len(files),
		ChunkCount: embedded,
	})

	if err := Save(idx); err != nil {
		return fmt.Errorf("failed to save kb.json: %w", err)
	}
	fmt.Printf("  %s  ingested %s  (%d chunks embedded)\n",
		utl.Gre("ok"), utl.Whi(abs), embedded)
	return nil
}
