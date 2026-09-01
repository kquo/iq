# First-Class CODE Stacks

- Use only Go, Rust, Swift, or Terraform as selectable CODE stacks.
- Use this reference for their selection, validation, installation, release prep, and scoped builds.

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

## Release Command

- Print one shell-safe two-argument release command after prep in every stack.
- Emit the validated release tag bare in the printed release command.
- Emit the release message single-quoted with POSIX quote escaping in the printed release command.
- Confine the tag to `v`, digits, and dots through each adapter's existing validation before emission.
- Route each adapter's release-command prints through one emit helper.

## Go

- Discover utilities from regular `cmd/<target>/main.go` files in byte order.
- Validate scoped target names against discovered utilities before running mutable build steps.
- Reject unknown, duplicate, and path-containing scoped targets.
- Compile each selected utility once in an invocation-owned external temporary directory.
- Validate every selected compiled `--version` output before installing any selected utility.
- Replace each installed utility atomically from an adjacent staging file.
- Preserve each installed utility until its replacement succeeds.
- Use the successful final full build and clean Ratify review as current Package evidence.
- Require applicable revalidation before Go prep when Package evidence is missing or stale.
- Keep Go prep limited to version, changelog, released-AC, and matching `plan.md` pointer bookkeeping.
- Run no canonical build, Go build, or Go dependency command during Go prep.
- Reject every Go prep result outside its planned transformations.
- Require every prep-changed version declaration to equal the unprefixed release tag.
- Require the canonical changelog shape during prep.
- Accept each `\|` pair in an existing summary cell as one escaped pipe during changelog shape validation.
- Insert one exact release row immediately after `Unreleased`.
- Reject multiline, over-80-byte, and Markdown-table-unsafe release messages.
- Reject every release-message pipe, escaped or raw.
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
- Capture the complete candidate Git tree without changing the repository index.
- Display the candidate files and exact release sequence before approval.
- Reject a changed candidate tree after approval.
- Verify the staged and committed trees against the approved candidate.
- Require the release commit to use the exact approved message.
- Require clean committed `HEAD` before release compilation.
- Reuse an approved retry commit only when the tag is absent, Git state is clean, the full commit message matches, and prepared metadata matches.
- Validate every discovered utility declaration before release compilation.
- Compile each discovered utility once in byte order with `go build -mod=readonly -buildvcs=true -o <temporary-output> -ldflags '-s -w' ./cmd/<target>`.
- Run no canonical validation phase during release compilation.
- Validate every compiled utility version and committed-HEAD provenance before installation.
- Require every prep-changed utility version to equal the unprefixed release tag.
- Preserve independent secondary utility versions during release compilation.
- Install every validated utility by atomic adjacent replacement.
- Recheck every installed utility version and committed-HEAD provenance before tagging.
- Create the release tag only after compilation, validation, installation, and rechecks pass.
- Push nothing after an earlier release failure.
- Remove invocation-owned release outputs on every handled exit.

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
