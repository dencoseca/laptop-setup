//go:build darwin || linux

package runner

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"
)

const processTreePollInterval = 100 * time.Millisecond

var errProcessGone = errors.New("process no longer exists")

type processIdentity string

type processSnapshot struct {
	pid      int
	parentID int
	groupID  int
	identity processIdentity
	zombie   bool
}

type processKey struct {
	pid      int
	identity processIdentity
}

func (p processSnapshot) key() processKey {
	return processKey{pid: p.pid, identity: p.identity}
}

type ownedProcess struct {
	snapshot processSnapshot
	handle   *processHandle
}

type ownedProcessTree struct {
	rootPID int
	groupID int

	refreshMu sync.Mutex
	mu        sync.Mutex
	processes map[processKey]*ownedProcess

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newOwnedProcessTree(rootPID int) (*ownedProcessTree, error) {
	root, err := processSnapshotForPID(rootPID)
	if err != nil {
		return nil, fmt.Errorf("inspect command process %d: %w", rootPID, err)
	}
	handle, err := openProcessHandle(root)
	if err != nil {
		return nil, fmt.Errorf("track command process %d: %w", rootPID, err)
	}

	tree := &ownedProcessTree{
		rootPID: rootPID,
		groupID: root.groupID,
		processes: map[processKey]*ownedProcess{
			root.key(): {snapshot: root, handle: handle},
		},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	if err = tree.refresh(); err != nil {
		closeErr := tree.closeProcessHandles()
		return nil, errors.Join(fmt.Errorf("inspect command process tree: %w", err), closeErr)
	}
	go tree.track()
	return tree, nil
}

func (t *ownedProcessTree) track() {
	defer close(t.done)
	ticker := time.NewTicker(processTreePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = t.refresh()
		case <-t.stop:
			return
		}
	}
}

func (t *ownedProcessTree) refresh() error {
	if t == nil {
		return nil
	}
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	var refreshErrs []error
	activeParents := make(map[int]struct{}, len(t.processes))
	queue := make([]int, 0, len(t.processes))
	for key, process := range t.processes {
		snapshot, err := processSnapshotForPID(key.pid)
		if errors.Is(err, errProcessGone) {
			if closeErr := process.handle.Close(); closeErr != nil {
				refreshErrs = append(refreshErrs, fmt.Errorf("close exited process %d handle: %w", key.pid, closeErr))
			}
			delete(t.processes, key)
			continue
		}
		if err != nil {
			refreshErrs = append(refreshErrs, err)
			return errors.Join(refreshErrs...)
		}
		if snapshot.identity != key.identity || snapshot.zombie {
			if closeErr := process.handle.Close(); closeErr != nil {
				refreshErrs = append(refreshErrs, fmt.Errorf("close stale process %d handle: %w", key.pid, closeErr))
			}
			delete(t.processes, key)
			continue
		}
		process.snapshot = snapshot
		activeParents[snapshot.pid] = struct{}{}
		queue = append(queue, snapshot.pid)
	}

	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]
		children, err := listChildProcessSnapshots(parentID)
		if err != nil {
			refreshErrs = append(refreshErrs, fmt.Errorf("list children of process %d: %w", parentID, err))
			return errors.Join(refreshErrs...)
		}
		for _, snapshot := range children {
			if _, ok := activeParents[snapshot.pid]; ok {
				continue
			}
			key := snapshot.key()
			if process, ok := t.processes[key]; ok {
				process.snapshot = snapshot
				activeParents[snapshot.pid] = struct{}{}
				queue = append(queue, snapshot.pid)
				continue
			}
			handle, openErr := openProcessHandle(snapshot)
			if errors.Is(openErr, errProcessGone) {
				continue
			}
			if openErr != nil {
				refreshErrs = append(refreshErrs, fmt.Errorf("track descendant process %d: %w", snapshot.pid, openErr))
				return errors.Join(refreshErrs...)
			}
			t.processes[key] = &ownedProcess{snapshot: snapshot, handle: handle}
			activeParents[snapshot.pid] = struct{}{}
			queue = append(queue, snapshot.pid)
		}
	}
	return errors.Join(refreshErrs...)
}

