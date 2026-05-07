// Package preptool stages a release: bumps version constants, inserts a
// CHANGELOG row, deletes completed AC files, sweeps matching AC-pointer IE
// lines from plan.md, runs validation builds around the write phases, and
// prints the canonical release command. It does not run the release itself;
// that remains the director's explicit approval via cmd/rel.
package preptool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Config holds the inputs for a prep run.
type Config struct {
	Version  string    // "vX.Y.Z" with v-prefix
	Message  string    // release message, ≤80 chars, becomes both the CHANGELOG row text and the release message
	RepoRoot string    // optional; defaults to os.Getwd() when empty
	DryRun   bool      // when true, skip phases 3 (pre-check build), 7 (writes), and 8 (post-check build)
	NoBuild  bool      // when true, skip phases 3 and 8
	Out      io.Writer // defaults to os.Stdout when nil; all tool-produced output flows here
}

var (
	semverTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	// programVersionRe matches the `programVersion = "x.y.z"` assignment line
	// regardless of whether it appears in the inline form (`const programVersion = "..."`)
	// or the grouped form (`const ( ... programVersion = "..." ... )`). The
	// preceding `const` keyword is intentionally NOT required by the regex:
	// the grouped form has it on a different line. preptool only scans
	// cmd/*/main.go files, where the convention restricts programVersion to a
	// const declaration in one of those two forms; false positives (e.g. a
	// `var programVersion` or a string literal containing this pattern) are
	// vanishingly unlikely in practice.
	programVersionRe = regexp.MustCompile(`(programVersion\s*(?:string\s*)?=\s*)"([^"]*)"`)
	templateConstRe  = regexp.MustCompile(`(const\s+TemplateVersion\s*=\s*)"([^"]+)"`)
	acRefRe          = regexp.MustCompile(`AC[0-9]+`)
	// acFileRe matches docs/ac<N>-<slug>.md and any companion suffix; we split
	// the canonical AC file from companions by checking the suffix separately.
	acFileRe = regexp.MustCompile(`^ac([0-9]+)-[^/]+\.md$`)
	// iePointerRe matches an AC-pointer in a plan.md IE line. Per the plan.md
	// convention, an AC-pointer ends in `→ docs/ac<N>-<slug>.md`. The trailing
	// `-` after the digits disambiguates ac1 from ac10/ac11/etc.
	iePointerRe = regexp.MustCompile(`→\s+docs/ac([0-9]+)-`)
)

const maxMessageLen = 80

// buildFn is the seam used for phases 3 and 8. Tests stub this to avoid
// invoking the real build script. The production value runs ./build.sh in
// the configured repo root.
var buildFn = defaultBuild

func defaultBuild(repoRoot string, out io.Writer) error {
	cmd := exec.Command("./build.sh")
	if repoRoot != "" {
		cmd.Dir = repoRoot
	}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build.sh: %w", err)
	}
	return nil
}

