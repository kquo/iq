package preptool

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubNoopBuild replaces buildFn with a no-op that counts invocations.
// Not parallel-safe.
func stubNoopBuild(t *testing.T) *int {
	t.Helper()
	orig := buildFn
	calls := 0
	buildFn = func(string, io.Writer) error {
		calls++
		return nil
	}
	t.Cleanup(func() { buildFn = orig })
	return &calls
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// gitInitFixture creates a throwaway git repo at dir with an initial commit.
// Tests that do not need a clean HEAD (most) should make further edits on top
// so the working tree has uncommitted changes when Run is called.
func gitInitFixture(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, string(out))
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("config", "commit.gpgsign", "false")
	// Seed file so we can make a commit.
	mustWrite(t, filepath.Join(dir, "seed.txt"), "seed\n")
	run("add", ".")
	run("commit", "-m", "seed")
}

// AT1: invalid semver inputs are rejected before any filesystem or git access.
func TestPrepValidatesVersion(t *testing.T) {
	cases := []string{"", "v1", "1.0.0", "vX.Y.Z", "v1.0", "1.0.0.0"}
	for _, bad := range cases {
		cfg := Config{Version: bad, Message: "m", RepoRoot: t.TempDir()}
		if err := Run(cfg); err == nil {
			t.Errorf("Version %q: expected error, got nil", bad)
		}
	}
}

// AT2: message length validation.
func TestPrepValidatesMessage(t *testing.T) {
	cfg := Config{Version: "v1.0.0", Message: strings.Repeat("x", 81), RepoRoot: t.TempDir()}
	if err := Run(cfg); err == nil {
		t.Error("81-char message: expected error, got nil")
	}
	cfg.Message = ""
	if err := Run(cfg); err == nil {
		t.Error("empty message: expected error, got nil")
	}
}

// AT3: rejected only when HEAD == latest tag AND tree is clean.
func TestPrepErrorsWhenNothingToRelease(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	// Tag the seed commit as v1.0.0.
	cmd := exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v: %s", err, string(out))
	}

	cfg := Config{Version: "v2.0.0", Message: "next", RepoRoot: dir, NoBuild: true}
	err := Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "nothing to release") {
		t.Fatalf("expected 'nothing to release' error, got: %v", err)
	}

	// Dirty the tree and re-run — should proceed past phase 2.
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "1.0.0\n")
	// Still no CHANGELOG — should fail at a later phase, not phase 2.
	err = Run(cfg)
	if err != nil && strings.Contains(err.Error(), "nothing to release") {
		t.Fatalf("dirty tree should pass phase 2, got: %v", err)
	}
}

// AT4: existing tag for target version is rejected.
func TestPrepErrorsOnExistingTag(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	cmd := exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v: %s", err, string(out))
	}
	// Dirty the tree so phase 2's "nothing to release" is not triggered first.
	mustWrite(t, filepath.Join(dir, "dirty.txt"), "x")

	cfg := Config{Version: "v1.0.0", Message: "m", RepoRoot: dir, NoBuild: true}
	err := Run(cfg)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

