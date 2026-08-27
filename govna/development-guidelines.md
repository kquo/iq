# Development Guidelines

Use these durable coding practices.
Use `AGENTS.md`, `development-cycle.md`, and `build-release.md` for workflow, validation, and Package.
Treat sections above `## Project Practices` as govna-maintained canon.
Keep repo-specific practices in `## Project Practices`.

## Identifier Strategy

- Choose a primary key strategy early and document it in `arch.md`
- Prefer surrogate keys for internal identity
- Keep external IDs as indexed attributes
- Maintain an explicit mapping layer between external ID systems.
- Prohibit assumptions that external IDs are interchangeable.

## Schema And Data Migrations

- Treat schema changes as first-class events: version them, document them, test the migration path
- Verify old data compatibility with new schemas.
- Write migration logic for old data.
- Fail explicitly when migration logic is unavailable.
- Audit all foreign key references when a migration changes identity or key structure.

## External Integration Patterns

- Validate external data at the boundary
- Treat upstream shape and completeness as untrusted
- Define and document a clear precedence order when reconciling data from multiple sources.
- Cache external data locally with explicit TTL or versioning
- Never silently serve stale data as fresh

## Generated Artifact Propagation

- Propagate source-of-truth fixes to every template and rendered-example copy in the same change.
- Grep the full repo for the pattern being changed before considering a fix complete
- Treat the template as authoritative when it diverges from rendered output.
- Keep `build.sh` self-contained.
- Do not add sourced production helper modules.

## Error Handling And Validation

- Validate at system boundaries (user input, external APIs, file I/O)
- Trust internal code
- Report explicit errors instead of returning wrong output.
- State the failed condition in user-facing errors.
- Name the affected path or option when available.
- Provide a recovery action when the user can recover.
- Treat static analysis and linting errors as build failures.
- Validate installable-target declarations before compiling or installing them.
- Follow the applicable stack guidance for release-prep evidence, validation ordering, and build-state reuse.
- Pass release-prep evidence through the applicable stack's canonical CLI option.

## Testing Expectations

- Test every new function and error path in the implementation pass.
- Document every coverage gap caused by out-of-scope mocking infrastructure.
- Label tests that require live systems or manual verification as `[Manual]`

## Dependency And Import Hygiene

- Prefer standard library over external dependencies when the capability is equivalent
- Justify every added dependency.
- Reject convenience alone as dependency justification.
- Keep import paths consistent after renames or reorganizations

## CLI Usage Formatting

- Accept `-h`, `-?`, and `--help` as help flags for every command.
- Use a shared formatting function for help output.
- Render `Usage:` in bold white.
- Indent each flag line by 2 spaces
- Align descriptions at column 38
- Combine short and long flag forms on one line.
- Add every new flag to the shared usage formatter.
- Do not rely on framework defaults for new flags.
- Describe each command by the repository content it reads or writes.
- Explain whether each command changes repository content.

## Documentation Alignment

- Ship behavior docs with code.
- Verify every referenced symbol or path.
- Keep `arch.md` limited to built architecture.

## Go Practices

- Add single-line godoc comments to exported functions in shared Go packages.
- Declare a non-empty `const programVersion` string literal in every installable `cmd/<name>/main.go`.
- Validate every `programVersion` declaration through `build.sh` before compiling installable binaries.
- Pin `staticcheck` to the repository-governed version and invoke the pinned installation path directly.
- Treat `go vet` and `staticcheck` findings as build failures.
- Scan all `.go` and `.go.tmpl` files for stale import paths after a module rename.

## Project Practices

- Follow existing repo patterns unless an approved improvement says otherwise.
