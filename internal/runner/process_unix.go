//go:build darwin || linux

package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const forceKillSignal = syscall.SIGKILL

func configureOwnedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateOwnedProcessGroup(processGroupID int, gracePeriod time.Duration) error {
	termErr := signalOwnedProcessGroup(processGroupID, syscall.SIGTERM)
	if termErr != nil && !errors.Is(termErr, syscall.ESRCH) {
		return fmt.Errorf("terminate command process group %d: %w", processGroupID, termErr)
	}
	if errors.Is(termErr, syscall.ESRCH) {
		return nil
	}

	if gracePeriod > 0 {
		timer := time.NewTimer(gracePeriod)
		<-timer.C
	}

	killErr := signalOwnedProcessGroup(processGroupID, forceKillSignal)
	if killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		return fmt.Errorf("force-kill command process group %d: %w", processGroupID, killErr)
	}
	return nil
}

func signalOwnedProcessGroup(processGroupID int, signal syscall.Signal) error {
	if processGroupID <= 0 {
		return errors.New("command process group id must be positive")
	}
	return unix.Kill(-processGroupID, signal)
}
