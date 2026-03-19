# CLAUDE.md

## Interaction Mode
- Treat all input as exploratory discussion. Only produce artifacts or make changes when explicitly authorized

## Build
  - **NEVER run `go test`, `go build`, `go vet`, `go fmt`, or any individual Go toolchain command directly** — always use `./build.sh`, because it:
    - Updates dependencies (`go mod tidy`)
    - Formats (`go fmt`) and fixes code (`go fix`) — `go fix` enforces idiomatic Go practices
    - Runs vet (`go vet`) and static analysis (`staticcheck`)
    - Runs all tests
    - Builds binaries in $GOPATH/bin
  - `./build.sh` with no arguments builds **all** utilities in the repo
  - `./build.sh <name>` (e.g. `./build.sh cash5`) builds only that utility

## Versioning
- Current version from latest Git tag, or from `programVersion` in `cmd/iq/main.go`
- IQ follows **semver** (MAJOR.MINOR.PATCH):
  - **PATCH** (0.x.y+1): bug fixes, test coverage, internal refactors, tooling — nothing a user would notice at the CLI level. Batch several PATCH fixes into a single release when possible; do not cut a release for every small fix.
  - **MINOR** (0.x+1.0): new user-visible features, new config fields, new commands or subcommands, new pipeline modes, or any behavioral change. Reset PATCH to 0.
  - **MAJOR**: not applicable until 1.0 (stable public API commitment).
- When deciding: ask "would a user notice this in `iq --help`, `iq cfg show`, or `iq start`?" If yes → MINOR. If it's invisible to them → PATCH.

## Pre-Commit Checklist
Before offering to run a release commit, always complete these steps in order:
0. **Run `./build.sh` (no tag) yourself** using the Bash tool. Read the output,
   fix any failures (test errors, vet issues, staticcheck), and re-run until clean.
   Only proceed to the steps below after a clean build.
1. Audit `arch.md` against the code — grep thresholds, constants, counts,
   formats, and field orders in source files and fix any drift.
2. Add version row to `arch.md` Version History. The section has exactly two parts:
   - **One visible row** (latest only) — the new entry, above the `<details>` block
   - **`<details>` block** — every previous version in descending order (newest-first),
     ending at v0.2.7
   Steps: (a) add new row above `<details>`; (b) move the previously-visible row to the
   top of the table inside `<details>`; (c) update the `<summary>` label's version range.
   The visible table must never accumulate more than one row.
3. Bump `programVersion` in `cmd/iq/main.go` to the new version string
4. Remove completed features from `plan.md`
5. Delete the FEAT's acceptance-criteria file from memory (if one was created) and remove its entry from `MEMORY.md`
6. Report to user they must now run ./build.sh <tag> "<message>" — message should be a brief phrase (under 80 chars), not a changelog

## Coding Rules / Style
- Follow idiomatic Go practices
- Comment public functions
- Preserve existing tests
- Add unit tests for every new FEAT — place them in a `_test.go` file in the same package. If no test file exists yet, create one.
- Every code change must include unit test coverage for the new or modified logic. No feature or fix is complete without a corresponding test.

## Notes for Claude
- Treat `build.sh` as a single canonical workflow for correctness, formatting, testing, and building
- Focus on reasoning about code changes, versioning, refactors, and releases
- Preserve test/build compatibility.
- Suggest improvements, but preserve workflow order
