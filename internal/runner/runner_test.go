package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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

func TestResultFromCommandReturnsTypedCommandError(t *testing.T) {
	command := Command{
		Name: "/bin/sh",
		Args: []string{"-c", "exit 9"},
	}
	runErr := exec.Command(command.Name, command.Args...).Run()
	if runErr == nil {
		t.Fatal("expected run error")
	}

	result, err := ResultFromCommand(command, "out", "err", runErr)
	if err == nil {
		t.Fatal("expected command error")
	}

	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected CommandError, got %T", err)
	}
	if result.ExitCode != 9 || commandErr.ExitCode != 9 {
		t.Fatalf("exit code mismatch: result=%d error=%d", result.ExitCode, commandErr.ExitCode)
	}
	if result.Stdout != "out" || result.Stderr != "err" {
		t.Fatalf("unexpected result output: %+v", result)
	}
	if commandErr.Stdout != result.Stdout || commandErr.Stderr != result.Stderr {
		t.Fatalf("error did not preserve result output: result=%+v error=%+v", result, commandErr)
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

func TestNewExecCommandContractExecutesWithDirAndEnv(t *testing.T) {
	workDir := t.TempDir()
	command := Command{
		Name: "/bin/sh",
		Args: []string{"-c", `printf "%s|%s" "$PWD" "$PORT_CONTRACT_VALUE"`},
		Dir:  workDir,
		Env:  []string{"PORT_CONTRACT_VALUE=ok"},
	}

	cmd, err := NewExecCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("NewExecCommand returned error: %v", err)
	}

	var stdout bytes.Buffer
	cmd.SetStdout(&stdout)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stdout.String() != workDir+"|ok" {
		t.Fatalf("stdout mismatch: got=%q want=%q", stdout.String(), workDir+"|ok")
	}
}

func TestOSCommandRunnerCancellationAllowsGracefulProcessGroupShutdown(t *testing.T) {
	workingDir := t.TempDir()
	startedPath := filepath.Join(workingDir, "started")
	terminatedPath := filepath.Join(workingDir, "terminated")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	command := Command{
		Name: testBinary,
		Args: []string{"-test.run", "^TestRunnerGracefulShutdownHelper$"},
		Env: []string{
			"RUNNER_GRACEFUL_HELPER=1",
			"RUNNER_STARTED_PATH=" + startedPath,
			"RUNNER_TERMINATED_PATH=" + terminatedPath,
		},
	}
	runner := &OSCommandRunner{terminationGracePeriod: 500 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, command)
		done <- err
	}()

	waitForTestFile(t, startedPath, 2*time.Second)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation error, got %v", err)
		}
		if strings.Contains(err.Error(), "force-kill") || strings.Contains(err.Error(), "operation not permitted") {
			t.Fatalf("graceful cancellation reported a force-kill failure: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("graceful command did not stop within the cancellation bound")
	}
	waitForTestFile(t, terminatedPath, time.Second)
}

func TestRunnerGracefulShutdownHelper(t *testing.T) {
	if os.Getenv("RUNNER_GRACEFUL_HELPER") != "1" {
		return
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := os.WriteFile(os.Getenv("RUNNER_STARTED_PATH"), []byte("started"), 0o600); err != nil {
		t.Fatalf("write started marker: %v", err)
	}
	<-signals
	if err := os.WriteFile(os.Getenv("RUNNER_TERMINATED_PATH"), []byte("terminated"), 0o600); err != nil {
		t.Fatalf("write terminated marker: %v", err)
	}
	os.Exit(0)
}

func TestOSCommandRunnerCancellationForceKillsCompleteProcessGroup(t *testing.T) {
	workingDir := t.TempDir()
	leaderPIDPath := filepath.Join(workingDir, "leader.pid")
	grandchildPIDPath := filepath.Join(workingDir, "grandchild.pid")
	command := Command{
		Name: "/bin/sh",
		Args: []string{
			"-c",
			`trap '' TERM; printf '%s' "$$" > "$1"; /bin/sh -c 'trap "" TERM; printf "%s" "$$" > "$1"; while :; do sleep 1; done' child "$2" & wait`,
			"laptop-setup-test",
			leaderPIDPath,
			grandchildPIDPath,
		},
	}
	runner := &OSCommandRunner{terminationGracePeriod: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, command)
		done <- err
	}()

	leaderPID := waitForTestPID(t, leaderPIDPath, 2*time.Second)
	grandchildPID := waitForTestPID(t, grandchildPIDPath, 2*time.Second)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process group did not stop within the cancellation bound")
	}

	waitForProcessExit(t, leaderPID, 2*time.Second)
	waitForProcessExit(t, grandchildPID, 2*time.Second)
}

