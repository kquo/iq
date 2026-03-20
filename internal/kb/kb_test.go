package kb

import (
	"strings"
	"testing"
)

func makeIndex(chunks []Chunk, sources []Source) *Index {
	return &Index{Chunks: chunks, Sources: sources}
}

func TestRemoveSourceExactFile(t *testing.T) {
	idx := makeIndex(
		[]Chunk{
			{Source: "/data/app/file.txt"},
			{Source: "/data/app2/file.txt"},
			{Source: "/data/app/sub/other.txt"},
		},
		[]Source{
			{Path: "/data/app/file.txt"},
			{Path: "/data/app2/file.txt"},
		},
	)
	RemoveSource(idx, "/data/app/file.txt")

	if len(idx.Chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(idx.Chunks))
	}
	for _, c := range idx.Chunks {
		if c.Source == "/data/app/file.txt" {
			t.Errorf("chunk %q should have been removed", c.Source)
		}
	}
	if len(idx.Sources) != 1 || idx.Sources[0].Path != "/data/app2/file.txt" {
		t.Errorf("unexpected sources: %v", idx.Sources)
	}
}

func TestRemoveSourceDirectoryBoundary(t *testing.T) {
	idx := makeIndex(
		[]Chunk{
			{Source: "/data/app/a.txt"},
			{Source: "/data/app/sub/b.txt"},
			{Source: "/data/app2/c.txt"},
			{Source: "/data/appstore/d.txt"},
		},
		[]Source{
			{Path: "/data/app"},
			{Path: "/data/app2"},
		},
	)
	RemoveSource(idx, "/data/app")

	// /data/app/a.txt and /data/app/sub/b.txt must be gone
	// /data/app2/c.txt and /data/appstore/d.txt must remain
	if len(idx.Chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d: %v", len(idx.Chunks), idx.Chunks)
	}
	for _, c := range idx.Chunks {
		if c.Source == "/data/app/a.txt" || c.Source == "/data/app/sub/b.txt" {
			t.Errorf("chunk %q should have been removed", c.Source)
		}
	}
	// Sources: /data/app removed, /data/app2 kept
	if len(idx.Sources) != 1 || idx.Sources[0].Path != "/data/app2" {
		t.Errorf("unexpected sources: %v", idx.Sources)
	}
}

func TestRemoveSourceNoPrefixCollision(t *testing.T) {
	idx := makeIndex(
		[]Chunk{
			{Source: "/data/app2/file.txt"},
			{Source: "/data/appstore/file.txt"},
		},
		[]Source{
			{Path: "/data/app2"},
			{Path: "/data/appstore"},
		},
	)
	RemoveSource(idx, "/data/app")

	// Nothing should be removed — no exact match and no /data/app/ prefix
	if len(idx.Chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(idx.Chunks))
	}
	if len(idx.Sources) != 2 {
		t.Fatalf("want 2 sources, got %d", len(idx.Sources))
	}
}

func TestRemoveSourceEmpty(t *testing.T) {
	idx := makeIndex(nil, nil)
	RemoveSource(idx, "/data/app")
	if len(idx.Chunks) != 0 || len(idx.Sources) != 0 {
		t.Errorf("expected empty index to remain empty")
	}
}

// ── truncateRunes ─────────────────────────────────────────────────────────────

func TestTruncateRunes(t *testing.T) {
	// Shorter than limit — unchanged.
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("truncateRunes short = %q, want %q", got, "hello")
	}
	// Exactly at limit — unchanged.
	if got := truncateRunes("abcde", 5); got != "abcde" {
		t.Errorf("truncateRunes exact = %q, want %q", got, "abcde")
	}
	// Over limit — truncated.
	if got := truncateRunes("abcdef", 4); got != "abcd" {
		t.Errorf("truncateRunes over = %q, want %q", got, "abcd")
	}
	// Multi-byte runes — truncates on rune boundary.
	s := "日本語テスト"
	if got := truncateRunes(s, 3); got != "日本語" {
		t.Errorf("truncateRunes multibyte = %q, want %q", got, "日本語")
	}
}

// ── ExtractKeywords ───────────────────────────────────────────────────────────

