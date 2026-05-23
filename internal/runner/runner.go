package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
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

type CommandRunner interface {
	Run(context.Context, Command) (Result, error)
}

type OSCommandRunner struct{}

func NewOSCommandRunner() *OSCommandRunner {
	return &OSCommandRunner{}
}

func (r *OSCommandRunner) Run(ctx context.Context, command Command) (Result, error) {
	if strings.TrimSpace(command.Name) == "" {
		return Result{}, errors.New("command name is required")
	}

	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	cmd.Dir = command.Dir
	if len(command.Env) > 0 {
		cmd.Env = append(cmd.Environ(), command.Env...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	if err == nil {
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}

	return result, fmt.Errorf("command failed: %s: %w", command.String(), err)
}
