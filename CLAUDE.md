# CLAUDE.md

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
1. Bump `program_version` in `cmd/iq/main.go` to the new version string
2. Add version row to `architecture.md` with a one-line summary of the changes
3. Offer to run `./build.sh <tag> "<message>"` — do not run it automatically; prompt the user to confirm first

## Coding Rules / Style
- Follow idiomatic Go practices
- Comment public functions
- Preserve existing tests

## Notes for Claude
- Treat `build.sh` as a single canonical workflow for correctness, formatting, testing, and building
- Focus on reasoning about code changes, versioning, refactors, and releases
- Preserve test/build compatibility.
- Suggest improvements, but preserve workflow order