func TestExtractKeywords(t *testing.T) {
	// Basic tokens, deduplication, min 4 chars.
	kws := ExtractKeywords("hello world foo bar")
	got := strings.Join(kws, " ")
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("ExtractKeywords basic = %v, want hello/world present", kws)
	}
	// "foo" and "bar" are < 4 chars — must not appear.
	for _, kw := range kws {
		if kw == "foo" || kw == "bar" {
			t.Errorf("ExtractKeywords: short token %q should be excluded", kw)
		}
	}

	// CamelCase expansion: "SearchResult" → ["searchresult", "search", "result"]
	kws2 := ExtractKeywords("SearchResult")
	kwMap := map[string]bool{}
	for _, k := range kws2 {
		kwMap[k] = true
	}
	if !kwMap["search"] || !kwMap["result"] {
		t.Errorf("ExtractKeywords camelCase = %v, want 'search' and 'result'", kws2)
	}

	// Deduplication: same token only once.
	kws3 := ExtractKeywords("hello hello world")
	count := 0
	for _, k := range kws3 {
		if k == "hello" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ExtractKeywords dedup: 'hello' appears %d times, want 1", count)
	}

	// Empty input — no crash, empty result.
	if got := ExtractKeywords(""); len(got) != 0 {
		t.Errorf("ExtractKeywords empty = %v, want []", got)
	}
}

// ── KeywordBoost ──────────────────────────────────────────────────────────────

func TestKeywordBoost(t *testing.T) {
	// No keywords — zero bonus.
	if b := KeywordBoost("some text here", "", nil); b != 0 {
		t.Errorf("KeywordBoost(nil) = %f, want 0", b)
	}

	// Keyword present in text — gets keywordBoostVal.
	b := KeywordBoost("the function search returns results", "", []string{"search"})
	if b < keywordBoostVal {
		t.Errorf("KeywordBoost(match) = %f, want >= %f", b, float32(keywordBoostVal))
	}

	// Function call pattern: text contains "search(" and label is NOT "func search" — gets callBoost.
	b2 := KeywordBoost("call search(query) here", "some other label", []string{"search"})
	expected := float32(keywordBoostVal + callBoostVal)
	if b2 < expected {
		t.Errorf("KeywordBoost(call) = %f, want >= %f", b2, expected)
	}

	// Definition label suppresses callBoost.
	b3 := KeywordBoost("func search(q string) {}", "func search", []string{"search"})
	// Gets keywordBoostVal but NOT callBoostVal (label says it's the definition).
	if b3 >= float32(keywordBoostVal+callBoostVal) {
		t.Errorf("KeywordBoost(definition) = %f, should not get callBoost", b3)
	}

	// Cap at maxBoostVal.
	many := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"}
	text := "alpha beta gamma delta epsilon zeta eta"
	b4 := KeywordBoost(text, "", many)
	if b4 > maxBoostVal {
		t.Errorf("KeywordBoost(cap) = %f, want <= %f", b4, float32(maxBoostVal))
	}
	if b4 != maxBoostVal {
		t.Errorf("KeywordBoost(cap) = %f, want exactly %f", b4, float32(maxBoostVal))
	}
}

// ── Context ───────────────────────────────────────────────────────────────────

func TestContext(t *testing.T) {
	// Empty results → empty string.
	if got := Context(nil); got != "" {
		t.Errorf("Context(nil) = %q, want %q", got, "")
	}
	if got := Context([]Result{}); got != "" {
		t.Errorf("Context([]) = %q, want %q", got, "")
	}

	// Single result contains expected header and text.
	results := []Result{
		{Chunk: Chunk{Source: "/src/foo.go", LineStart: 1, LineEnd: 10, Text: "func Foo() {}"}, Score: 0.9},
	}
	got := Context(results)
	if !strings.Contains(got, "Relevant context from knowledge base:") {
		t.Errorf("Context: missing header, got %q", got)
	}
	if !strings.Contains(got, "KB Result Chunk 01:") {
		t.Errorf("Context: missing chunk label, got %q", got)
	}
	if !strings.Contains(got, "func Foo() {}") {
		t.Errorf("Context: missing chunk text, got %q", got)
	}

	// Multiple results — chunks numbered sequentially.
	results2 := []Result{
		{Chunk: Chunk{Source: "/a.go", LineStart: 1, LineEnd: 5, Text: "first"}, Score: 0.9},
		{Chunk: Chunk{Source: "/b.go", LineStart: 6, LineEnd: 10, Text: "second"}, Score: 0.8},
	}
	got2 := Context(results2)
	if !strings.Contains(got2, "KB Result Chunk 01:") || !strings.Contains(got2, "KB Result Chunk 02:") {
		t.Errorf("Context: expected Chunk 01 and 02, got %q", got2)
	}
}

