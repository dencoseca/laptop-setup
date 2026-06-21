package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

type ExecutionRequest struct {
	Resume      bool
	DryRun      bool
	Current     *state.RunState
	Plan        []state.StageID
	Decisions   stages.DecisionSet
	SelectedIDs []string
}

type ExecutionRun struct {
	RunState     *state.RunState
	DryRun       bool
	RunDir       string
	HumanLogPath string
	EventsPath   string
	HumanLog     io.WriteCloser
	EventsLog    io.WriteCloser
}

type ExecutionHooks struct {
	OnStageStatus        func(state.StageID, state.StageStatus)
	OnFailure            func(context.Context, execution.Failure) (execution.FailureAction, error)
	OnInteractiveCommand func(context.Context, runner.Command) (runner.Result, error)
}

type ExecutionService interface {
	PrepareExecution(context.Context, ExecutionRequest) (ExecutionRun, error)
	Execute(context.Context, ExecutionRun, ExecutionHooks) error
}

const maxInteractiveOutputCaptureBytes = 1 << 20

type limitedOutputBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

type capturingExecCommand struct {
	cmd    *exec.Cmd
	stdout *limitedOutputBuffer
	stderr *limitedOutputBuffer
}

func newLimitedOutputBuffer(limit int) *limitedOutputBuffer {
	return &limitedOutputBuffer{limit: limit}
}

func (b *limitedOutputBuffer) Write(payload []byte) (int, error) {
	if b.limit <= 0 || b.buffer.Len() >= b.limit {
		b.truncated = b.truncated || len(payload) > 0
		return len(payload), nil
	}
	remaining := b.limit - b.buffer.Len()
	if len(payload) > remaining {
		_, _ = b.buffer.Write(payload[:remaining])
		b.truncated = true
		return len(payload), nil
	}
	_, _ = b.buffer.Write(payload)
	return len(payload), nil
}

func (b *limitedOutputBuffer) String() string {
	if b == nil {
		return ""
	}
	output := b.buffer.String()
	if b.truncated {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += "<output truncated>\n"
	}
	return output
}

func (c capturingExecCommand) Run() error {
	return c.cmd.Run()
}

func (c capturingExecCommand) SetStdin(reader io.Reader) {
	if c.cmd.Stdin == nil {
		c.cmd.Stdin = reader
	}
}

func (c capturingExecCommand) SetStdout(writer io.Writer) {
	if c.cmd.Stdout == nil {
		c.cmd.Stdout = io.MultiWriter(writer, c.stdout)
	}
}

func (c capturingExecCommand) SetStderr(writer io.Writer) {
	if c.cmd.Stderr == nil {
		c.cmd.Stderr = io.MultiWriter(writer, c.stderr)
	}
}

func (m *model) startExecutionFromReview() (tea.Model, tea.Cmd) {
	plan, err := m.resolvePlan()
	if err != nil {
		m.planError = err.Error()
		return *m, nil
	}
	if m.executionService == nil {
		m.planError = "execution service is required"
		return *m, nil
	}

	request := ExecutionRequest{
		Resume:      m.resumeRun,
		DryRun:      m.config.DryRun,
		Current:     m.current,
		Plan:        stringsToStageIDs(plan),
		Decisions:   m.collectDecisions(),
		SelectedIDs: m.selectedBrewIDs(),
	}
	if m.resumeRun && m.current != nil {
		request.Decisions = m.current.Decisions
		request.SelectedIDs = m.current.SelectedIDs
	}

	run, err := m.executionService.PrepareExecution(m.ctx, request)
	if err != nil {
		m.planError = err.Error()
		return *m, nil
	}

	m.planError = ""
	m.runState = run.RunState
	m.humanLogPath = run.HumanLogPath
	m.eventsLogPath = run.EventsPath
	m.stageOrder = stageIDsToStrings(run.RunState.ResolvedPlan)
	m.initialiseStageStatuses()

	m.screen = screenExecuting
	m.executing = true
	m.tailedLogs = nil
	m.logTailOffset = 0
	m.logTailCarry = ""
	m.updates = make(chan tea.Msg, 32)
	return *m, tea.Batch(
		startExecutionWorker(m.ctx, m.updates, run, m.executionService),
		waitForExecutionUpdate(m.updates),
		scheduleLogTailTick(),
		m.spinner.Tick,
		tea.Sequence(m.stopwatch.Reset(), m.stopwatch.Start()),
	)
}

