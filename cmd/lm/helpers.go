package main

import (
	"fmt"

	iembed "iq/internal/embed"
	"iq/internal/lm"
	"iq/internal/sidecar"
)

// truncate returns s truncated to at most n runes, with an ellipsis appended
// when truncation occurs.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// startSidecar resolves model/python paths and delegates to sidecar.StartInfer.
func startSidecar(modelID string) error {
	modelPath, err := lm.SnapshotDir(modelID)
	if err != nil {
		return fmt.Errorf("cannot resolve model path: %w", err)
	}
	pyPath, err := iembed.MlxVenvPython()
	if err != nil {
		return fmt.Errorf("cannot resolve Python interpreter: %w", err)
	}
	_, err = sidecar.StartInfer(modelID, modelPath, pyPath)
	return err
}
