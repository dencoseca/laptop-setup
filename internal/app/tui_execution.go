package app

import (
	"context"
	"fmt"
	"time"

	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
)

type tuiExecutionService struct {
	deps          Dependencies
	store         StateRepository
	catalog       []stages.Stage
	repoRoot      string
	homeDir       string
	commandRunner runner.CommandRunner
	templateStore stages.TemplateStore
}

func (s tuiExecutionService) PrepareExecution(ctx context.Context, request ui.ExecutionRequest) (ui.ExecutionRun, error) {
	var (
		runState *state.RunState
		dryRun   bool
	)

	if request.Resume {
		if request.Current == nil {
			return ui.ExecutionRun{}, fmt.Errorf("resume requested but no existing run state found")
		}
		runState = request.Current
		dryRun = runState.Mode.IsDryRun()
		if err := execution.ValidateRunStateForCatalog(runState, s.catalog, dryRun); err != nil {
			return ui.ExecutionRun{}, err
		}
	} else {
		dryRun = request.DryRun
		now := time.Now()
		runState = &state.RunState{
			RunID:        state.NewRunID(now),
			StartAt:      now.UTC(),
			Mode:         state.ModeFromDryRun(dryRun),
			ResolvedPlan: request.Plan,
			Decisions:    request.Decisions,
			SelectedIDs:  request.SelectedIDs,
			Stages:       make(map[state.StageID]state.StageStatus, len(s.catalog)),
		}
	}

	if !request.Resume {
		runState.SelectedIDs = request.SelectedIDs
		runState.ResolvedPlan = request.Plan
		runState.Decisions = request.Decisions
	}
	if err := state.NormalizeRunState(runState); err != nil {
		return ui.ExecutionRun{}, fmt.Errorf("normalize run state: %w", err)
	}
	if err := runState.Decisions.Validate(); err != nil {
		return ui.ExecutionRun{}, fmt.Errorf("validate decisions: %w", err)
	}

	if err := s.store.Save(ctx, runState); err != nil {
		return ui.ExecutionRun{}, err
	}

	logs, err := s.deps.RunLogs.Open(runState.RunID)
	if err != nil {
		return ui.ExecutionRun{}, err
	}

	return ui.ExecutionRun{
		RunState:     runState,
		DryRun:       dryRun,
		RunDir:       logs.RunDir,
		HumanLogPath: logs.HumanLogPath,
		EventsPath:   logs.EventsPath,
		HumanLog:     logs.HumanLog,
		EventsLog:    logs.EventLog,
	}, nil
}

func (s tuiExecutionService) Execute(ctx context.Context, run ui.ExecutionRun, hooks ui.ExecutionHooks) error {
	logger := runner.NewEventLogger(run.HumanLog, run.EventsLog)
	var interactiveRunner runner.InteractiveRunner
	if hooks.OnInteractiveCommand != nil {
		interactiveRunner = runner.InteractiveRunnerFunc(hooks.OnInteractiveCommand)
	} else if s.deps.InteractiveRunner != nil {
		interactiveRunner = s.deps.InteractiveRunner()
	}
	return s.deps.Executor.Execute(ctx, execution.Options{
		Store:             s.store,
		RunState:          run.RunState,
		Catalog:           s.catalog,
		DryRun:            run.DryRun,
		DryRunDelay:       s.deps.DryRunStageDelay,
		RepoRoot:          s.repoRoot,
		HomeDir:           s.homeDir,
		RunDir:            run.RunDir,
		CommandRunner:     s.commandRunner,
		InteractiveRunner: interactiveRunner,
		FileSystem:        s.deps.FileSystem,
		TemplateStore:     s.templateStore,
		PackageManager:    s.deps.PackageManager,
		Logger:            logger,
		Hooks: execution.Hooks{
			OnStageStatus:        hooks.OnStageStatus,
			OnFailure:            hooks.OnFailure,
			OnInteractiveCommand: hooks.OnInteractiveCommand,
		},
	})
}