func (m *model) initialiseStageStatuses() {
	if m.runState == nil {
		return
	}
	for _, stageID := range m.stageOrder {
		status := m.runState.Stages[state.StageID(stageID)]
		if status.Status == "" {
			status.Status = stages.StatusPending
		}
		m.stageStatuses[stageID] = status
	}
}

func (m *model) abortExecutionIfNeeded(action execution.FailureAction) {
	if m.failurePrompt != nil {
		m.resolveFailure(action)
	}
	if m.interactivePrompt != nil {
		select {
		case m.interactivePrompt.Response <- interactiveCommandResult{
			Result: runner.Result{ExitCode: -1},
			Err:    context.Canceled,
		}:
		default:
		}
		m.interactivePrompt = nil
	}
	if m.executing {
		m.cancel()
	}
}

func (m *model) resolveFailure(action execution.FailureAction) {
	if m.failurePrompt == nil {
		return
	}
	select {
	case m.failurePrompt.Response <- action:
	default:
	}
	m.failurePrompt = nil
}

func startExecutionWorker(
	ctx context.Context,
	updates chan<- tea.Msg,
	run ExecutionRun,
	service ExecutionService,
) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(updates)
			defer run.HumanLog.Close()
			defer run.EventsLog.Close()

			err := service.Execute(ctx, run, ExecutionHooks{
				OnStageStatus: func(stageID state.StageID, status state.StageStatus) {
					select {
					case updates <- stageStatusMsg{StageID: stageID.String(), Status: status}:
					case <-ctx.Done():
					}
				},
				OnFailure: func(inner context.Context, failure execution.Failure) (execution.FailureAction, error) {
					response := make(chan execution.FailureAction, 1)
					request := failureRequest{
						StageID:  failure.Stage.ID.String(),
						Title:    failure.Stage.Title,
						Attempt:  failure.Attempt,
						CanSkip:  failure.Stage.CanSkip,
						Message:  failure.Err.Error(),
						Response: response,
					}
					select {
					case updates <- failureRequestMsg{Request: request}:
					case <-inner.Done():
						return execution.ActionAbort, inner.Err()
					}

					select {
					case action := <-response:
						return action, nil
					case <-inner.Done():
						return execution.ActionAbort, inner.Err()
					}
				},
				OnInteractiveCommand: func(inner context.Context, command runner.Command) (runner.Result, error) {
					response := make(chan interactiveCommandResult, 1)
					request := interactiveCommandRequest{
						Command:  command,
						Response: response,
					}
					select {
					case updates <- interactiveCommandRequestMsg{Request: request}:
					case <-inner.Done():
						return runner.Result{ExitCode: -1}, inner.Err()
					}

					select {
					case result := <-response:
						return result.Result, result.Err
					case <-inner.Done():
						return runner.Result{ExitCode: -1}, inner.Err()
					}
				},
			})

			select {
			case updates <- executionDoneMsg{Err: err}:
			case <-ctx.Done():
			}
		}()
		return nil
	}
}

