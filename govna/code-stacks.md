# First-Class CODE Stacks

Use this reference for stack selection, canonical validation, artifacts, installation, release prep, and scoped-build behavior.

## Multi-Utility Versioning

- Keep the repository/package release version separate from each installable utility version.
- Identify each installable utility by the canonical target name selected by the stack's existing build/install mechanism.
- Require one explicit strict stable SemVer declaration per installable utility.
- Require each utility's `--version` result to be exactly `<utility-id> <MAJOR.MINOR.PATCH>` or `<utility-id> v<MAJOR.MINOR.PATCH>` plus its newline on stdout with no stderr output.
- Let each stack adapter choose declaration syntax and source layout while reporting the normalized utility contract.
- Validate normalized declarations before compilation.
- Validate compiled version results before installation or release-metadata writes.
- Preserve independent utility versions during repository release prep.

## Go

- Infer Go from `go.mod`; select it explicitly with `--stack Go`.
- Require the Go toolchain and the pinned staticcheck version installed by `build.sh`.
- Run dependency tidying, formatting, fixes, vetting, tests with coverage, staticcheck, and compilation.
- Install command binaries into `$(go env GOPATH)/bin`.
- Bump the single detected `programVersion` during release prep; validate and preserve independent utility versions in multi-utility repositories.
- Accept command names for scoped builds while retaining package-wide shared validation.

## Rust

- Infer Rust from `Cargo.toml`; select it explicitly with `--stack Rust`.
- Require Cargo, rustfmt, and Clippy.
- Run formatting, Clippy, tests, and release compilation.
- Keep compilation in an invocation-owned external Cargo target.
- Install binaries into `$CARGO_HOME/bin`, or `$HOME/.cargo/bin` when `CARGO_HOME` is unset.
- Bump the root package version and refresh `Cargo.lock` during release prep.
- Accept declared binary names for scoped builds and preserve package-wide shared validation.
- Require one literal `PROGRAM_VERSION: &str` strict stable SemVer declaration in each declared binary path.
- Validate every declaration before compilation and each compiled binary before installation.
- Validate every compiled binary before release-metadata writes.

## Terraform

- Infer Terraform from `.terraform.lock.hcl` or root Terraform files; select it explicitly with `--stack Terraform`.
- Require the Terraform CLI.
- Run recursive formatting checks and module validation.
- Keep Terraform working data in repository-local ignored artifact directories.
- Derive release versions from Git tags without a source version bump.
- Reject scoped builds because Terraform validation is repository-wide.

## Swift

- Infer Swift from a root `Package.swift`; select it explicitly with `--stack Swift`.
- Prefer Go, Terraform, and Rust manifests over Swift; prefer Swift over Node, Python, and Java manifests.
- Require Swift 6.0 or newer, Git, and one root SwiftPM package on macOS or Linux.
- Run strict toolchain formatting, debug compilation, tests, and release compilation with compiler warnings as errors.
- Keep SwiftPM artifacts in one invocation-owned external scratch directory and clean it on success, failure, and handled signals.
- Keep project-level `.swiftpm/` configuration trackable.
- Keep `Package.resolved` tracked for leaf packages with dependencies; treat it as optional for dependency libraries.
- Derive release versions from Git tags and leave `Package.swift` unchanged during release prep.
- Install executable products into `${SWIFT_BIN_HOME:-$HOME/.local/bin}` by atomically replacing regular destination files and refusing unsafe entries.
- Let library-only packages complete without installation.
- Use the canonical color and plain-text presentation policy for build, prep, and release output.
- Accept executable-product names for scoped builds while retaining package-wide formatting and tests.
- Build and install only selected executable products during scoped builds.
- Treat native Xcode projects and Apple application bundles as a possible future backend.
