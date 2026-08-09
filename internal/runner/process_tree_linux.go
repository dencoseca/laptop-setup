//go:build linux

package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type processHandle struct {
	pidfd int
}

func listChildProcessSnapshots(parentID int) ([]processSnapshot, error) {
	return listLinuxProcessSnapshots(func(snapshot processSnapshot) bool {
		return snapshot.parentID == parentID
	})
}

func listProcessGroupSnapshots(groupID int) ([]processSnapshot, error) {
	return listLinuxProcessSnapshots(func(snapshot processSnapshot) bool {
		return snapshot.groupID == groupID
	})
}

func listLinuxProcessSnapshots(include func(processSnapshot) bool) ([]processSnapshot, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	snapshots := make([]processSnapshot, 0, len(entries))
	for _, entry := range entries {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil {
			continue
		}
		snapshot, snapshotErr := processSnapshotForPID(pid)
		if errors.Is(snapshotErr, errProcessGone) || errors.Is(snapshotErr, os.ErrPermission) {
			continue
		}
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if include(snapshot) {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots, nil
}

func processSnapshotForPID(pid int) (processSnapshot, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return processSnapshot{}, errProcessGone
		}
		return processSnapshot{}, err
	}
	stat := string(payload)
	commandEnd := strings.LastIndexByte(stat, ')')
	if commandEnd < 0 {
		return processSnapshot{}, fmt.Errorf("parse /proc/%d/stat: command is not terminated", pid)
	}
	fields := strings.Fields(stat[commandEnd+1:])
	if len(fields) <= 19 {
		return processSnapshot{}, fmt.Errorf("parse /proc/%d/stat: got %d fields after command", pid, len(fields))
	}
	parentID, err := strconv.Atoi(fields[1])
	if err != nil {
		return processSnapshot{}, fmt.Errorf("parse /proc/%d/stat parent: %w", pid, err)
	}
	groupID, err := strconv.Atoi(fields[2])
	if err != nil {
		return processSnapshot{}, fmt.Errorf("parse /proc/%d/stat group: %w", pid, err)
	}
	return processSnapshot{
		pid:      pid,
		parentID: parentID,
		groupID:  groupID,
		identity: processIdentity(fields[19]),
		zombie:   fields[0] == "Z",
	}, nil
}

func openProcessHandle(snapshot processSnapshot) (*processHandle, error) {
	pidfd, err := unix.PidfdOpen(snapshot.pid, 0)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil, errProcessGone
		}
		return nil, err
	}
	current, err := processSnapshotForPID(snapshot.pid)
	if err != nil || current.identity != snapshot.identity {
		_ = unix.Close(pidfd)
		if err != nil {
			return nil, err
		}
		return nil, errProcessGone
	}
	return &processHandle{pidfd: pidfd}, nil
}

func (h *processHandle) Signal(signal syscall.Signal) error {
	if h == nil || h.pidfd < 0 {
		return errProcessGone
	}
	err := unix.PidfdSendSignal(h.pidfd, unix.Signal(signal), nil, 0)
	if errors.Is(err, syscall.ESRCH) {
		return errProcessGone
	}
	return err
}

func (h *processHandle) Close() error {
	if h == nil || h.pidfd < 0 {
		return nil
	}
	err := unix.Close(h.pidfd)
	h.pidfd = -1
	return err
}
