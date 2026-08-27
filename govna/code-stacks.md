# First-Class CODE Stacks

Use this reference for CODE stack selection, validation, installation, release prep, and scoped builds.

## Multi-Utility Versioning

- Keep the repository/package release version separate from each installable utility version.
- Identify each utility by its stack-selected canonical target.
- Require one strict stable SemVer declaration for each utility.
- Require each utility's `--version` result to be exactly `<utility-id> <MAJOR.MINOR.PATCH>` or `<utility-id> v<MAJOR.MINOR.PATCH>` plus its newline on stdout with no stderr output.
- Let each stack adapter choose declaration syntax and source layout while reporting the normalized utility contract.
- Validate declarations before compilation.
- Validate compiled versions before installation.
- Validate compiled versions before writing release metadata.
- Preserve independent utility versions during repository release prep.

## Go

- Discover utilities from regular `cmd/<target>/main.go` files in byte order.
- Validate scoped target names against discovered utilities before running mutable build steps.
- Reject unknown, duplicate, and path-containing scoped targets.
- Compile each selected utility once in an invocation-owned external temporary directory.
- Validate every selected compiled `--version` output before installing any selected utility.
- Replace each installed utility atomically from an adjacent staging file.
- Preserve each installed utility until its replacement succeeds.
- Run ordinary canonical pre-change validation during Go release prep.
- Run ordinary canonical post-change validation during Go release prep.
- Emit no validation token from Go builds.
- Remove invocation-owned build, coverage, version-probe, and installation-staging outputs on every handled exit.
- Terminate after handling HUP, INT, or TERM.
- Infer Go from `go.mod`.
- Select Go explicitly with `--stack Go`.
- Require the Go toolchain and the pinned staticcheck version installed by `build.sh`.
- Run dependency tidying, formatting, fixes, vetting, tests with coverage, staticcheck, and compilation.
- Install command binaries into `$(go env GOPATH)/bin`.
- Bump the single detected `programVersion` during release prep.
- Validate independent utility versions in multi-utility repositories.
- Preserve independent utility versions in multi-utility repositories.
- Accept command names for scoped builds while retaining package-wide shared validation.

## Rust

- Infer Rust from `Cargo.toml`.
- Select Rust explicitly with `--stack Rust`.
- Require Cargo, rustfmt, and Clippy.
- Run formatting, Clippy, tests, and release compilation.
- Keep compilation in an invocation-owned external Cargo target.
- Install binaries into `$CARGO_HOME/bin`, or `$HOME/.cargo/bin` when `CARGO_HOME` is unset.
- Bump the root package version during release prep.
- Refresh `Cargo.lock` during release prep.
- Accept declared binary names for scoped builds.
- Preserve package-wide shared validation during scoped builds.
- Require one literal `PROGRAM_VERSION: &str` strict stable SemVer declaration in each declared binary path.
- Validate every declaration before compilation and each compiled binary before installation.
- Validate every compiled binary before release-metadata writes.

## Terraform

- Infer Terraform from `.terraform.lock.hcl` or root Terraform files.
- Select Terraform explicitly with `--stack Terraform`.
- Require the Terraform CLI.
- Run recursive formatting checks and module validation.
- Keep Terraform working data in repository-local ignored artifact directories.
- Derive release versions from Git tags without a source version bump.
- Reject scoped builds because Terraform validation is repository-wide.

## Swift

- Infer Swift from a root `Package.swift`.
- Select Swift explicitly with `--stack Swift`.
- Prefer Go, Terraform, and Rust manifests over Swift.
- Prefer Swift over Node, Python, and Java manifests.
- Require Swift 6.0 or newer, Git, and one root SwiftPM package on macOS or Linux.
- Run strict toolchain formatting, debug compilation, tests, and release compilation with compiler warnings as errors.
- Keep SwiftPM artifacts in one invocation-owned external scratch directory and clean it on success, failure, and handled signals.
- Keep project-level `.swiftpm/` configuration trackable.
- Keep `Package.resolved` tracked for leaf packages with dependencies.
- Treat `Package.resolved` as optional for dependency libraries.
- Derive release versions from Git tags.
- Leave `Package.swift` unchanged during release prep.
- Install executable products into `${SWIFT_BIN_HOME:-$HOME/.local/bin}` by atomically replacing regular destination files and refusing unsafe entries.
- Let library-only packages complete without installation.
- Use the canonical color and plain-text presentation policy for build, prep, and release output.
- Accept executable-product names for scoped builds while retaining package-wide formatting and tests.
- Build only selected executable products during scoped builds.
- Install only selected executable products during scoped builds.
- Treat native Xcode projects and Apple application bundles as a possible future backend.