// ParseArgs parses the CLI positional arguments into a Config. Returns
// (_, true, nil) when help was requested or no args supplied.
func ParseArgs(args []string) (Config, bool, error) {
	if len(args) == 0 {
		return Config{}, true, nil
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		return Config{}, true, nil
	}
	positional := make([]string, 0, len(args))
	cfg := Config{}
	for _, arg := range args {
		if isHelpArg(arg) {
			return Config{}, false, errors.New("help flags must be used by themselves")
		}
		switch arg {
		case "--dry-run", "-n":
			cfg.DryRun = true
			continue
		case "--no-build":
			cfg.NoBuild = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return Config{}, false, fmt.Errorf("unsupported option %q; use -h, -?, --help, --dry-run, -n, or --no-build", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 2 {
		return Config{}, false, errors.New("usage: prep vX.Y.Z \"release message\" [--dry-run|-n] [--no-build]")
	}
	cfg.Version = strings.TrimSpace(positional[0])
	cfg.Message = strings.TrimSpace(positional[1])
	return cfg, false, nil
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "-?" || arg == "--help"
}

// Usage returns the formatted help text for the prep command.
func Usage() string {
	return `prep vX.Y.Z "release message" [--dry-run|-n] [--no-build]

Stages a release by bumping version constants, inserting a CHANGELOG row,
deleting completed AC files, and running validation builds before and after.

Flags:
  -h, -?, --help   show this help
  --dry-run, -n    print intended writes without modifying the working tree
  --no-build       skip the pre-check and post-check build invocations

Prints the canonical release command on success. Does not run the release
itself — present the printed command for the director to run.
`
}

// Run stages the release per the documented phases.
func Run(cfg Config) error {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}

	// Phase 1: validate inputs.
	if err := validateInputs(&cfg); err != nil {
		return fmt.Errorf("prep: %w", err)
	}

	// Phase 2: validate git state.
	if err := validateGitState(cfg.RepoRoot, cfg.Version); err != nil {
		return fmt.Errorf("prep: %w", err)
	}

	// Phase 3: pre-check build.
	if !cfg.DryRun && !cfg.NoBuild {
		fmt.Fprintln(cfg.Out, "prep: running pre-check build")
		if err := buildFn(cfg.RepoRoot, cfg.Out); err != nil {
			return fmt.Errorf("prep: pre-check build: %w", err)
		}
	}

	// Phase 4: detect version targets.
	versionTargets, multiUtilityWarning, err := detectVersionTargets(cfg.RepoRoot)
	if err != nil {
		return fmt.Errorf("prep: detect version targets: %w", err)
	}
	if multiUtilityWarning != "" {
		fmt.Fprintln(cfg.Out, multiUtilityWarning)
	}

	// Phase 5: detect CHANGELOG targets + fail-fast idempotency guard.
	changelogTargets, err := detectChangelogTargets(cfg.RepoRoot, cfg.Version)
	if err != nil {
		return fmt.Errorf("prep: detect CHANGELOG targets: %w", err)
	}

	// Phase 6: parse AC refs from message and locate files.
	acNums := parseACRefs(cfg.Message)
	acFiles, err := findACFiles(cfg.RepoRoot, acNums)
	if err != nil {
		return fmt.Errorf("prep: find AC files: %w", err)
	}

	versionStripped := strings.TrimPrefix(cfg.Version, "v")

	if cfg.DryRun {
		ieLines, err := findACPointerIELines(cfg.RepoRoot, acNums)
		if err != nil {
			return fmt.Errorf("prep: scan plan.md for AC-pointer IEs: %w", err)
		}
		printDryRun(cfg.Out, versionTargets, changelogTargets, versionStripped, cfg.Message, acFiles, ieLines)
		emitReleaseCommand(cfg.Out, cfg.Version, cfg.Message)
		return nil
	}

	// Phase 7a: apply version bumps.
	for _, t := range versionTargets {
		if err := applyVersionBump(t, versionStripped); err != nil {
			return fmt.Errorf("prep: bump %s: %w", t.path, err)
		}
	}

	// Phase 7b: insert CHANGELOG rows.
	for _, path := range changelogTargets {
		if err := applyChangelogInsert(path, versionStripped, cfg.Message); err != nil {
			return fmt.Errorf("prep: insert CHANGELOG row in %s: %w", path, err)
		}
	}

	// Phase 7c: delete AC files.
	for _, path := range acFiles {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("prep: delete %s: %w", path, err)
		}
		fmt.Fprintf(cfg.Out, "prep: deleted %s\n", path)
	}

	// Phase 7d: sweep AC-pointer IE lines from plan.md. Codifies the AGENTS.md
	// Project Rule: "Remove AC-pointer IEs when their AC ships and the file is
	// deleted at release prep — plan.md is not a historical record."
	ieLines, err := findACPointerIELines(cfg.RepoRoot, acNums)
	if err != nil {
		return fmt.Errorf("prep: scan plan.md for AC-pointer IEs: %w", err)
	}
	if err := removeACPointerIELines(cfg.RepoRoot, ieLines); err != nil {
		return fmt.Errorf("prep: sweep plan.md AC-pointer IEs: %w", err)
	}
	for _, line := range ieLines {
		fmt.Fprintf(cfg.Out, "prep: removed plan.md IE line: %s\n", strings.TrimSpace(line))
	}

	// Phase 8: post-check build.
	if !cfg.NoBuild {
		fmt.Fprintln(cfg.Out, "prep: running post-check build")
		if err := buildFn(cfg.RepoRoot, cfg.Out); err != nil {
			return fmt.Errorf("prep: post-check build: %w", err)
		}
	}

	// Phase 9: emit release command.
	emitReleaseCommand(cfg.Out, cfg.Version, cfg.Message)
	return nil
}

func validateInputs(cfg *Config) error {
	if !semverTagPattern.MatchString(cfg.Version) {
		return fmt.Errorf("version must match vMAJOR.MINOR.PATCH: %q", cfg.Version)
	}
	if cfg.Message == "" {
		return errors.New("message must be non-empty")
	}
	if len(cfg.Message) > maxMessageLen {
		return fmt.Errorf("message must be %d characters or fewer (got %d)", maxMessageLen, len(cfg.Message))
	}
	if cfg.RepoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		cfg.RepoRoot = cwd
	}
	return nil
}

