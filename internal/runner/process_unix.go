//go:build darwin || linux

package runner

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const forceKillSignal = syscall.SIGKILL

func configureOwnedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalOwnedProcessGroup(processGroupID int, signal syscall.Signal) error {
	if processGroupID <= 0 {
		return errors.New("command process group id must be positive")
	}
	return unix.Kill(-processGroupID, signal)
}
