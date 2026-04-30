// Package preptool stages a release: bumps version constants, inserts a
// CHANGELOG row, deletes completed AC files (plus -critique.md and
// -dispositions.md companions), runs validation builds around the write
// phases, and prints the canonical release command. It does not run the
// release itself; that remains the director's explicit approval via cmd/rel.
// (AC60)
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

// Run stages the release per the phases documented in AC60.
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
	acFiles, critiqueFiles, dispFiles, err := findACCompanions(cfg.RepoRoot, acNums)
	if err != nil {
		return fmt.Errorf("prep: find AC companions: %w", err)
	}

	// AC78: feedback-credit validation retired. Release messages no longer
	// carry feedback markers; any such text in historical AC files is ignored.

	versionStripped := strings.TrimPrefix(cfg.Version, "v")

	if cfg.DryRun {
		printDryRun(cfg.Out, versionTargets, changelogTargets, versionStripped, cfg.Message, acFiles, critiqueFiles, dispFiles)
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

	// Phase 7c: delete AC + companion files.
	for _, path := range acFiles {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("prep: delete %s: %w", path, err)
		}
		fmt.Fprintf(cfg.Out, "prep: deleted %s\n", path)
	}
	for _, path := range critiqueFiles {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("prep: delete %s: %w", path, err)
		}
		fmt.Fprintf(cfg.Out, "prep: deleted %s\n", path)
	}
	for _, path := range dispFiles {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("prep: delete %s: %w", path, err)
		}
		fmt.Fprintf(cfg.Out, "prep: deleted %s\n", path)
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

// detectVersionTargets scans the repo for programVersion declarations and
// template-version targets. The programVersion scan applies a safe auto-detect
// filter: if exactly one cmd/*/main.go declares programVersion, it is bumped
// (single-utility repo, repo-tracked behavior). If multiple declare it, all
// are dropped from the target list (multi-utility repo, per-utility-independent
// behavior — each utility owns its own version per its own AC). The skip is
// announced via the returned warning, which the caller logs.
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
	// Safe auto-detect: 1 target → bump (repo-tracked); >1 targets → skip all
	// (multi-utility repo, per-utility-independent default). The skip avoids
	// the clobber risk of bumping every utility to the repo release version.
	switch {
	case len(pvTargets) == 1:
		targets = append(targets, pvTargets[0])
	case len(pvTargets) > 1:
		paths := make([]string, len(pvTargets))
		for i, t := range pvTargets {
			paths[i] = t.path
		}
		warning = fmt.Sprintf("multi-utility repo detected (%d programVersion targets): per-utility programVersion bumps skipped (each utility owns its own version per its own AC). Skipped: %s",
			len(pvTargets), strings.Join(paths, ", "))
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

// findACCompanions locates per-AC files to act on. For each AC number,
// finds the main AC file (docs/ac<N>-<slug>.md excluding companion suffixes),
// its -critique.md and -dispositions.md companions. -feedback.md companions
// are no longer a convention post-AC78 and are not enumerated.
func findACCompanions(repoRoot string, acNums []int) (acFiles, critiqueFiles, dispFiles []string, err error) {
	if len(acNums) == 0 {
		return nil, nil, nil, nil
	}
	docsDir := filepath.Join(repoRoot, "docs")
	entries, readErr := os.ReadDir(docsDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, readErr
	}
	wanted := make(map[int]bool, len(acNums))
	for _, n := range acNums {
		wanted[n] = true
	}
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
		full := filepath.Join(docsDir, name)
		switch {
		case strings.HasSuffix(name, "-critique.md"):
			critiqueFiles = append(critiqueFiles, full)
		case strings.HasSuffix(name, "-dispositions.md"):
			dispFiles = append(dispFiles, full)
		case strings.HasSuffix(name, "-feedback.md"):
			// AC78: retired convention. Ignore silently.
			continue
		default:
			acFiles = append(acFiles, full)
		}
	}
	sort.Strings(acFiles)
	sort.Strings(critiqueFiles)
	sort.Strings(dispFiles)
	return acFiles, critiqueFiles, dispFiles, nil
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

func printDryRun(out io.Writer, versionTargets []versionTarget, changelogTargets []string, versionStripped, message string, acFiles, critiqueFiles, dispFiles []string) {
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
	for _, p := range critiqueFiles {
		fmt.Fprintf(out, "  delete %s (-critique companion)\n", p)
	}
	for _, p := range dispFiles {
		fmt.Fprintf(out, "  delete %s (-dispositions companion)\n", p)
	}
	fmt.Fprintln(out, "--- end dry run ---")
}