// AT5: programVersion detection covers single-utility and multi-utility cases.
// Single-utility (1 target) is bumped (repo-tracked). Multi-utility (>1 targets)
// is skipped per the safe auto-detect: per-utility independence is the default
// when ambiguous, avoiding the clobber risk of bumping every utility to the
// repo release version. Binaries without a programVersion constant are ignored.
func TestPrepDetectsProgramVersionConstants(t *testing.T) {
	t.Run("single utility is bumped", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "cmd", "foo", "main.go"),
			"package main\n\nconst programVersion = \"0.1.0\"\n\nfunc main() {}\n")
		mustWrite(t, filepath.Join(dir, "cmd", "bar", "main.go"),
			"package main\n\nfunc main() {}\n") // no programVersion

		targets, warning, err := detectVersionTargets(dir)
		if err != nil {
			t.Fatalf("detectVersionTargets: %v", err)
		}
		if warning != "" {
			t.Errorf("single-utility case: unexpected warning %q", warning)
		}
		var paths []string
		for _, tgt := range targets {
			if tgt.kind == "programVersion" {
				paths = append(paths, tgt.path)
			}
		}
		want := []string{filepath.Join(dir, "cmd", "foo", "main.go")}
		if len(paths) != 1 || paths[0] != want[0] {
			t.Fatalf("programVersion targets = %v, want %v (bar/main.go must not appear)", paths, want)
		}
	})

	t.Run("multi-utility is skipped with warning", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "cmd", "foo", "main.go"),
			"package main\n\nconst programVersion = \"0.1.0\"\n\nfunc main() {}\n")
		mustWrite(t, filepath.Join(dir, "cmd", "baz", "main.go"),
			"package main\n\nconst programVersion = \"0.2.0\"\n\nfunc main() {}\n")

		targets, warning, err := detectVersionTargets(dir)
		if err != nil {
			t.Fatalf("detectVersionTargets: %v", err)
		}
		for _, tgt := range targets {
			if tgt.kind == "programVersion" {
				t.Errorf("multi-utility case: programVersion target %s leaked through (must be skipped)", tgt.path)
			}
		}
		if !strings.Contains(warning, "multi-utility") {
			t.Errorf("multi-utility case: warning must mention multi-utility; got %q", warning)
		}
	})

	t.Run("grouped const form is matched", func(t *testing.T) {
		dir := t.TempDir()
		groupedSrc := "package main\n\nconst (\n" +
			"\tprogramName    = \"foo\"\n" +
			"\tprogramVersion = \"1.0.0\"\n" +
			")\n\nfunc main() {}\n"
		mustWrite(t, filepath.Join(dir, "cmd", "foo", "main.go"), groupedSrc)

		targets, warning, err := detectVersionTargets(dir)
		if err != nil {
			t.Fatalf("detectVersionTargets: %v", err)
		}
		if warning != "" {
			t.Errorf("single-utility grouped form: unexpected warning %q", warning)
		}
		var found bool
		for _, tgt := range targets {
			if tgt.kind == "programVersion" {
				found = true
			}
		}
		if !found {
			t.Fatal("grouped const form: programVersion target not detected (regex must match block form)")
		}
	})

	t.Run("grouped const form is bumped end-to-end", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "cmd", "foo", "main.go")
		groupedSrc := "package main\n\nconst (\n" +
			"\tprogramName    = \"foo\"\n" +
			"\tprogramVersion = \"1.0.0\"\n" +
			")\n\nfunc main() {}\n"
		mustWrite(t, mainPath, groupedSrc)

		targets, _, err := detectVersionTargets(dir)
		if err != nil {
			t.Fatalf("detectVersionTargets: %v", err)
		}
		for _, tgt := range targets {
			if tgt.kind != "programVersion" {
				continue
			}
			if err := applyVersionBump(tgt, "9.9.9"); err != nil {
				t.Fatalf("applyVersionBump: %v", err)
			}
		}
		updated, err := os.ReadFile(mainPath)
		if err != nil {
			t.Fatalf("read after bump: %v", err)
		}
		if !strings.Contains(string(updated), `programVersion = "9.9.9"`) {
			t.Errorf("grouped const not bumped: %s", string(updated))
		}
		if strings.Contains(string(updated), `programVersion = "1.0.0"`) {
			t.Errorf("old version still present: %s", string(updated))
		}
	})
}

// AT6: TEMPLATE_VERSION + TemplateVersion detection is presence-gated.
func TestPrepDetectsTemplateVersionFiles(t *testing.T) {
	dir := t.TempDir()
	targets, _, err := detectVersionTargets(dir)
	if err != nil {
		t.Fatalf("detectVersionTargets (empty): %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("empty repo: expected no targets, got %v", targets)
	}

	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "version.go"),
		"package templates\n\nconst TemplateVersion = \"0.1.0\"\n")
	// AC62: template-version detection is gated on internal/templates/base/ presence.
	// This fixture represents the governa-the-template-repo path.
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")

	targets, _, err = detectVersionTargets(dir)
	if err != nil {
		t.Fatalf("detectVersionTargets: %v", err)
	}
	kinds := map[string]bool{}
	for _, t := range targets {
		kinds[t.kind] = true
	}
	if !kinds["TEMPLATE_VERSION"] {
		t.Errorf("TEMPLATE_VERSION not detected")
	}
	if !kinds["TemplateVersion"] {
		t.Errorf("TemplateVersion not detected")
	}
}

