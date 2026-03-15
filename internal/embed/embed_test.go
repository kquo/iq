package embed

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// embedTestPort extracts the port from an httptest.Server URL.
func embedTestPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

// ── CosineSimilarity tests ────────────────────────────────────────────────────

func TestCosineSimilarity(t *testing.T) {
	const eps = 1e-6
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"same direction 2d", []float32{1, 1}, []float32{1, 1}, 1.0},
		{"zero vector a", []float32{0, 0}, []float32{1, 0}, 0.0},
		{"zero vector b", []float32{1, 0}, []float32{0, 0}, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CosineSimilarity(tc.a, tc.b)
			if math.Abs(float64(got-tc.want)) > eps {
				t.Errorf("CosineSimilarity = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── TextsOnPort tests ─────────────────────────────────────────────────────────

func TestTextsOnPort(t *testing.T) {
	wantEmb := []float32{0.1, 0.2, 0.3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" {
			http.NotFound(w, r)
			return
		}
		resp, _ := json.Marshal(map[string]any{
			"embeddings": [][]float32{wantEmb},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	defer srv.Close()

	embs, err := TextsOnPort([]string{"hello"}, "test-model", embedTestPort(t, srv), "query")
	if err != nil {
		t.Fatalf("TextsOnPort error: %v", err)
	}
	if len(embs) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(embs))
	}
	if len(embs[0]) != len(wantEmb) {
		t.Fatalf("embedding length %d, want %d", len(embs[0]), len(wantEmb))
	}
	const eps = 1e-6
	for i, v := range wantEmb {
		if math.Abs(float64(embs[0][i]-v)) > eps {
			t.Errorf("embedding[0][%d] = %v, want %v", i, embs[0][i], v)
		}
	}
}

func TestTextsOnPortNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	port := embedTestPort(t, srv)
	_, err := TextsOnPort([]string{"hello"}, "test-model", port, "query")
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", port)) {
		t.Errorf("error %q should mention port %d", err.Error(), port)
	}
}

func TestTextsOnPortMultipleInputs(t *testing.T) {
	inputs := []string{"first", "second", "third"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return one embedding per input.
		embs := make([][]float32, len(inputs))
		for i := range embs {
			embs[i] = []float32{float32(i) * 0.1}
		}
		resp, _ := json.Marshal(map[string]any{"embeddings": embs})
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	defer srv.Close()

	embs, err := TextsOnPort(inputs, "test-model", embedTestPort(t, srv), "document")
	if err != nil {
		t.Fatalf("TextsOnPort error: %v", err)
	}
	if len(embs) != len(inputs) {
		t.Errorf("got %d embeddings, want %d", len(embs), len(inputs))
	}
}

// ── keywordScore tests ────────────────────────────────────────────────────────

func TestKeywordScore(t *testing.T) {
	tests := []struct {
		input   string
		cueName string
		wantGT0 bool
	}{
		// Single-word cue: exact word-boundary match required.
		{"write python code", "code", true},
		{"explain quantum physics", "code", false},
		{"that is a general question", "general", true},
		{"explain quantum physics", "general", false},
		{"what is the math here", "math", true},
		{"mathematics is hard", "math", true}, // "math" is a substring of "mathematics"
		{"calculus is hard", "math", false},   // "math" not in input at all
		// Multi-word cue (underscore → space): substring match.
		{"do a code review of this", "code_review", true},
		{"review my work", "code_review", false},
		// "initial" always returns 0.
		{"initial catch-all test", "initial", false},
		// Short cue names (len < 4) are not matched.
		{"run it", "run", false},
	}
	for _, tc := range tests {
		got := keywordScore(tc.input, tc.cueName)
		if tc.wantGT0 && got == 0 {
			t.Errorf("keywordScore(%q, %q) = 0, want >0", tc.input, tc.cueName)
		}
		if !tc.wantGT0 && got != 0 {
			t.Errorf("keywordScore(%q, %q) = %v, want 0", tc.input, tc.cueName, got)
		}
	}
}
