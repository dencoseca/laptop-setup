package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOSCommandRunnerReturnsTypedCommandError(t *testing.T) {
	runner := NewOSCommandRunner()
	command := Command{
		Name: "/bin/sh",
		Args: []string{"-c", `printf "out"; printf "err" >&2; exit 7`},
	}

	result, err := runner.Run(context.Background(), command)
	if err == nil {
		t.Fatal("expected command error")
	}

	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandError, got %T", err)
	}
	if commandErr.Command.String() != command.String() {
		t.Fatalf("command mismatch: got=%q want=%q", commandErr.Command.String(), command.String())
	}
	if commandErr.ExitCode != 7 {
		t.Fatalf("exit code mismatch: got=%d want=7", commandErr.ExitCode)
	}
	if commandErr.Stdout != "out" || commandErr.Stderr != "err" {
		t.Fatalf("unexpected captured output: stdout=%q stderr=%q", commandErr.Stdout, commandErr.Stderr)
	}
	if result.ExitCode != commandErr.ExitCode || result.Stdout != commandErr.Stdout || result.Stderr != commandErr.Stderr {
		t.Fatalf("result did not preserve command error output: result=%+v error=%+v", result, commandErr)
	}
}

func TestOSCommandRunnerLookPathUsesEnvironment(t *testing.T) {
	binDir := t.TempDir()
	commandPath := filepath.Join(binDir, "fake-command")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake command: %v", err)
	}
	t.Setenv("PATH", binDir)

	path, err := NewOSCommandRunner().LookPath(context.Background(), "fake-command")
	if err != nil {
		t.Fatalf("look path: %v", err)
	}
	if path != commandPath {
		t.Fatalf("path mismatch: got=%q want=%q", path, commandPath)
	}
}

func TestOSCommandRunnerContractExecutesWithDirAndEnv(t *testing.T) {
	workDir := t.TempDir()
	command := Command{
		Name: "/bin/sh",
		Args: []string{"-c", `printf "%s|%s" "$PWD" "$PORT_CONTRACT_VALUE"`},
		Dir:  workDir,
		Env:  []string{"PORT_CONTRACT_VALUE=ok"},
	}

	result, err := NewOSCommandRunner().Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code mismatch: %d", result.ExitCode)
	}
	if result.Stdout != workDir+"|ok" {
		t.Fatalf("stdout mismatch: got=%q want=%q", result.Stdout, workDir+"|ok")
	}
	if result.Stderr != "" {
		t.Fatalf("expected empty stderr, got %q", result.Stderr)
	}
}
