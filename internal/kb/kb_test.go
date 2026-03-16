package kb

import "testing"

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
