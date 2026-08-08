//go:build linux

package runner

import (
	"errors"

	"golang.org/x/sys/unix"
)

type processExitWatcher struct {
	pidfd int
}

func newProcessExitWatcher(pid int) (*processExitWatcher, error) {
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	return &processExitWatcher{pidfd: pidfd}, nil
}

func (w *processExitWatcher) Wait() error {
	pollFDs := []unix.PollFd{{Fd: int32(w.pidfd), Events: unix.POLLIN}}
	for {
		count, err := unix.Poll(pollFDs, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
	}
}

func (w *processExitWatcher) Close() error {
	return unix.Close(w.pidfd)
}
