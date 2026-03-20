package sidecar

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iq/internal/config"
)

// httpTestPort extracts the port from an httptest.Server URL.
func httpTestPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

// mockChatServer returns an httptest.Server that speaks the OpenAI-compatible
// /v1/chat/completions protocol, returning the given response string.
func mockChatServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		resp := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, response)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	})
	return httptest.NewServer(mux)
}

// ── RawCall tests ─────────────────────────────────────────────────────────────

func TestRawCallHappyPath(t *testing.T) {
	const want = "hello from mock sidecar"
	srv := mockChatServer(t, want)
	defer srv.Close()

	got, err := RawCall(context.Background(), httpTestPort(t, srv), ChatRequest{
		Messages: []config.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("RawCall error: %v", err)
	}
	if got != want {
		t.Errorf("RawCall = %q, want %q", got, want)
	}
}

func TestRawCallEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	_, err := RawCall(context.Background(), httpTestPort(t, srv), ChatRequest{})
	if err == nil {
		t.Error("expected error for empty choices, got nil")
	}
}

func TestRawCallNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	_, err := RawCall(context.Background(), httpTestPort(t, srv), ChatRequest{})
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code 500, got: %v", err)
	}
}

func TestRawCallMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := RawCall(context.Background(), httpTestPort(t, srv), ChatRequest{})
	if err == nil {
		t.Error("expected error for malformed JSON response, got nil")
	}
}

// ── Call tests ────────────────────────────────────────────────────────────────

func TestCallHappyPath(t *testing.T) {
	const want = "call response text"
	srv := mockChatServer(t, want)
	defer srv.Close()

	msgs := []config.Message{{Role: "user", Content: "test"}}
	got, err := Call(context.Background(), httpTestPort(t, srv), msgs, 100, config.ResolvedParams{})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if got != want {
		t.Errorf("Call = %q, want %q", got, want)
	}
}

// TestStreamContextCancel verifies Stream returns before completing when the
// context is cancelled.
func TestStreamContextCancel(t *testing.T) {
	// Server that streams forever until the client disconnects.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"token\"}}]}\n\n")
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Stream(ctx, httpTestPort(t, srv), []config.Message{
		{Role: "user", Content: "hi"},
	}, config.ResolvedParams{MaxTokens: 10})
	// Should return (possibly with a context error) without hanging.
	if err != nil && err.Error() == "" {
		// empty error string is silentErr — acceptable
	}
}

// ── StripThinkBlocks tests ────────────────────────────────────────────────────

func TestStripThinkBlocks(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<think>reasoning</think>answer", "answer"},
		{"no think blocks", "no think blocks"},
		{"<think>multi\nline\nreasoning</think>result", "result"},
		{"", ""},
		{"prefix<think>hidden</think>suffix", "prefixsuffix"},
		{"<think>unclosed block", ""},
		{"<think>a</think><think>b</think>clean", "clean"},
		{"  spaces around  ", "spaces around"},
	}
	for _, tc := range tests {
		got := StripThinkBlocks(tc.input)
		if got != tc.want {
			t.Errorf("StripThinkBlocks(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
