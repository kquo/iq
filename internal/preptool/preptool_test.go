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
	// template-version detection is gated on internal/templates/base/ presence.
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

// consumer repo (no internal/templates/base/) — TEMPLATE_VERSION
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

// template repo (internal/templates/base/ present) — both
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

// cmd/*/main.go programVersion detection is orthogonal to the
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
	// template-version detection gated on internal/templates/base/ presence.
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
	acFiles, err := findACFiles(dir, acNums)
	if err != nil {
		t.Fatalf("findACFiles: %v", err)
	}
	if len(acFiles) != 1 || !strings.HasSuffix(acFiles[0], "ac60-prep-tool.md") {
		t.Fatalf("single-AC: expected only ac60-prep-tool.md, got %v", acFiles)
	}

	// Composite message.
	acNums = parseACRefs("AC60+AC61: bundle")
	acFiles, err = findACFiles(dir, acNums)
	if err != nil {
		t.Fatalf("findACFiles composite: %v", err)
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

// AT11: DryRun skips phases 3, 7, 8 and prints the intended writes.
func TestPrepDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	tvPath := filepath.Join(dir, "TEMPLATE_VERSION")
	mustWrite(t, tvPath, "0.1.0\n")
	// TEMPLATE_VERSION detection gated on internal/templates/base/ presence.
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

// exact labeled-block shape emitted by emitReleaseCommand.
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
		t.Errorf("composite AC ref test: got %v", got)
	}
	got = parseACRefs("AC60: simple")
	if len(got) != 1 || got[0] != 60 {
		t.Errorf("simple AC ref test: got %v", got)
	}
	got = parseACRefs("AC60, AC60 duplicate")
	if len(got) != 1 || got[0] != 60 {
		t.Errorf("duplicate AC ref test: got %v", got)
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

// Phase 7d: prep sweeps an AC-pointer IE line from plan.md when the AC it
// points at is being deleted.
func TestSweepACPointerIE_SingleACMatch(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	mustWrite(t, planPath, "# Plan\n\n## Ideas To Explore\n\nIE14: prep IE sweep → docs/ac14-prep-ie-sweep.md\nIE15: keeper → docs/ac15-keeper.md\n")

	acNums := parseACRefs("AC14: prep IE sweep")
	matches, err := findACPointerIELines(dir, acNums)
	if err != nil {
		t.Fatalf("findACPointerIELines: %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "IE14:") {
		t.Fatalf("expected only IE14 match, got %v", matches)
	}
	if err := removeACPointerIELines(dir, matches); err != nil {
		t.Fatalf("removeACPointerIELines: %v", err)
	}
	got, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	if strings.Contains(string(got), "IE14:") {
		t.Errorf("plan.md still contains IE14 line after sweep:\n%s", string(got))
	}
	if !strings.Contains(string(got), "IE15:") {
		t.Errorf("plan.md must retain IE15 line:\n%s", string(got))
	}
}

// Phase 7d: composite release (multiple ACs in one message) sweeps every
// matching IE line in one pass.
func TestSweepACPointerIE_CompositeRelease(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	mustWrite(t, planPath, "# Plan\n\nIE60: prep tool → docs/ac60-prep-tool.md\nIE61: another → docs/ac61-another.md\nIE99: untouched → docs/ac99-untouched.md\n")

	acNums := parseACRefs("AC60+AC61: bundle")
	matches, err := findACPointerIELines(dir, acNums)
	if err != nil {
		t.Fatalf("findACPointerIELines: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
	if err := removeACPointerIELines(dir, matches); err != nil {
		t.Fatalf("removeACPointerIELines: %v", err)
	}
	got, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	for _, gone := range []string{"IE60:", "IE61:"} {
		if strings.Contains(string(got), gone) {
			t.Errorf("plan.md still contains %s line after composite sweep:\n%s", gone, string(got))
		}
	}
	if !strings.Contains(string(got), "IE99:") {
		t.Errorf("plan.md must retain IE99 line:\n%s", string(got))
	}
}

// Phase 7d: when no IE line matches the released AC, plan.md is left
// untouched. Covers the common case (Director skipped the IE entirely
// for a single-cycle AC, per AGENTS.md Project Rules).
func TestSweepACPointerIE_NoMatchingIE(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	original := "# Plan\n\nIE10: future work → docs/ac10-future.md\n"
	mustWrite(t, planPath, original)

	acNums := parseACRefs("AC60: prep")
	matches, err := findACPointerIELines(dir, acNums)
	if err != nil {
		t.Fatalf("findACPointerIELines: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no matches, got %v", matches)
	}
	if err := removeACPointerIELines(dir, matches); err != nil {
		t.Fatalf("removeACPointerIELines: %v", err)
	}
	got, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	if string(got) != original {
		t.Errorf("plan.md must be byte-identical when no IE matches.\nwant:\n%s\ngot:\n%s", original, string(got))
	}
}

// Phase 7d: an IE pointing at an AC that is NOT being deleted must remain
// untouched. Guards against AC-number prefix collisions
// and accidental cross-AC deletion.
func TestSweepACPointerIE_UnrelatedIEUntouched(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	original := "# Plan\n\nIE1: short → docs/ac1-short.md\nIE10: longer → docs/ac10-longer.md\n"
	mustWrite(t, planPath, original)

	// Prefix-collision guard: a short AC ID must NOT match a longer AC ID's IE line.
	acNums := parseACRefs("AC1: short")
	matches, err := findACPointerIELines(dir, acNums)
	if err != nil {
		t.Fatalf("findACPointerIELines: %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "IE1:") {
		t.Fatalf("expected only IE1 match (no prefix-collision into IE10), got %v", matches)
	}
	if err := removeACPointerIELines(dir, matches); err != nil {
		t.Fatalf("removeACPointerIELines: %v", err)
	}
	got, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	if !strings.Contains(string(got), "IE10:") {
		t.Errorf("plan.md must retain IE10 line (prefix-collision guard):\n%s", string(got))
	}
	if strings.Contains(string(got), "IE1:") {
		t.Errorf("plan.md still contains IE1 line after sweep:\n%s", string(got))
	}
}

// Phase 7d: end-to-end Run wiring — when invoked normally (not dry-run), prep
// removes the AC-pointer IE line from plan.md and emits the canonical
// "removed plan.md IE line:" log line. Guards against a future refactor that
// drops the Phase 7d call site from Run; the helper-level tests would still
// pass without this one.
func TestPrepRunSweepsACPointerIE(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")
	mustWrite(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac14-prep-ie-sweep.md"), "# AC14\n")
	planPath := filepath.Join(dir, "plan.md")
	mustWrite(t, planPath, "# Plan\n\nIE14: prep IE sweep → docs/ac14-prep-ie-sweep.md\nIE99: keeper → docs/ac99-keeper.md\n")

	stubNoopBuild(t)

	var buf bytes.Buffer
	cfg := Config{Version: "v0.2.0", Message: "AC14: prep IE sweep", RepoRoot: dir, Out: &buf}
	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := string(mustRead(t, planPath))
	if strings.Contains(got, "IE14:") {
		t.Errorf("Run did not sweep IE14 from plan.md (Phase 7d not wired?):\n%s", got)
	}
	if !strings.Contains(got, "IE99:") {
		t.Errorf("Run incorrectly removed unrelated IE99 line:\n%s", got)
	}
	out := buf.String()
	if !strings.Contains(out, "removed plan.md IE line:") {
		t.Errorf("Run did not emit the canonical sweep log line. got:\n%s", out)
	}
}

// Phase 7d (dry-run): the new "plan.md AC-pointer IE removals:" section is
// printed when matching IEs exist, and dry-run leaves plan.md untouched.
func TestPrepDryRunReportsIESweep(t *testing.T) {
	dir := t.TempDir()
	gitInitFixture(t, dir)
	mustWrite(t, filepath.Join(dir, "TEMPLATE_VERSION"), "0.1.0\n")
	mustWrite(t, filepath.Join(dir, "internal", "templates", "base", "AGENTS.md"), "# AGENTS.md\n")
	mustWrite(t, filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n| Version | Summary |\n|---|---|\n| Unreleased | |\n| 0.1.0 | first |\n")
	mustWrite(t, filepath.Join(dir, "docs", "ac14-prep-ie-sweep.md"), "# AC14\n")
	planPath := filepath.Join(dir, "plan.md")
	original := "# Plan\n\nIE14: prep IE sweep → docs/ac14-prep-ie-sweep.md\n"
	mustWrite(t, planPath, original)

	stubNoopBuild(t)

	var buf bytes.Buffer
	cfg := Config{Version: "v0.2.0", Message: "AC14: prep IE sweep", RepoRoot: dir, DryRun: true, Out: &buf}
	if err := Run(cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(mustRead(t, planPath)); got != original {
		t.Errorf("DryRun must not modify plan.md.\nwant:\n%s\ngot:\n%s", original, got)
	}
	out := buf.String()
	for _, want := range []string{"plan.md AC-pointer IE removals:", "remove: IE14:"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q. got:\n%s", want, out)
		}
	}
}

// Phase 7d (regex): tab/multi-space whitespace between → and docs/ still
// matches; reverse prefix-collision (longer ID must not match shorter ID)
// holds; empty acNums short-circuits before any plan.md read.
func TestSweepACPointerIE_RegexAndShortCircuit(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	mustWrite(t, planPath, "# Plan\n\nIE1: lo → docs/ac1-lo.md\nIE10: hi →\tdocs/ac10-hi.md\nIE11: spaced →   docs/ac11-spaced.md\n")

	// Reverse prefix-collision: a longer AC ID matches only its own IE entry.
	matches, err := findACPointerIELines(dir, parseACRefs("AC10: hi"))
	if err != nil {
		t.Fatalf("findACPointerIELines (longer-ID branch): %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "IE10:") {
		t.Fatalf("longer AC ID must match only its own IE (no collision into shorter ID), got %v", matches)
	}

	// Whitespace tolerance: an AC release matches even with multi-space.
	matches, err = findACPointerIELines(dir, parseACRefs("AC11: spaced"))
	if err != nil {
		t.Fatalf("findACPointerIELines (whitespace branch): %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "IE11:") {
		t.Fatalf("whitespace tolerance: AC ref with multi-space must match, got %v", matches)
	}

	// Empty acNums: short-circuit returns (nil, nil) without reading plan.md.
	// We can't easily prove no-read, but we can assert the contract.
	matches, err = findACPointerIELines(dir, nil)
	if err != nil {
		t.Fatalf("findACPointerIELines empty acNums: %v", err)
	}
	if matches != nil {
		t.Errorf("empty acNums must return nil matches, got %v", matches)
	}
}
