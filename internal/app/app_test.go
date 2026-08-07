package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestParseConfigDiscardStateFlag(t *testing.T) {
	cfg, err := parseConfig([]string{"--discard-state", "--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.discardState {
		t.Fatal("expected discardState=true")
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

type blockingUIRunner struct {
	started chan<- struct{}
}

func (r blockingUIRunner) Run(ctx context.Context, _ ui.Options) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
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

	lock, lockErr := state.AcquirePathLock(statePath)
	if lockErr != nil {
		t.Fatalf("startup failure retained the state lock: %v", lockErr)
	}
	if lockErr = lock.Close(); lockErr != nil {
		t.Fatalf("release verification lock: %v", lockErr)
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
	if uiRunner.options.FileSystem == nil {
		t.Fatal("expected filesystem to be wired")
	}
	if uiRunner.options.ExecutionService == nil {
		t.Fatal("expected execution service to be wired")
	}
}

func TestRunOpensRepositoryAtCanonicalLockedPath(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatalf("create real state directory: %v", err)
	}
	linkedDir := filepath.Join(dir, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("create state directory symlink: %v", err)
	}
	statePath := filepath.Join(linkedDir, "state.json")
	uiRunner := &capturingUIRunner{}
	app := newStatePolicyTestApp(statePath, uiRunner)

	if err := app.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("App.Run returned error: %v", err)
	}
	store, ok := uiRunner.options.Store.(interface{ Path() string })
	if !ok {
		t.Fatalf("UI store does not expose its path: %T", uiRunner.options.Store)
	}
	canonicalDir, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("resolve real state directory: %v", err)
	}
	if got, want := store.Path(), filepath.Join(canonicalDir, "state.json"); got != want {
		t.Fatalf("repository path: got=%q want=%q", got, want)
	}
}

func TestRunLocksStatePathForEntireUILifetime(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	paths := fakePathResolver{
		workingDir:       t.TempDir(),
		homeDir:          t.TempDir(),
		defaultStatePath: statePath,
		runsDir:          filepath.Join(t.TempDir(), "runs"),
	}
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	firstApp := New(Dependencies{
		Paths: paths,
		TTY:   staticTTYDetector(true),
		UI:    blockingUIRunner{started: started},
	})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- firstApp.Run(ctx, []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("first app did not reach the UI while holding the state lock")
	}

	secondUI := &capturingUIRunner{}
	secondApp := New(Dependencies{
		Paths: paths,
		TTY:   staticTTYDetector(true),
		UI:    secondUI,
	})
	err := secondApp.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "another laptop-setup process") {
		t.Fatalf("expected actionable contention error, got %v", err)
	}
	if secondUI.calls != 0 {
		t.Fatalf("contending process reached UI: calls=%d", secondUI.calls)
	}

	otherPath := filepath.Join(dir, "other-state.json")
	if err = secondApp.Run(context.Background(), []string{"--state-file", otherPath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("different state path was blocked: %v", err)
	}
	if secondUI.calls != 1 {
		t.Fatalf("different state path did not reach UI: calls=%d", secondUI.calls)
	}

	cancel()
	select {
	case err = <-firstResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled first run, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled app did not release the state lock")
	}
	if err = secondApp.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("state lock was not released after cancellation: %v", err)
	}
}

func TestRunRejectsResumeWithDiscardState(t *testing.T) {
	app := New(Dependencies{})
	err := app.Run(
		context.Background(),
		[]string{"--resume", "--discard-state", "--state-file", filepath.Join(t.TempDir(), "state.json")},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || err.Error() != "--resume cannot be combined with --discard-state" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRequiresExplicitChoiceForUnfinishedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)
	unfinished := testPersistedAppRun("unfinished-run", false)
	if err := store.Save(context.Background(), unfinished); err != nil {
		t.Fatalf("save unfinished state: %v", err)
	}

	uiRunner := &capturingUIRunner{}
	app := newStatePolicyTestApp(statePath, uiRunner)
	err := app.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected fresh run to reject unfinished state")
	}
	if !strings.Contains(err.Error(), `unfinished run "unfinished-run"`) ||
		!strings.Contains(err.Error(), "--resume") ||
		!strings.Contains(err.Error(), "--discard-state") {
		t.Fatalf("unfinished-state error is not actionable: %v", err)
	}
	if uiRunner.calls != 0 {
		t.Fatalf("fresh run reached UI without an explicit choice: calls=%d", uiRunner.calls)
	}

	if err = app.Run(
		context.Background(),
		[]string{"--discard-state", "--state-file", statePath},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("explicit discard did not allow fresh run: %v", err)
	}
	if uiRunner.calls != 1 {
		t.Fatalf("explicit discard did not reach UI: calls=%d", uiRunner.calls)
	}
}

