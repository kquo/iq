# AGENTS.md

## Governed Sections

- Edit governed sections only in AGENTS.md.

Note: CLAUDE.md is a symlink that mirrors AGENTS.md.

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
- Add new rules under the best-fit existing section.
- Preserve the fixed `##` section list.
- Edit sections in place.
- Change section order or the `##` section list only when the user explicitly requests a contract amendment.
- Name the exact sections to change during every update.
- Keep edits local during every update.
- Edit this file as a governed config artifact, with rule-shaped bullets only.
- Use `##` for top-level sections.
- Use `###` for thematic groupings inside a section.
- Cap header nesting at `###`.
- Apply the `## Instruction Style` section below to every new or rewritten instruction in this file.
- Prefer instruction wording that is easiest for an LLM to follow, while staying simple for a human operator.
- Treat AGENTS.md as the authoritative source for the rules it describes.
- Conform overlay templates and other canon files to AGENTS.md.

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
- Create files only after explicit user authorization — including draft files, scratch scripts, scaffolding, and config tweaks.
- Make repository edits only after explicit user authorization.
- Make the smallest change that satisfies the request once authorized.
- Surface assumptions, ambiguities, and missing context before any direction-changing action.
- Operate as the Operator on every interaction under `govna/roles.md`.
- Keep the Operator role fixed and unannounced.
- Place each structured deliverable (AC, plan, doc draft, scope card) in its target file.
- Never paste a structured deliverable's full body in chat.
- Report each written deliverable with a one-paragraph chat summary plus the file path.
- Quote at most short, targeted snippets from a written file when discussing a specific change.

### Plain Language

- Apply plain-language rules to responses, ACs, findings, completion reports, and release summaries.
- Lead with the concrete problem, effect, or decision in plain language.
- Pair each necessary Govna label with its plain-language meaning at first use.

### Session Entry

- Treat AGENTS.md as the active operating contract for this repository.
- State "Govna contract loaded." before the first substantive govna-governed action of a session, and only after internalizing AGENTS.md.
- Treat planning, editing, reviewing, command choice, and implementation work as substantive actions.
- Confirm the gate set before any primary-repository file change: AC status, explicit authorization, scoped edits, tests in the same pass, and no agent-run commits.
- Route ancillary-repository and path changes through `### Primary And Ancillary Scope`.
- Treat changed-content integrity, AC-template structure, Plain Language, Instruction Style, and applicable Pre-Implementation Verification as the tests-in-the-same-pass gate when a change pass creates or edits only an active AC document.
- Resolve instruction conflicts in this order: user instruction within authorized scope, then AGENTS.md, then referenced govna docs, then model defaults.
- Follow an explicit Director workflow override without requiring contract-amendment language.
- Stop when a request lacks authorization, scope, or required context.
- Ask for the missing authorization, scope, or context.

### Contract Integrity

- Apply contract-integrity reporting when governance instructions are contradictory, circular, unexecutable, repeatedly produce a workflow loop, or present demonstrated contract-growth evidence.
- Define a repeated workflow loop as the same conflict forcing at least two unnecessary phase returns, correction cycles, or Director round-trips.
- Report a directly demonstrated contradiction, circular dependency, or unexecutable instruction without waiting for repetition.
- Require repository evidence, an observed workflow consequence, or a directly demonstrable consequence for every finding.
- Avoid executing a broken or unsafe path solely to produce finding evidence.
- Exclude wording preferences, harmless redundancy, raw size, speculative maintainability concerns, speculative conflicts, and disagreement with a settled Director decision.
- Cite each source path, section heading, short targeted instruction snippet, and operational effect.
- State whether the finding blocks the active authorized work.
- Recommend one minimal correction when one viable correction exists.
- Present the best two bounded corrections and a recommendation when multiple viable corrections exist.
- Classify repository-specific tools, architecture, release, content, or operating-preference findings as `Consumer-local`.
- Route `Consumer-local` corrections to `## Project Rules` or the owning repo document.
- Classify shared phase, approval, scope, role, canon, or template findings as `Govna canon`.
- Route `Govna canon` corrections to the authoritative source and every applicable consumer path.
- State `Upstream Govna canon change required.` for every Govna-canon finding observed in a consumer repository.
- Cite the authoritative upstream section or document for every consumer-observed Govna-canon finding.
- Prohibit permanent local governance from recording an upstream canon correction.
- Pair a blocking `Govna canon` recommendation with a temporary consumer mitigation only when the mitigation remains compatible with canon.
- Mark every temporary consumer mitigation explicitly.
- State every temporary consumer mitigation's removal condition.
- Prohibit a temporary consumer mitigation from overriding or contradicting canon.
- Classify a finding as `Unclear` when repository evidence supports both destinations.
- Present both candidate destinations and defer an `Unclear` classification to the Director.
- Report a newly observed blocking finding before further direction-changing work.
- Stop when a finding prevents safe compliance or requires a Director-owned decision.
- Continue unaffected authorized work when a finding is non-blocking.
- Report a non-blocking finding in the next substantive response.
- Avoid repeating an unchanged finding after the Director acknowledges or defers it.
- Recheck only new or unresolved contract-integrity findings during Audit completion, Implement completion, closure audit, and Ratify.
- Recheck an acknowledged or deferred finding silently while its evidence, impact, classification, and recommended correction remain unchanged.
- Report an acknowledged or deferred finding again only when one of those elements changes.
- Apply the existing phase-routing rules when a finding affects the active AC.
- Keep an unauthorized finding in chat and the active session.
- Record an authorized correction in the governance document that owns the topic.
- Prevent the governance-record rule from bypassing authorization for a governance edit.
- Continue prohibiting memory entries, `feedback.md`, and session-note artifacts for repository-behavior corrections.
- Require explicit Director authorization before changing consumer-local governance or govna canon.
- Prevent contract-integrity reporting from authorizing a new AC phase, governance edit, delegation, commit, publication, or release action.

