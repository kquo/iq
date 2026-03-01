# CLAUDE.md
## Project Overview
- This is a Go project. Utilities live under ./cmd/<utility_name>.
- The main build script is `build.sh`. It:
  - Updates dependencies (`go mod tidy`)
  - Formats (`go fmt`) and fixes code (`go fix`) — `go fix` enforces idiomatic Go practices
  - Runs vet (`go vet`) and static analysis (`staticcheck`)
  - Runs all tests
  - Builds binaries in $GOPATH/bin

## Build Instructions
- To build all utilities: `./build.sh`
- To build specific utilities: `./build.sh <utility_name>...`
- Use `-ldflags "-s -w"` for release builds.
- Generated binaries live in `$GOPATH/bin` (with `.exe` extension on Windows).

## Versioning
- Current version from latest Git tag.
- Next tag increments patch number automatically.
- To release: `./build.sh <tag> "<message>"` — e.g. `./build.sh v0.3.2 "feat: MLX embed sidecars"`
  - If a valid semver tag (`vX.Y.Z`) is the first argument, `build.sh` will run `git add . && git commit && git tag && git push` automatically after a successful build.
  - Omitting the tag just builds without committing.

## Coding Rules / Style
- Format code using `go fmt`.
- Ensure code passes `go vet` and `staticcheck`.
- Follow idiomatic Go practices (`go fix` is applied).
- Comment all public functions.
- Respect existing unit tests; 
# - generate new ones as needed.

## Notes for Claude
- Always reason about builds using the `build.sh` sequence.
- For refactors, maintain compatibility with tests and build expectations.
- Suggest improvements, but preserve workflow order.

