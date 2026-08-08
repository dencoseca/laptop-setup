//go:build darwin

package runner

import (
	"errors"

	"golang.org/x/sys/unix"
)

type processExitWatcher struct {
	kqueue int
}

func newProcessExitWatcher(pid int) (*processExitWatcher, error) {
	kqueue, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}

	changes := []unix.Kevent_t{{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}}
	if _, err = unix.Kevent(kqueue, changes, nil, nil); err != nil {
		_ = unix.Close(kqueue)
		return nil, err
	}
	return &processExitWatcher{kqueue: kqueue}, nil
}

func (w *processExitWatcher) Wait() error {
	events := make([]unix.Kevent_t, 1)
	for {
		count, err := unix.Kevent(w.kqueue, nil, events, nil)
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
	return unix.Close(w.kqueue)
}
