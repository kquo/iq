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

## Versioning
- Current version from latest Git tag, or from `programVersion` in `cmd/iq/main.go`

## Pre-Commit Checklist
Before offering to run a release commit, always complete these steps in order:
1. Audit `arch.md` against the code — grep thresholds, constants, counts,
   formats, and field orders in source files and fix any drift.
2. Add version row to `arch.md`: new entry goes above the `<details>` block (visible);
   move the previously-visible entry inside the `<details>` block; update the summary
   label's version range. Only ONE entry is ever visible — all others stay collapsed.
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