func TestRunAllowsCompletedStateReplacement(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)
	if err := store.Save(context.Background(), testPersistedAppRun("completed-run", true)); err != nil {
		t.Fatalf("save completed state: %v", err)
	}

	uiRunner := &capturingUIRunner{}
	app := newStatePolicyTestApp(statePath, uiRunner)
	if err := app.Run(context.Background(), []string{"--state-file", statePath}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("completed state blocked fresh run: %v", err)
	}
	if uiRunner.calls != 1 {
		t.Fatalf("fresh run did not reach UI: calls=%d", uiRunner.calls)
	}
}

func newStatePolicyTestApp(statePath string, uiRunner UIRunner) *App {
	return New(Dependencies{
		Paths: fakePathResolver{
			workingDir:       filepath.Dir(statePath),
			homeDir:          filepath.Dir(statePath),
			defaultStatePath: statePath,
			runsDir:          filepath.Join(filepath.Dir(statePath), "runs"),
		},
		TTY: staticTTYDetector(true),
		UI:  uiRunner,
	})
}

func testPersistedAppRun(runID state.RunID, completed bool) *state.RunState {
	run := &state.RunState{
		RunID:        runID,
		StartAt:      time.Now().UTC(),
		Mode:         state.ModeNormal,
		ResolvedPlan: []state.StageID{"stage"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"stage"}),
		Stages: map[state.StageID]state.StageStatus{
			"stage": {Status: state.StageStatusPending},
		},
	}
	if completed {
		endAt := time.Now().UTC()
		run.EndAt = &endAt
		run.Stages["stage"] = state.StageStatus{Status: state.StageStatusSuccess, Attempts: 1}
	}
	return run
}

func TestRunHandlesUnreadablePreviousStateByMode(t *testing.T) {
	testCases := []struct {
		name        string
		payload     string
		resumeError string
	}{
		{
			name:        "malformed JSON",
			payload:     `{"run_id":`,
			resumeError: "decode state file: invalid JSON",
		},
		{
			name:        "schema-invalid state",
			payload:     `{}`,
			resumeError: "validate state file: field run_id",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, resume := range []bool{false, true} {
				mode := "fresh"
				if resume {
					mode = "resume"
				}

				t.Run(mode, func(t *testing.T) {
					statePath := filepath.Join(t.TempDir(), "state.json")
					if err := os.WriteFile(statePath, []byte(testCase.payload), 0o600); err != nil {
						t.Fatalf("write previous state: %v", err)
					}

					uiRunner := &capturingUIRunner{}
					app := New(Dependencies{
						Paths: fakePathResolver{
							workingDir:       t.TempDir(),
							homeDir:          t.TempDir(),
							defaultStatePath: statePath,
							runsDir:          filepath.Join(t.TempDir(), "runs"),
						},
						TTY: staticTTYDetector(true),
						UI:  uiRunner,
					})

					args := []string{"--state-file", statePath}
					if resume {
						args = append([]string{"--resume"}, args...)
					}
					err := app.Run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{})

					if resume {
						if err == nil {
							t.Fatal("expected unreadable state to reject resume")
						}
						if !strings.Contains(err.Error(), testCase.resumeError) {
							t.Fatalf("unexpected resume error: %v", err)
						}
						if uiRunner.calls != 0 {
							t.Fatalf("expected UI not to run, got %d calls", uiRunner.calls)
						}
						return
					}

					if err != nil {
						t.Fatalf("fresh run returned error: %v", err)
					}
					if uiRunner.calls != 1 {
						t.Fatalf("expected UI to run once, got %d calls", uiRunner.calls)
					}
					if uiRunner.options.Current != nil {
						t.Fatalf("fresh run loaded previous state: %+v", uiRunner.options.Current)
					}
					payload, readErr := os.ReadFile(statePath)
					if readErr != nil {
						t.Fatalf("read previous state: %v", readErr)
					}
					if string(payload) != testCase.payload {
						t.Fatalf("fresh startup changed previous state: got %q want %q", payload, testCase.payload)
					}
				})
			}
		})
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
