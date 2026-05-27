package stages

import (
	"context"
	"errors"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/runner"
)

func runCommand(ctx context.Context, execCtx ExecutionContext, command runner.Command) error {
	if execCtx.Runner == nil {
		return errors.New("runner is required")
	}

	if execCtx.Logger != nil {
		if err := execCtx.Logger.Log(runner.Event{
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: runner.EventTypeCommandStarted,
			Command:   command.String(),
		}); err != nil {
			return err
		}
	}

	run := execCtx.Runner.Run
	if command.Interactive && execCtx.InteractiveRunner != nil {
		run = execCtx.InteractiveRunner.RunInteractive
	}
	result, err := run(ctx, command)
	if execCtx.Logger != nil {
		if logErr := logCommandOutput(execCtx, runner.EventTypeCommandStdout, result.Stdout); logErr != nil {
			return logErr
		}
		if logErr := logCommandOutput(execCtx, runner.EventTypeCommandStderr, result.Stderr); logErr != nil {
			return logErr
		}

		exitCode := result.ExitCode
		event := runner.Event{
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: runner.EventTypeCommandCompleted,
			Command:   command.String(),
			ExitCode:  &exitCode,
		}
		if err != nil {
			event.Level = "error"
			event.Message = err.Error()
		} else {
			event.Message = "ok"
		}
		if logErr := execCtx.Logger.Log(event); logErr != nil {
			return logErr
		}
	}

	if err != nil {
		var commandErr *runner.CommandError
		if errors.As(err, &commandErr) {
			return commandErr
		}
		return &runner.CommandError{
			Command:  command,
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			Err:      err,
		}
	}
	return nil
}

func logCommandOutput(execCtx ExecutionContext, eventType runner.EventType, output string) error {
	if execCtx.Logger == nil || output == "" {
		return nil
	}
	for _, line := range splitOutputLines(output) {
		message := line
		if message == "" {
			message = "<blank>"
		}
		event := runner.Event{
			RunID:     execCtx.RunID.String(),
			StageID:   execCtx.StageID.String(),
			Attempt:   execCtx.Attempt,
			Mode:      execCtx.Mode.String(),
			EventType: eventType,
			Message:   message,
		}
		if eventType == runner.EventTypeCommandStderr {
			event.Level = "warn"
		}
		if err := execCtx.Logger.Log(event); err != nil {
			return err
		}
	}
	return nil
}

func splitOutputLines(output string) []string {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 {
		return nil
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func logSimulation(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID.String(),
		StageID:   execCtx.StageID.String(),
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode.String(),
		EventType: runner.EventTypeSimulation,
		Message:   message,
	})
}

func logStageMessage(execCtx ExecutionContext, message string) error {
	if execCtx.Logger == nil {
		return nil
	}
	return execCtx.Logger.Log(runner.Event{
		RunID:     execCtx.RunID.String(),
		StageID:   execCtx.StageID.String(),
		Attempt:   execCtx.Attempt,
		Mode:      execCtx.Mode.String(),
		EventType: runner.EventTypeStageMessage,
		Message:   message,
	})
}

func remoteInstallCommand(url string, env []string, interpreter string, args ...string) string {
	parts := make([]string, 0, len(env)+len(args)+2)
	parts = append(parts, env...)
	parts = append(parts, shellQuote(interpreter), "\"$install_script\"")
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join([]string{
		"set -e",
		"install_script=$(mktemp -t laptop-setup-install.XXXXXX)",
		"trap 'rm -f \"$install_script\"' EXIT",
		"curl -fsSL " + shellQuote(url) + " -o \"$install_script\"",
		strings.Join(parts, " "),
	}, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
