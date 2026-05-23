package execution

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
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

func TestExecuteDryRunUsesSimulate(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "dry-run",
		ResolvedPlan: []string{"simulate_only"},
		Stages:       map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	runCalls := 0
	simulateCalls := 0
	catalog := []stages.Stage{
		{
			ID:      "simulate_only",
			Title:   "Simulate Only",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				runCalls++
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error {
				simulateCalls++
				return nil
			},
		},
	}

	logger := runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err := Execute(context.Background(), Options{
		Store:         store,
		RunState:      runState,
		Catalog:       catalog,
		DryRun:        true,
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if runCalls != 0 {
		t.Fatalf("expected run path not to execute in dry-run, got runCalls=%d", runCalls)
	}
	if simulateCalls != 1 {
		t.Fatalf("expected simulate path once, got simulateCalls=%d", simulateCalls)
	}
	if status := runState.Stages["simulate_only"]; status.Status != string(stages.StatusSimulatedSuccess) {
		t.Fatalf("expected simulated_success status, got %q", status.Status)
	}
}

func TestExecuteResumeSkipsTerminalStages(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"done", "pending"},
		Stages: map[string]state.StageStatus{
			"done": {Status: string(stages.StatusSuccess), Attempts: 1},
		},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	doneCalls := 0
	pendingCalls := 0
	catalog := []stages.Stage{
		{
			ID:      "done",
			Title:   "Done",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				doneCalls++
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
		{
			ID:      "pending",
			Title:   "Pending",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				pendingCalls++
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
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if doneCalls != 0 {
		t.Fatalf("expected terminal stage not to re-run, got doneCalls=%d", doneCalls)
	}
	if pendingCalls != 1 {
		t.Fatalf("expected pending stage to run once, got pendingCalls=%d", pendingCalls)
	}
	if status := runState.Stages["pending"]; status.Status != string(stages.StatusSuccess) {
		t.Fatalf("expected pending stage success, got %q", status.Status)
	}
}

func TestExecuteSkipDeniedForNonSkippableStage(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"cannot_skip"},
		Stages:       map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	catalog := []stages.Stage{
		{
			ID:      "cannot_skip",
			Title:   "Cannot Skip",
			CanSkip: false,
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
	if err == nil {
		t.Fatal("expected error when skipping non-skippable stage")
	}
	if !strings.Contains(err.Error(), "cannot be skipped") {
		t.Fatalf("expected non-skippable error, got %v", err)
	}
	if status := runState.Stages["cannot_skip"]; status.Status != string(stages.StatusFailed) {
		t.Fatalf("expected failed status to persist, got %q", status.Status)
	}
}

func TestExecutePassesPersistedDecisionsToStageContext(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []string{"decision_stage"},
		Decisions: map[string]any{
			stages.DecisionNodeToolchain: stages.NodeToolchainNvmPnpm,
		},
		Stages: map[string]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	seenValue := ""
	catalog := []stages.Stage{
		{
			ID:      "decision_stage",
			Title:   "Decision Stage",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(_ context.Context, execCtx stages.ExecutionContext) error {
				seenValue = stages.NodeToolchainFromDecisions(execCtx.Decisions)
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
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if seenValue != stages.NodeToolchainNvmPnpm {
		t.Fatalf("expected decision in stage context, got %q", seenValue)
	}
}