### Contract Growth

- Run a prospective contract-growth review during Audit or Refine when an active AC proposes governance instructions.
- Run a measurable contract-growth review after authorized governance edits and before Implement completion.
- Measure authorized AGENTS.md hunks against the phase-entry baseline while excluding unrelated working-tree changes.
- Report added, removed, and net AGENTS.md line and rule-shaped-bullet counts.
- Prevent contract-growth measurements alone from becoming findings.
- Check every new or rewritten AGENTS.md rule for overlap and placement.
- Keep shared triggers, authorization boundaries, safety constraints, and required outcomes in AGENTS.md.
- Place rationale, examples, and domain-specific procedure in an explicitly referenced owning governance document.
- Preserve short atomic imperative instructions as the default.
- Prohibit general compression through compound instructions.
- Prefer thematic `###` groupings and shared invariants over an unbounded flat instruction list.
- Merge or retire an overlapping instruction only within authorized scope and settled semantics.
- Report an out-of-scope or decision-bearing overlap without editing it.
- Return to Refine for an out-of-scope or decision-bearing overlap.
- Test representative triggers, authorization boundaries, allowed actions, exceptions, and exit conditions after governance restructuring.
- Record each governance-restructuring scenario input and expected outcome in the closure-audit record.
- Treat material duplication, misplaced procedural detail, excessive flat density with operational effect, or demonstrated retrieval or execution impairment as contract-growth evidence.

## Approval Boundaries

### General Gates

