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
	file      *os.File
	statePath string
}

// AcquirePathLock acquires a non-blocking interprocess lock for statePath.
func AcquirePathLock(statePath string) (*PathLock, error) {
	absolutePath, err := absoluteStatePath(statePath)
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Dir(absolutePath)
	if err = os.MkdirAll(stateDir, privateDirPerm); err != nil {
		return nil, fmt.Errorf("create state lock directory: %w", err)
	}
	realStateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve state lock directory: %w", err)
	}
	resolvedStatePath := filepath.Join(realStateDir, filepath.Base(absolutePath))
	if err = validateUniqueStatePath(resolvedStatePath); err != nil {
		return nil, err
	}
	lockPath := resolvedStatePath + ".lock"

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
	if err = validateUniqueStatePath(resolvedStatePath); err != nil {
		return closeWithError(err)
	}

	return &PathLock{file: lockFile, statePath: resolvedStatePath}, nil
}

// StatePath returns the canonical state path protected by this lock.
func (lock *PathLock) StatePath() string {
	if lock == nil {
		return ""
	}
	return lock.statePath
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

func validateUniqueStatePath(statePath string) error {
	var info unix.Stat_t
	if err := unix.Lstat(statePath, &info); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect state path %q: %w", statePath, err)
	}

	switch info.Mode & unix.S_IFMT {
	case unix.S_IFLNK:
		return fmt.Errorf("state path %q is a symbolic link; use its target path directly", statePath)
	case unix.S_IFREG:
	default:
		return fmt.Errorf("state path %q is not a regular file", statePath)
	}
	if info.Nlink != 1 {
		return fmt.Errorf("state path %q has %d hard links; use a unique state file", statePath, info.Nlink)
	}
	return nil
}
