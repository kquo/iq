package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/queone/utl"
	"iq/internal/tools"
)

// ── Trace helpers ────────────────────────────────────────────────────────────

func printToolCallTrace(call tools.Call) {
	argsJSON, _ := json.Marshal(call.Args)
	traceField("tool_call", fmt.Sprintf("%s(%s)", call.Name, string(argsJSON)))
}

func printToolResultTrace(r tools.Result) {
	if r.Error != "" {
		traceField("tool_error", truncate(r.Error, 200))
	} else {
		traceField("tool_result", truncate(r.Output, 200))
	}
}

// printToolStatus prints a short tool-use indicator to stderr.
func printToolStatus(call tools.Call) {
	fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("[tool: %s]", call.Name)))
}
