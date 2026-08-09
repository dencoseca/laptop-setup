package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultTerminationGracePeriod = 2 * time.Second

type Command struct {
	Name        string
	Args        []string
	Dir         string
	Env         []string
	Interactive bool
	Prompt      string
}

func (c Command) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Name)
	for _, arg := range c.Args {
		if strings.ContainsAny(arg, " \t\n") {
			parts = append(parts, fmt.Sprintf("%q", arg))
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type CommandError struct {
	Command  Command
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

func (e *CommandError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("command failed (exit=%d): %s", e.ExitCode, e.Command.String())
	}
	return fmt.Sprintf("command failed (exit=%d): %s: %v", e.ExitCode, e.Command.String(), e.Err)
}

func (e *CommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ExecCommand adapts a Command to terminal executors such as Bubble Tea's tea.Exec.
type ExecCommand struct {
	ctx                    context.Context
	cmd                    *exec.Cmd
	terminationGracePeriod time.Duration
}

// NewExecCommand builds an executable command with the runner package's shared
// command validation, working directory, and environment semantics.
func NewExecCommand(ctx context.Context, command Command) (*ExecCommand, error) {
	cmd, err := newExecCommand(ctx, command)
	if err != nil {
		return nil, err
	}
	return &ExecCommand{
		ctx:                    ctx,
		cmd:                    cmd,
		terminationGracePeriod: defaultTerminationGracePeriod,
	}, nil
}

func (c *ExecCommand) Run() error {
	return runOwnedCommand(c.ctx, c.cmd, c.terminationGracePeriod)
}

func (c *ExecCommand) SetStdin(reader io.Reader) {
	if c.cmd.Stdin == nil {
		c.cmd.Stdin = reader
	}
}

func (c *ExecCommand) SetStdout(writer io.Writer) {
	if c.cmd.Stdout == nil {
		c.cmd.Stdout = writer
	}
}

func (c *ExecCommand) SetStderr(writer io.Writer) {
	if c.cmd.Stderr == nil {
		c.cmd.Stderr = writer
	}
}

// ResultFromCommand converts a completed command run into a Result and, when
// needed, the shared CommandError type used by command runners.
func ResultFromCommand(command Command, stdout string, stderr string, err error) (Result, error) {
	result := Result{
		ExitCode: 0,
		Stdout:   stdout,
		Stderr:   stderr,
	}
	if err == nil {
		return result, nil
	}

	result.ExitCode = commandExitCode(err)
	return result, &CommandError{
		Command:  command,
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Err:      err,
	}
}

type CommandRunner interface {
	Run(context.Context, Command) (Result, error)
	LookPath(context.Context, string) (string, error)
}

type InteractiveRunner interface {
	RunInteractive(context.Context, Command) (Result, error)
}

type InteractiveRunnerFunc func(context.Context, Command) (Result, error)

func (f InteractiveRunnerFunc) RunInteractive(ctx context.Context, command Command) (Result, error) {
	return f(ctx, command)
}

type OSCommandRunner struct {
	terminationGracePeriod time.Duration
}

func NewOSCommandRunner() *OSCommandRunner {
	return &OSCommandRunner{terminationGracePeriod: defaultTerminationGracePeriod}
}

func (r *OSCommandRunner) Run(ctx context.Context, command Command) (Result, error) {
	cmd, err := newExecCommand(ctx, command)
	if err != nil {
		return Result{}, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = runOwnedCommand(ctx, cmd, r.gracePeriod())
	return ResultFromCommand(command, stdout.String(), stderr.String(), err)
}

func (r *OSCommandRunner) gracePeriod() time.Duration {
	if r == nil || r.terminationGracePeriod < 0 {
		return defaultTerminationGracePeriod
	}
	return r.terminationGracePeriod
}

func (r *OSCommandRunner) LookPath(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", errors.New("command name is required")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("command %q not found: %w", name, err)
	}
	return path, nil
}

type OSInteractiveRunner struct{}

func NewOSInteractiveRunner() *OSInteractiveRunner {
	return &OSInteractiveRunner{}
}

func (r *OSInteractiveRunner) RunInteractive(ctx context.Context, command Command) (Result, error) {
	cmd, err := newExecCommand(ctx, command)
	if err != nil {
		return Result{}, err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = runOwnedCommand(ctx, cmd, defaultTerminationGracePeriod)
	return ResultFromCommand(command, "", "", err)
}

func newExecCommand(ctx context.Context, command Command) (*exec.Cmd, error) {
	if strings.TrimSpace(command.Name) == "" {
		return nil, errors.New("command name is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cmd := exec.Command(command.Name, command.Args...)
	cmd.Dir = command.Dir
	if len(command.Env) > 0 {
		cmd.Env = append(cmd.Environ(), command.Env...)
	}
	return cmd, nil
}

func runOwnedCommand(ctx context.Context, cmd *exec.Cmd, gracePeriod time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if gracePeriod < 0 {
		gracePeriod = 0
	}

	configureOwnedProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}

	watcher, err := newProcessExitWatcher(cmd.Process.Pid)
	if err != nil {
		killErr := signalOwnedProcessGroup(cmd.Process.Pid, forceKillSignal)
		waitErr := cmd.Wait()
		return errors.Join(fmt.Errorf("watch command process: %w", err), killErr, waitErr)
	}
	defer watcher.Close()
	processTree, err := newOwnedProcessTree(cmd.Process.Pid)
	if err != nil {
		killErr := signalOwnedProcessGroup(cmd.Process.Pid, forceKillSignal)
		waitErr := cmd.Wait()
		return errors.Join(fmt.Errorf("track command process tree: %w", err), killErr, waitErr)
	}
	defer processTree.Close()

	exited := make(chan error, 1)
	go func() {
		exited <- watcher.Wait()
	}()

	select {
	case watchErr := <-exited:
		return finishOwnedCommandAfterLeaderExit(ctx, cmd, processTree, gracePeriod, watchErr)
	case <-ctx.Done():
		// The platform watcher observes exit without reaping the group leader.
		// Keeping that PID reserved until the grace period and force-kill finish
		// prevents the process-group ID from being reused for unrelated work.
		terminationErr := processTree.terminate(gracePeriod)
		watchErr := <-exited
		waitErr := cmd.Wait()
		return errors.Join(ctx.Err(), terminationErr, watchErr, waitErr)
	}
}

func finishOwnedCommandAfterLeaderExit(
	ctx context.Context,
	cmd *exec.Cmd,
	processTree *ownedProcessTree,
	gracePeriod time.Duration,
	watchErr error,
) error {
	if watchErr != nil {
		terminationErr := processTree.terminate(0)
		waitErr := cmd.Wait()
		return errors.Join(fmt.Errorf("watch command process: %w", watchErr), terminationErr, waitErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		terminationErr := processTree.terminate(gracePeriod)
		waitErr := cmd.Wait()
		return errors.Join(ctxErr, terminationErr, waitErr)
	}
	return cmd.Wait()
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
