# CLAUDE.md

## Interaction Mode
- Treat all input as exploratory discussion. Only produce artifacts or make changes when explicitly authorized

## Project Overview
- This is a Go project. Utilities live under ./cmd/<utility_name>
- Build script `./build.sh`
  - Updates dependencies (`go mod tidy`)
  - Formats (`go fmt`) and fixes code (`go fix`) — `go fix` enforces idiomatic Go practices
  - Runs vet (`go vet`) and static analysis (`staticcheck`)
  - Runs all tests
  - Builds binaries in $GOPATH/bin

## Versioning
- Current version from latest Git tag, or from `program_version` in `cmd/iq/main.go`

## Pre-Commit Checklist
Before offering to run a release commit, always complete these steps in order:
1. Audit `arch.md` against the code — grep thresholds, constants, counts,
   formats, and field orders in source files and fix any drift.
2. Also add version row to `arch.md` with a one-line summary of the changes
3. Bump `program_version` in `cmd/iq/main.go` to the new version string
4. Report to user they must now run ./build.sh <tag> "<message>" — message should be a brief phrase (under 80 chars), not a changelog

## Coding Rules / Style
- Follow idiomatic Go practices
- Comment public functions
- Preserve existing tests

## Notes for Claude
- Treat `build.sh` as a single canonical workflow for correctness, formatting, testing, and building
- Focus on reasoning about code changes, versioning, refactors, and releases
- Preserve test/build compatibility.
- Suggest improvements, but preserve workflow order
