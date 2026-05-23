package execution

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func TestExecuteRetryThenSuccess(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"flaky"},
		Stages:       map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	attempt := 0
	catalog := []stages.Stage{
		{
			ID:      "flaky",
			Title:   "Flaky",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				attempt++
				if attempt == 1 {
					return errors.New("first attempt failed")
				}
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
	}

	logger := runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err := Execute(context.Background(), Options{
		Store:         store,
		RunState:      runState,
		Catalog:       catalog,
		DryRun:        false,
		Environment:   "home",
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
		Hooks: Hooks{
			OnFailure: func(context.Context, Failure) (FailureAction, error) {
				return ActionRetry, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	status := runState.Stages["flaky"]
	if status.Status != string(stages.StatusSuccess) {
		t.Fatalf("expected success status, got %q", status.Status)
	}
	if status.Attempts != 2 {
		t.Fatalf("expected attempts=2, got %d", status.Attempts)
	}
}

func TestExecuteSkipAfterFailure(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"fails", "next"},
		Stages:       map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	catalog := []stages.Stage{
		{
			ID:      "fails",
			Title:   "Fails",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return errors.New("boom") },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
		{
			ID:      "next",
			Title:   "Next",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return nil },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
	}

	logger := runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err := Execute(context.Background(), Options{
		Store:         store,
		RunState:      runState,
		Catalog:       catalog,
		DryRun:        false,
		Environment:   "home",
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
		Hooks: Hooks{
			OnFailure: func(context.Context, Failure) (FailureAction, error) {
				return ActionSkip, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if status := runState.Stages["fails"]; status.Status != string(stages.StatusSkipped) {
		t.Fatalf("expected skipped status for failed stage, got %q", status.Status)
	}
	if status := runState.Stages["next"]; status.Status != string(stages.StatusSuccess) {
		t.Fatalf("expected next stage success, got %q", status.Status)
	}
}

func TestExecuteAbortOnFailure(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"fails"},
		Stages:       map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	catalog := []stages.Stage{
		{
			ID:      "fails",
			Title:   "Fails",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return errors.New("boom") },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
	}

	logger := runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err := Execute(context.Background(), Options{
		Store:         store,
		RunState:      runState,
		Catalog:       catalog,
		DryRun:        false,
		Environment:   "home",
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
		Hooks: Hooks{
			OnFailure: func(context.Context, Failure) (FailureAction, error) {
				return ActionAbort, nil
			},
		},
	})
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("expected ErrAborted, got %v", err)
	}

	status := runState.Stages["fails"]
	if status.Status != string(stages.StatusFailed) {
		t.Fatalf("expected failed status, got %q", status.Status)
	}
}
