package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"iq/internal/config"
	"iq/internal/sidecar"
)

// mockInferServer returns an httptest.Server that speaks the
// OpenAI-compatible /v1/chat/completions protocol.
// handler receives the decoded sidecar.ChatRequest and returns the text response.
func mockInferServer(t *testing.T, handler func(sidecar.ChatRequest) string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req sidecar.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		text := handler(req)
		resp := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, text)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	})
	return httptest.NewServer(mux)
}

// serverPort extracts the port number from an httptest.Server URL.
func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

// setupTestEnv creates a minimal ~/.config/iq structure under a temp dir,
// writes config.yaml and a sidecar state file, and sets HOME so all
// config/sidecar lookups resolve there.
func setupTestEnv(t *testing.T, modelID string, tier string, port int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "iq")
	runDir := filepath.Join(cfgDir, "run")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write config.yaml with the model in the specified tier.
	cfg := &config.Config{
		Tiers: map[string]*config.TierConfig{
			"fast": {Models: []string{}},
			"slow": {Models: []string{}},
		},
	}
	if tc, ok := cfg.Tiers[tier]; ok {
		tc.Models = []string{modelID}
	}
	cfgData, _ := json.Marshal(cfg) // yaml would be better but json is valid yaml
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), cfgData, 0644)

	// Write sidecar state file so PickSidecar finds it.
	// PID = our own process, which PidAlive always reports as true.
	state := &sidecar.State{
		Tier:    tier,
		Model:   modelID,
		PID:     os.Getpid(),
		Port:    port,
		Started: "2025-01-01T00:00:00Z",
	}
	stateData, _ := json.MarshalIndent(state, "", "  ")
	slug := strings.ReplaceAll(modelID, "/", "--")
	os.WriteFile(filepath.Join(runDir, slug+".json"), stateData, 0644)

	return home
}

// TestEndToEndInference exercises the full classify → route → assemble → infer
// pipeline using a mock sidecar, verifying that executePrompt wires everything
// together correctly.
func TestEndToEndInference(t *testing.T) {
	const modelID = "test-org/test-model"
	const wantResponse = "Mock inference response."

	srv := mockInferServer(t, func(req sidecar.ChatRequest) string {
		// Verify the assembled messages look right.
		if len(req.Messages) < 1 {
			t.Error("expected at least 1 message in request")
		}
		// Last message should be the user's input.
		last := req.Messages[len(req.Messages)-1]
		if last.Role != "user" {
			t.Errorf("last message role = %q, want user", last.Role)
		}
		return wantResponse
	})
	defer srv.Close()
	port := serverPort(t, srv)

	setupTestEnv(t, modelID, "fast", port)

	// Execute with cue+tier forced, all optional features off, no streaming.
	opts := promptOpts{
		tier:     "fast",
		noKB:     true,
		noCache:  true,
		toolMode: "off",
		noStream: true,
	}

	// Capture stdout so we can assert on the printed response.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	sess, err := executePrompt("what is Go?", opts, nil)
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("executePrompt error: %v", err)
	}
	if sess != nil {
		t.Errorf("expected nil session, got %+v", sess)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	got := strings.TrimSpace(string(buf[:n]))

	if got != wantResponse {
		t.Errorf("response = %q, want %q", got, wantResponse)
	}
}

// TestDumpPrompt verifies that --dump-prompt writes the assembled message
// array as JSON and stops before inference.
func TestDumpPrompt(t *testing.T) {
	const modelID = "test-org/test-model"

	// Server should never be called — dump-prompt stops before inference.
	srv := mockInferServer(t, func(req sidecar.ChatRequest) string {
		t.Error("inference should not be called with --dump-prompt")
		return ""
	})
	defer srv.Close()
	port := serverPort(t, srv)

	home := setupTestEnv(t, modelID, "fast", port)

	dumpFile := filepath.Join(home, "prompt.json")
	opts := promptOpts{
		tier:       "fast",
		noKB:       true,
		noCache:    true,
		toolMode:   "off",
		noStream:   true,
		dumpPrompt: dumpFile,
	}

	// Suppress stdout/stderr from trace output.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	os.Stderr = devNull

	_, err := executePrompt("hello world", opts, nil)
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	devNull.Close()

	if err != nil {
		t.Fatalf("executePrompt error: %v", err)
	}

	data, err := os.ReadFile(dumpFile)
	if err != nil {
		t.Fatalf("dump file not written: %v", err)
	}

	var messages []config.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("dump file is not valid JSON: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("dump file has no messages")
	}

	// The last message should be the user input.
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Content != "hello world" {
		t.Errorf("last message = %+v, want {Role:user Content:hello world}", last)
	}
}

// TestSessionLocking verifies that concurrent saves to the same session file
// do not corrupt the YAML — each writer holds the exclusive lock before writing.
func TestSessionLocking(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed an initial session on disk.
	base := &session{
		ID:   "test-lock-session",
		Name: "initial",
		Cue:  "general",
		Tier: "fast",
	}
	if err := saveSession(base); err != nil {
		t.Fatalf("initial saveSession: %v", err)
	}

	// Concurrently save 20 updates, each appending a user message.
	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(n int) {
			defer wg.Done()
			s := &session{
				ID:       base.ID,
				Name:     fmt.Sprintf("worker-%d", n),
				Cue:      base.Cue,
				Tier:     base.Tier,
				Messages: []config.Message{{Role: "user", Content: fmt.Sprintf("msg-%d", n)}},
			}
			if err := saveSession(s); err != nil {
				t.Errorf("worker %d saveSession: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	// The file must be valid YAML — corruption would cause unmarshal to fail.
	got, err := loadSession(base.ID)
	if err != nil {
		t.Fatalf("loadSession after concurrent writes: %v", err)
	}
	if got == nil {
		t.Fatal("loadSession returned nil after concurrent writes")
	}
}
