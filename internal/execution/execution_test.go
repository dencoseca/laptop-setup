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
		ResolvedPlan: []state.StageID{"flaky"},
		Stages:       map[state.StageID]state.StageStatus{},
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
	if status.Status != stages.StatusSuccess {
		t.Fatalf("expected success status, got %q", status.Status)
	}
	if status.Attempts != 2 {
		t.Fatalf("expected attempts=2, got %d", status.Attempts)
	}
}

func decisionSetWithNodeToolchain(toolchain stages.NodeToolchain) stages.DecisionSet {
	decisions := stages.DefaultDecisions()
	decisions.NodeToolchain = toolchain
	return decisions
}

func TestExecuteSkipAfterFailure(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"fails", "next"},
		Stages:       map[state.StageID]state.StageStatus{},
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

	if status := runState.Stages["fails"]; status.Status != stages.StatusSkipped {
		t.Fatalf("expected skipped status for failed stage, got %q", status.Status)
	}
	if status := runState.Stages["next"]; status.Status != stages.StatusSuccess {
		t.Fatalf("expected next stage success, got %q", status.Status)
	}
}

func TestExecuteSkipClearsRunFailureButKeepsStageError(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"fails"},
		Stages:       map[state.StageID]state.StageStatus{},
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
				return ActionSkip, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	persistedState, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	if persistedState == nil {
		t.Fatal("expected persisted state")
	}

	status := persistedState.Stages["fails"]
	if status.Status != stages.StatusSkipped {
		t.Fatalf("expected skipped status for failed stage, got %q", status.Status)
	}
	if status.LastError != "boom" {
		t.Fatalf("expected skipped stage to keep failure reason, got %q", status.LastError)
	}
	if persistedState.LastFailure != "" {
		t.Fatalf("expected handled skipped failure to clear run LastFailure, got %q", persistedState.LastFailure)
	}
	if persistedState.EndAt == nil {
		t.Fatal("expected run to complete after skipping final failed stage")
	}
}

func TestExecuteAbortOnFailure(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"fails"},
		Stages:       map[state.StageID]state.StageStatus{},
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
	if status.Status != stages.StatusFailed {
		t.Fatalf("expected failed status, got %q", status.Status)
	}
}

func TestExecuteDryRunUsesSimulate(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "dry-run",
		ResolvedPlan: []state.StageID{"simulate_only"},
		Stages:       map[state.StageID]state.StageStatus{},
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
	if status := runState.Stages["simulate_only"]; status.Status != stages.StatusSimulatedSuccess {
		t.Fatalf("expected simulated_success status, got %q", status.Status)
	}
}

func TestExecuteRejectsUnknownStageBeforeStartingRun(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"missing"},
		Stages:       map[state.StageID]state.StageStatus{},
	}

	catalog := []stages.Stage{
		{
			ID:      "known",
			Title:   "Known",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return nil },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
	}

	var humanLog bytes.Buffer
	logger := runner.NewEventLogger(&humanLog, &bytes.Buffer{})
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
	if err == nil {
		t.Fatal("expected unknown stage validation error")
	}
	if !strings.Contains(err.Error(), `resolved_plan[0]`) || !strings.Contains(err.Error(), `unknown stage id "missing"`) {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if strings.Contains(humanLog.String(), "run_started") {
		t.Fatalf("expected validation to fail before run_started event, got log %q", humanLog.String())
	}
}

