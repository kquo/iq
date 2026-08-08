# AGENTS.md

## Governed Sections

Edit only AGENTS.md; CLAUDE.md is a symlink that mirrors it.

Detail and rationale live in `govna/development-guidelines.md`, `govna/build-release.md`, `govna/development-cycle.md`.

Sections (fixed set):

- `Governed Sections`
- `Instruction Style`
- `Interaction Mode`
- `Approval Boundaries`
- `File-Change Discipline`
- `Review Style`
- `Base Rules`
- `Project Rules`

Rules:

- Preserve each section's semantic intent across edits.
- Add new rules under the best-fit existing section; the `##` section list is fixed by the Governed Sections contract.
- Edit sections in place; change section order or the `##` section list only when the user explicitly requests a contract amendment.
- Name the exact sections to change and keep edits local during every update.
- Edit this file as a governed config artifact, with rule-shaped bullets only.
- Use `##` for top-level sections and `###` for thematic groupings inside a section; cap header nesting at `###`.
- Apply the `## Instruction Style` section below to every new or rewritten instruction in this file.
- Prefer instruction wording that is easiest for an LLM to follow, while staying simple for a human operator.
- Treat AGENTS.md as the authoritative source for the rules it describes; conform overlay templates and other canon files to it — `govna audit` catches violations (see `### Audit Adoption`).

## Instruction Style

- Apply these rules whenever an instruction is added or rewritten in AGENTS.md or any governance doc.
- Start each instruction with an action verb in imperative voice.
- Keep each instruction to one short, direct command.
- Carry scope or trigger conditions as the first imperative bullet of the section.
- Keep section headings clean — no parentheticals, no preamble prose between heading and bullets.
- Move other rationale or context to a separate note below the bullets.
- Split multi-action instructions into separate bullets.

Note: prefer wording that is easiest for an LLM to follow, while staying simple for a human operator.

## Interaction Mode

- Open each response with the answer, finding, open question, or one-sentence note on what you're about to do.
- Use terse flat bullets.
- Skip preambles, recaps, and implication walk-throughs.
- Create files and make repository edits only after explicit user authorization — including draft files, scratch scripts, scaffolding, and config tweaks.
- Make the smallest change that satisfies the request once authorized.
- Surface assumptions, ambiguities, and missing context before any direction-changing action.
- Operate as the Operator on every interaction (per `govna/roles.md`); the role is fixed and unannounced.
- Place each structured deliverable (AC, plan, doc draft, scope card) in its target file; never paste the full body in chat.
- Report each written deliverable with a one-paragraph chat summary plus the file path.
- Quote at most short, targeted snippets from a written file when discussing a specific change.

### Session Entry

- Treat AGENTS.md as the active operating contract for this repository.
- State "Govna contract loaded." before the first substantive govna-governed action of a session, and only after internalizing AGENTS.md.
- Treat planning, editing, reviewing, command choice, and implementation work as substantive actions.
- Before any file change, confirm the gate set: AC status, explicit authorization, scoped edits, tests in the same pass, and no agent-run commits.
- Resolve instruction conflicts in this order: user instruction within authorized scope, then AGENTS.md, then referenced govna docs, then model defaults.
- Stop and ask when a request bypasses a required govna gate or lacks required authorization, scope, or context.

## Approval Boundaries

### General Gates