// AC62 AT1: consumer repo (no internal/templates/base/) — TEMPLATE_VERSION
// and internal/templates/version.go must NOT be picked up as bump targets.
// Simulates skout's v0.44.1 release-prep scenario.
func TestPrepSkipsTemplateVersionOnConsumerRepo(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.38.0\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "version.go"),
		"package templates\n\nconst TemplateVersion = \"0.38.0\"\n")
	// Consumer scenario: no internal/templates/base/ directory.

	targets, _, err := detectVersionTargets(dir)
	if err != nil {
		t.Fatalf("detectVersionTargets: %v", err)
	}
	for _, tgt := range targets {
		if tgt.kind == "TEMPLATE_VERSION" {
			t.Errorf("consumer repo: TEMPLATE_VERSION must not be detected, got %v", tgt)
		}
		if tgt.kind == "TemplateVersion" {
			t.Errorf("consumer repo: TemplateVersion must not be detected, got %v", tgt)
		}
	}
}

// AC62 AT2: template repo (internal/templates/base/ present) — both
// TEMPLATE_VERSION and TemplateVersion are detected. Governa-case regression guard.
func TestPrepDetectsTemplateVersionOnTemplateRepo(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "version.go"),
		"package templates\n\nconst TemplateVersion = \"0.1.0\"\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")

	targets, _, err := detectVersionTargets(dir)
	if err != nil {
		t.Fatalf("detectVersionTargets: %v", err)
	}
	kinds := map[string]bool{}
	for _, tgt := range targets {
		kinds[tgt.kind] = true
	}
	if !kinds["TEMPLATE_VERSION"] {
		t.Errorf("template repo: TEMPLATE_VERSION not detected")
	}
	if !kinds["TemplateVersion"] {
		t.Errorf("template repo: TemplateVersion not detected")
	}
}

// AC62 AT3: cmd/*/main.go programVersion detection is orthogonal to the
// template-marker gate — detected in both consumer and template repos.
func TestPrepDetectsProgramVersionBothRepoKinds(t *testing.T) {
	t.Run("consumer (no base/)", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "cmd", "foo", "main.go"),
			"package main\n\nconst programVersion = \"0.1.0\"\n\nfunc main() {}\n")
		targets, _, _ := detectVersionTargets(dir)
		found := false
		for _, tgt := range targets {
			if tgt.kind == "programVersion" {
				found = true
			}
		}
		if !found {
			t.Error("programVersion must be detected in consumer repo")
		}
	})
	t.Run("template (with base/)", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "cmd", "foo", "main.go"),
			"package main\n\nconst programVersion = \"0.1.0\"\n\nfunc main() {}\n")
		mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")
		targets, _, _ := detectVersionTargets(dir)
		found := false
		for _, tgt := range targets {
			if tgt.kind == "programVersion" {
				found = true
			}
		}
		if !found {
			t.Error("programVersion must be detected in template repo")
		}
	})
}