func TestExecuteDryRunAppliesStageDelayBeforeSimulate(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "dry-run",
		ResolvedPlan: []state.StageID{"simulate_only"},
		Stages:       map[state.StageID]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	callOrder := make([]string, 0, 2)
	catalog := []stages.Stage{
		{
			ID:      "simulate_only",
			Title:   "Simulate Only",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				callOrder = append(callOrder, "run")
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error {
				callOrder = append(callOrder, "simulate")
				return nil
			},
		},
	}

	logger := runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err := Execute(context.Background(), Options{
		Store:    store,
		RunState: runState,
		Catalog:  catalog,
		DryRun:   true,
		DryRunDelay: func(context.Context, stages.ExecutionContext) error {
			callOrder = append(callOrder, "delay")
			return nil
		},
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if len(callOrder) != 2 {
		t.Fatalf("unexpected call order length: %v", callOrder)
	}
	if callOrder[0] != "delay" || callOrder[1] != "simulate" {
		t.Fatalf("unexpected call order: %v", callOrder)
	}
}

func TestRandomDryRunStageDelayHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delay := RandomDryRunStageDelay(2*time.Second, 5*time.Second)
	err := delay(ctx, stages.ExecutionContext{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestExecuteResumeSkipsTerminalStages(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"done", "pending"},
		Stages: map[state.StageID]state.StageStatus{
			"done": {Status: stages.StatusSuccess, Attempts: 1},
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
	if status := runState.Stages["pending"]; status.Status != stages.StatusSuccess {
		t.Fatalf("expected pending stage success, got %q", status.Status)
	}
}

func TestExecuteSkipDeniedForNonSkippableStage(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"cannot_skip"},
		Stages:       map[state.StageID]state.StageStatus{},
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
	if status := runState.Stages["cannot_skip"]; status.Status != stages.StatusFailed {
		t.Fatalf("expected failed status to persist, got %q", status.Status)
	}
}

func TestExecutePassesPersistedDecisionsToStageContext(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"decision_stage"},
		Decisions:    decisionSetWithNodeToolchain(stages.NodeToolchainNvmPnpm),
		Stages:       map[state.StageID]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	var seenValue stages.NodeToolchain
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

func TestExecuteResumeAfterFailureUsesPersistedPlanAndDecisions(t *testing.T) {
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	runState := &state.RunState{
		RunID:        state.NewRunID(time.Now()),
		StartAt:      time.Now().UTC(),
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"first", "second", "third"},
		Decisions:    decisionSetWithNodeToolchain(stages.NodeToolchainNvmPnpm),
		Stages:       map[state.StageID]state.StageStatus{},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	failSecond := true
	firstCalls := 0
	secondCalls := 0
	thirdCalls := 0
	var seenDecision stages.NodeToolchain
	catalog := []stages.Stage{
		{
			ID:      "first",
			Title:   "First",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				firstCalls++
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
		{
			ID:      "second",
			Title:   "Second",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(_ context.Context, execCtx stages.ExecutionContext) error {
				secondCalls++
				if failSecond {
					return errors.New("intentional interruption")
				}
				seenDecision = stages.NodeToolchainFromDecisions(execCtx.Decisions)
				return nil
			},
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
		{
			ID:      "third",
			Title:   "Third",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run: func(context.Context, stages.ExecutionContext) error {
				thirdCalls++
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
	if err == nil {
		t.Fatal("expected first execution to fail")
	}
	if !strings.Contains(err.Error(), "stage failed for second") {
		t.Fatalf("unexpected first execution error: %v", err)
	}

	persisted, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load persisted failed state: %v", err)
	}
	if persisted == nil {
		t.Fatal("expected persisted run state after failure")
	}
	if status := persisted.Stages["first"].Status; status != stages.StatusSuccess {
		t.Fatalf("expected first stage success before resume, got %q", status)
	}
	if status := persisted.Stages["second"].Status; status != stages.StatusFailed {
		t.Fatalf("expected second stage failed before resume, got %q", status)
	}

	failSecond = false
	logger = runner.NewEventLogger(&bytes.Buffer{}, &bytes.Buffer{})
	err = Execute(context.Background(), Options{
		Store:         store,
		RunState:      persisted,
		Catalog:       catalog,
		DryRun:        false,
		RepoRoot:      t.TempDir(),
		HomeDir:       t.TempDir(),
		RunDir:        t.TempDir(),
		CommandRunner: runner.NewOSCommandRunner(),
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("resume execution returned error: %v", err)
	}

	if firstCalls != 1 {
		t.Fatalf("expected first stage to run once total, got %d", firstCalls)
	}
	if secondCalls != 2 {
		t.Fatalf("expected second stage to run twice total, got %d", secondCalls)
	}
	if thirdCalls != 1 {
		t.Fatalf("expected third stage to run once after resume, got %d", thirdCalls)
	}
	if seenDecision != stages.NodeToolchainNvmPnpm {
		t.Fatalf("expected resumed stage to read persisted decision, got %q", seenDecision)
	}
	if status := persisted.Stages["second"].Status; status != stages.StatusSuccess {
		t.Fatalf("expected second stage success after resume, got %q", status)
	}
	if status := persisted.Stages["third"].Status; status != stages.StatusSuccess {
		t.Fatalf("expected third stage success after resume, got %q", status)
	}
}
