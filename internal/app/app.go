package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

type config struct {
	yes       bool
	resume    bool
	dryRun    bool
	statePath string
}

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}

	store := state.NewStore(cfg.statePath)
	current, err := store.Load(ctx)
	if err != nil {
		return err
	}

	var runState *state.RunState
	if cfg.resume {
		if current == nil {
			return errors.New("no previous run state found for --resume")
		}
		runState = current
	} else {
		catalog := stages.DefaultCatalog()
		runState = &state.RunState{
			RunID:        state.NewRunID(time.Now()),
			StartAt:      time.Now().UTC(),
			Mode:         modeName(cfg.dryRun),
			ResolvedPlan: stages.IDs(catalog),
			Stages:       make(map[string]state.StageStatus, len(catalog)),
		}
	}

	if err = store.Save(ctx, runState); err != nil {
		return err
	}

	runDir, err := state.RunDir(runState.RunID)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	humanLogPath := filepath.Join(runDir, "run.log")
	eventsPath := filepath.Join(runDir, "events.jsonl")

	humanLog, err := os.OpenFile(humanLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	defer humanLog.Close()

	eventLog, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer eventLog.Close()

	logger := runner.NewEventLogger(io.MultiWriter(stdout, humanLog), eventLog)

	if err = logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_started",
		Message:   "Starting stage execution",
	}); err != nil {
		return err
	}

	stageIndex := make(map[string]stages.Stage)
	for _, stage := range stages.DefaultCatalog() {
		stageIndex[stage.ID] = stage
	}

	for _, stageID := range runState.ResolvedPlan {
		stage, ok := stageIndex[stageID]
		if !ok {
			return fmt.Errorf("unknown stage in plan: %s", stageID)
		}

		progress := runState.Stages[stageID]
		if progress.Status == string(stages.StatusSuccess) ||
			progress.Status == string(stages.StatusAlreadyDone) ||
			progress.Status == string(stages.StatusSimulatedSuccess) {
			continue
		}

		progress.Status = string(stages.StatusRunning)
		progress.Attempts++
		runState.Stages[stageID] = progress
		if err = store.Save(ctx, runState); err != nil {
			return err
		}

		if err = logger.Log(runner.Event{
			RunID:     runState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      runState.Mode,
			EventType: "stage_started",
			Message:   stage.Title,
		}); err != nil {
			return err
		}

		checkResult, err := stage.Precheck(ctx, stages.ExecutionContext{DryRun: cfg.dryRun})
		if err != nil {
			progress.Status = string(stages.StatusFailed)
			progress.LastError = err.Error()
			runState.Stages[stageID] = progress
			runState.LastFailure = err.Error()
			_ = store.Save(ctx, runState)
			return fmt.Errorf("precheck failed for %s: %w", stageID, err)
		}
		if checkResult.Satisfied {
			progress.Status = string(stages.StatusAlreadyDone)
			runState.Stages[stageID] = progress
			if err = store.Save(ctx, runState); err != nil {
				return err
			}
			if err = logger.Log(runner.Event{
				RunID:     runState.RunID,
				StageID:   stageID,
				Attempt:   progress.Attempts,
				Mode:      runState.Mode,
				EventType: "stage_already_done",
				Message:   checkResult.Message,
			}); err != nil {
				return err
			}
			continue
		}

		execCtx := stages.ExecutionContext{DryRun: cfg.dryRun}
		if cfg.dryRun {
			if err = stage.Simulate(ctx, execCtx); err != nil {
				progress.Status = string(stages.StatusFailed)
				progress.LastError = err.Error()
				runState.Stages[stageID] = progress
				runState.LastFailure = err.Error()
				_ = store.Save(ctx, runState)
				return fmt.Errorf("simulate failed for %s: %w", stageID, err)
			}
			progress.Status = string(stages.StatusSimulatedSuccess)
		} else {
			if err = stage.Run(ctx, execCtx); err != nil {
				progress.Status = string(stages.StatusFailed)
				progress.LastError = err.Error()
				runState.Stages[stageID] = progress
				runState.LastFailure = err.Error()
				_ = store.Save(ctx, runState)
				return fmt.Errorf("stage failed for %s: %w", stageID, err)
			}
			progress.Status = string(stages.StatusSuccess)
		}

		runState.Stages[stageID] = progress
		if err = store.Save(ctx, runState); err != nil {
			return err
		}

		if err = logger.Log(runner.Event{
			RunID:     runState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      runState.Mode,
			EventType: "stage_completed",
			Message:   progress.Status,
		}); err != nil {
			return err
		}
	}

	endAt := time.Now().UTC()
	runState.EndAt = &endAt
	if err = store.Save(ctx, runState); err != nil {
		return err
	}

	if err = logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_completed",
		Message:   "All planned stages processed",
	}); err != nil {
		return err
	}

	return nil
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("laptop-setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.BoolVar(&cfg.yes, "yes", false, "Run non-interactively")
	fs.BoolVar(&cfg.yes, "y", false, "Run non-interactively")
	fs.BoolVar(&cfg.resume, "resume", false, "Resume from existing run state")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate execution without machine changes")
	fs.StringVar(&cfg.statePath, "state-file", "", "Override state file path")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if cfg.statePath == "" {
		defaultPath, err := state.DefaultPath()
		if err != nil {
			return config{}, err
		}
		cfg.statePath = defaultPath
	}

	return cfg, nil
}

func modeName(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "normal"
}