func (t *ownedProcessTree) terminate(gracePeriod time.Duration) error {
	if t == nil {
		return errors.New("owned process tree is required")
	}
	if gracePeriod < 0 {
		gracePeriod = 0
	}

	var terminationErrs []error
	if err := t.refresh(); err != nil {
		terminationErrs = append(terminationErrs, fmt.Errorf("refresh command process tree before termination: %w", err))
	}
	if err := t.signalGroup(syscall.SIGTERM); err != nil {
		terminationErrs = append(terminationErrs, fmt.Errorf("terminate command process group %d: %w", t.groupID, err))
	}
	termSignaled := make(map[processKey]struct{})
	if err := t.signalTracked(syscall.SIGTERM, true, termSignaled); err != nil {
		terminationErrs = append(terminationErrs, err)
	}

	if gracePeriod > 0 {
		timer := time.NewTimer(gracePeriod)
		ticker := time.NewTicker(processTreePollInterval)
		for {
			select {
			case <-ticker.C:
				_ = t.refresh()
				if err := t.signalTracked(syscall.SIGTERM, true, termSignaled); err != nil {
					terminationErrs = append(terminationErrs, err)
				}
			case <-timer.C:
				ticker.Stop()
				goto forceKill
			}
		}
	}

forceKill:
	// Freeze every process already known to belong to the command before the
	// final refresh. That prevents a surviving signal handler from forking a
	// detached child in the gap between discovery and force-kill.
	if err := t.signalGroup(syscall.SIGSTOP); err != nil {
		terminationErrs = append(terminationErrs, fmt.Errorf("stop command process group %d before force-kill: %w", t.groupID, err))
	}
	if err := t.signalTracked(syscall.SIGSTOP, false, nil); err != nil {
		terminationErrs = append(terminationErrs, err)
	}
	if err := t.refresh(); err != nil {
		terminationErrs = append(terminationErrs, fmt.Errorf("refresh command process tree before force-kill: %w", err))
	}
	if err := t.signalGroup(forceKillSignal); err != nil {
		terminationErrs = append(terminationErrs, fmt.Errorf("force-kill command process group %d: %w", t.groupID, err))
	}
	if err := t.signalTracked(forceKillSignal, false, nil); err != nil {
		terminationErrs = append(terminationErrs, err)
	}
	return errors.Join(terminationErrs...)
}

func (t *ownedProcessTree) signalGroup(signal syscall.Signal) error {
	err := signalOwnedProcessGroup(t.groupID, signal)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if errors.Is(err, syscall.EPERM) {
		live, inspectErr := processGroupHasLiveMembers(t.groupID)
		if inspectErr == nil && !live {
			return nil
		}
		return errors.Join(err, inspectErr)
	}
	return err
}

func processGroupHasLiveMembers(groupID int) (bool, error) {
	snapshots, err := listProcessGroupSnapshots(groupID)
	if err != nil {
		return false, err
	}
	for _, snapshot := range snapshots {
		if snapshot.groupID == groupID && !snapshot.zombie {
			return true, nil
		}
	}
	return false, nil
}

func (t *ownedProcessTree) signalTracked(
	signal syscall.Signal,
	detachedOnly bool,
	alreadySignaled map[processKey]struct{},
) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var signalErrs []error
	for key, process := range t.processes {
		if process.snapshot.pid == t.rootPID || process.snapshot.zombie {
			continue
		}
		if detachedOnly && process.snapshot.groupID == t.groupID {
			continue
		}
		if _, ok := alreadySignaled[key]; ok {
			continue
		}
		err := process.handle.Signal(signal)
		if err == nil || errors.Is(err, errProcessGone) || errors.Is(err, syscall.ESRCH) {
			if alreadySignaled != nil {
				alreadySignaled[key] = struct{}{}
			}
			if err != nil {
				if closeErr := process.handle.Close(); closeErr != nil {
					signalErrs = append(signalErrs, fmt.Errorf("close exited process %d handle: %w", process.snapshot.pid, closeErr))
				}
				delete(t.processes, key)
			}
			continue
		}
		signalErrs = append(signalErrs, fmt.Errorf("signal descendant process %d with %s: %w", process.snapshot.pid, signal, err))
	}
	return errors.Join(signalErrs...)
}

func (t *ownedProcessTree) Close() error {
	if t == nil {
		return nil
	}
	t.closeOnce.Do(func() {
		close(t.stop)
		<-t.done
		t.closeErr = t.closeProcessHandles()
	})
	return t.closeErr
}

func (t *ownedProcessTree) closeProcessHandles() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var closeErrs []error
	for _, process := range t.processes {
		if err := process.handle.Close(); err != nil {
			closeErrs = append(closeErrs, err)
		}
	}
	return errors.Join(closeErrs...)
}
