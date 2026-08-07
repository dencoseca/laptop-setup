package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrPathLocked indicates that another process owns the state path lock.
var ErrPathLocked = errors.New("state path is locked")

// PathLock owns the advisory lock associated with one state file path.
// The lock file is intentionally persistent: process ownership is represented
// by the kernel lock, so a file left by a terminated process is safe to reuse.
type PathLock struct {
	file *os.File
}

// AcquirePathLock acquires a non-blocking interprocess lock for statePath.
func AcquirePathLock(statePath string) (*PathLock, error) {
	resolvedStatePath, err := absoluteStatePath(statePath)
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Dir(resolvedStatePath)
	if err = os.MkdirAll(stateDir, privateDirPerm); err != nil {
		return nil, fmt.Errorf("create state lock directory: %w", err)
	}
	realStateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve state lock directory: %w", err)
	}
	lockPath := filepath.Join(realStateDir, filepath.Base(resolvedStatePath)) + ".lock"

	fd, err := unix.Open(
		lockPath,
		unix.O_CLOEXEC|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_RDWR,
		privateFilePerm,
	)
	if err != nil {
		return nil, fmt.Errorf("open state lock file %q: %w", lockPath, err)
	}
	lockFile := os.NewFile(uintptr(fd), lockPath)
	if lockFile == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open state lock file %q: invalid file descriptor", lockPath)
	}

	closeWithError := func(lockErr error) (*PathLock, error) {
		if closeErr := lockFile.Close(); closeErr != nil {
			lockErr = errors.Join(lockErr, fmt.Errorf("close state lock file: %w", closeErr))
		}
		return nil, lockErr
	}

	info, err := lockFile.Stat()
	if err != nil {
		return closeWithError(fmt.Errorf("inspect state lock file %q: %w", lockPath, err))
	}
	if !info.Mode().IsRegular() {
		return closeWithError(fmt.Errorf("state lock path %q is not a regular file", lockPath))
	}
	if err = lockFile.Chmod(privateFilePerm); err != nil {
		return closeWithError(fmt.Errorf("secure state lock file %q: %w", lockPath, err))
	}

	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return closeWithError(fmt.Errorf("%w: %s", ErrPathLocked, statePath))
		}
		return closeWithError(fmt.Errorf("lock state path %q: %w", statePath, err))
	}

	return &PathLock{file: lockFile}, nil
}

func (lock *PathLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}

	file := lock.file
	lock.file = nil
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock state path: %w", unlockErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close state lock file: %w", closeErr)
	}
	return errors.Join(unlockErr, closeErr)
}

func absoluteStatePath(statePath string) (string, error) {
	if statePath == "" {
		return "", errors.New("state path is empty")
	}
	absolutePath, err := filepath.Abs(statePath)
	if err != nil {
		return "", fmt.Errorf("resolve state path: %w", err)
	}
	return filepath.Clean(absolutePath), nil
}
