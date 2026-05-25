package execution

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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

func (action FailureAction) Validate() error {
	switch action {
	case ActionAbort, ActionRetry, ActionSkip:
		return nil
	default:
		return fmt.Errorf("unknown failure action %q", action)
	}
}

type Failure struct {
	Stage   stages.Stage
	Attempt int
	Err     error
}

type Hooks struct {
	OnEvent       func(event runner.Event)
	OnStageStatus func(stageID state.StageID, status state.StageStatus)
	OnFailure     func(ctx context.Context, failure Failure) (FailureAction, error)
}

type DryRunStageDelayFunc func(ctx context.Context, execCtx stages.ExecutionContext) error

type StateRepository interface {
	Save(context.Context, *state.RunState) error
}

type Options struct {
	Store          StateRepository
	RunState       *state.RunState
	Catalog        []stages.Stage
	DryRun         bool
	DryRunDelay    DryRunStageDelayFunc
	RepoRoot       string
	HomeDir        string
	RunDir         string
	CommandRunner  runner.CommandRunner
	FileSystem     stages.FileSystem
	TemplateStore  stages.TemplateStore
	PackageManager stages.PackageManager
	Logger         *runner.EventLogger
	Hooks          Hooks
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
	if err := ValidateRunStateForCatalog(options.RunState, options.Catalog, options.DryRun); err != nil {
		return err
	}

	runState := options.RunState
	if runState.Stages == nil {
		runState.Stages = make(map[state.StageID]state.StageStatus, len(options.Catalog))
	}

	stageIndex := make(map[stages.StageID]stages.Stage, len(options.Catalog))
	for _, stage := range options.Catalog {
		stageIndex[stage.ID] = stage
	}

	logger := &hookLogger{
		base:    options.Logger,
		onEvent: options.Hooks.OnEvent,
	}

	if err := logger.Log(runner.Event{
		RunID:     runState.RunID.String(),
		Mode:      runState.Mode.String(),
		EventType: runner.EventTypeRunStarted,
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
			progress.Status = stages.StatusRunning
			progress.Attempts++
			progress.LastError = ""
			runState.Stages[stageID] = progress
			if err := options.Store.Save(ctx, runState); err != nil {
				return err
			}
			emitStageStatus(options.Hooks, stageID, progress)

			if err := logger.Log(runner.Event{
				RunID:     runState.RunID.String(),
				StageID:   stageID.String(),
				Attempt:   progress.Attempts,
				Mode:      runState.Mode.String(),
				EventType: runner.EventTypeStageStarted,
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
				FileSystem:            options.FileSystem,
				TemplateStore:         options.TemplateStore,
				PackageManager:        options.PackageManager,
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
				progress.Status = stages.StatusAlreadyDone
				progress.LastError = ""
				runState.Stages[stageID] = progress
				runState.LastFailure = ""
				if err := options.Store.Save(ctx, runState); err != nil {
					return err
				}
				emitStageStatus(options.Hooks, stageID, progress)
				if err := logger.Log(runner.Event{
					RunID:     runState.RunID.String(),
					StageID:   stageID.String(),
					Attempt:   progress.Attempts,
					Mode:      runState.Mode.String(),
					EventType: runner.EventTypeStageAlreadyDone,
					Message:   checkResult.Message,
				}); err != nil {
					return err
				}
				break
			}

			var runErr error
			if options.DryRun {
				if options.DryRunDelay != nil {
					if err := options.DryRunDelay(ctx, execCtx); err != nil {
						runErr = err
					}
				}
				if runErr == nil {
					runErr = stage.Simulate(ctx, execCtx)
				}
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
				progress.Status = stages.StatusSimulatedSuccess
			} else {
				progress.Status = stages.StatusSuccess
			}
			progress.LastError = ""
			runState.Stages[stageID] = progress
			runState.LastFailure = ""
			if err := options.Store.Save(ctx, runState); err != nil {
				return err
			}
			emitStageStatus(options.Hooks, stageID, progress)
			if err := logger.Log(runner.Event{
				RunID:     runState.RunID.String(),
				StageID:   stageID.String(),
				Attempt:   progress.Attempts,
				Mode:      runState.Mode.String(),
				EventType: runner.EventTypeStageCompleted,
				Message:   progress.Status.String(),
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
		RunID:     runState.RunID.String(),
		Mode:      runState.Mode.String(),
		EventType: runner.EventTypeRunCompleted,
		Message:   "All planned stages processed",
	}); err != nil {
		return err
	}

	return nil
}

func ValidateRunStateForCatalog(runState *state.RunState, catalog []stages.Stage, dryRun bool) error {
	if err := state.ValidateRunState(runState); err != nil {
		return fmt.Errorf("resume state: %w", err)
	}
	if runState.Mode.IsDryRun() != dryRun {
		return fmt.Errorf("resume state field mode: saved mode %q is incompatible with requested dry-run=%t", runState.Mode, dryRun)
	}
	if len(catalog) == 0 {
		return errors.New("resume state: catalog is empty")
	}

	stageIndex := make(map[stages.StageID]stages.Stage, len(catalog))
	for index, stage := range catalog {
		if stage.ID == "" {
			return fmt.Errorf("resume state: catalog stage at index %d has empty id", index)
		}
		if _, exists := stageIndex[stage.ID]; exists {
			return fmt.Errorf("resume state: catalog contains duplicate stage id %q", stage.ID)
		}
		stageIndex[stage.ID] = stage
	}

	for index, stageID := range runState.ResolvedPlan {
		stage, ok := stageIndex[stageID]
		if !ok {
			return fmt.Errorf("resume state field resolved_plan[%d]: unknown stage id %q", index, stageID)
		}
		if stage.Precheck == nil {
			return fmt.Errorf("resume state field resolved_plan[%d]: stage %q has no precheck", index, stageID)
		}
		if dryRun {
			if stage.Simulate == nil {
				return fmt.Errorf("resume state field resolved_plan[%d]: stage %q has no dry-run simulation", index, stageID)
			}
			continue
		}
		if stage.Run == nil {
			return fmt.Errorf("resume state field resolved_plan[%d]: stage %q has no runner", index, stageID)
		}
	}

	for stageID := range runState.Stages {
		if _, ok := stageIndex[stageID]; !ok {
			return fmt.Errorf("resume state field stages.%s: unknown stage id %q", stageID, stageID)
		}
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
	progress.Status = stages.StatusFailed
	progress.LastError = stageErr.Error()
	options.RunState.Stages[stageID] = progress
	options.RunState.LastFailure = stageErr.Error()
	if err := options.Store.Save(ctx, options.RunState); err != nil {
		return ActionAbort, err
	}
	emitStageStatus(options.Hooks, stageID, progress)

	if err := logger.Log(runner.Event{
		Level:     "error",
		RunID:     options.RunState.RunID.String(),
		StageID:   stageID.String(),
		Attempt:   progress.Attempts,
		Mode:      options.RunState.Mode.String(),
		EventType: runner.EventTypeStageFailed,
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

	if err := action.Validate(); err != nil {
		return ActionAbort, err
	}

	switch action {
	case ActionRetry:
		if err = logger.Log(runner.Event{
			RunID:     options.RunState.RunID.String(),
			StageID:   stageID.String(),
			Attempt:   progress.Attempts,
			Mode:      options.RunState.Mode.String(),
			EventType: runner.EventTypeStageRetry,
			Message:   "Retrying failed stage",
		}); err != nil {
			return ActionAbort, err
		}
		return ActionRetry, nil
	case ActionSkip:
		if !stage.CanSkip {
			return ActionAbort, fmt.Errorf("stage %s cannot be skipped", stageID)
		}
		progress.Status = stages.StatusSkipped
		options.RunState.Stages[stageID] = progress
		if err = options.Store.Save(ctx, options.RunState); err != nil {
			return ActionAbort, err
		}
		emitStageStatus(options.Hooks, stageID, progress)
		if err = logger.Log(runner.Event{
			RunID:     options.RunState.RunID.String(),
			StageID:   stageID.String(),
			Attempt:   progress.Attempts,
			Mode:      options.RunState.Mode.String(),
			EventType: runner.EventTypeStageSkipped,
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

func emitStageStatus(hooks Hooks, stageID state.StageID, status state.StageStatus) {
	if hooks.OnStageStatus != nil {
		hooks.OnStageStatus(stageID, status)
	}
}

func isTerminalStatus(status state.StageStatusValue) bool {
	return state.IsTerminalStatus(status)
}

func RandomDryRunStageDelay(minDelay time.Duration, maxDelay time.Duration) DryRunStageDelayFunc {
	if minDelay < 0 {
		minDelay = 0
	}
	if maxDelay < 0 {
		maxDelay = 0
	}
	if maxDelay < minDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	return func(ctx context.Context, _ stages.ExecutionContext) error {
		delay := minDelay
		if maxDelay > minDelay {
			delay += time.Duration(rng.Int63n(int64(maxDelay-minDelay) + 1))
		}
		if delay <= 0 {
			return nil
		}

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
}