func validateGitState(repoRoot, version string) error {
	// Must be inside a git work tree.
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify git repo: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "true" {
		return errors.New("not inside a git work tree")
	}

	// Tag must not already exist.
	cmd = exec.Command("git", "rev-parse", "-q", "--verify", "refs/tags/"+version)
	cmd.Dir = repoRoot
	if err := cmd.Run(); err == nil {
		return fmt.Errorf("tag %s already exists", version)
	}

	// Something must exist to release: HEAD ≠ latest tag, or dirty tree.
	latestTag, err := latestTagReachable(repoRoot)
	if err != nil {
		// No tags yet — any HEAD state is releasable.
		return nil
	}
	headEqualsTag, err := headEquals(repoRoot, latestTag)
	if err != nil {
		return fmt.Errorf("compare HEAD to %s: %w", latestTag, err)
	}
	if !headEqualsTag {
		return nil
	}
	dirty, err := workingTreeDirty(repoRoot)
	if err != nil {
		return fmt.Errorf("check working tree: %w", err)
	}
	if !dirty {
		return fmt.Errorf("nothing to release: HEAD is at %s and working tree is clean", latestTag)
	}
	return nil
}

func latestTagReachable(repoRoot string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func headEquals(repoRoot, ref string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	headOut, err := cmd.Output()
	if err != nil {
		return false, err
	}
	cmd = exec.Command("git", "rev-parse", ref)
	cmd.Dir = repoRoot
	refOut, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(headOut)) == strings.TrimSpace(string(refOut)), nil
}

func workingTreeDirty(repoRoot string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

type versionTarget struct {
	path string
	kind string // "programVersion", "TemplateVersion", "TEMPLATE_VERSION"
}

// parseModuleBasename returns the basename of the module path declared in
// repoRoot/go.mod (e.g., "governa" for `module github.com/queone/governa`).
// Returns "" when go.mod is missing, unreadable, or has no `module` line.
// Used by detectVersionTargets to apply the primary-cmd convention:
// cmd/<basename>/main.go is the primary binary and bumps with the repo;
// other cmd/*/main.go are secondaries with independent versioning.
func parseModuleBasename(repoRoot string) string {
	content, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "module ") && !strings.HasPrefix(trimmed, "module\t") {
			continue
		}
		modulePath := strings.TrimSpace(strings.TrimPrefix(trimmed, "module"))
		if modulePath == "" {
			return ""
		}
		return filepath.Base(modulePath)
	}
	return ""
}

