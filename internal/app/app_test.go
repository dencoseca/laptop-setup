package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
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

func TestParseConfigRejectsRemovedYesFlag(t *testing.T) {
	_, err := parseConfig([]string{"--yes", "--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected --yes to be rejected")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTreatsHelpAsSuccess(t *testing.T) {
	for _, helpFlag := range []string{"-h", "--help"} {
		t.Run(helpFlag, func(t *testing.T) {
			var stderr bytes.Buffer

			err := Run(context.Background(), []string{helpFlag}, &bytes.Buffer{}, &stderr)
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}

			output := stderr.String()
			if got := strings.Count(output, "Usage of laptop-setup:"); got != 1 {
				t.Fatalf("expected usage once, got %d occurrences in %q", got, output)
			}
			if strings.Contains(output, "flag: help requested") {
				t.Fatalf("help output contains an error: %q", output)
			}
		})
	}
}

func TestRunRejectsInvalidFlag(t *testing.T) {
	var stderr bytes.Buffer

	err := Run(context.Background(), []string{"--not-a-flag"}, &bytes.Buffer{}, &stderr)
	if err == nil {
		t.Fatal("expected invalid flag error")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage of laptop-setup:") {
		t.Fatalf("expected useful flag output, got %q", stderr.String())
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

type capturingUIRunner struct {
	calls   int
	options ui.Options
}

func (r *capturingUIRunner) Run(_ context.Context, options ui.Options) error {
	r.calls++
	r.options = options
	return nil
}

func TestRunRequiresInteractiveTTY(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	app := New(Dependencies{
		Paths: fakePathResolver{
			workingDir:       t.TempDir(),
			homeDir:          t.TempDir(),
			defaultStatePath: statePath,
			runsDir:          filepath.Join(t.TempDir(), "runs"),
		},
		TTY: staticTTYDetector(false),
	})

	err := app.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected TTY error")
	}
	if err.Error() != "laptop-setup requires an interactive TTY" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStartsInteractiveUIWithConfig(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	uiRunner := &capturingUIRunner{}
	paths := fakePathResolver{
		workingDir:       t.TempDir(),
		homeDir:          t.TempDir(),
		defaultStatePath: statePath,
		runsDir:          filepath.Join(t.TempDir(), "runs"),
	}
	app := New(Dependencies{
		Catalog: func() []stages.Stage {
			return []stages.Stage{
				{ID: "first", Title: "First"},
				{ID: "second", Title: "Second"},
			}
		},
		CommandRunner: func() runner.CommandRunner { return &noOpCommandRunner{} },
		Paths:         paths,
		UI:            uiRunner,
		TTY:           staticTTYDetector(true),
	})

	err := app.Run(context.Background(), []string{
		"--dry-run",
		"--from", "second",
		"--only", "first,second",
		"--skip", "first",
		"--state-file", statePath,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("App.Run returned error: %v", err)
	}

	if uiRunner.calls != 1 {
		t.Fatalf("expected UI to run once, got %d", uiRunner.calls)
	}
	if !uiRunner.options.Config.DryRun || uiRunner.options.Config.From != "second" {
		t.Fatalf("unexpected UI config: %+v", uiRunner.options.Config)
	}
	if len(uiRunner.options.Config.Only) != 2 || uiRunner.options.Config.Only[0] != "first" || uiRunner.options.Config.Only[1] != "second" {
		t.Fatalf("unexpected only config: %v", uiRunner.options.Config.Only)
	}
	if len(uiRunner.options.Config.Skip) != 1 || uiRunner.options.Config.Skip[0] != "first" {
		t.Fatalf("unexpected skip config: %v", uiRunner.options.Config.Skip)
	}
	if uiRunner.options.ExecutionService == nil {
		t.Fatal("expected execution service to be wired")
	}
}

func TestFilesystemRunLogFactoryCreatesPrivateArtifacts(t *testing.T) {
	runID := state.RunID("run-1")
	paths := fakePathResolver{runsDir: filepath.Join(t.TempDir(), "runs")}
	logs, err := (filesystemRunLogFactory{Paths: paths}).Open(runID)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = logs.HumanLog.Close()
		_ = logs.EventLog.Close()
	})

	for path, want := range map[string]os.FileMode{
		logs.RunDir:       0o700,
		logs.HumanLogPath: 0o600,
		logs.EventsPath:   0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("permissions for %s: got=%#o want=%#o", path, got, want)
		}
	}
}

func TestRunResumeRejectsUnknownStageID(t *testing.T) {
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
		TTY:   staticTTYDetector(true),
	})

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"missing"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"missing"}),
		Stages:       map[state.StageID]state.StageStatus{},
	}

	if err := store.Save(context.Background(), current); err != nil {
		t.Fatalf("save invalid resume state: %v", err)
	}

	err := app.Run(context.Background(), []string{"--resume", "--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected resume validation error")
	}
	if !strings.Contains(err.Error(), `resolved_plan[0]`) || !strings.Contains(err.Error(), `unknown stage id "missing"`) {
		t.Fatalf("unexpected resume validation error: %v", err)
	}
}

func TestRunResumeRejectsNormalRunAsDryRun(t *testing.T) {
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
		TTY:   staticTTYDetector(true),
	})

	current := &state.RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []state.StageID{"known"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"known"}),
		Stages:       map[state.StageID]state.StageStatus{},
	}

	if err := store.Save(context.Background(), current); err != nil {
		t.Fatalf("save resume state: %v", err)
	}

	err := app.Run(context.Background(), []string{"--resume", "--dry-run", "--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected dry-run compatibility error")
	}
	if err.Error() != "cannot resume a normal run as dry-run" {
		t.Fatalf("unexpected compatibility error: %v", err)
	}
}
