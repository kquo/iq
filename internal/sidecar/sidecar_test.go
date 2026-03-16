package sidecar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTestState writes a state file into the fake HOME used by the test.
func writeTestState(t *testing.T, home, modelID string, port, pid int) {
	t.Helper()
	runDir := filepath.Join(home, ".config", "iq", "run")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	state := &State{
		Tier:    "fast",
		Model:   modelID,
		PID:     pid,
		Port:    port,
		Started: "2025-01-01T00:00:00Z",
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	slug := ModelSlug(modelID)
	path := filepath.Join(runDir, slug+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}

// TestNextAvailablePortSkipsDeadPIDs verifies that state files with dead PIDs
// do not reserve ports — their ports are immediately reusable.
func TestNextAvailablePortSkipsDeadPIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a state file with a dead PID. PID 99999999 is astronomically
	// unlikely to exist on any real system.
	const deadPID = 99999999
	writeTestState(t, home, "org/dead-model", PortBase, deadPID)

	port, err := NextAvailablePort()
	if err != nil {
		t.Fatalf("NextAvailablePort: %v", err)
	}
	// Dead state must not reserve PortBase — it should be returned as available.
	if port != PortBase {
		t.Errorf("got port %d, want %d (dead PID should not block port)", port, PortBase)
	}
}

// TestNextAvailablePortRespectsLivePIDs verifies that a state file with a live
// PID (the current process) does reserve its port.
func TestNextAvailablePortRespectsLivePIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a state with the current process's PID — guaranteed alive.
	writeTestState(t, home, "org/live-model", PortBase, os.Getpid())

	port, err := NextAvailablePort()
	if err != nil {
		t.Fatalf("NextAvailablePort: %v", err)
	}
	// Live state must block PortBase; next port should be PortBase+1.
	if port != PortBase+1 {
		t.Errorf("got port %d, want %d (live PID should block port)", port, PortBase+1)
	}
}

// TestNextAvailablePortMixedStates verifies the combination: one live state
// (blocks its port) and one dead state (does not block its port).
func TestNextAvailablePortMixedStates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestState(t, home, "org/live-model", PortBase, os.Getpid())
	writeTestState(t, home, "org/dead-model", PortBase+1, 99999999)

	port, err := NextAvailablePort()
	if err != nil {
		t.Fatalf("NextAvailablePort: %v", err)
	}
	// PortBase is blocked (live). PortBase+1 is free (dead PID).
	if port != PortBase+1 {
		t.Errorf("got port %d, want %d", port, PortBase+1)
	}
}
