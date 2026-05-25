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

func (r *noOpCommandRunner) LookPath(context.Context, string) (string, error) {
	return "/usr/local/bin/test-command", nil
}

type missingCommandRunner struct {
	lookPathCalls []string
	runCalls      int
}

func (r *missingCommandRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	r.runCalls++
	return runner.Result{}, nil
}

func (r *missingCommandRunner) LookPath(_ context.Context, name string) (string, error) {
	r.lookPathCalls = append(r.lookPathCalls, name)
	return "", os.ErrNotExist
}

type fakePathResolver struct {
	workingDir       string
	homeDir          string
	defaultStatePath string
	runsDir          string
}

func (r fakePathResolver) WorkingDir() (string, error) {
	return r.workingDir, nil
}

func (r fakePathResolver) HomeDir() (string, error) {
	return r.homeDir, nil
}

func (r fakePathResolver) DefaultStatePath() (string, error) {
	return r.defaultStatePath, nil
}

func (r fakePathResolver) RunDir(runID state.RunID) (string, error) {
	return filepath.Join(r.runsDir, runID.String()), nil
}

type staticTTYDetector bool

func (d staticTTYDetector) CanPrompt() (bool, error) {
	return bool(d), nil
}

func writeTestBrewfile(t *testing.T, repoRoot string) {
	t.Helper()
	templatesDir := filepath.Join(repoRoot, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("create templates directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "Brewfile"), []byte("brew \"go\"\ncask \"warp\"\n"), 0o644); err != nil {
		t.Fatalf("write Brewfile template: %v", err)
	}
}

func TestRunNonInteractiveEndToEndPath(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	writeTestBrewfile(t, repoRoot)

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)

	firstRunCalls := 0
	secondRunCalls := 0
	paths := fakePathResolver{
		workingDir:       repoRoot,
		homeDir:          homeDir,
		defaultStatePath: statePath,
		runsDir:          filepath.Join(t.TempDir(), "runs"),
	}
	app := New(Dependencies{
		Catalog: func() []stages.Stage {
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
		},
		CommandRunner: func() runner.CommandRunner { return &noOpCommandRunner{} },
		Paths:         paths,
		RunLogs:       filesystemRunLogFactory{Paths: paths},
		TTY:           staticTTYDetector(false),
	})

	var stdout bytes.Buffer
	err := app.Run(context.Background(), []string{"--yes", "--state-file", statePath}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("App.Run returned error: %v", err)
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

	runDir, err := paths.RunDir(loaded.RunID)
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

func TestRunNonInteractiveResumeContinuesFailedRun(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	writeTestBrewfile(t, repoRoot)

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)
	runState := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"first", "second"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"first", "second"}),
		Stages: map[state.StageID]state.StageStatus{
			"first":  {Status: stages.StatusSuccess, Attempts: 1},
			"second": {Status: stages.StatusFailed, Attempts: 1, LastError: "previous failure"},
		},
	}
	if err := store.Save(context.Background(), runState); err != nil {
		t.Fatalf("save resumable state: %v", err)
	}

	firstCalls := 0
	secondCalls := 0
	paths := fakePathResolver{
		workingDir:       repoRoot,
		homeDir:          homeDir,
		defaultStatePath: statePath,
		runsDir:          filepath.Join(t.TempDir(), "runs"),
	}
	app := New(Dependencies{
		Catalog: func() []stages.Stage {
			return []stages.Stage{
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
					Run: func(context.Context, stages.ExecutionContext) error {
						secondCalls++
						return nil
					},
					Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
				},
			}
		},
		CommandRunner: func() runner.CommandRunner { return &noOpCommandRunner{} },
		Paths:         paths,
		RunLogs:       filesystemRunLogFactory{Paths: paths},
		TTY:           staticTTYDetector(false),
	})

	var stdout bytes.Buffer
	err := app.Run(context.Background(), []string{"--yes", "--resume", "--state-file", statePath}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("App.Run resume returned error: %v", err)
	}

	if firstCalls != 0 {
		t.Fatalf("expected completed stage not to run during resume, got %d", firstCalls)
	}
	if secondCalls != 1 {
		t.Fatalf("expected failed stage to retry once during resume, got %d", secondCalls)
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load resumed state: %v", err)
	}
	if status := loaded.Stages["second"]; status.Status != stages.StatusSuccess || status.Attempts != 2 {
		t.Fatalf("expected second stage success after resume with attempts=2, got %+v", status)
	}
}

func TestRunNonInteractiveMissingPrerequisiteFailsStage(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	writeTestBrewfile(t, repoRoot)

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)
	commandRunner := &missingCommandRunner{}
	paths := fakePathResolver{
		workingDir:       repoRoot,
		homeDir:          homeDir,
		defaultStatePath: statePath,
		runsDir:          filepath.Join(t.TempDir(), "runs"),
	}
	app := New(Dependencies{
		CommandRunner: func() runner.CommandRunner { return commandRunner },
		Paths:         paths,
		RunLogs:       filesystemRunLogFactory{Paths: paths},
		TTY:           staticTTYDetector(false),
	})

	var stdout bytes.Buffer
	err := app.Run(context.Background(), []string{"--yes", "--only", "brew_bundle", "--state-file", statePath}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected missing Homebrew prerequisite to fail")
	}
	if !strings.Contains(err.Error(), "brew executable not found") {
		t.Fatalf("expected Homebrew availability error, got %v", err)
	}
	if !slices.Equal(commandRunner.lookPathCalls, []string{"brew", "brew"}) {
		t.Fatalf("expected precheck and run availability checks through command runner, got %v", commandRunner.lookPathCalls)
	}
	if commandRunner.runCalls != 0 {
		t.Fatalf("expected brew bundle command not to execute when brew is missing, got %d calls", commandRunner.runCalls)
	}

	loaded, loadErr := store.Load(context.Background())
	if loadErr != nil {
		t.Fatalf("load failed state: %v", loadErr)
	}
	status := loaded.Stages["brew_bundle"]
	if status.Status != stages.StatusFailed {
		t.Fatalf("expected brew_bundle failed status, got %q", status.Status)
	}
	if loaded.LastFailure == "" || !strings.Contains(loaded.LastFailure, "brew executable not found") {
		t.Fatalf("expected last failure to record missing brew, got %q", loaded.LastFailure)
	}
}

func TestRunNonInteractiveResumeRejectsUnknownStageID(t *testing.T) {
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)

	paths := fakePathResolver{workingDir: repoRoot, homeDir: homeDir, defaultStatePath: statePath, runsDir: filepath.Join(t.TempDir(), "runs")}
	app := New(Dependencies{
		Catalog: func() []stages.Stage {
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
		},
		Paths: paths,
	})

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"missing"},
		Stages:       map[state.StageID]state.StageStatus{},
	}

	var stdout bytes.Buffer
	err := app.runNonInteractive(context.Background(), config{
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

	paths := fakePathResolver{workingDir: repoRoot, homeDir: homeDir, defaultStatePath: statePath, runsDir: filepath.Join(t.TempDir(), "runs")}
	app := New(Dependencies{
		Catalog: func() []stages.Stage {
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
		},
		Paths: paths,
	})

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"known"},
		Stages:       map[state.StageID]state.StageStatus{},
	}

	var stdout bytes.Buffer
	err := app.runNonInteractive(context.Background(), config{
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
