package state

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	pathLockHelperEnv     = "LAPTOP_SETUP_PATH_LOCK_HELPER"
	pathLockHelperPathEnv = "LAPTOP_SETUP_PATH_LOCK_PATH"
)

func TestAcquirePathLockExcludesAnotherProcess(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	helper := startPathLockHelper(t, statePath)

	contendedLock, err := AcquirePathLock(statePath)
	if contendedLock != nil {
		_ = contendedLock.Close()
		t.Fatal("second process unexpectedly acquired the same state path")
	}
	if !errors.Is(err, ErrPathLocked) {
		t.Fatalf("expected ErrPathLocked, got %v", err)
	}

	helper.release(t)

	recoveredLock, err := AcquirePathLock(statePath)
	if err != nil {
		t.Fatalf("acquire state path after owner exit: %v", err)
	}
	if err = recoveredLock.Close(); err != nil {
		t.Fatalf("release recovered lock: %v", err)
	}
}

func TestAcquirePathLockRejectsFinalSymlinkAliasAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), privateFilePerm); err != nil {
		t.Fatalf("seed state file: %v", err)
	}
	aliasPath := filepath.Join(dir, "alias.json")
	if err := os.Symlink(filepath.Base(statePath), aliasPath); err != nil {
		t.Fatalf("create state file symlink: %v", err)
	}
	helper := startPathLockHelper(t, statePath)

	aliasLock, err := AcquirePathLock(aliasPath)
	if aliasLock != nil {
		_ = aliasLock.Close()
		t.Fatal("symlinked state path unexpectedly acquired a separate lock")
	}
	if err == nil || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Fatalf("expected actionable symbolic-link rejection, got %v", err)
	}

	helper.release(t)
}

func TestAcquirePathLockRejectsHardLinkedStateFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), privateFilePerm); err != nil {
		t.Fatalf("seed state file: %v", err)
	}
	if err := os.Link(statePath, filepath.Join(dir, "alias.json")); err != nil {
		t.Fatalf("create state file hard link: %v", err)
	}

	lock, err := AcquirePathLock(statePath)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("hard-linked state path unexpectedly acquired a lock")
	}
	if err == nil || !strings.Contains(err.Error(), "has 2 hard links") {
		t.Fatalf("expected actionable hard-link rejection, got %v", err)
	}
}

func TestPathLockHelperProcess(t *testing.T) {
	if os.Getenv(pathLockHelperEnv) != "1" {
		return
	}
	lock, err := AcquirePathLock(os.Getenv(pathLockHelperPathEnv))
	if err != nil {
		t.Fatalf("acquire helper lock: %v", err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			t.Errorf("release helper lock: %v", err)
		}
	}()
	if _, err = fmt.Fprintln(os.Stdout, "ready"); err != nil {
		t.Fatalf("announce helper readiness: %v", err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

type pathLockHelper struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	waited  bool
}

func startPathLockHelper(t *testing.T, statePath string) *pathLockHelper {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPathLockHelperProcess$")
	command.Env = append(
		os.Environ(),
		pathLockHelperEnv+"=1",
		pathLockHelperPathEnv+"="+statePath,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatalf("open helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		t.Fatalf("open helper stdout: %v", err)
	}
	if err = command.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		t.Fatalf("start helper process: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		_ = stdin.Close()
		waitErr := command.Wait()
		cancel()
		t.Fatalf("helper did not acquire lock: output=%q scanErr=%v waitErr=%v", scanner.Text(), scanner.Err(), waitErr)
	}

	helper := &pathLockHelper{command: command, stdin: stdin, cancel: cancel}
	t.Cleanup(helper.cleanup)
	return helper
}

func (helper *pathLockHelper) release(t *testing.T) {
	t.Helper()
	if helper.waited {
		return
	}
	if err := helper.stdin.Close(); err != nil {
		t.Fatalf("release helper process: %v", err)
	}
	err := helper.command.Wait()
	helper.waited = true
	helper.cancel()
	if err != nil {
		t.Fatalf("wait for helper process: %v", err)
	}
}

func (helper *pathLockHelper) cleanup() {
	_ = helper.stdin.Close()
	if !helper.waited {
		_ = helper.command.Process.Kill()
		_ = helper.command.Wait()
		helper.waited = true
	}
	helper.cancel()
}

func TestAcquirePathLockRecoversPersistentUnlockedFile(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	lockPath := statePath + ".lock"
	if err := os.WriteFile(lockPath, []byte("stale owner metadata"), 0o644); err != nil {
		t.Fatalf("seed persistent lock file: %v", err)
	}

	lock, err := AcquirePathLock(statePath)
	if err != nil {
		t.Fatalf("acquire unlocked persistent lock file: %v", err)
	}
	if err = lock.Close(); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat persistent lock file: %v", err)
	}
	if got := info.Mode().Perm(); got != privateFilePerm {
		t.Fatalf("lock file permissions: got=%#o want=%#o", got, os.FileMode(privateFilePerm))
	}
}

func TestAcquirePathLockAllowsDifferentStatePaths(t *testing.T) {
	dir := t.TempDir()
	first, err := AcquirePathLock(filepath.Join(dir, "first.json"))
	if err != nil {
		t.Fatalf("acquire first path: %v", err)
	}
	defer func() { _ = first.Close() }()

	second, err := AcquirePathLock(filepath.Join(dir, "second.json"))
	if err != nil {
		t.Fatalf("acquire second path: %v", err)
	}
	if err = second.Close(); err != nil {
		t.Fatalf("release second path: %v", err)
	}
}

func TestAcquirePathLockNormalizesEquivalentPaths(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	lock, err := AcquirePathLock(statePath)
	if err != nil {
		t.Fatalf("acquire state path: %v", err)
	}
	defer func() { _ = lock.Close() }()

	equivalentPath := filepath.Join(dir, ".", "state.json")
	duplicate, err := AcquirePathLock(equivalentPath)
	if duplicate != nil {
		_ = duplicate.Close()
		t.Fatal("equivalent state path unexpectedly acquired a separate lock")
	}
	if !errors.Is(err, ErrPathLocked) {
		t.Fatalf("expected equivalent path contention, got %v", err)
	}
}

func TestAcquirePathLockNormalizesSymlinkedParentDirectories(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatalf("create real state directory: %v", err)
	}
	linkedDir := filepath.Join(dir, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("create state directory symlink: %v", err)
	}

	lock, err := AcquirePathLock(filepath.Join(realDir, "state.json"))
	if err != nil {
		t.Fatalf("acquire real state path: %v", err)
	}
	defer func() { _ = lock.Close() }()
	canonicalDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("resolve real state directory: %v", err)
	}
	if got, want := lock.StatePath(), filepath.Join(canonicalDir, "state.json"); got != want {
		t.Fatalf("canonical state path: got=%q want=%q", got, want)
	}

	duplicate, err := AcquirePathLock(filepath.Join(linkedDir, "state.json"))
	if duplicate != nil {
		_ = duplicate.Close()
		t.Fatal("symlinked parent directory unexpectedly acquired a separate lock")
	}
	if !errors.Is(err, ErrPathLocked) {
		t.Fatalf("expected symlinked path contention, got %v", err)
	}
}