// detectVersionTargets scans the repo for programVersion declarations and
// template-version targets. The programVersion scan applies a primary-cmd
// convention: if cmd/<module-basename>/main.go declares programVersion,
// that file is the primary and is bumped; other cmd/*/main.go are secondaries
// (independent versioning, never bumped by prep). When no primary exists, fall
// back to the historical auto-detect: 1 target → bump (single-utility repo);
// >1 → skip all with multi-utility warning (utils-style, per-utility-independent
// default). The skip avoids the clobber risk of bumping every utility to the
// repo release version.
func detectVersionTargets(repoRoot string) ([]versionTarget, string, error) {
	var targets []versionTarget
	var warning string

	// cmd/*/main.go scan for programVersion. Legitimate in both template and
	// consumer repos — always scanned. The programVersionRe regex matches
	// both inline (`const programVersion = "..."`) and grouped
	// (`const ( ... programVersion = "..." ... )`) forms.
	var pvTargets []versionTarget
	cmdDir := filepath.Join(repoRoot, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			mainPath := filepath.Join(cmdDir, entry.Name(), "main.go")
			content, readErr := os.ReadFile(mainPath)
			if readErr != nil {
				continue
			}
			if programVersionRe.Match(content) {
				pvTargets = append(pvTargets, versionTarget{path: mainPath, kind: "programVersion"})
			}
		}
	}

	// cmd/<module-basename>/main.go is the
	// primary binary and bumps with the repo. Other cmd/*/main.go are
	// secondaries — independent versioning, like utils-style multi-utility
	// repos. Only kicks in when go.mod is parseable AND the primary cmd
	// has programVersion; otherwise falls through to the historical
	// auto-detect (1 → bump, >1 → skip-all).
	basename := parseModuleBasename(repoRoot)
	var primaryTarget *versionTarget
	var secondaries []versionTarget
	if basename != "" {
		primaryPath := filepath.Join(repoRoot, "cmd", basename, "main.go")
		for i := range pvTargets {
			if pvTargets[i].path == primaryPath {
				t := pvTargets[i]
				primaryTarget = &t
			} else {
				secondaries = append(secondaries, pvTargets[i])
			}
		}
	}

	switch {
	case primaryTarget != nil:
		// Primary-cmd convention applies. Bump the primary; secondaries
		// are independent and skipped (announced when present).
		targets = append(targets, *primaryTarget)
		if len(secondaries) > 0 {
			paths := make([]string, len(secondaries))
			for i, t := range secondaries {
				paths[i] = t.path
			}
			warning = fmt.Sprintf("primary cmd/%s/main.go bumped; %d secondary programVersion target(s) skipped (independent versioning, each utility owns its own version per its own AC). Skipped: %s",
				basename, len(secondaries), strings.Join(paths, ", "))
		}
	case len(pvTargets) == 1:
		// Fallback: single utility, repo-tracked. Bump it.
		targets = append(targets, pvTargets[0])
	case len(pvTargets) > 1:
		// Fallback: multi-utility, no primary → skip all + warning.
		paths := make([]string, len(pvTargets))
		for i, t := range pvTargets {
			paths[i] = t.path
		}
		primaryHint := "no go.mod-derived primary cmd"
		if basename != "" {
			primaryHint = fmt.Sprintf("no primary cmd/%s/main.go", basename)
		}
		warning = fmt.Sprintf("multi-utility repo detected (%d programVersion targets, %s): per-utility programVersion bumps skipped (each utility owns its own version per its own AC). Skipped: %s",
			len(pvTargets), primaryHint, strings.Join(paths, ", "))
	}

	// Template-version targets (TEMPLATE_VERSION + internal/templates/version.go)
	// are gated on internal/templates/base/ presence. That directory exists only
	// in governa itself; consumer repos do not have this file.
	if info, err := os.Stat(filepath.Join(repoRoot, "internal", "templates", "base")); err == nil && info.IsDir() {
		tvPath := filepath.Join(repoRoot, "TEMPLATE_VERSION")
		if _, err := os.Stat(tvPath); err == nil {
			targets = append(targets, versionTarget{path: tvPath, kind: "TEMPLATE_VERSION"})
		}
		tvGoPath := filepath.Join(repoRoot, "internal", "templates", "version.go")
		if content, err := os.ReadFile(tvGoPath); err == nil {
			if templateConstRe.Match(content) {
				targets = append(targets, versionTarget{path: tvGoPath, kind: "TemplateVersion"})
			}
		}
	}

	sort.SliceStable(targets, func(i, j int) bool { return targets[i].path < targets[j].path })
	return targets, warning, nil
}