func TestOSCommandRunnerCancellationKillsDetachedDescendantWithInheritedDescriptors(t *testing.T) {
	workingDir := t.TempDir()
	descendantPIDPath := filepath.Join(workingDir, "detached.pid")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	command := Command{
		Name: "/bin/sh",
		Args: []string{
			"-c",
			`"$1" -test.run '^TestRunnerPersistentDescendantHelper$' & wait`,
			"laptop-setup-test",
			testBinary,
		},
		Env: []string{
			"RUNNER_PERSISTENT_HELPER=1",
			"RUNNER_DETACH_HELPER=1",
			"RUNNER_DESCENDANT_PID_PATH=" + descendantPIDPath,
		},
	}
	runner := &OSCommandRunner{terminationGracePeriod: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(ctx, command)
		done <- runErr
	}()

	descendantPID := waitForTestPID(t, descendantPIDPath, 2*time.Second)
	killProcessOnTestCleanup(t, descendantPID)
	cancel()
	select {
	case runErr := <-done:
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("expected cancellation error, got %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner remained blocked on descriptors inherited by detached descendant")
	}
	waitForProcessExit(t, descendantPID, 2*time.Second)
}

func TestLeaderExitRaceStillTerminatesKnownDescendantsAfterCancellation(t *testing.T) {
	workingDir := t.TempDir()
	descendantPIDPath := filepath.Join(workingDir, "descendant.pid")
	allowLeaderExitPath := filepath.Join(workingDir, "exit-leader")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	cmd := exec.Command(
		"/bin/sh",
		"-c",
		`"$1" -test.run '^TestRunnerPersistentDescendantHelper$' & while [ ! -f "$2" ]; do /bin/sleep 0.01; done`,
		"laptop-setup-test",
		testBinary,
		allowLeaderExitPath,
	)
	cmd.Env = append(cmd.Environ(),
		"RUNNER_PERSISTENT_HELPER=1",
		"RUNNER_DESCENDANT_PID_PATH="+descendantPIDPath,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	configureOwnedProcess(cmd)
	if err = cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	watcher, err := newProcessExitWatcher(cmd.Process.Pid)
	if err != nil {
		_ = signalOwnedProcessGroup(cmd.Process.Pid, forceKillSignal)
		_ = cmd.Wait()
		t.Fatalf("watch command: %v", err)
	}
	defer watcher.Close()
	processTree, err := newOwnedProcessTree(cmd.Process.Pid)
	if err != nil {
		_ = signalOwnedProcessGroup(cmd.Process.Pid, forceKillSignal)
		_ = cmd.Wait()
		t.Fatalf("track command process tree: %v", err)
	}
	t.Cleanup(func() {
		_ = processTree.Close()
	})
	commandFinished := false
	t.Cleanup(func() {
		if commandFinished {
			return
		}
		_ = processTree.terminate(0)
		_ = cmd.Wait()
	})

	descendantPID := waitForTestPID(t, descendantPIDPath, 2*time.Second)
	killProcessOnTestCleanup(t, descendantPID)
	if err = processTree.refresh(); err != nil {
		t.Fatalf("refresh process tree: %v", err)
	}
	if err = os.WriteFile(allowLeaderExitPath, []byte("exit"), 0o600); err != nil {
		t.Fatalf("allow leader exit: %v", err)
	}
	if err = watcher.Wait(); err != nil {
		t.Fatalf("wait for leader exit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = finishOwnedCommandAfterLeaderExit(ctx, cmd, processTree, 100*time.Millisecond, nil)
	commandFinished = true
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	waitForProcessExit(t, descendantPID, 2*time.Second)
}

func killProcessOnTestCleanup(t *testing.T, pid int) {
	t.Helper()
	snapshot, err := processSnapshotForPID(pid)
	if err != nil {
		t.Fatalf("inspect process %d for cleanup: %v", pid, err)
	}
	handle, err := openProcessHandle(snapshot)
	if err != nil {
		t.Fatalf("track process %d for cleanup: %v", pid, err)
	}
	t.Cleanup(func() {
		_ = handle.Signal(forceKillSignal)
		_ = handle.Close()
	})
}

func TestRunnerPersistentDescendantHelper(t *testing.T) {
	if os.Getenv("RUNNER_PERSISTENT_HELPER") != "1" {
		return
	}
	if os.Getenv("RUNNER_DETACH_HELPER") == "1" {
		if _, err := unix.Setsid(); err != nil {
			t.Fatalf("detach helper session: %v", err)
		}
	}
	signal.Ignore(syscall.SIGTERM)
	defer signal.Reset(syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv("RUNNER_DESCENDANT_PID_PATH"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("write descendant pid: %v", err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func waitForTestFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat test file %q: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for test file %q", path)
}

func waitForTestPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	waitForTestFile(t, path, timeout)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file %q: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil {
		t.Fatalf("parse pid file %q: %v", path, err)
	}
	return pid
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := unix.Kill(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			return
		}
		if err != nil && !errors.Is(err, unix.EPERM) {
			t.Fatalf("inspect process %d: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d remained alive after cancellation", pid)
}
