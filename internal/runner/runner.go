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
)

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
	cmd *exec.Cmd
}

// NewExecCommand builds an executable command with the runner package's shared
// command validation, working directory, and environment semantics.
func NewExecCommand(ctx context.Context, command Command) (*ExecCommand, error) {
	cmd, err := newExecCommand(ctx, command)
	if err != nil {
		return nil, err
	}
	return &ExecCommand{cmd: cmd}, nil
}

func (c *ExecCommand) Run() error {
	return c.cmd.Run()
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

type OSCommandRunner struct{}

func NewOSCommandRunner() *OSCommandRunner {
	return &OSCommandRunner{}
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

	err = cmd.Run()
	return ResultFromCommand(command, stdout.String(), stderr.String(), err)
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

	err = cmd.Run()
	return ResultFromCommand(command, "", "", err)
}

func newExecCommand(ctx context.Context, command Command) (*exec.Cmd, error) {
	if strings.TrimSpace(command.Name) == "" {
		return nil, errors.New("command name is required")
	}

	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	if len(command.Env) > 0 {
		cmd.Env = append(cmd.Environ(), command.Env...)
	}
	return cmd, nil
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