func detectChangelogTargets(repoRoot, version string) ([]string, error) {
	var targets []string
	versionStripped := strings.TrimPrefix(version, "v")

	candidates := []string{
		filepath.Join(repoRoot, "CHANGELOG.md"),
		filepath.Join(repoRoot, "internal", "templates", "CHANGELOG.md"),
	}
	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if changelogHasRow(string(content), versionStripped) {
			return nil, fmt.Errorf("%s already has a row for %s (prep is not idempotent on CHANGELOG)", path, versionStripped)
		}
		targets = append(targets, path)
	}
	return targets, nil
}

// changelogHasRow reports whether content has a row whose first column
// matches versionStripped (e.g. "0.37.0"). The canonical row shape is
// "| <version> | <summary> |" — we match on the opening column.
func changelogHasRow(content, versionStripped string) bool {
	marker := "| " + versionStripped + " |"
	return strings.Contains(content, marker)
}

func parseACRefs(message string) []int {
	matches := acRefRe.FindAllString(message, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int]bool)
	out := make([]int, 0, len(matches))
	for _, m := range matches {
		numStr := strings.TrimPrefix(m, "AC")
		var num int
		if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
			continue
		}
		if seen[num] {
			continue
		}
		seen[num] = true
		out = append(out, num)
	}
	sort.Ints(out)
	return out
}

// findACFiles locates the main per-AC files to delete: docs/ac<N>-<slug>.md
// for each AC number named in the release message. Critique and disposition
// content live inside the AC file (see docs/critique-protocol.md), so there
// are no separate companion files to enumerate. ac-template.md is always
// skipped.
func findACFiles(repoRoot string, acNums []int) ([]string, error) {
	if len(acNums) == 0 {
		return nil, nil
	}
	docsDir := filepath.Join(repoRoot, "docs")
	entries, readErr := os.ReadDir(docsDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil
		}
		return nil, readErr
	}
	wanted := make(map[int]bool, len(acNums))
	for _, n := range acNums {
		wanted[n] = true
	}
	var acFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "ac-template.md" {
			continue
		}
		m := acFileRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(m[1], "%d", &num); err != nil {
			continue
		}
		if !wanted[num] {
			continue
		}
		acFiles = append(acFiles, filepath.Join(docsDir, name))
	}
	sort.Strings(acFiles)
	return acFiles, nil
}

