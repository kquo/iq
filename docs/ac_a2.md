# A2 Acceptance Criteria — Drop the Routing Grammar Harness

## Motivation

The routing grammar harness was built when embed-based tool detection didn't exist. It
constrained the model's first tokens to `<tool:NAME>` or `<no_tool>` via a logits processor
in `infer_server.py`, guaranteeing a structured routing decision from small models that
couldn't emit it organically. Since then, the embed short-circuit handles all 5 tool signals
directly — the grammar path is only reached today via `tt.Reason == "file path"` or
`"forced"`. Removing the grammar simplifies the Python sidecar, the transport layer, and the
Go tool loop. It also removes the hard coupling between `infer_server.py` and the tool name
list.

## Codebase scan

| File | What changes |
|---|---|
| `cmd/iq/prompt.go` | Replace grammar path (`CallWithGrammar` + `ParseRoutingPrefix` split) with a standard `Call` feeding into the existing passes-2+ tool loop; trace label "routing grammar" → "pass 1" |
| `cmd/iq/perf.go` | `runToolBench`: replace `CallWithGrammar` + `ParseRoutingPrefix` detection with `Call` + `ParseCalls`/`ParseRoutingPrefix` fallback; `routeCorrect` still measures whether the right tool was called |
| `internal/sidecar/transport.go` | Remove `CallWithGrammar`, `RouteGrammar` type, and `RoutingGrammar *RouteGrammar` field from `ChatRequest` |
| `internal/sidecar/infer_server.py` | Remove `RoutingGrammarProcessor` class and all `routing_grammar` parameter handling |
| `internal/tools/tools.go` | Rename `BuildRoutingPrompt` → `BuildToolPrompt`; drop grammar-paired constraints ("first output MUST be", "no text before"); keep `<tool:NAME>` as the instructed call format and `<no_tool>` as the no-tool option; remove `RegistryNames` (only caller was the grammar) |
| `internal/tools/tools_test.go` | Update `BuildRoutingPrompt` → `BuildToolPrompt` references; remove `TestToolRegistryNames` |
| `cmd/iq/prompt_test.go` | Update any tests that exercise the grammar path |
| `arch.md` | Update Step 2 ROUTE description, Step 5 INFERENCE LOOP, debug trace format, source file table |

## Scope

### In scope
- Remove `RouteGrammar`, `CallWithGrammar`, and `RoutingGrammar` field from `ChatRequest`
- Remove `RoutingGrammarProcessor` and all `routing_grammar` handling from `infer_server.py`
- Collapse grammar path in `prompt.go` into the unified tool loop
- Update `perf.go` tool bench to use model-driven dispatch
- Rename `BuildRoutingPrompt` → `BuildToolPrompt`; drop grammar-paired constraints; keep `<tool:NAME>` format
- Remove `RegistryNames` and its test
- All tests updated

### Out of scope
- `ParseRoutingPrefix` and `ParseRoutingArgs` — kept as fallback parsers in the tool loop
  (models may emit `<tool:NAME>` format; parsers are tested and correct)
- Embed short-circuit path — unchanged
- Tool definitions, parameters, or the 8 available tools
- A3 (context budget management)

## New tool loop shape

**Before:** Two phases — a constrained pass 1 forcing `<tool:NAME>`/`<no_tool>` via grammar,
then passes 2+ parsing `<tool_call>` blocks.

**After:** One unified loop for all non-embed tool paths:

1. `Call(messages)` — model responds organically with tool prompt in system message
2. Try `ParseCalls` for `<tool_call>` blocks; fallback `ParseRoutingPrefix` for `<tool:NAME>`
3. If tool found: execute → inject result → loop (up to 5 iterations)
4. If no tool found: print response directly

## `BuildToolPrompt` format

Drops the grammar-paired constraints. The `<tool:NAME>` call format and `<no_tool>` option
are kept — `ParseRoutingPrefix` already handles both. Only the "MUST" / ordering language
(which was enforced by the logits processor, not the model) is removed.

**Old (grammar-paired):**
```
Your first output MUST be one of:
  <tool:TOOL_NAME>  — to call a tool, followed by JSON arguments
  <no_tool>         — to respond without using a tool, followed by your answer

Use <no_tool> ONLY for questions that no tool can answer (general knowledge, explanations, etc.).
Do not produce any text before the routing prefix.
```

**New (model-driven):**
```
When a question can be answered by a tool, call it using:
  <tool:TOOL_NAME>  followed by JSON arguments

If no tool is needed, answer directly or emit <no_tool> followed by your answer.
```

## Acceptance tests

1. **Clean build**: `./build.sh` passes with no vet, staticcheck, or test failures.
2. **File path trigger** (`iq ask "read ./go.mod"`): tools enabled via `hasFilePath`; model
   responds with a tool call or direct answer; no `routing_grammar` field sent to sidecar;
   tool executes correctly if called.
3. **Forced tools** (`iq ask -T "what time is it"`): `get_time` executes; no grammar used.
4. **No tool needed** (`iq ask -T "explain recursion"`): model answers directly; no tool call
   emitted or executed; response printed cleanly.
5. **Multi-turn tool chain** (passes 2+): a prompt requiring two sequential tool calls
   completes without error.
6. **`iq perf bench --type tool`**: runs without error; routing accuracy and exec OK metrics
   still reported.
7. No references to `RouteGrammar`, `CallWithGrammar`, `RoutingGrammarProcessor`,
   `BuildRoutingPrompt`, or `RegistryNames` remain in non-migration code.
8. `infer_server.py` no longer contains `RoutingGrammarProcessor` or `routing_grammar`
   parameter handling.