// AT7: version bumps rewrite the constant; idempotent on already-matching.
func TestPrepBumpsVersionConstants(t *testing.T) {
	dir := t.TempDir()
	tvPath := filepath.Join(dir, "TEMPLATE_VERSION")
	mustWrite(t, tvPath, "0.1.0\n")
	mainPath := filepath.Join(dir, "cmd", "foo", "main.go")
	mustWrite(t, mainPath,
		"package main\n\nconst programVersion = \"0.1.0\"\n\nfunc main() {}\n")
	tvGo := filepath.Join(dir, "internal", "templates", "version.go")
	mustWrite(t, tvGo, "package templates\n\nconst TemplateVersion = \"0.1.0\"\n")
	// AC62: template-version detection gated on internal/templates/base/ presence.
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")

	targets, _, _ := detectVersionTargets(dir)
	for _, t2 := range targets {
		if err := applyVersionBump(t2, "0.2.0"); err != nil {
			t.Fatalf("applyVersionBump %s: %v", t2.path, err)
		}
	}

	readTrim := func(p string) string {
		b, _ := os.ReadFile(p)
		return strings.TrimSpace(string(b))
	}
	if got := readTrim(tvPath); got != "0.2.0" {
		t.Errorf("TEMPLATE_VERSION after bump = %q, want 0.2.0", got)
	}
	if !strings.Contains(string(mustRead(t, mainPath)), `programVersion = "0.2.0"`) {
		t.Errorf("programVersion not bumped in %s", mainPath)
	}
	if !strings.Contains(string(mustRead(t, tvGo)), `TemplateVersion = "0.2.0"`) {
		t.Errorf("TemplateVersion not bumped in %s", tvGo)
	}

	// Idempotent second call: file should already match, no-op write is fine.
	for _, t2 := range targets {
		if err := applyVersionBump(t2, "0.2.0"); err != nil {
			t.Fatalf("idempotent applyVersionBump %s: %v", t2.path, err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// AT8: CHANGELOG row inserted under | Unreleased | row.
func TestPrepInsertsChangelogRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CHANGELOG.md")
	mustWrite(t, path, "# Changelog\n\n| Version | Summary |\n|---------|---------|\n| Unreleased | |\n| 0.1.0 | first |\n")
	if err := applyChangelogInsert(path, "0.2.0", "second release"); err != nil {
		t.Fatalf("applyChangelogInsert: %v", err)
	}
	content := string(mustRead(t, path))
	want := "| Unreleased | |\n| 0.2.0 | second release |\n| 0.1.0 | first |\n"
	if !strings.Contains(content, want) {
		t.Fatalf("CHANGELOG missing new row.\nwant substring:\n%s\ngot:\n%s", want, content)
	}
}

// AT9: AC files named in the message are deleted; ac-template.md and other
// AC numbers are untouched.
func TestPrepDeletesNamedACFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "docs", "ac60-prep-tool.md"), "# AC60\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac61-other.md"), "# AC61\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac-template.md"), "# template\n")

	acNums := parseACRefs("AC60: prep tool")
	acFiles, _, _, err := findACCompanions(dir, acNums)
	if err != nil {
		t.Fatalf("findACCompanions: %v", err)
	}
	if len(acFiles) != 1 || !strings.HasSuffix(acFiles[0], "ac60-prep-tool.md") {
		t.Fatalf("single-AC: expected only ac60-prep-tool.md, got %v", acFiles)
	}

	// Composite message.
	acNums = parseACRefs("AC60+AC61: bundle")
	acFiles, _, _, err = findACCompanions(dir, acNums)
	if err != nil {
		t.Fatalf("findACCompanions composite: %v", err)
	}
	if len(acFiles) != 2 {
		t.Fatalf("composite: expected 2 AC files, got %v", acFiles)
	}
	for _, f := range acFiles {
		if strings.HasSuffix(f, "ac-template.md") {
			t.Fatalf("ac-template.md must never be included, got %v", acFiles)
		}
	}
}

// AT9b: -critique.md and -dispositions.md companions are deleted alongside AC.
func TestPrepDeletesCritiqueAndDispositionsCompanions(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "docs", "ac60-prep-tool.md"), "# AC60\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac60-prep-tool-critique.md"), "crit\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac60-prep-tool-dispositions.md"), "disp\n")

	acNums := parseACRefs("AC60: prep")
	ac, crit, disp, err := findACCompanions(dir, acNums)
	if err != nil {
		t.Fatalf("findACCompanions: %v", err)
	}
	if len(ac) != 1 {
		t.Fatalf("ac files = %v", ac)
	}
	if len(crit) != 1 {
		t.Fatalf("critique files = %v", crit)
	}
	if len(disp) != 1 {
		t.Fatalf("dispositions files = %v", disp)
	}
}

// AT11: DryRun skips phases 3, 7, 8 and prints the intended writes.
func TestPrepDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	tvPath := filepath.Join(dir, "TEMPLATE_VERSION")
	mustWrite(t, tvPath, "0.1.0\n")
	// AC62: TEMPLATE_VERSION detection gated on internal/templates/base/ presence.
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")
	chPath := filepath.Join(dir, "CHANGELOG.md")
	mustWrite(t, chPath, "# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")
	acPath := filepath.Join(dir, "docs", "ac60-x.md")
	mustWrite(t, acPath, "# AC60\n")

	buildCalls := stubNoopBuild(t)

	var buf bytes.Buffer
	cfg := Config{Version: "v0.2.0", Message: "AC60: x", RepoRoot: dir, DryRun: true, Out: &buf}
	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if *buildCalls != 0 {
		t.Errorf("DryRun: buildFn should not be invoked, got %d calls", *buildCalls)
	}
	// Files untouched.
	if got := strings.TrimSpace(string(mustRead(t, tvPath))); got != "0.1.0" {
		t.Errorf("TEMPLATE_VERSION modified: %q", got)
	}
	if _, err := os.Stat(acPath); err != nil {
		t.Errorf("AC file should still exist: %v", err)
	}
	// Dry-run output lists intended writes.
	out := buf.String()
	for _, want := range []string{"dry run", "TEMPLATE_VERSION", "CHANGELOG", "delete", "release command"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q. got:\n%s", want, out)
		}
	}
}

