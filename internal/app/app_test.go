package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func TestParseConfigAllowsInteractiveDefaults(t *testing.T) {
	cfg, err := parseConfig([]string{"--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.statePath != "/tmp/state.json" {
		t.Fatalf("state path mismatch: %q", cfg.statePath)
	}
}

func TestParseConfigResumeFlag(t *testing.T) {
	cfg, err := parseConfig([]string{"--resume", "--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.resume {
		t.Fatal("expected resume=true")
	}
}

func TestParseConfigParsesSelectionFlags(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--from", "brew_bundle",
		"--only", "homebrew_install,brew_bundle",
		"--skip", "brew_bundle",
		"--state-file", "/tmp/state.json",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.from != "brew_bundle" {
		t.Fatalf("from mismatch: %q", cfg.from)
	}
	if len(cfg.only) != 2 || cfg.only[0] != "homebrew_install" || cfg.only[1] != "brew_bundle" {
		t.Fatalf("unexpected only list: %v", cfg.only)
	}
	if len(cfg.skip) != 1 || cfg.skip[0] != "brew_bundle" {
		t.Fatalf("unexpected skip list: %v", cfg.skip)
	}
}

func TestParseConfigRejectsUnexpectedPositionalArgs(t *testing.T) {
	_, err := parseConfig([]string{"--state-file", "/tmp/state.json", "extra"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected positional argument parsing error")
	}
	if !strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseCSVDeduplicatesAndTrims(t *testing.T) {
	got := parseCSV("a, b,a, ,c")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got=%v want=%v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("value mismatch at index %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}

type noOpCommandRunner struct{}

func (r *noOpCommandRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	return runner.Result{}, nil
}

func TestRunNonInteractiveEndToEndPath(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	t.Setenv("HOME", homeDir)

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)

	origCatalogFn := defaultCatalogFn
	origResolveSelected := resolveSelectedBrewIDs
	origNewRunner := newCommandRunner
	origGetwd := getwd
	origHome := userHomeDirectory
	t.Cleanup(func() {
		defaultCatalogFn = origCatalogFn
		resolveSelectedBrewIDs = origResolveSelected
		newCommandRunner = origNewRunner
		getwd = origGetwd
		userHomeDirectory = origHome
	})

	firstRunCalls := 0
	secondRunCalls := 0
	defaultCatalogFn = func() []stages.Stage {
		return []stages.Stage{
			{
				ID:      "first",
				Title:   "First",
				CanSkip: true,
				Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
					return stages.CheckResult{Satisfied: false}, nil
				},
				Run: func(context.Context, stages.ExecutionContext) error {
					firstRunCalls++
					return nil
				},
				Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
			},
			{
				ID:      "second",
				Title:   "Second",
				CanSkip: true,
				Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
					return stages.CheckResult{Satisfied: true, Message: "already configured"}, nil
				},
				Run: func(context.Context, stages.ExecutionContext) error {
					secondRunCalls++
					return nil
				},
				Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
			},
		}
	}
	resolveSelectedBrewIDs = func(string) ([]string, error) {
		return []string{"go", "warp"}, nil
	}
	newCommandRunner = func() runner.CommandRunner { return &noOpCommandRunner{} }
	getwd = func() (string, error) { return repoRoot, nil }
	userHomeDirectory = func() (string, error) { return homeDir, nil }

	var stdout bytes.Buffer
	err := runNonInteractive(context.Background(), config{
		yes:       true,
		statePath: statePath,
	}, store, nil, &stdout)
	if err != nil {
		t.Fatalf("runNonInteractive returned error: %v", err)
	}

	if firstRunCalls != 1 {
		t.Fatalf("expected first stage to execute once, got %d", firstRunCalls)
	}
	if secondRunCalls != 0 {
		t.Fatalf("expected prechecked stage not to run, got %d", secondRunCalls)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected persisted run state")
	}

	if !slices.Equal(loaded.ResolvedPlan, []state.StageID{"first", "second"}) {
		t.Fatalf("resolved plan mismatch: %v", loaded.ResolvedPlan)
	}
	if !slices.Equal(loaded.SelectedIDs, []string{"go", "warp"}) {
		t.Fatalf("selected brew ids mismatch: %v", loaded.SelectedIDs)
	}
	if status := loaded.Stages["first"].Status; status != stages.StatusSuccess {
		t.Fatalf("first stage status mismatch: %q", status)
	}
	if status := loaded.Stages["second"].Status; status != stages.StatusAlreadyDone {
		t.Fatalf("second stage status mismatch: %q", status)
	}
	if got := stages.NodeToolchainFromDecisions(loaded.Decisions); got != stages.NodeToolchainVitePlus {
		t.Fatalf("default node toolchain mismatch: %q", got)
	}

	runDir, err := state.RunDir(loaded.RunID)
	if err != nil {
		t.Fatalf("resolve run dir: %v", err)
	}
	if _, err = os.Stat(filepath.Join(runDir, "run.log")); err != nil {
		t.Fatalf("expected run log to exist: %v", err)
	}
	if _, err = os.Stat(filepath.Join(runDir, "events.jsonl")); err != nil {
		t.Fatalf("expected events log to exist: %v", err)
	}
}

func TestRunNonInteractiveResumeRejectsUnknownStageID(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)

	origCatalogFn := defaultCatalogFn
	origGetwd := getwd
	origHome := userHomeDirectory
	t.Cleanup(func() {
		defaultCatalogFn = origCatalogFn
		getwd = origGetwd
		userHomeDirectory = origHome
	})

	defaultCatalogFn = func() []stages.Stage {
		return []stages.Stage{
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
	}
	getwd = func() (string, error) { return repoRoot, nil }
	userHomeDirectory = func() (string, error) { return homeDir, nil }

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"missing"},
		Stages:       map[state.StageID]state.StageStatus{},
	}

	var stdout bytes.Buffer
	err := runNonInteractive(context.Background(), config{
		yes:       true,
		resume:    true,
		statePath: statePath,
	}, store, current, &stdout)
	if err == nil {
		t.Fatal("expected resume validation error")
	}
	if !strings.Contains(err.Error(), `resolved_plan[0]`) || !strings.Contains(err.Error(), `unknown stage id "missing"`) {
		t.Fatalf("unexpected resume validation error: %v", err)
	}
}

func TestRunNonInteractiveResumeRejectsNormalRunAsDryRun(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)

	origCatalogFn := defaultCatalogFn
	origGetwd := getwd
	origHome := userHomeDirectory
	t.Cleanup(func() {
		defaultCatalogFn = origCatalogFn
		getwd = origGetwd
		userHomeDirectory = origHome
	})

	defaultCatalogFn = func() []stages.Stage {
		return []stages.Stage{
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
	}
	getwd = func() (string, error) { return repoRoot, nil }
	userHomeDirectory = func() (string, error) { return homeDir, nil }

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"known"},
		Stages:       map[state.StageID]state.StageStatus{},
	}

	var stdout bytes.Buffer
	err := runNonInteractive(context.Background(), config{
		yes:       true,
		resume:    true,
		dryRun:    true,
		statePath: statePath,
	}, store, current, &stdout)
	if err == nil {
		t.Fatal("expected dry-run compatibility error")
	}
	if err.Error() != "cannot resume a normal run as dry-run" {
		t.Fatalf("unexpected compatibility error: %v", err)
	}
}