- Treat each authorization as scope-limited.
- Require fresh approval for every new action.
- Treat an explicit request to run govna audit as authorization for integrated audit adoption under ### Audit Adoption.
- Continue Director-authorized Implement authority only for an eligible bounded completeness correction under `### Four-Phase Workflow`.
- Require explicit approval for: create, delete, rename, publish, release, or any destructive change.
- Require explicit approval for: governance files, CI/release config, secrets handling, external integrations.
- Edit only the files listed in the AC's `## In Scope` section, even after the user has authorized implementation.
- Apply the effective-scope exception in `### Effective Implementation Scope` during Implement, closure-audit correction, and Ratify correction.
- Apply the audit effective-scope exception in `### Audit Adoption` when a Director resolves any routing action.
- Apply the same effective-implementation-scope principle to any other emitted-AC tool with Director-resolved routing decisions (e.g., `rm`'s Routing Decisions) — the named target is in scope once resolved, even when absent from `## In Scope`.
- Stop when a request is ambiguous or the change is hard to reverse.
- Ask for direction before proceeding.
- Wait for explicit user request before preparing, executing, publishing, deploying, or distributing — including drafting commit messages, commit commands, version bumps, or release notes.
- **Leave every `git commit` for the user to execute. No EXCEPTION.**
- Treat an explicit valid Package instruction for an established Ratified release batch as the trigger for release-prep bookkeeping.
- Follow the Pre-Release Checklist in `govna/build-release.md` when executing release-prep bookkeeping.

### Effective Implementation Scope

- Apply these rules during Implement, closure-audit correction, and Ratify's implementation-only correction loop.
- Change an omitted existing artifact only when an authorized in-scope change directly breaks it.
- Require the omitted artifact to fail compilation, execution, rendering, regeneration, settled-behavior verification, or exact-fact accuracy without the change.
- Limit verification artifacts to tests, fixtures, snapshots, golden files, mocks, test data, test helpers, and deterministic generated verification outputs.
- Update an omitted production reference only to conform mechanically to a settled interface.
- Preserve behavior in every omitted production-reference change.
- Require every omitted production-reference change to have no materially distinct valid outcome.
- Update an omitted lockfile only as deterministic output from an explicitly in-scope dependency decision.
- Return to Refine when lockfile resolution exposes an unexpected dependency, source, version, feature, or graph choice.
- Update an omitted documentation reference only for an exact settled identifier, signature, command, path, or output.
- Preserve the documentation's editorial intent.
- Create a verification artifact only when a settled acceptance test requires its coverage.
- Create that verification artifact only when no eligible existing artifact can contain the coverage without weakening it.
- Exempt only that eligible verification artifact from a second file-creation authorization.
- Preserve settled product behavior, contract meaning, acceptance requirements, and verification intent.
- Prohibit new production behavior, production files, interfaces, dependency decisions, migrations, or architectural choices.
- Prohibit security decisions, destructive actions, external integrations, publication decisions, or release decisions.
- Return to Refine when an adjustment changes or interprets a Director-owned decision.
- Return to Refine when an adjustment expands production scope, changes expected results, or adds a requirement.
- Return to Refine when more than one materially distinct valid outcome exists.
- Record each effective-scope path, triggering in-scope change, and eligibility rule in the applicable Implement or Ratify completion report.
- Record the same evidence in the closure-audit record when applicable.
- Require no second Director authorization for an eligible effective-scope adjustment.

### Delegation and sub-agent use

- Make inline work the default for every AC phase and implementation task.
- Do not spawn or delegate to sub-agents without explicit Director authorization for the active AC.
- State the inline constraint, proposed bounded task split, agent count, and token/time tradeoff before requesting delegation.
- Ask the Director to narrow the task or split the AC before proposing delegation when the task exceeds practical inline capacity.
- Limit authorized delegation to the active AC's named scope.
- Prevent recursive or unbounded sub-agent spawning.
- Treat tool availability, time pressure, and task size alone as insufficient delegation authorization.
- Keep primary-agent ownership of integration, validation, adversarial verification, and closure reporting.
- Distinguish parallel shell commands from sub-agent spawning.

Note: this rule does not prohibit batching independent commands.

### AC-First Workflow

- Evaluate AC ceremony during initial request triage when the Director has not selected Draft.
- Recommend direct handling when a change is small, bounded, low-risk, and materially simpler without an AC.
- Treat quick documentation changes across linked references as direct-handling candidates.
- Treat quick CLI output-formatting changes across a few files as direct-handling candidates.
- Count production and test source files when estimating change size.
- Exclude documentation, generated outputs, fixtures, lockfiles, and governance templates from the count.
- Treat changes exceeding eight counted files as presumptively AC-worthy.
- Require an AC for architecture, schema, dependency, security, migration, external-integration, destructive, governance, or release decisions regardless of file count.
- Preserve explicit authorization, scope, documentation, and same-pass test requirements for direct handling.
- Treat every non-trivial change as AC-first work unless the Director explicitly overrides it.
- Draft `govna/ac<N>-<slug>.md` before implementation using `govna/ac-template.md`.
- Define scope, out-of-scope, and acceptance tests in the AC.
- Wait for explicit user confirmation that the AC is implementation-ready before starting implementation.

### Four-Phase Workflow

- Follow the lifecycle `Draft → Audit → Refine → Implement → Ratify → Package` for every governed AC.
- Report each automatic phase transition and its evidence.
- Pause immediately when an automatic-transition eligibility condition fails.
- Pause immediately when a Director-owned decision appears.
- Prevent automatic advancement from authorizing Implement, Ratify, Package, release preparation, publication, delegation, or commits.
- Treat standalone `Draft` or `draft` as the Director-authorized pre-cycle action that creates the active AC.
- Keep Draft outside the AC phases.
- Enter Audit automatically when Draft completes the active AC with populated scope and acceptance tests.
- Keep an unscoped stub paused until the Director scopes it.
- Treat the Draft authorization as continuing authority for that automatic Audit entry.
- Start each governed AC cycle in Audit when the AC is ready for adversarial review.
- Challenge the AC, repository behavior, referenced documentation, scope, edge cases, omissions, and testability during Audit.
- Recheck new or unresolved contract-integrity findings before completing Audit.
- Keep Audit non-mutating.
- Do not edit the AC or repository during Audit.
- Pause after Audit until the Director requests Refine unless integrated audit adoption or eligible automatic Refine entry applies.
- Enter Refine automatically when Audit completes with only advancement-eligible findings.
- Define an advancement-eligible finding as one outside every Director-owned category in `### General Gates` and roles.md `What the Operator Must Defer` with exactly one materially valid correction.
- Block automatic Refine entry on any unresolved contract-integrity finding.
- Report every autonomously resolved Audit finding with its resolution in the Refine completion.
- Resolve Audit findings and incorporate settled Director decisions during Refine.
- Pause Refine when a Director-specific decision remains unresolved.
- Edit the AC during Refine when an Audit finding or settled Director decision requires an AC change.
- Complete Refine without editing the AC when no Audit finding or settled Director decision requires an AC change and no Director-specific decision remains unresolved.
- Run Pre-Implementation Verification after eligible automatic Refine completion.
- Report implementation readiness only when that verification passes.
- Remain in Refine when that verification finds a gap.
- Validate AC-document-only Draft and Refine edits with the required document checks.
- Keep AC-document-only Draft and Refine edits outside canonical validation cycles.
- Do not begin implementation during Refine.
- Pause after Refine and await explicit Director implementation-ready confirmation to Implement.
- Implement only the settled AC scope during Implement.
- Return to Refine when Implement reveals a contract, scope, or Director decision change.
- Return to Implement when Implement reveals an implementation-only correction.
- Apply the bounded completeness exception only after the Director authorizes Implement.
- Apply the exception only during Implement or its closure audit.
- Define the settled objective as the outcome in the active AC's `## Summary`.
- Define the settled corrective behavior class as a correction required by an active acceptance test.
- Define the settled repository surface as an existing artifact family named in the active AC.
- Require repository evidence of the missed path or instruction.
- Cite the active requirement that the gap violates.
- Explain why the correction has only one materially valid outcome.
- Exclude every Director-owned decision category from the exception.
- Treat the original Implement authorization as continuing authority for an eligible correction round.
- Limit autonomous Refine edits to the AC scope, path list, inventory, and acceptance coverage needed to close the gap.
- Limit autonomous implementation edits to existing artifacts in the settled repository surface.
- Prohibit the exception from creating a production or governance artifact.
- Run Pre-Implementation Verification after each autonomous Refine correction.
- Re-enter Implement automatically only after that verification passes.
- Count one round for one automatic Implement-to-Refine transition, its AC correction, successful Pre-Implementation Verification, and one automatic Refine-to-Implement transition.
- Reset the round counter only when the Director authorizes Implement again.
- Limit the exception to three correction rounds per Director-authorized Implement.
- Pause for the Director before a fourth correction round.
- Prevent the exception from authorizing initial Implement, Audit, Ratify, Package, release preparation, publication, delegation, or commits.
- Include tests, adversarial verification, and defect correction in Implement.
- Apply `### Effective Implementation Scope` to eligible omitted artifacts during Implement and closure-audit correction.
- Recheck new or unresolved contract-integrity findings before completing Implement and during the closure audit.
- Run one exhaustive, non-mutating closure audit after Implement, validation, adversarial verification, and defect correction.
- Keep the closure-audit working record in the active agent's session.
- Do not create a separate closure-audit artifact.
- Capture one deterministic Implement evidence snapshot in the closure-audit working record.
- Capture the snapshot after final validation and the last repository mutation.
- Complete the snapshot before Implement completion.
- Follow `govna/development-cycle.md` `### Implement Evidence Snapshot` for snapshot contents and dependency boundaries.
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
- Check the Implement evidence snapshot with non-mutating state, version, content-identity, and diff checks.
- Treat evidence as current only when the snapshot is present, complete, and identity-matching.
- Treat every non-current snapshot as stale evidence.
- Treat an unrecorded input as stale when a reused validation or acceptance check can read it.
- Treat uncertainty about an input's relevance as stale evidence.
- Distinguish evidence currentness from acceptance-test outcome.
- Preserve each recorded acceptance-test disposition exactly during reuse.
- Apply existing Ratify routing to every current non-clean disposition.
- Reuse current closure-audit findings, acceptance-test dispositions, render evidence, and final validation during Ratify.
- Prohibit a new scratch directory, canon render, build, or test solely to repeat current evidence.
- Rerun applicable validation when Ratify evidence is missing or stale.
- Perform the final review during the same Ratify turn.
- Recheck new or unresolved contract-integrity findings during Ratify.
- Complete Ratify in that turn when the review finds no issue.
- Apply the Approval Boundaries > General Gates and roles.md `What the Operator Must Defer` boundaries to classify any other Director-owned Ratify finding.
- Return Ratify feedback to Refine, without completing Ratify, for a contract, scope, product, security, destructive, publication, or release finding.
- Auto-correct an implementation-only finding inline during Ratify.
- Apply `### Effective Implementation Scope` to eligible omitted artifacts during Ratify correction.
- Rerun applicable validation after an inline Ratify correction.
- Skip `./build.sh` in that revalidation only when the correction is documentation-only and not covered by this repo's own build validation.
- Run the applicable document, render, or diff check in place of a skipped `./build.sh`.
- Repeat the correct-validate-review cycle automatically for at least 3 rounds before treating an implementation-only finding as unresolved.
- Return Ratify feedback to Implement, without completing Ratify, for an implementation-only finding still unresolved after 3 rounds.
- Skip requests for a second acceptance signal after a clean Ratify review.
- Treat `Package` as the separate post-Ratify name for release preparation, not as a fifth AC phase.
- Start `Package` only after an explicit Director request.
- Do not infer Package from Ratify acceptance.
- Treat standalone `Package`, `package`, `pack`, and `prep` as equivalent names for `Package` only after Ratify acceptance.
- Use the successful final full build and clean Ratify review as current pre-change Package evidence.
- Pass the full build's validation token to Rust prep during `Package` only when the repository provides Rust validation-token support.
- Fall back to a pre-change full build only when Rust validation-token support exists and its prep evidence is missing or stale.
- Define the pending release batch as every unpackaged AC whose implementation is present in the unreleased repository state.
- Include an implemented AC in the pending release batch while it awaits Ratify.
- Require every pending release-batch member to complete Ratify before Package.
- Require the established release batch to equal the complete pending release batch.
- Reject Package while excluded implemented work remains in the unreleased repository state.
- Require the release-message AC-reference set to equal the established release batch before Package runs prep.
- Preserve release-prep mutations, release behavior, and approval boundaries during `Package`.

### Phase-Advancement Rules

- Treat only explicit Director action language as authorization to enter the named next action.
- Exempt integrated audit adoption, eligible automatic Refine entry, and an eligible bounded completeness correction from a fresh Refine action instruction.
- Exempt completed-Draft automatic Audit entry from a fresh Audit action instruction.
- Exempt only an eligible bounded completeness correction from a fresh Implement action instruction.
- Treat standalone `Draft` or `draft` as the pre-cycle action that creates the active AC.
- Require the Director to authorize Draft before creating the AC.
- Start an AC cycle only after the Director identifies the AC and authorizes Audit, integrated audit adoption identifies the emitted AC, or a completed Draft identifies the active AC.
- Apply an unnumbered Audit, Refine, Implement, or Ratify instruction when exactly one AC can enter the requested phase.
- Require the AC number when multiple ACs can enter the requested phase.
- Ask the Director for the AC number and last completed lifecycle action when phase eligibility cannot be established.
- Treat one active Ratified AC as an established one-AC release batch only when it is the complete pending release batch.
- Treat only a Director-named complete pending release batch as an established multi-AC release batch.
- Accept only Package followed by a plus-joined list of uppercase AC<number> references as the named-batch Package form.
- Apply standalone Package, package, pack, or prep to the established Ratified release batch.
- Ask the Director to name the release batch when multiple ungrouped Ratified ACs can enter Package.
- Reject a named release batch that contains a non-Ratified AC.
- Measure the projected complete pending release batch with one private provisional prefix-plus-summary string before another AC enters Implement.
- Use the provisional string only for the 80-byte fit check.
- Discard the provisional string after the fit check.
- Start another Implement only when the projected complete pending release batch can fit one compliant release message.
- Require Package for the current fitting batch before another Implement when the projection cannot fit.
- Recheck the complete pending release batch and exact release message before Package runs prep.
- Reject an oversized or partial release batch.
- Prohibit automatic release-batch splitting.
- Treat a compound request as authorization for only the named action.
- Pause before any unnamed action.
- Treat ambiguous, unrelated, or implicit replies as non-advancing feedback.
- Interpret Audit, Refine, Implement, and Ratify as workflow phases only in the context of the active AC cycle.
- Interpret standalone `Package`, `package`, `pack`, or `prep` as post-Ratify Package only after acceptance and explicit request.
- Do not interpret `run ./build.sh prep ...`, `pack the binary`, `prepare the build`, or non-standalone `prep` as workflow advancement.
- Treat ordinary coding language such as `build`, `package the binary`, or a package-manager command as unrelated to phase advancement.
- Require explicit operational wording such as `run ./build.sh` before executing a repository command.
- Never infer a shell command from an action name.

### Primary And Ancillary Scope

- Capture the resolved current working directory as the primary repository at session entry.
- Track the primary repository and current phase internally.
- Report the primary repository or current phase only for ancillary work, repository or phase ambiguity, a phase correction, or a repository switch.
- Label work in another repository or path as `Ancillary work` only after the Director explicitly requests it.
- Report the ancillary repository or path and authorization separately from the primary phase.
- Prevent ancillary work from satisfying primary-repository scope, tests, validation, or phase gates.
- Restate the primary repository and paused phase when returning from ancillary work.

### AC Critique Gate

- Wait for the Director's explicit implementation-ready confirmation.

Note: the Director flags scope concerns in chat during this window.

### Pre-Implementation Verification

- Run this checklist after the Director resolves all review questions.
- Confirm each settled decision landed verbatim in the AC.
- Treat a Director-resolved routing decision or explicit workflow override recorded in chat for an immutable emitted AC as satisfying the verbatim-in-AC check.
- Confirm ATs match settled wording.
- Confirm the AC title and Summary lead with the concrete outcome in plain language.
- Confirm every new or rewritten instruction in AGENTS.md follows Instruction Style.
- List ✓ for each check.
- Flag every gap.
- Authorize implementation only when the checklist is clean.

### Audit Adoption

- Apply integrated audit adoption only when the Director explicitly requests govna audit.
- Enter Audit only when govna audit emits or reuses one guarded adoption AC.
- Keep a clean audit result or pre-emission failure outside the AC phases.
- Audit the emitted AC immediately.
- Authorize the bounded scratch-review procedure in `govna/audit.md` from the original explicit `govna audit` request.
- Bind the scratch review to the resolved Govna executable and the emitted AC marker versions.
- Limit scratch-review authority to one unique system-temporary directory outside the consumer repository.
- Keep the emitted AC and consumer repository unchanged during integrated Audit and Refine.
- Remove the exact scratch directory before reporting Audit completion or a blocker.
- End scratch-review authority when Audit ends.
- Pause integrated audit adoption while any blocking finding or Director decision remains unresolved.
- Resume Refine after the Director resolves every blocking finding and decision.
- Require a new audit emission when a required correction would change the immutable AC.
- Complete Refine without editing the emitted AC when no blocker remains.
- Run Pre-Implementation Verification after integrated Refine.
- Report implementation readiness only when Pre-Implementation Verification passes.
- Remain in Refine when Pre-Implementation Verification finds a gap.
- Stop integrated audit adoption before Implement.
- Track the active phase in the session instead of the emitted AC.
- Apply these rules whenever implementing an audit-emitted AC.
- Treat every Director-resolved routing target as effective implementation scope, even when it is absent from `## In Scope`.
- Treat each explicitly named migration destination as effective implementation scope with its routed source.
- Treat `govna/preserve.txt` as effective implementation scope only when a resolved routing outcome requires creating or changing it.
- Treat `CHANGELOG.md` as effective implementation scope only when a resolved legacy-phrase outcome requires removing an exact phrase.
- Require no second Director authorization for an effective-scope preserve-registry change.
- Require the Director to name every migration destination.
- Apply each resolved routing action while leaving the emitted AC stub unchanged.
- Install each missing canon-backed replacement before retired-source routing.
- Render canon into a scratch directory using `govna render <scratch>`.
- Inspect changes per `## In Scope` item by running `diff -ru <scratch>/<path> <path>`.
- Add each resolved preserve target's exact path to `govna/preserve.txt`.
- Remove each resolved sync, delete, or canon-backed migration target from `govna/preserve.txt`.
- Create the registry with the `govna-preserve-v1` header when the first preserve entry is required.
- Keep preserve-registry entries unique and byte-sorted.
- Preserve unrelated preserve-registry entries.
- Leave the registry absent or unchanged when its state already satisfies every resolved outcome.
- Treat exact legacy preserve phrases in the Unreleased CHANGELOG Summary as migration evidence only.
- Remove each exact legacy phrase only after verifying its resolved target and registry state.
- Preserve unrelated CHANGELOG Summary text and historical rows.
- Ensure the parent directory exists for each `## In Scope` item: `mkdir -p "$(dirname <path>)"`.
- Categorize each `## In Scope` item as pure-canon or mixed-content before applying.
- Apply pure-canon items by copying from canon: `cp <scratch>/<path> <path>`.
- Apply mixed-content items by hunk-merge.
- Replace canon-zone content above each registered boundary heading.
- Use `## Project Rules` as the AGENTS.md boundary.
- Use `## Project Practices` as the boundary for `govna/development-guidelines.md`, `govna/editing-guidelines.md`, and CODE `govna/build-release.md`.
- Preserve the boundary heading and every line below it as repo-owned content.
- Resolve an unresolved emitted repository check in chat.
- Run the chosen repository command after all selected sync, migration, and deletion work.
- Cite repository evidence when choosing `Not applicable` for the repository check.
- Write `govna/canon-baseline.txt` from the scratch render only after every other applicable acceptance test and routing outcome passes and the resolved repository check succeeds or its `Not applicable` evidence holds.
- Refresh Rust validation evidence from the same scratch baseline only when the repository provides Rust validation-token support and the installed `govna/canon-baseline.txt` is verified.
- Use the refreshed Rust validation token as Package evidence only when the repository provides Rust validation-token support.
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
- Complete every mid-implementation decision change in one pass across files, docs, and tests.
- Never leave a half-migrated state.
- Update user-facing docs when commands, setup, workflows, outputs, published structure, or operating instructions change.
- Update architecture, planning, or style docs only when materially affected.
- End every AC doc with a `## Status` section using `PENDING`, `IN PROGRESS`, or `DEFERRED` with a reason.
- Use per-phase status for partial completion.
- Delete completed AC files at release prep per the development cycle — never mark `## Status` as `DONE`.
- Record follow-on improvements in `plan.md`.
- Note follow-on improvements to the user when no planning artifact exists.
- Keep the current task strictly within its authorized scope.
- Use repo-relative paths or placeholders like `<project-root>` in committed content.
- Scan staged content for `/Users/`, `/home/`, or `C:\` before committing.
- Replace every staged absolute-path match.
- **Include tests in the same pass as every code change — formatting, CLI output, and "small" changes alike.**
- **Record every authorized repository-behavior correction in its owning governance document.**
- **Never record a repository-behavior correction as a memory entry, `feedback.md`, or session note.**

## Review Style

- Lead each review with findings.
- Assign Audit findings sequential identifiers in the form `F<#> [High|Medium|Low|Nit]`.
- Preserve each finding identifier and severity through Refine resolution.
- Reuse each finding identifier when it resurfaces during Implement or Ratify.
- Keep one finding sequence for the active AC lifecycle.
- Start a separate finding sequence for each standalone contract-integrity report.
- Exclude acceptance tests, verification evidence, workflow mechanics, and non-finding commentary from finding identifiers.
- Cite file paths and concrete behavior.
- Skip preamble summaries.
- Prioritize bugs, regressions, missing tests, and drift from documented behavior.
- Treat AC-document ceremony issues as nits after implementation starts and release prep will delete the AC.
- Prioritize defects that affect the delivered contract, implementation scope, tests, or release safety.
- Report "no issues" directly when none are found.
- Note every residual risk or verification gap.
- Open every substantive phase completion with one short outcome sentence.
- Keep completions terse with changed items, flat bullets, and a final `Awaiting <specific Director-initiated next>.` line.
- Focus completion summaries on changed behavior, material verification, findings, corrections, residual risks, and Director decisions.
- Suppress routine repository labels, phase labels, phase mechanics, expected skips, duplicated status, and expected no-action confirmations.
- Report suppressed workflow details only when exceptional or actionable.
- Keep independently useful results, corrections, findings, and risks in separate bullets.
- Avoid compressing independently useful facts with commas or semicolons.
- Skip "What's in it", "Main conclusion", and "Next steps" headers unless asked.
- Never prescribe commit, push, or release actions in Ratify.

Note: the Director triggers those actions; Ratify names what is pending.
- Skip settled repo mechanics in completions, including symlink behavior, mirror mechanics, governance structure, and contract conventions.
- Default to plain text and simple bullets.
- Use tables or richer structure only when content clearly benefits.
- Note skipped checks only when the omission is unusual or affects confidence.
- Run every required validation gate.
- Report successful routine gates only when they materially affect confidence.
- Always report failures and skipped required gates.
- Present architectural decisions to the director as: a recommendation when one viable option exists; two bounded options plus a recommendation when two exist; the best two plus a one-line note on the rest when more than two exist.
- Include the three-part self-review structure (Verified / Red-teamed / Not checked) defined in `govna/roles.md` in every substantial completion report, even when the default is terse.
- Color `Verified:`, `Red-teamed:`, and `Not checked:` cyan only when the response channel explicitly supports native color or ANSI color.
- Preserve plain-text self-review headings when response color is unavailable or disabled.
- Place a section's sole item on the heading line without a bullet.
- Use terse flat bullets when a self-review section has multiple items.
- Start every Package completion report with the plain, unbulleted, unindented line `Package complete.`.
- Insert exactly one blank line after `Package complete.` before `Verified:`.
- Keep `Verified:`, `Red-teamed:`, `Not checked:`, and `Run below to release:` in the Package completion report.
- State `No commit or release command executed.`.
- Present the exact drafted release command.

## Base Rules

### Build Verification

- Treat a change pass that creates or edits only an active AC document as outside a canonical validation cycle.
- Validate an AC-document-only change pass with the document checks in `### Session Entry`.
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

- Follow semver with PATCH for invisible changes and MINOR for user-visible changes.
- Batch PATCH-level changes.
- Pin dependencies to explicit versions.
- Document every reason to stay on an older dependency version.

### Errors

- Wrap user-facing errors with operation context and recovery guidance.

### AC Mechanics

- Label each acceptance test with source axis (`[Automated]` / `[Manual]`) and timing axis (`[Pre-release gate]` default; `[Post-release verification]` explicit). See `govna/ac-template.md`.
- Name test identifiers, output labels, comments, and errors by behavior.
- Reserve bare AC and AT identifiers for CHANGELOG rows, commit messages, active `govna/ac<N>-<slug>.md` documents, literal examples in `govna/ac-template.md`, and `Historical:` comments.
- Reserve bare Class, Part, and Round identifiers for CHANGELOG rows, commit messages, and `Historical:` comments.
- Reserve bare IE identifiers for CHANGELOG rows, commit messages, `plan.md`'s own `IE<N>:` bullets, and `Historical:` comments.
- Treat every other Markdown documentation file as out of bounds for bare AC, AT, Class, Part, Round, and IE identifiers.
- Use the `Historical:` prefix only for a relevant shipped-AC comment.
- Delete an irrelevant shipped-AC reference.

### Code Style and Conventions

- Pair every new CLI flag with a leading one-letter short form and a long-form alias.
- Migrate existing flags when their code is next touched.
- Follow existing repo patterns unless an approved improvement says otherwise.
- Comment public functions.
- Avoid product or vendor names in identifiers.
- Use product or vendor names only when an identifier names a real product-specific artifact or compatibility surface.

Note: `CLAUDE.md` is an example of an exempt identifier — it names a product-specific compatibility symlink that mirrors AGENTS.md.

### Tool Use

- Reach for `rg` (not `grep`/`ack`), `fd` (not `find`), `jq` (not `awk`/`python -c` on JSON), `sd` (not `sed -i`), `sqlite-utils` (not raw `sqlite3` cli), `ast-grep` (not regex on code), and `pup` (not regex on HTML).
- Send independent shell calls in a single message so they run in parallel.
- Reuse content from files already in conversation context.
- Reach for `Read` only to fetch unseen content or check for recent changes.

## Project Rules

- **Multi-utility repo.** This repo builds three installable binaries from `cmd/iq/`, `cmd/lm/`, `cmd/kb/`. `./build.sh` validates and installs all three binaries.
- **Per-utility programVersion.** Each `cmd/<name>/main.go` declares its own `programVersion` const. `./build.sh prep` treats `cmd/iq` as primary (module basename match) and bumps only its `programVersion`; `cmd/kb` and `cmd/lm` are secondary and skipped. Bump secondary binary versions manually in their own ACs.
- **Semver decision test.** Would a user notice the change in `iq --help`, `iq cfg show`, or `iq start`? Yes → MINOR (reset PATCH to 0). No → PATCH. Batch several PATCH fixes into one release. MAJOR not until a stable public API commitment.
- **External runtime deps.** iq depends on Apple Silicon, Python `mlx-lm` (via pipx), the `hf` CLI, and `mlx-embedding-models` injected into the `mlx-lm` venv. Verify with `iq doc`. iq is offline-first — do not introduce code paths that assume cloud LLM availability.
- Follow existing repo patterns unless an approved improvement says otherwise.