// AT12: release command is emitted as the final non-empty lines.
func TestPrepPrintsReleaseCommand(t *testing.T) {
	var buf bytes.Buffer
	emitReleaseCommand(&buf, "v0.2.0", "the message")
	out := buf.String()
	if !strings.Contains(out, "./build.sh v0.2.0 \"the message\"") {
		t.Fatalf("expected release command in output, got: %q", out)
	}
}

// AC95 AT2: exact labeled-block shape emitted by emitReleaseCommand.
func TestEmitReleaseCommandExactShape(t *testing.T) {
	var buf bytes.Buffer
	emitReleaseCommand(&buf, "v1.2.3", "AC95: template integrity")
	got := buf.String()
	want := "\nrelease command:\n  ./build.sh v1.2.3 \"AC95: template integrity\"\n"
	if got != want {
		t.Fatalf("emitReleaseCommand shape mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

// AT13: --no-build skips phases 3 and 8.
func TestPrepNoBuildFlagSkipsBuilds(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	mustWrite(t, filepath.Join(dir, "CHANGELOG.md"),
		"# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")

	buildCalls := stubNoopBuild(t)
	cfg := Config{Version: "v0.2.0", Message: "m", RepoRoot: dir, NoBuild: true, Out: &bytes.Buffer{}}
	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if *buildCalls != 0 {
		t.Errorf("NoBuild: buildFn should not be invoked, got %d calls", *buildCalls)
	}
}

// AT13b: buildFn is invoked twice on default flags, pre-check error skips phase 7.
func TestPrepBuildFnInvokedAndErrorPropagates(t *testing.T) {
	// Happy path: buildFn invoked in both phase 3 and phase 8.
	dir := t.TempDir()
	gitInitFixture(t, dir)
	mustWrite(t, filepath.Join(dir, "CHANGELOG.md"),
		"# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")
	buildCalls := stubNoopBuild(t)
	cfg := Config{Version: "v0.2.0", Message: "m", RepoRoot: dir, Out: &bytes.Buffer{}}
	if err := Run(cfg); err != nil {
		t.Fatalf("happy path Run: %v", err)
	}
	if *buildCalls != 2 {
		t.Errorf("expected 2 buildFn calls, got %d", *buildCalls)
	}

	// Pre-check error: phase 7 writes must not happen.
	dir2 := t.TempDir()
	gitInitFixture(t, dir2)
	tvPath := filepath.Join(dir2, "TEMPLATE_VERSION")
	mustWrite(t, tvPath, "0.1.0\n")
	mustWrite(t, filepath.Join(dir2, "CHANGELOG.md"),
		"# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")

	orig := buildFn
	t.Cleanup(func() { buildFn = orig })
	buildFn = func(string, io.Writer) error {
		return &prepTestError{"pre-check failed"}
	}

	cfg2 := Config{Version: "v0.2.0", Message: "m", RepoRoot: dir2, Out: &bytes.Buffer{}}
	err := Run(cfg2)
	if err == nil || !strings.Contains(err.Error(), "pre-check") {
		t.Fatalf("expected pre-check error wrapping, got: %v", err)
	}
	// TEMPLATE_VERSION untouched (phase 7a did not run).
	if got := strings.TrimSpace(string(mustRead(t, tvPath))); got != "0.1.0" {
		t.Errorf("phase 7a ran despite pre-check failure: TEMPLATE_VERSION = %q", got)
	}
}

type prepTestError struct{ msg string }

func (e *prepTestError) Error() string { return e.msg }

// AT13c: CHANGELOG idempotency guard fails fast with zero writes.
func TestPrepIdempotencyGuardFailsFast(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	tvPath := filepath.Join(dir, "TEMPLATE_VERSION")
	mustWrite(t, tvPath, "0.1.0\n")
	chPath := filepath.Join(dir, "CHANGELOG.md")
	mustWrite(t, chPath,
		"# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.2.0 | already here |\n| 0.1.0 | first |\n")

	stubNoopBuild(t)
	var buf bytes.Buffer
	cfg := Config{Version: "v0.2.0", Message: "m", RepoRoot: dir, Out: &buf}
	err := Run(cfg)
	if err == nil {
		t.Fatal("expected idempotency error, got nil")
	}
	if !strings.Contains(err.Error(), "already has a row for 0.2.0") {
		t.Errorf("error should name the existing row; got: %v", err)
	}
	// Disk untouched (TEMPLATE_VERSION still 0.1.0, CHANGELOG unchanged).
	if got := strings.TrimSpace(string(mustRead(t, tvPath))); got != "0.1.0" {
		t.Errorf("TEMPLATE_VERSION modified despite idempotency guard: %q", got)
	}
	chAfter := string(mustRead(t, chPath))
	if strings.Count(chAfter, "| 0.2.0 |") != 1 {
		t.Errorf("CHANGELOG modified despite guard:\n%s", chAfter)
	}
}

// ParseArgs coverage.
func TestParseArgs(t *testing.T) {
	// Help cases.
	_, help, err := ParseArgs(nil)
	if err != nil || !help {
		t.Errorf("nil args: want help=true, got help=%v err=%v", help, err)
	}
	_, help, err = ParseArgs([]string{"-h"})
	if err != nil || !help {
		t.Errorf("-h: want help=true, got help=%v err=%v", help, err)
	}
	// Flag + positional.
	cfg, help, err := ParseArgs([]string{"--dry-run", "v1.0.0", "msg"})
	if err != nil || help {
		t.Fatalf("dry-run: err=%v help=%v", err, help)
	}
	if !cfg.DryRun || cfg.Version != "v1.0.0" || cfg.Message != "msg" {
		t.Errorf("parsed cfg = %+v", cfg)
	}
	cfg, _, err = ParseArgs([]string{"v1.0.0", "msg", "--no-build"})
	if err != nil {
		t.Fatalf("--no-build: %v", err)
	}
	if !cfg.NoBuild {
		t.Errorf("NoBuild should be true, got false")
	}
	// Unknown flag.
	if _, _, err := ParseArgs([]string{"--unknown", "v1.0.0", "m"}); err == nil {
		t.Error("unknown flag: expected error")
	}
	// Wrong positional count.
	if _, _, err := ParseArgs([]string{"v1.0.0"}); err == nil {
		t.Error("single positional: expected error")
	}
}

// changelogHasRow correctness.
func TestChangelogHasRow(t *testing.T) {
	content := "| 0.2.0 | rel |\n| 0.1.0 | first |\n"
	if !changelogHasRow(content, "0.2.0") {
		t.Error("should match existing row")
	}
	if changelogHasRow(content, "0.3.0") {
		t.Error("should not match missing version")
	}
	// Guard against prefix match: "0.1" should not match "0.1.0".
	if changelogHasRow(content, "0.1") {
		t.Error("0.1 must not match 0.1.0 row (pipe delimiter required)")
	}
}

// parseACRefs correctness (dedupe + sort + composite).
func TestParseACRefs(t *testing.T) {
	got := parseACRefs("AC60+AC61: bundle")
	if len(got) != 2 || got[0] != 60 || got[1] != 61 {
		t.Errorf("AC60+AC61: got %v", got)
	}
	got = parseACRefs("AC60: simple")
	if len(got) != 1 || got[0] != 60 {
		t.Errorf("AC60: got %v", got)
	}
	got = parseACRefs("AC60, AC60 duplicate")
	if len(got) != 1 || got[0] != 60 {
		t.Errorf("duplicate AC60: got %v", got)
	}
	if got := parseACRefs("no refs here"); got != nil {
		t.Errorf("no refs: got %v", got)
	}
}

// Usage text is non-empty and mentions the canonical invocation shape.
func TestUsage(t *testing.T) {
	t.Parallel()
	u := Usage()
	if len(u) == 0 {
		t.Fatal("Usage() returned empty string")
	}
	for _, want := range []string{"prep vX.Y.Z", "--dry-run", "--no-build", "--help"} {
		if !strings.Contains(u, want) {
			t.Errorf("Usage() missing %q. got:\n%s", want, u)
		}
	}
}