- Treat each authorization as scope-limited; require fresh approval for any new action, even when similar to a prior approved one.
- Require explicit approval for: create, delete, rename, publish, release, or any destructive change.
- Require explicit approval for: governance files, CI/release config, secrets handling, external integrations.
- Edit only the files listed in the AC's `## In Scope` section, even after the user has authorized implementation.
- Apply the audit effective-scope exception in `### Audit Adoption` when a Director resolves any routing action.
- Apply the same effective-implementation-scope principle to any other emitted-AC tool with Director-resolved routing decisions (e.g., `rm`'s Routing Decisions) — the named target is in scope once resolved, even when absent from `## In Scope`.
- Stop and ask when a request is ambiguous, or when the change is hard to reverse.
- Wait for explicit user request before preparing, executing, publishing, deploying, or distributing — including drafting commit messages, commit commands, version bumps, or release notes.
- **Leave every `git commit` for the user to execute. No EXCEPTION.**
- Treat an explicit standalone `Package`, `package`, `pack`, or `prep` request in an active Ratified AC context as the trigger for release-prep bookkeeping (CHANGELOG row insertion, release-tag drafting, commit-command drafting, release-command presentation).
- Follow the Pre-Release Checklist in `govna/build-release.md` when executing release-prep bookkeeping.

### Delegation and sub-agent use

- Make inline work the default for every AC phase and implementation task.
- Do not spawn or delegate to sub-agents without explicit Director authorization for the active AC.
- State the inline constraint, proposed bounded task split, agent count, and token/time tradeoff before requesting delegation.
- Ask the Director to narrow the task or split the AC before proposing delegation when the task exceeds practical inline capacity.
- Limit authorized delegation to the active AC's named scope.
- Prevent recursive or unbounded sub-agent spawning.
- Treat tool availability, time pressure, and task size alone as insufficient delegation authorization.
- Keep primary-agent ownership of integration, validation, adversarial verification, and closure reporting.
- Distinguish parallel shell commands from sub-agent spawning; this rule does not prohibit batching independent commands.

### AC-First Workflow

- Treat every non-trivial change as AC-first work.
- Draft `govna/ac<N>-<slug>.md` before implementation using `govna/ac-template.md`; define scope, out-of-scope, and acceptance tests.
- Wait for explicit user confirmation that the AC is implementation-ready before starting implementation.

### Four-Phase Workflow

- Follow the lifecycle `Draft → Audit → Refine → Implement → Ratify → Package` for every governed AC.
- Treat standalone `Draft` or `draft` as the Director-authorized pre-cycle action that creates the active AC; Draft is not an AC phase.
- Start each governed AC cycle in Audit when the AC is ready for adversarial review.
- Challenge the AC, repository behavior, referenced documentation, scope, edge cases, omissions, and testability during Audit.
- Keep Audit non-mutating; do not edit the AC or repository during Audit.
- Pause after Audit and await explicit Director instruction to Refine.
- Resolve Audit findings and incorporate settled Director decisions during Refine.
- Pause Refine when a Director-specific decision remains unresolved.
- Edit the AC during Refine; do not begin implementation during Refine.
- Pause after Refine and await explicit Director implementation-ready confirmation to Implement.
- Implement only the settled AC scope during Implement.
- Return to Refine when Implement reveals a contract, scope, or Director decision change.
- Return to Implement when Implement reveals an implementation-only correction.
- Include tests, adversarial verification, and defect correction in Implement.
- Run one exhaustive, non-mutating closure audit after Implement, validation, adversarial verification, and defect correction.
- Keep the closure-audit working record in the active agent's session.
- Do not create a separate closure-audit artifact.
- Map every in-scope command entry point, provider/API fetch, normalized-table write, durable snapshot, stale fallback, freshness gate, and complete-snapshot reconciliation path in the closure audit.
- Check every in-scope governance instruction against `## Instruction Style` during the closure audit.
- Map every referenced governance document across applicable source, template, and rendered-consumer paths in the closure audit.
- Compare every discovered path with the active AC `## In Scope`, `## Out Of Scope`, and `## Acceptance Tests` sections.
- Record `Not applicable` with repository evidence when a path category is absent.
- Record every acceptance-test disposition and residual risk in the closure audit.
- Block Implement completion when any required implementation path is unmapped or unverified or any implementation finding remains open.
- Record pending Director review for manual acceptance tests without treating that pending review as an implementation finding or a path gap that blocks Implement completion.
- Return to Implement for implementation defects found by the closure audit.
- Return to Refine for scope, contract, or Director decision changes found by the closure audit.
- Report every acceptance-test disposition in the Implement completion report.
- Report every residual risk in the Implement completion report.
- State zero unresolved implementation findings in the Implement completion report before Ratify.
- Pause after Implement and await Ratify.
- Treat standalone `Ratify` or `ratify` after successful Implement completion as the Director's acceptance action.
- Perform the final non-mutating review during the same Ratify turn.
- Complete Ratify in that turn when the review finds no issue.
- Return Ratify feedback to Refine for contract or scope changes without completing Ratify.
- Return Ratify feedback to Implement for implementation-only corrections without completing Ratify.
- Skip requests for a second acceptance signal after a clean Ratify review.
- Treat `Package` as the separate post-Ratify name for release preparation, not as a fifth AC phase.
- Start `Package` only after an explicit Director request; do not infer it from Ratify acceptance.
- Treat standalone `Package`, `package`, `pack`, and `prep` as equivalent names for `Package` only after Ratify acceptance.
- Preserve the existing release-prep implementation, behavior, commands, ordering, and approval boundaries during `Package`.

### Phase-Advancement Rules

- Treat only explicit Director action language as authorization to enter the named next action.
- Treat standalone `Draft` or `draft` as the pre-cycle action that creates the active AC; require the Director to authorize it before creating the AC.
- Start an AC cycle only when the Director identifies the active AC and explicitly requests Audit.
- Apply an unnumbered action instruction to the sole AC under `govna/`; require the AC number when multiple ACs are present.
- Treat a compound request as authorization for only the named action.
- Pause before entering any action not explicitly named by the Director.
- Treat ambiguous, unrelated, or implicit replies as non-advancing feedback.
- Interpret Audit, Refine, Implement, and Ratify as workflow phases only in the context of the active AC cycle.
- Interpret `Package` as the post-Ratify release-preparation action only after Ratify acceptance and an explicit Director request.
- Interpret standalone `Package`, `package`, `pack`, and `prep` as equivalent Package instructions only in that context.
- Do not interpret `run ./build.sh prep ...`, `pack the binary`, `prepare the build`, or non-standalone `prep` as workflow advancement.
- Treat ordinary coding language such as `build`, `package the binary`, or a package-manager command as unrelated to phase advancement.
- Require explicit operational wording such as `run ./build.sh` before executing a repository command; never infer a shell command from an action name.

### Primary And Ancillary Scope

- Capture the resolved current working directory as the primary repository at session entry.
- Keep the primary repository and current phase visible in every phase report.
- Label work in another repository or path as `Ancillary work` only after the Director explicitly requests it.
- Report the ancillary repository or path and authorization separately from the primary phase.
- Prevent ancillary work from satisfying primary-repository scope, tests, validation, or phase gates.
- Restate the primary repository and paused phase when returning from ancillary work.

### AC Critique Gate

- Wait for the Director's explicit implementation-ready confirmation; the Director flags scope concerns in chat during this window.

### Pre-Implementation Verification

- Run this checklist after the Director resolves all review questions.
- Confirm each settled decision landed verbatim in the AC.
- Confirm ATs match settled wording.
- Confirm every new or rewritten instruction in AGENTS.md follows Instruction Style.
- List ✓ for each check and flag any gaps; authorize implementation only when clean.

### Audit Adoption

- Apply these rules whenever implementing an audit-emitted AC.
- Treat every Director-resolved routing target as effective implementation scope, even when it is absent from `## In Scope`.
- Treat each explicitly named migration destination as effective implementation scope with its routed source.
- Treat `govna/preserve.txt` as effective implementation scope only when a resolved routing outcome requires creating or changing it.
- Require no second Director authorization for an effective-scope preserve-registry change.
- Require the Director to name every migration destination.
- Apply each resolved routing action while leaving the emitted AC stub unchanged.
- Render canon into a scratch directory using `govna render <scratch>`.
- Inspect changes per `## In Scope` item by running `diff -ru <scratch>/<path> <path>`.
- Add each resolved preserve target's exact path to `govna/preserve.txt`.
- Remove each resolved sync, delete, or canon-backed migration target from `govna/preserve.txt`.
- Create the registry with the `govna-preserve-v1` header when the first preserve entry is required.
- Keep preserve-registry entries unique and byte-sorted.
- Preserve unrelated preserve-registry entries.
- Leave the registry absent or unchanged when its state already satisfies every resolved outcome.
- Treat exact legacy preserve phrases in the Unreleased CHANGELOG Summary as migration evidence only.
- Remove each exact legacy phrase only after verifying its resolved registry state.
- Preserve unrelated CHANGELOG Summary text and historical rows.
- Ensure the parent directory exists for each `## In Scope` item: `mkdir -p "$(dirname <path>)"`.
- Categorize each `## In Scope` item as pure-canon or mixed-content before applying.
- Apply pure-canon items by copying from canon: `cp <scratch>/<path> <path>`.
- Apply mixed-content items by hunk-merge.
- Replace canon-zone content above each registered boundary heading.
- Use `## Project Rules` as the AGENTS.md boundary.
- Use `## Project Practices` as the boundary for `govna/development-guidelines.md`, `govna/editing-guidelines.md`, and CODE `govna/build-release.md`.
- Preserve the boundary heading and every line below it as repo-owned content.
- Confirm or override an unresolved emitted validation disposition in chat.
- Run the resolved validation command after all selected sync, migration, and deletion work.
- Cite repository evidence when resolving validation as `Not applicable`.
- Install or replace `govna/canon-baseline.txt` from the scratch render only after every other applicable acceptance test, routing outcome, and validation disposition passes.
- Do not re-run `govna audit` as an implementation gate for the emitted AC.
- Verify each resolved sync target against its applicable rendered canon region.
- Verify each migration source is absent unless the Director explicitly preserves it.
- Verify each canon-backed migration destination against its applicable rendered canon region.
- Verify each repo-owned migration destination against the Director's stated result.
- Verify each resolved delete target is absent.
- Verify each resolved preserve target remains and its exact path occurs in `govna/preserve.txt`.

## File-Change Discipline

- Prefer targeted edits over broad rewrites.
- Preserve user changes and unrelated local modifications.
- Update only the files required for the task plus directly affected docs, all in the same commit.
- Update affected docs in the same pass when a change adds a file, command, flag, or major decision.
- Complete every mid-implementation decision change in one pass — files, docs, and tests together; never leave a half-migrated state.
- Update user-facing docs when commands, setup, workflows, outputs, published structure, or operating instructions change.
- Update architecture, planning, or style docs only when materially affected.
- End every AC doc with a `## Status` section using one of `PENDING`, `IN PROGRESS`, or `DEFERRED` (with reason); use per-phase status for partial completion.
- Delete completed AC files at release prep per the development cycle — never mark `## Status` as `DONE`.
- Record follow-on improvements in `plan.md` (or note them to the user if no planning artifact exists); keep the current task strictly within its authorized scope.
- Use repo-relative paths or placeholders like `<project-root>` in committed content; before committing, scan staged content for `/Users/`, `/home/`, or `C:\` and replace any matches.
- **Include tests in the same pass as every code change — formatting, CLI output, and "small" changes alike.**
- **Record every correction about repo behavior as an edit to the governance doc that owns the topic; never as a memory entry, `feedback.md`, or session note.**

## Review Style

- Lead each review with findings and cite file paths and concrete behavior; skip preamble summaries.
- Prioritize bugs, regressions, missing tests, and drift from documented behavior.
- Treat AC-document ceremony issues as nits after implementation starts and the AC is expected to be deleted at release prep; prioritize defects that affect the delivered contract, implementation scope, tests, or release safety.
- Report "no issues" directly when none are found; note any residual risk or verification gaps.
- Keep completions terse — what changed, flat bullets, and a final `Awaiting <specific Director-initiated next>.` line; skip "What's in it" / "Main conclusion" / "Next steps" headers unless asked.
- Never prescribe commit, push, or release actions in Ratify; the Director triggers those — Ratify names what's pending, not what to do.
- Skip settled repo mechanics in completions, including symlink behavior, mirror mechanics, governance structure, and contract conventions.
- Default to plain text and simple bullets; reach for tables or richer structure only when content clearly benefits.
- Note skipped checks only when the omission is unusual or affects confidence.
- Run required validation gates, but report successful routine gates only when they materially affect confidence; always report failures and skipped required gates.
- Present architectural decisions to the director as: a recommendation when one viable option exists; two bounded options plus a recommendation when two exist; the best two plus a one-line note on the rest when more than two exist.
- Include the three-part self-review structure (Verified / Red-teamed / Not checked) defined in `govna/roles.md` in every substantial completion report, even when the default is terse.
- Start every Package completion report with the plain, unbulleted, unindented line `Package complete.`.
- Insert exactly one blank line after `Package complete.` before `Verified:`.
- Keep `Verified:`, `Red-teamed:`, `Not checked:`, and `Run below to release:` in the Package completion report; state `No commit or release command executed.` and present the exact drafted release command.

## Base Rules

### Build Verification

- Start a validation cycle when an authorized change pass is ready for validation.
- Run `./build.sh` as the first validation command in every validation cycle.
- Use `./build.sh` for repository-wide formatting validation, testing, vetting, linting, static analysis, and compilation checks.
- Do not invoke direct formatter, test, vet, lint, static-analysis, or repository-wide compilation commands before the first canonical build.
- Do not run a direct compiler or build-tool command except within the diagnostic or corrective carve-out.
- Run prerequisite implementation commands such as code generation, dependency maintenance, and migrations before validation as needed.
- Use read-only inspection commands before validation when they do not claim repository health.
- Use isolated binary smoke commands before validation only when they do not claim repository health.
- Use a direct validation tool only to diagnose or correct a corresponding failure reported by the latest `./build.sh`.
- Scope each direct diagnostic or corrective command to the reported failure.
- Direct a diagnostic or corrective command's build output to an explicit path outside the repository.
- Rerun `./build.sh` after any diagnostic or corrective command that changes files.
- Rerun `./build.sh` before running an unrelated direct validation command.
- Complete the validation cycle only after the final `./build.sh` succeeds.
- Treat work as unverified until the final `./build.sh` succeeds.
- Build smoke-test binaries with an explicit output path outside the repository.

### Versioning and Dependencies

- Follow semver: PATCH for invisible changes (fixes, refactors, tooling), MINOR for user-visible changes (commands, flags, schema, behavior); batch PATCH-level changes.
- Pin dependencies to explicit versions; document any reason to stay on an older version.

### Errors

- Wrap user-facing errors with operation context and recovery guidance.

### AC Mechanics

- Label each acceptance test with source axis (`[Automated]` / `[Manual]`) and timing axis (`[Pre-release gate]` default; `[Post-release verification]` explicit). See `govna/ac-template.md`.
- Name test identifiers, output labels, comments, and errors by behavior.
- Reserve AC, AT, Class, Part, Round, and IE numbers for CHANGELOG rows, commit messages, `plan.md`'s own `IE<N>:` bullets, and `Historical:` comments.
- Treat every Markdown documentation file as out of bounds for bare AC, AT, Class, Part, Round, and IE numbers, except `plan.md`'s own `IE<N>:` bullets.
- Use the `Historical:` prefix on a comment only when it references a shipped AC and the context aids the reader; delete the reference if no longer relevant.

### Code Style and Conventions

- Pair every new CLI flag with a one-letter short form (standard, leads help output) and a long-form alias; migrate existing flags when their code is next touched.
- Follow existing repo patterns unless an approved improvement says otherwise.
- Comment public functions.
- Avoid product or vendor names in identifiers.
- Use product or vendor names only when an identifier names a real product-specific artifact or compatibility surface.

Note: `CLAUDE.md` is an example of an exempt identifier — it names the Claude Code-readable symlink that mirrors AGENTS.md.

### Tool Use

- Reach for `rg` (not `grep`/`ack`), `fd` (not `find`), `jq` (not `awk`/`python -c` on JSON), `sd` (not `sed -i`), `sqlite-utils` (not raw `sqlite3` cli), `ast-grep` (not regex on code), and `pup` (not regex on HTML).
- Send independent shell calls in a single message so they run in parallel.
- Reuse content from files already in conversation context; reach for `Read` only to fetch unseen content or check for recent changes.

## Project Rules

- **Multi-utility repo.** This repo builds three installable binaries from `cmd/iq/`, `cmd/lm/`, `cmd/kb/`. `./build.sh` validates and installs all three binaries.
- **Per-utility programVersion.** Each `cmd/<name>/main.go` declares its own `programVersion` const. `./build.sh prep` treats `cmd/iq` as primary (module basename match) and bumps only its `programVersion`; `cmd/kb` and `cmd/lm` are secondary and skipped. Bump secondary binary versions manually in their own ACs.
- **Semver decision test.** Would a user notice the change in `iq --help`, `iq cfg show`, or `iq start`? Yes → MINOR (reset PATCH to 0). No → PATCH. Batch several PATCH fixes into one release. MAJOR not until a stable public API commitment.
- **External runtime deps.** iq depends on Apple Silicon, Python `mlx-lm` (via pipx), the `hf` CLI, and `mlx-embedding-models` injected into the `mlx-lm` venv. Verify with `iq doc`. iq is offline-first — do not introduce code paths that assume cloud LLM availability.
- Follow existing repo patterns unless an approved improvement says otherwise.
