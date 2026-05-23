package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

var ErrAborted = errors.New("execution aborted")

type FailureAction string

const (
	ActionAbort FailureAction = "abort"
	ActionRetry FailureAction = "retry"
	ActionSkip  FailureAction = "skip"
)

type Failure struct {
	Stage   stages.Stage
	Attempt int
	Err     error
}

type Hooks struct {
	OnEvent       func(event runner.Event)
	OnStageStatus func(stageID string, status state.StageStatus)
	OnFailure     func(ctx context.Context, failure Failure) (FailureAction, error)
}

type Options struct {
	Store         *state.Store
	RunState      *state.RunState
	Catalog       []stages.Stage
	DryRun        bool
	RepoRoot      string
	HomeDir       string
	RunDir        string
	CommandRunner runner.CommandRunner
	Logger        *runner.EventLogger
	Hooks         Hooks
}

type hookLogger struct {
	base    *runner.EventLogger
	onEvent func(event runner.Event)
}

func (l *hookLogger) Log(event runner.Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "info"
	}
	if err := l.base.Log(event); err != nil {
		return err
	}
	if l.onEvent != nil {
		l.onEvent(event)
	}
	return nil
}

func Execute(ctx context.Context, options Options) error {
	if options.Store == nil {
		return errors.New("state store is required")
	}
	if options.RunState == nil {
		return errors.New("run state is required")
	}
	if options.CommandRunner == nil {
		return errors.New("command runner is required")
	}
	if options.Logger == nil {
		return errors.New("event logger is required")
	}

	runState := options.RunState
	if runState.Stages == nil {
		runState.Stages = make(map[string]state.StageStatus, len(options.Catalog))
	}

	stageIndex := make(map[string]stages.Stage, len(options.Catalog))
	for _, stage := range options.Catalog {
		stageIndex[stage.ID] = stage
	}

	logger := &hookLogger{
		base:    options.Logger,
		onEvent: options.Hooks.OnEvent,
	}

	if err := logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_started",
		Message:   "Starting stage execution",
	}); err != nil {
		return err
	}

	for _, stageID := range runState.ResolvedPlan {
		stage, ok := stageIndex[stageID]
		if !ok {
			return fmt.Errorf("unknown stage in plan: %s", stageID)
		}

		progress := runState.Stages[stageID]
		if isTerminalStatus(progress.Status) {
			emitStageStatus(options.Hooks, stageID, progress)
			continue
		}

		for {
			progress.Status = string(stages.StatusRunning)
			progress.Attempts++
			progress.LastError = ""
			runState.Stages[stageID] = progress
			if err := options.Store.Save(ctx, runState); err != nil {
				return err
			}
			emitStageStatus(options.Hooks, stageID, progress)

			if err := logger.Log(runner.Event{
				RunID:     runState.RunID,
				StageID:   stageID,
				Attempt:   progress.Attempts,
				Mode:      runState.Mode,
				EventType: "stage_started",
				Message:   stage.Title,
			}); err != nil {
				return err
			}

			execCtx := stages.ExecutionContext{
				DryRun:                options.DryRun,
				Runner:                options.CommandRunner,
				Logger:                logger,
				RunID:                 runState.RunID,
				Mode:                  runState.Mode,
				StageID:               stageID,
				Attempt:               progress.Attempts,
				RunDir:                options.RunDir,
				RepoRoot:              options.RepoRoot,
				HomeDir:               options.HomeDir,
				Decisions:             runState.Decisions,
				SelectedBrewIDs:       runState.SelectedIDs,
				GeneratedBrewfilePath: runState.GeneratedFile,
				SetGeneratedBrewfilePath: func(path string) {
					runState.GeneratedFile = path
				},
			}

			checkResult, precheckErr := stage.Precheck(ctx, execCtx)
			if precheckErr != nil {
				action, err := processFailure(ctx, options, logger, stage, progress, precheckErr)
				if err != nil {
					return err
				}
				if action == ActionRetry {
					progress = runState.Stages[stageID]
					continue
				}
				break
			}

			if checkResult.Satisfied {
				progress.Status = string(stages.StatusAlreadyDone)
				progress.LastError = ""
				runState.Stages[stageID] = progress
				runState.LastFailure = ""
				if err := options.Store.Save(ctx, runState); err != nil {
					return err
				}
				emitStageStatus(options.Hooks, stageID, progress)
				if err := logger.Log(runner.Event{
					RunID:     runState.RunID,
					StageID:   stageID,
					Attempt:   progress.Attempts,
					Mode:      runState.Mode,
					EventType: "stage_already_done",
					Message:   checkResult.Message,
				}); err != nil {
					return err
				}
				break
			}

			var runErr error
			if options.DryRun {
				runErr = stage.Simulate(ctx, execCtx)
			} else {
				runErr = stage.Run(ctx, execCtx)
			}
			if runErr != nil {
				action, err := processFailure(ctx, options, logger, stage, progress, runErr)
				if err != nil {
					return err
				}
				if action == ActionRetry {
					progress = runState.Stages[stageID]
					continue
				}
				break
			}

			if options.DryRun {
				progress.Status = string(stages.StatusSimulatedSuccess)
			} else {
				progress.Status = string(stages.StatusSuccess)
			}
			progress.LastError = ""
			runState.Stages[stageID] = progress
			runState.LastFailure = ""
			if err := options.Store.Save(ctx, runState); err != nil {
				return err
			}
			emitStageStatus(options.Hooks, stageID, progress)
			if err := logger.Log(runner.Event{
				RunID:     runState.RunID,
				StageID:   stageID,
				Attempt:   progress.Attempts,
				Mode:      runState.Mode,
				EventType: "stage_completed",
				Message:   progress.Status,
			}); err != nil {
				return err
			}
			break
		}
	}

	endAt := time.Now().UTC()
	runState.EndAt = &endAt
	if err := options.Store.Save(ctx, runState); err != nil {
		return err
	}

	if err := logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_completed",
		Message:   "All planned stages processed",
	}); err != nil {
		return err
	}

	return nil
}