// ── EmbedText ─────────────────────────────────────────────────────────────────

func TestEmbedText(t *testing.T) {
	// With label.
	out := EmbedText("/root", "/root/pkg/file.go", "func Foo", "body text")
	if !strings.Contains(out, "File: pkg/file.go") {
		t.Errorf("EmbedText: missing file prefix, got %q", out)
	}
	if !strings.Contains(out, "func Foo") {
		t.Errorf("EmbedText: missing label, got %q", out)
	}
	if !strings.Contains(out, "body text") {
		t.Errorf("EmbedText: missing text, got %q", out)
	}

	// Without label — no extra newline between prefix and text.
	out2 := EmbedText("/root", "/root/file.txt", "", "content here")
	if !strings.Contains(out2, "File: file.txt") {
		t.Errorf("EmbedText(no label): missing file prefix, got %q", out2)
	}
	if !strings.Contains(out2, "content here") {
		t.Errorf("EmbedText(no label): missing text, got %q", out2)
	}

	// Truncation: text longer than MaxRunes is capped.
	long := strings.Repeat("x", MaxRunes*2)
	out3 := EmbedText("/r", "/r/f.go", "", long)
	if len([]rune(out3)) > MaxRunes {
		t.Errorf("EmbedText(long): output %d runes, want <= %d", len([]rune(out3)), MaxRunes)
	}
}

// ── extractGoLabel ────────────────────────────────────────────────────────────

func TestExtractGoLabel(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"func Foo(x int) string {", "func Foo"},
		{"func standalone() {", "func standalone"},
		{"func noParens", "func noParens"}, // no "(" found — falls through to name = "func " + rest
		{"type MyStruct struct {", "type MyStruct"},
		{"type Iface interface {", "type Iface"},
		{"const (", "const block"},
		{"const X = 1", "const block"},
		{"var (", "var block"},
		{"var x = 5", "var block"},
		{"something else", "something else"},
	}
	for _, tc := range cases {
		got := extractGoLabel(tc.line)
		if got != tc.want {
			t.Errorf("extractGoLabel(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// ── PathFor / LoadFrom / SaveTo tests ────────────────────────────────────────

func TestPathFor(t *testing.T) {
	dir := "/tmp/test-kb"
	got := PathFor(dir)
	want := "/tmp/test-kb/kb.json"
	if got != want {
		t.Errorf("PathFor(%q) = %q, want %q", dir, got, want)
	}
}

func TestLoadFrom_notExist(t *testing.T) {
	dir := t.TempDir()
	idx, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom on empty dir: %v", err)
	}
	if idx == nil {
		t.Fatal("LoadFrom returned nil index")
	}
	if len(idx.Chunks) != 0 {
		t.Errorf("expected empty chunks, got %d", len(idx.Chunks))
	}
}

func TestSaveToLoadFrom_roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := PathFor(dir)

	idx := &Index{
		Version: 1,
		Sources: []Source{{Path: "/foo", ChunkCount: 2}},
		Chunks: []Chunk{
			{ID: "c1", Source: "/foo/a.txt", Text: "hello"},
			{ID: "c2", Source: "/foo/b.txt", Text: "world"},
		},
	}

	if err := SaveTo(path, idx); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(loaded.Chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(loaded.Chunks))
	}
	if loaded.Chunks[0].ID != "c1" || loaded.Chunks[1].ID != "c2" {
		t.Errorf("unexpected chunk IDs: %v", loaded.Chunks)
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Path != "/foo" {
		t.Errorf("unexpected sources: %v", loaded.Sources)
	}
}