// findACPointerIELines reads plan.md and returns the IE lines whose
// AC-pointer arrow targets one of the given AC numbers. Returns (nil, nil)
// when plan.md is missing (consumer repos may not have one) or when no IE
// lines match. Read-only: callers use removeACPointerIELines to write.
func findACPointerIELines(repoRoot string, acNums []int) ([]string, error) {
	if len(acNums) == 0 {
		return nil, nil
	}
	planPath := filepath.Join(repoRoot, "plan.md")
	content, err := os.ReadFile(planPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	wanted := make(map[int]bool, len(acNums))
	for _, n := range acNums {
		wanted[n] = true
	}
	var matches []string
	for line := range strings.SplitSeq(string(content), "\n") {
		m := iePointerRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(m[1], "%d", &num); err != nil {
			continue
		}
		if wanted[num] {
			matches = append(matches, line)
		}
	}
	return matches, nil
}

// removeACPointerIELines re-reads plan.md and writes it back with the given
// lines removed. No-op when lines is empty. Callers obtain lines via
// findACPointerIELines, which already returns nil when plan.md is missing,
// so an empty-lines guard is sufficient — we never reach here without a
// readable plan.md. Idempotent: re-running with the same lines after the
// first sweep yields no further removals (matched lines are already gone).
func removeACPointerIELines(repoRoot string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	planPath := filepath.Join(repoRoot, "plan.md")
	content, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	drop := make(map[string]bool, len(lines))
	for _, line := range lines {
		drop[line] = true
	}
	src := strings.Split(string(content), "\n")
	keep := make([]string, 0, len(src))
	for _, line := range src {
		if drop[line] {
			continue
		}
		keep = append(keep, line)
	}
	return os.WriteFile(planPath, []byte(strings.Join(keep, "\n")), 0o644)
}

func applyVersionBump(t versionTarget, versionStripped string) error {
	switch t.kind {
	case "TEMPLATE_VERSION":
		content, err := os.ReadFile(t.path)
		if err != nil {
			return err
		}
		current := strings.TrimSpace(string(content))
		if current == versionStripped {
			return nil // no-op idempotent
		}
		return os.WriteFile(t.path, []byte(versionStripped+"\n"), 0o644)
	case "programVersion":
		return replaceVersionConstant(t.path, programVersionRe, versionStripped)
	case "TemplateVersion":
		return replaceVersionConstant(t.path, templateConstRe, versionStripped)
	default:
		return fmt.Errorf("unknown version target kind: %s", t.kind)
	}
}

func replaceVersionConstant(path string, re *regexp.Regexp, versionStripped string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m := re.FindSubmatch(content)
	if m == nil {
		return fmt.Errorf("no version constant matched in %s", path)
	}
	if string(m[2]) == versionStripped {
		return nil // no-op idempotent
	}
	replacement := string(m[1]) + "\"" + versionStripped + "\""
	updated := re.ReplaceAll(content, []byte(replacement))
	return os.WriteFile(path, updated, 0o644)
}

func applyChangelogInsert(path, versionStripped, message string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")
	unreleasedIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "| Unreleased |") {
			unreleasedIdx = i
			break
		}
	}
	if unreleasedIdx < 0 {
		return fmt.Errorf("%s has no | Unreleased | row", path)
	}
	newRow := fmt.Sprintf("| %s | %s |", versionStripped, message)
	// Insert immediately after the Unreleased row.
	updated := make([]string, 0, len(lines)+1)
	updated = append(updated, lines[:unreleasedIdx+1]...)
	updated = append(updated, newRow)
	updated = append(updated, lines[unreleasedIdx+1:]...)
	return os.WriteFile(path, []byte(strings.Join(updated, "\n")), 0o644)
}

func emitReleaseCommand(out io.Writer, version, message string) {
	fmt.Fprintf(out, "\nrelease command:\n  ./build.sh %s %q\n", version, message)
}

func printDryRun(out io.Writer, versionTargets []versionTarget, changelogTargets []string, versionStripped, message string, acFiles, ieLines []string) {
	fmt.Fprintln(out, "\n--- dry run (no writes) ---")
	fmt.Fprintln(out, "version bumps:")
	for _, t := range versionTargets {
		fmt.Fprintf(out, "  %s → %s (%s)\n", t.path, versionStripped, t.kind)
	}
	fmt.Fprintln(out, "CHANGELOG inserts:")
	for _, path := range changelogTargets {
		fmt.Fprintf(out, "  %s: | %s | %s |\n", path, versionStripped, message)
	}
	fmt.Fprintln(out, "AC deletions:")
	for _, p := range acFiles {
		fmt.Fprintf(out, "  delete %s\n", p)
	}
	fmt.Fprintln(out, "plan.md AC-pointer IE removals:")
	for _, line := range ieLines {
		fmt.Fprintf(out, "  remove: %s\n", strings.TrimSpace(line))
	}
	fmt.Fprintln(out, "--- end dry run ---")
}
