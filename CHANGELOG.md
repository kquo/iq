# Changelog

| Version | Summary |
|---|---|
| Unreleased | |
| 0.15.0 | AC1: adopt governa v0.97.2 governance template |
| 0.14.0 | Phase 2 — new `kb` binary (v0.1.0): `kb ingest/list/search/rm/clear/ask/start/stop/restart/status/config`; `kb <query>` synonymous with `kb ask <query>`; KB index at `~/.config/kb/kb.json` (separate from iq's); `config.DirFor`/`LoadAt`/`SaveAt` added; `kb.PathFor`/`LoadFrom`/`SaveTo`/`IngestInto` added |
| 0.13.0 | Phase 1 — extract `cmd/lm/`: `lm.go`+`perf.go`+`bench_corpus.yaml` move to new `lm` binary (v0.1.0); `lm search/get/list/show/rm` and `lm perf bench/sweep/show/clear` are now top-level `lm` commands; `iq lm` and `iq perf` removed from `iq`; `lm` installed alongside `iq` by `build.sh` |
| 0.12.0 | A3 — context budget management: `context_window` field on `ModelEntry`; chars/4 token estimation; trim KB chunks then session turns to fit `context_window − max_tokens` budget; gray warning on trim; Step 4 trace shows `est_tokens`/`budget`/`trimmed`; `iq cfg show` displays `context_window` per model |
| 0.11.1 | A2 — drop routing grammar harness: remove `CallWithGrammar`, `RouteGrammar`, `RoutingGrammarProcessor`; rename `BuildRoutingPrompt`→`BuildToolPrompt`; remove `RegistryNames`; collapse grammar path into unified model-driven tool loop; fix root help stale `tier`/`--tier` references |
| 0.11.0 | A1B — schema v2: flat `models:` list replaces `tiers:` map; `ModelEntry` with per-model param overrides; `ConfigVersion = 2`; `migrateV1` converts v1 tiers to flat list preserving param overrides; `iq pool list/add/rm` replaces `iq tier show/add/rm`; `iq lm get` prints `iq pool add`; `SuggestTier` → `SuggestSize` (returns "small"/"large"); `--tier` flag → `--model` flag on `iq ask`/`iq pry`; TIER column removed from `iq st`; sweep no longer needs `--tier` flag |
| 0.10.0 | Design pivot (A1): retire `pipeline:` config field and two_tier/single_pool routing modes; flat model pool is now the only inference path; `resolveRoute` replaces `resolveSinglePool`+`resolveRoute`; `TierSource` = "pool"; `iq restart` command added; `iq stop` works with no models assigned; `pipeline:` silently ignored on load; `docs/design-pivot-01.md` added |
| 0.9.4 | `internal/color`: all funcs accept `any` via fmt.Sprint; add GrnR (reverse-video green), Blu, Cya; color test coverage 77→81% |
| 0.9.3 | Test coverage: cmd/iq (9→11%), kb (3→21%), lm (36→53%), sidecar +CallWithGrammar; pure-function tests across cmd/iq and internal/*; build.sh coverage display: domain (internal/*) as primary signal with total in parentheses |
| 0.9.2 | FEAT9850: context.Context threaded through hot-path pipeline (executePrompt, sidecar transport, embed HTTP, kb.Search); sync.WaitGroup replaced with errgroup in HFEnrichModels/HFFetchTags; signal.NotifyContext(SIGINT) at iq ask and root command entry points |
| 0.9.1 | Test coverage: cache (0→86%), color (0→77%), cue (0→78%), lm (0→37%); cache save() errors made explicit with _ =; arch.md v0.9.0 row added |
| 0.9.0 | Housekeeping: semver discipline adopted (PATCH/MINOR rules); plan.md consolidated to 4 groups (A=Pipeline, B=Knowledge & Context, C=Capabilities & Integration, D=Platform & Observability); CONTRIBUTING.md and CLA.md merged |
| 0.8.19 | `pipeline: single_pool` mode (FEAT9810): `PipelineSinglePool` constant; `pickAnySidecar` picks first live non-embed sidecar; `resolveSinglePool` routes with cue system prompt but no tier discrimination; pipeline guard replaced with switch; 2 new tests |
| 0.8.18 | Config schema versioning (FEAT9840): `version:` field in config.yaml; `ConfigVersion = 1`; version-dispatched `Load` (v0 → migration chain, v>current → error); `migrateV0` extracts all legacy migration logic; `normalizeConfig` helper; 3 new schema version tests |
| 0.8.17 | Stale sidecar state / port exhaustion (FEAT9860): `NextAvailablePort` uses `AllLiveStates` (skips dead PIDs); `StartInfer` and `StartSidecar` remove state + kill process on early crash and readiness timeout; 3 new port-allocation tests |
| 0.8.16 | `RawCall` timeout + status-code guard (FEAT9870): swap bare `http.Post` for `inferClient` (`http.Client{Timeout: 5m}`); explicit non-200 error with status code and body; `Stream` unchanged (timeout would cancel mid-stream); `TestRawCallNonOK` added |
| 0.8.15 | Synthesis pass for `read_file` short-circuit: after file is read, model answers original question using content (same pattern as `web_search`); fixes "does arch.md have version history?", "print last 10 lines", etc.; other file tools remain self-contained (FEAT9872) |
| 0.8.14 | `GuardArgs` for `read_file`/`file_info`: extract filename from natural language via `extractFilePath`/`looksLikeFilePath`; handles "tail arch.md", "print main.go", "does file X have…"; nil on no-match → falls back to inference; 9 new unit tests (FEAT9874) |
| 0.8.13 | Fix embed short-circuit tool selection: `SelectTool(signal, input)` replaces hard-coded `expected[0]`; keyword dispatch routes `file_access` to `list_dir`/`file_info`/`read_file` and `file_search` to `count_lines`/`search_text`; `GuardArgs` for `list_dir` extracts path or defaults to "."; 15 new unit tests (FEAT9875) |
| 0.8.12 | Fix `RemoveSource` prefix collision: replace bare `strings.HasPrefix` with exact-path + directory-boundary match (`== absPath \|\| HasPrefix(absPath+"/")`) to prevent silent deletion of sibling paths; add `internal/kb/kb_test.go` with 4 table tests (FEAT9880) |
| 0.8.11 | Test coverage expansion: `ParseCallsStrict` table tests, `resolveRoute` tier fallback, sidecar transport (`RawCall`/`Call`/`StripThinkBlocks`) with httptest, config migration paths (`migrateFlatTiers`/`migrateOldFourTier`/legacy embed model), embed (`CosineSimilarity`/`TextsOnPort`/`keywordScore`); aggregate coverage 11.8%→15.1% (FEAT9890) |
| 0.8.10 | Replace `queone/utl` color wrappers with zero-dependency `internal/color` package; TTY detection via `os.Stdout.Stat()`, respects `NO_COLOR`/`TERM=dumb`; rename `Gre`→`Grn`; binary shrinks ~110KB; removes 7 transitive dependencies (FEAT9900) |
| 0.8.9 | Error handling audit: config parse errors now surfaced (was silently swallowed); DDG body leak fixed; `web_search` fallback parser extracts `query` arg; `BuildRoutingPrompt`/`RegistryNames`/`ParseCalls`/`ParseCallsStrict` accept explicit `[]Tool` registry; `build.sh` suppresses PASS lines, adds weighted coverage summary and `-v` flag (FEAT9910) |
| 0.8.8 | Extended inference parameters: `top_p`, `min_p`, `top_k`, `stop`, `seed` added to `InferParams`/`ResolvedParams`; threaded through `ChatRequest`, `Call`/`CallWithGrammar`/`Stream`, and `infer_server.py`; nil = unset (mlx_lm defaults); stop sequences trim post-generation; `iq config` surfaces all 8 params with mlx_lm defaults annotated (FEAT9920) |
| 0.8.7 | `TestHelpFlagCoverage` uses `VisitAll` to assert every registered flag appears in hand-crafted help output; fixed genuine drift: `--dump-prompt` missing from `iq ask -h`, `--kb` missing from `iq pry -h`; build.sh highlights FAIL lines in red (FEAT9930) |
| 0.8.6 | Advisory `flock` locking for session reads/writes (`syscall.Flock`); shared lock on load, exclusive on save; `.lock` sidecar file per session; concurrent-write unit test (FEAT9940) |
| 0.8.5 | `pipeline:` mode selector in config.yaml (`two_tier` default); single `config.Load` at top of `executePrompt` replaces 4 internal loads; pipeline validation at entry; `iq config` shows effective pipeline mode (FEAT9950) |
| 0.8.4 | `NewRegistry()` constructor replaces `init()` for tools package; `Registry` global initialized via constructor; unit test for tool count, names, and instance isolation (FEAT9960) |
| 0.8.3 | Idiomatic Go naming: `program_name`/`program_version` → `programName`/`programVersion` (FEAT9980); `errSilent` sentinel replaced with named `silentErr` type so `errors.Is` works correctly (FEAT9970) |
| 0.8.2 | `kb_min_score` configurable in config.yaml; STEP 3 trace shows threshold and zero-result message; `traceBlock` truncates KB chunk content to 4-line preview; trace mode forces non-streaming to fix debug output corruption; fix double-print of pre-think tokens in streaming think models |
| 0.8.1 | Embed short-circuit generalized to all tool signals (FEAT9990): skip routing grammar pass when embed is confident; `extractCalcExpression` converts natural language to math for calc tool; fallback strips tool system prompt to prevent re-invocation markup leak |
| 0.8.0 | `iq perf bench` auto-starts/stops sidecar for infer/tool types; red download hint for missing models in bench and sweep (`lm.IsModelNotDownloaded`) |
| 0.7.9 | Per-tier tuning guide in arch.md; `short\|long` alias format in help; trim trailing blank lines from all help output; README "Find Your Best Models" section |
| 0.7.8 | Async KB prefetch with 5s timeout; `iq perf sweep` automates model comparison; README onboarding guide |
| 0.7.7 | `iq config show/validate`: canonical config inspection and validation command |
| 0.7.6 | Hybrid cue classification: keyword boost prevents embedding drift; strict tool schema validation (ValidateCall/ParseCallsStrict); multi-model benchmark harness (--models flag) |
| 0.7.5 | Concurrency-safe `search.Client` struct replaces package-level braveAPIKey; tool execution safety: 30s timeout, 32KB output cap, ReadOnly/confirm gating |
| 0.7.4 | Extract sidecar HTTP transport to `internal/sidecar/transport.go`; extract LM domain logic (~500 lines) to `internal/lm/lm.go`; `cmd/iq/lm.go` becomes thin CLI shim |
| 0.7.3 | `--dump-prompt` flag writes assembled message array as JSON before inference; end-to-end orchestration test with mock sidecar (httptest); build.sh v2.2.0: indented output, `-v` tests, green command echo |
| 0.7.2 | Housekeeping: rename search.SearchParam/SearchResult → Param/Result; unify chatMessage/cache.Message into config.Message; broaden MlxVenvPython fallback paths (PIPX_HOME, /opt/homebrew/bin); config.Load resilient to read-only filesystems |
| 0.7.1 | Web search hardening: rate limiter, pinned CSS selectors with fixture test, Brave Search API fallback; config.yaml populates all defaults on first creation |
| 0.7.0 | Configurable inference parameters: per-tier and global `repetition_penalty`, `temperature`, `max_tokens`; structured `TierConfig` with auto-migration from flat-list format; temperature support in `infer_server.py` |
| 0.6.15 | Add test assertion for tool/signal registry coverage drift |
| 0.6.14 | Replace 24-bit timestamp session IDs with 32-bit crypto/rand |
| 0.6.13 | Python sidecar dev hot-reload from `~/.config/iq/`; fix `~` not expanded in PATH for `mlx_lm.server` lookup; move tools tests to `internal/tools` |
| 0.6.12 | Update arch.md section headers and file paths; minor build.sh adjustment |
| 0.6.11 | Extract `kb` to `internal/kb` domain package — completes `internal/` restructuring |
| 0.6.10 | Extract `tools` to `internal/tools` domain package |
| 0.6.9 | Extract `cache` to `internal/cache` domain package |
| 0.6.8 | Extract `embed` to `internal/embed` domain package |
| 0.6.7 | Extract `cue` to `internal/cue` domain package |
| 0.6.6 | Extract `sidecar` to `internal/sidecar` domain package |
| 0.6.5 | Extract `search` to `internal/search` domain package |
| 0.6.4 | Begin `internal/` restructuring — extract `config` as first domain package; planned: search, sidecar, embed, cache, tools, kb |
| 0.6.3 | Web search tool: DuckDuckGo integration via `web_search` tool and embed signal; short-circuit skips routing grammar for web queries; synthesis prompt with date injection; toolMinScore 0.66→0.60 |
| 0.6.2 | Tool use benchmark (`iq perf bench --type tool`): 14 prompts across 7 tools, measures routing accuracy and execution success; `-v` flag for per-prompt debug detail |
| 0.6.1 | Robust tool arg parsing (broken JSON, unquoted keys, `=` separators, `--flag=value` CLI format); print successful tool output directly instead of pass 2 re-inference; inject cwd into tool system prompt; PASS/GUARD/latency debug trace format; parse `<tool:NAME>` routing prefix on follow-up passes |
| 0.6.0 | TASK label `feature-extraction` displayed as `embedding` (green); `lm rm` auto-stops sidecars and clears tier/embed assignments with yellow warnings instead of blocking; yellow confirmation prompt; README documents HF as official registry with token recommendation |
| 0.5.11 | Flatten CLI: promote `iq svc` subcommands to root (`iq start/stop/status/doc/tier/embed`); `iq svc` kept as hidden backward-compat alias |
| 0.5.10 | Display raw HF pipeline_tag (lowercase with hyphens); local task inference from config.json as fallback when HF returns no tag (checks vision indicators before model_type) |
| 0.5.9 | Model task display: show HF pipeline_tag (TASK column) in lm search/list/show with green/red color coding; warn on non-text-generation downloads; cache task in manifest with parallel backfill |
| 0.5.8 | VLM guard: reject vision-language models at svc start (checks config.json for vision indicators); early crash detection via cmd.Wait() goroutine replaces zombie-prone signal-0 check for immediate failure reporting |
| 0.5.7 | Routing grammar: replace mlx_lm.server with custom infer_server.py sidecar supporting constrained decoding via logits processors; routing grammar forces `<tool:NAME>` or `<no_tool>` prefix on pass 1; tool guard direct-calls tool when model picks `<no_tool>` despite embed signal; toolMinScore 0.72→0.66 |
| 0.5.6 | Move Step 1b before Step 2; tool guard reprompt on pass-1 simulation; disable cache when tools enabled; document tool execution model in arch.md |
| 0.5.5 | Arg validation UX: yellow error + command help on wrong args |
| 0.5.4 | Tune KB and tool thresholds: kbMinScore 0.50→0.72, kbDefaultK 5→3, toolMinScore 0.50→0.72; use kbDefaultK constant in all call sites; instruct model to use tool results on follow-up pass |
| 0.5.3 | Response cache (Steps 4b/5b): FNV64a-keyed response cache with 1h TTL, --no-cache flag; rename Step 4→ASSEMBLE, Step 5→INFERENCE LOOP; capitalize all step names; add pass numbers to tool loop trace; add call trace for non-tool path |
| 0.5.2 | Fix `iq pry` to resolve embed sidecar by model ID; reject embed models with clear error instead of 404 |
| 0.5.1 | Architecture docs rewritten: add tool system, perf/bench, debug trace format, embed sidecar details, hybrid KB scoring, structure-aware chunking, source file map; fix diagram and data flow |
| 0.5.0 | Embed-based tool detection replaces keyword lists; reuse input vector from classify step (zero extra API calls); new debug trace format with step headers, call/task sub-fields, and Step 1b tool detect |
| 0.4.8 | Consolidate 58→17 cues across 8 categories; keyword-rich descriptions for embedding separation; lower classifyMinScore 0.68→0.40; bench accuracy 29%→100% (28/28); print threshold in bench output |
| 0.4.7 | Root-level prompts (`iq "message"`); `-?` help alias; extract `addPromptFlags` helper |
| 0.4.6 | Skip embed sidecar start when model not downloaded (immediate hint); print last log lines on embed sidecar timeout |
| 0.4.5 | First-run hint for `iq svc start` when no tier models configured; update Quick Start with recommended defaults |
| 0.4.4 | Merge dual embed sidecars into single `embed` sidecar on :27000; default to bge-small-en-v1.5-bf16; auto-migrate cue_model/kb_model → embed_model |
| 0.4.3 | Rename `iq probe` → `iq pry` (probe kept as alias) |
| 0.4.2 | Rename `iq prompt` → `iq ask` (prompt kept as alias); add pre-commit checklist to CLAUDE.md |
| 0.4.1 | fix: version bump, remove Ollama from docs, fix diagram alignment |
| 0.4.0 | Replace Ollama with local MLX embed sidecars (embed_server.py, cue :27000 / kb :27001); fix mxbai int attention-mask via _construct_batch patch; mlx-lm decoder fallback for Qwen3-Embedding; registerInManifest for embed models; embed model guard in lm rm; build.sh auto-commit/tag/push; cue classifier confidence threshold (0.68); KB RAG uses cue system prompt instead of hardcoded reading-comprehension template; architecture docs purged of Ollama references |
| 0.3.1 | MLX embed sidecars, dual embed roles (cue/kb), hybrid KB retrieval, RAG quality improvements |
| 0.3.0 | RAG knowledge base (iq kb), KB retrieval in prompt and probe |
| 0.2.10 | switch embed library to mlx-embedding-models, fix BertTokenizer compat |
| 0.2.9 | embedding-based classification, normalise suggested_tier values |
| 0.2.8 | rename role→cue, add initial fallback cue, probe --cue flag |
| 0.2.7 | Initial public release |