func runInteractiveCommand(request interactiveCommandRequest) tea.Cmd {
	command := request.Command
	cmd := exec.Command(command.Name, command.Args...)
	cmd.Dir = command.Dir
	if len(command.Env) > 0 {
		cmd.Env = append(cmd.Environ(), command.Env...)
	}
	stdout := newLimitedOutputBuffer(maxInteractiveOutputCaptureBytes)
	stderr := newLimitedOutputBuffer(maxInteractiveOutputCaptureBytes)
	execCommand := capturingExecCommand{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
	}
	return tea.Exec(execCommand, func(err error) tea.Msg {
		result := runner.Result{ExitCode: 0, Stdout: stdout.String(), Stderr: stderr.String()}
		if err != nil {
			result.ExitCode = commandExitCode(err)
			err = &runner.CommandError{
				Command:  command,
				ExitCode: result.ExitCode,
				Stdout:   result.Stdout,
				Stderr:   result.Stderr,
				Err:      err,
			}
		}
		return interactiveCommandFinishedMsg{
			Request: request,
			Result:  interactiveCommandResult{Result: result, Err: err},
		}
	})
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func waitForExecutionUpdate(updates <-chan tea.Msg) tea.Cmd {
	if updates == nil {
		return nil
	}
	return func() tea.Msg {
		message, ok := <-updates
		if !ok {
			return nil
		}
		return message
	}
}

func scheduleLogTailTick() tea.Cmd {
	return tea.Tick(logTailPollInterval, func(at time.Time) tea.Msg {
		return logTailTickMsg(at)
	})
}

func (m *model) pollRunLog() {
	if strings.TrimSpace(m.humanLogPath) == "" {
		return
	}

	file, err := os.Open(m.humanLogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}
	if info.Size() < m.logTailOffset {
		m.logTailOffset = 0
		m.logTailCarry = ""
	}
	if _, err = file.Seek(m.logTailOffset, io.SeekStart); err != nil {
		return
	}

	buffer := make([]byte, 4096)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			m.consumeLogTailChunk(string(buffer[:count]))
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return
			}
			break
		}
	}

	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}
	m.logTailOffset = offset
}

func (m *model) consumeLogTailChunk(chunk string) {
	if chunk == "" {
		return
	}
	payload := m.logTailCarry + chunk
	lines := strings.Split(payload, "\n")
	if strings.HasSuffix(payload, "\n") {
		m.logTailCarry = ""
	} else {
		m.logTailCarry = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		parsed := parseRunLogLine(line)
		if strings.TrimSpace(parsed.Line) == "" {
			continue
		}
		m.tailedLogs = append(m.tailedLogs, parsed)
	}
	if len(m.tailedLogs) > bufferedLogLineLimit {
		m.tailedLogs = append([]tailedLogLine(nil), m.tailedLogs[len(m.tailedLogs)-bufferedLogLineLimit:]...)
	}
}

func parseRunLogLine(raw string) tailedLogLine {
	line := strings.TrimSpace(raw)
	if line == "" {
		return tailedLogLine{}
	}

	parsed := tailedLogLine{Line: line}
	parts := strings.Split(line, " | ")
	if len(parts) < 4 {
		return parsed
	}
	if !isLogLevel(parts[1]) {
		return parsed
	}

	candidate := strings.TrimSpace(parts[2])
	if candidate == "" || isEventToken(candidate) {
		return parsed
	}

	parsed.StageID = candidate
	return parsed
}

func isLogLevel(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "INFO", "WARN", "ERROR", "DEBUG":
		return true
	default:
		return false
	}
}

func isEventToken(value string) bool {
	switch strings.TrimSpace(value) {
	case string(runner.EventTypeRunStarted),
		string(runner.EventTypeRunCompleted),
		string(runner.EventTypeStageStarted),
		string(runner.EventTypeStageAlreadyDone),
		string(runner.EventTypeStageCompleted),
		string(runner.EventTypeStageFailed),
		string(runner.EventTypeStageRetry),
		string(runner.EventTypeStageSkipped),
		string(runner.EventTypeCommandStarted),
		string(runner.EventTypeCommandCompleted),
		string(runner.EventTypeCommandStdout),
		string(runner.EventTypeCommandStderr),
		string(runner.EventTypeSimulation),
		string(runner.EventTypeStageMessage):
		return true
	default:
		return false
	}
}

func currentLogStageID(stageOrder []string, statuses map[string]state.StageStatus) string {
	for _, stageID := range stageOrder {
		if statuses[stageID].Status == stages.StatusRunning {
			return stageID
		}
	}
	for idx := len(stageOrder) - 1; idx >= 0; idx-- {
		stageID := stageOrder[idx]
		status := statuses[stageID].Status
		if status != "" && status != stages.StatusPending {
			return stageID
		}
	}
	return ""
}