func processFailure(
	ctx context.Context,
	options Options,
	logger *hookLogger,
	stage stages.Stage,
	progress state.StageStatus,
	stageErr error,
) (FailureAction, error) {
	stageID := stage.ID
	progress.Status = string(stages.StatusFailed)
	progress.LastError = stageErr.Error()
	options.RunState.Stages[stageID] = progress
	options.RunState.LastFailure = stageErr.Error()
	if err := options.Store.Save(ctx, options.RunState); err != nil {
		return ActionAbort, err
	}
	emitStageStatus(options.Hooks, stageID, progress)

	if err := logger.Log(runner.Event{
		Level:     "error",
		RunID:     options.RunState.RunID,
		StageID:   stageID,
		Attempt:   progress.Attempts,
		Mode:      options.RunState.Mode,
		EventType: "stage_failed",
		Message:   stageErr.Error(),
	}); err != nil {
		return ActionAbort, err
	}

	if options.Hooks.OnFailure == nil {
		return ActionAbort, fmt.Errorf("stage failed for %s: %w", stageID, stageErr)
	}

	action, err := options.Hooks.OnFailure(ctx, Failure{
		Stage:   stage,
		Attempt: progress.Attempts,
		Err:     stageErr,
	})
	if err != nil {
		return ActionAbort, err
	}

	switch action {
	case ActionRetry:
		if err = logger.Log(runner.Event{
			RunID:     options.RunState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      options.RunState.Mode,
			EventType: "stage_retry",
			Message:   "Retrying failed stage",
		}); err != nil {
			return ActionAbort, err
		}
		return ActionRetry, nil
	case ActionSkip:
		if !stage.CanSkip {
			return ActionAbort, fmt.Errorf("stage %s cannot be skipped", stageID)
		}
		progress.Status = string(stages.StatusSkipped)
		options.RunState.Stages[stageID] = progress
		if err = options.Store.Save(ctx, options.RunState); err != nil {
			return ActionAbort, err
		}
		emitStageStatus(options.Hooks, stageID, progress)
		if err = logger.Log(runner.Event{
			RunID:     options.RunState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      options.RunState.Mode,
			EventType: "stage_skipped",
			Message:   "Skipped after failure",
		}); err != nil {
			return ActionAbort, err
		}
		return ActionSkip, nil
	case ActionAbort:
		return ActionAbort, fmt.Errorf("%w: stage %s: %v", ErrAborted, stageID, stageErr)
	default:
		return ActionAbort, fmt.Errorf("unknown failure action %q", action)
	}
}

func emitStageStatus(hooks Hooks, stageID string, status state.StageStatus) {
	if hooks.OnStageStatus != nil {
		hooks.OnStageStatus(stageID, status)
	}
}

func isTerminalStatus(status string) bool {
	switch status {
	case string(stages.StatusSuccess),
		string(stages.StatusAlreadyDone),
		string(stages.StatusSimulatedSuccess),
		string(stages.StatusSkipped):
		return true
	default:
		return false
	}
}
