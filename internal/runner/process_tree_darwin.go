//go:build darwin

package runner

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const darwinZombieProcessStatus = 5

const (
	darwinProcInfoCallListPIDs = 1
	darwinProcGroupOnly        = 2
	darwinProcParentOnly       = 6
)

type processHandle struct {
	pid      int
	identity processIdentity
}

func listChildProcessSnapshots(parentID int) ([]processSnapshot, error) {
	return listDarwinProcessSnapshots(darwinProcParentOnly, parentID)
}

func listProcessGroupSnapshots(groupID int) ([]processSnapshot, error) {
	return listDarwinProcessSnapshots(darwinProcGroupOnly, groupID)
}

func listDarwinProcessSnapshots(listType int, typeInfo int) ([]processSnapshot, error) {
	pids, err := listDarwinPIDs(listType, typeInfo)
	if err != nil {
		return nil, err
	}
	snapshots := make([]processSnapshot, 0, len(pids))
	for _, pid := range pids {
		snapshot, snapshotErr := processSnapshotForPID(pid)
		if errors.Is(snapshotErr, errProcessGone) {
			continue
		}
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func listDarwinPIDs(listType int, typeInfo int) ([]int, error) {
	requiredBytes, _, errno := unix.Syscall6(
		unix.SYS_PROC_INFO,
		darwinProcInfoCallListPIDs,
		uintptr(listType),
		uintptr(typeInfo),
		0,
		0,
		0,
	)
	if errno != 0 {
		return nil, errno
	}
	if requiredBytes == 0 {
		return nil, nil
	}
	pids := make([]int32, int(requiredBytes)/4)
	writtenBytes, _, errno := unix.Syscall6(
		unix.SYS_PROC_INFO,
		darwinProcInfoCallListPIDs,
		uintptr(listType),
		uintptr(typeInfo),
		0,
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)*4),
	)
	if errno != 0 {
		return nil, errno
	}
	result := make([]int, 0, int(writtenBytes)/4)
	for _, pid := range pids[:int(writtenBytes)/4] {
		if pid > 0 {
			result = append(result, int(pid))
		}
	}
	return result, nil
}

func processSnapshotForPID(pid int) (processSnapshot, error) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EIO) {
			return processSnapshot{}, errProcessGone
		}
		return processSnapshot{}, err
	}
	if process.Proc.P_pid != int32(pid) {
		return processSnapshot{}, errProcessGone
	}
	return darwinProcessSnapshot(*process), nil
}

func darwinProcessSnapshot(process unix.KinfoProc) processSnapshot {
	start := process.Proc.P_starttime
	return processSnapshot{
		pid:      int(process.Proc.P_pid),
		parentID: int(process.Eproc.Ppid),
		groupID:  int(process.Eproc.Pgid),
		identity: processIdentity(fmt.Sprintf("%d:%d", start.Sec, start.Usec)),
		zombie:   process.Proc.P_stat == darwinZombieProcessStatus,
	}
}

func openProcessHandle(snapshot processSnapshot) (*processHandle, error) {
	current, err := processSnapshotForPID(snapshot.pid)
	if err != nil {
		return nil, err
	}
	if current.identity != snapshot.identity {
		return nil, errProcessGone
	}
	return &processHandle{pid: snapshot.pid, identity: snapshot.identity}, nil
}

func (h *processHandle) Signal(signal syscall.Signal) error {
	if h == nil {
		return errProcessGone
	}
	current, err := processSnapshotForPID(h.pid)
	if err != nil {
		return err
	}
	if current.identity != h.identity || current.zombie {
		return errProcessGone
	}
	return unix.Kill(h.pid, signal)
}

func (h *processHandle) Close() error {
	return nil
}
