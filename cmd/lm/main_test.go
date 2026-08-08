package main

import (
	"io"
	"os"
	"testing"
)

func TestVersionAliases(t *testing.T) {
	for _, alias := range []string{"-v", "--version"} {
		t.Run(alias, func(t *testing.T) {
			stdout, stderr := runCLIOutput(t, alias)
			want := programName + " v" + programVersion + "\n"
			if stdout != want {
				t.Fatalf("stdout = %q, want %q", stdout, want)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func runCLIOutput(t *testing.T, args ...string) (string, string) {
	t.Helper()

	oldArgs, oldStdout, oldStderr := os.Args, os.Stdout, os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		t.Fatalf("create stderr pipe: %v", err)
	}

	os.Args = append([]string{programName}, args...)
	os.Stdout, os.Stderr = stdoutW, stderrW
	runCLI()
	os.Args, os.Stdout, os.Stderr = oldArgs, oldStdout, oldStderr
	stdoutW.Close()
	stderrW.Close()

	stdout, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	stderr, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	stdoutR.Close()
	stderrR.Close()
	return string(stdout), string(stderr)
}
