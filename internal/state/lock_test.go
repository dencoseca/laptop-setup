package state

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	pathLockHelperEnv     = "LAPTOP_SETUP_PATH_LOCK_HELPER"
	pathLockHelperPathEnv = "LAPTOP_SETUP_PATH_LOCK_PATH"
)

func TestAcquirePathLockExcludesAnotherProcess(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	command := exec.Command(os.Args[0], "-test.run=^TestPathLockHelperProcess$")
	command.Env = append(
		os.Environ(),
		pathLockHelperEnv+"=1",
		pathLockHelperPathEnv+"="+statePath,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open helper stdout: %v", err)
	}
	if err = command.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		_ = stdin.Close()
		waitErr := command.Wait()
		waited = true
		t.Fatalf("helper did not acquire lock: output=%q scanErr=%v waitErr=%v", scanner.Text(), scanner.Err(), waitErr)
	}

	contendedLock, err := AcquirePathLock(statePath)
	if contendedLock != nil {
		_ = contendedLock.Close()
		t.Fatal("second process unexpectedly acquired the same state path")
	}
	if !errors.Is(err, ErrPathLocked) {
		t.Fatalf("expected ErrPathLocked, got %v", err)
	}

	if err = stdin.Close(); err != nil {
		t.Fatalf("release helper process: %v", err)
	}
	if err = command.Wait(); err != nil {
		t.Fatalf("wait for helper process: %v", err)
	}
	waited = true

	recoveredLock, err := AcquirePathLock(statePath)
	if err != nil {
		t.Fatalf("acquire state path after owner exit: %v", err)
	}
	if err = recoveredLock.Close(); err != nil {
		t.Fatalf("release recovered lock: %v", err)
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

	duplicate, err := AcquirePathLock(filepath.Join(linkedDir, "state.json"))
	if duplicate != nil {
		_ = duplicate.Close()
		t.Fatal("symlinked parent directory unexpectedly acquired a separate lock")
	}
	if !errors.Is(err, ErrPathLocked) {
		t.Fatalf("expected symlinked path contention, got %v", err)
	}
}
