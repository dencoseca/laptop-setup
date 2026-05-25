package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	laptopsetup "github.com/dencoseca/laptop-setup"
	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
)

type StateRepository interface {
	Load(context.Context) (*state.RunState, error)
	Save(context.Context, *state.RunState) error
	Path() string
}

type StateRepositoryFactory interface {
	Open(path string) StateRepository
}

type PathResolver interface {
	WorkingDir() (string, error)
	HomeDir() (string, error)
	DefaultStatePath() (string, error)
	RunDir(state.RunID) (string, error)
}

type RunLogs struct {
	RunDir       string
	HumanLogPath string
	EventsPath   string
	HumanLog     io.WriteCloser
	EventLog     io.WriteCloser
}

type RunLogFactory interface {
	Open(runID state.RunID) (RunLogs, error)
}

type TemplateStoreFactory interface {
	New(repoRoot string, fs stages.FileSystem) stages.TemplateStore
}

type UIRunner interface {
	Run(context.Context, ui.Options) error
}

type Executor interface {
	Execute(context.Context, execution.Options) error
}

type TTYDetector interface {
	CanPrompt() (bool, error)
}

type Dependencies struct {
	Catalog           func() []stages.Stage
	CommandRunner     func() runner.CommandRunner
	StateRepositories StateRepositoryFactory
	Paths             PathResolver
	RunLogs           RunLogFactory
	TemplateStores    TemplateStoreFactory
	FileSystem        stages.FileSystem
	PackageManager    stages.PackageManager
	UI                UIRunner
	Executor          Executor
	TTY               TTYDetector
	DryRunStageDelay  execution.DryRunStageDelayFunc
}

type stateStoreFactory struct{}

func (stateStoreFactory) Open(path string) StateRepository {
	return state.NewStore(path)
}

type osPathResolver struct{}

func (osPathResolver) WorkingDir() (string, error) {
	return os.Getwd()
}

func (osPathResolver) HomeDir() (string, error) {
	return os.UserHomeDir()
}

func (osPathResolver) DefaultStatePath() (string, error) {
	return state.DefaultPath()
}

func (osPathResolver) RunDir(runID state.RunID) (string, error) {
	return state.RunDir(runID)
}

type filesystemRunLogFactory struct {
	Paths PathResolver
}

func (f filesystemRunLogFactory) Open(runID state.RunID) (RunLogs, error) {
	paths := f.Paths
	if paths == nil {
		paths = osPathResolver{}
	}
	runDir, err := paths.RunDir(runID)
	if err != nil {
		return RunLogs{}, err
	}
	if err = os.MkdirAll(runDir, 0o755); err != nil {
		return RunLogs{}, fmt.Errorf("create run directory: %w", err)
	}

	humanLogPath := filepath.Join(runDir, "run.log")
	eventsPath := filepath.Join(runDir, "events.jsonl")

	humanLog, err := os.OpenFile(humanLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return RunLogs{}, fmt.Errorf("open run log: %w", err)
	}
	eventLog, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = humanLog.Close()
		return RunLogs{}, fmt.Errorf("open event log: %w", err)
	}

	return RunLogs{
		RunDir:       runDir,
		HumanLogPath: humanLogPath,
		EventsPath:   eventsPath,
		HumanLog:     humanLog,
		EventLog:     eventLog,
	}, nil
}

type filesystemTemplateStoreFactory struct{}

func (filesystemTemplateStoreFactory) New(repoRoot string, fs stages.FileSystem) stages.TemplateStore {
	return stages.FilesystemTemplateStore{RepoRoot: repoRoot, FS: fs}
}

type embeddedTemplateStoreFactory struct{}

func (embeddedTemplateStoreFactory) New(_ string, fs stages.FileSystem) stages.TemplateStore {
	return stages.FSTemplateStore{
		FS:         laptopsetup.TemplateFS(),
		SourceName: "embedded templates",
		FileSystem: fs,
	}
}

type uiRunnerFunc func(context.Context, ui.Options) error

func (f uiRunnerFunc) Run(ctx context.Context, options ui.Options) error {
	return f(ctx, options)
}

type executorFunc func(context.Context, execution.Options) error

func (f executorFunc) Execute(ctx context.Context, options execution.Options) error {
	return f(ctx, options)
}

type stdinTTYDetector struct{}

func (stdinTTYDetector) CanPrompt() (bool, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect stdin: %w", err)
	}
	return (stat.Mode() & os.ModeCharDevice) != 0, nil
}

func DefaultDependencies() Dependencies {
	paths := osPathResolver{}
	commandRunner := runner.NewOSCommandRunner()
	return Dependencies{
		Catalog:           stages.DefaultCatalog,
		CommandRunner:     func() runner.CommandRunner { return commandRunner },
		StateRepositories: stateStoreFactory{},
		Paths:             paths,
		RunLogs:           filesystemRunLogFactory{Paths: paths},
		TemplateStores:    embeddedTemplateStoreFactory{},
		FileSystem:        stages.OSFileSystem{},
		PackageManager:    stages.HomebrewPackageManager{Runner: commandRunner},
		UI:                uiRunnerFunc(ui.Run),
		Executor:          executorFunc(execution.Execute),
		TTY:               stdinTTYDetector{},
		DryRunStageDelay:  execution.RandomDryRunStageDelay(2*time.Second, 5*time.Second),
	}
}

func (deps Dependencies) withDefaults() Dependencies {
	defaults := DefaultDependencies()
	if deps.Catalog == nil {
		deps.Catalog = defaults.Catalog
	}
	if deps.CommandRunner == nil {
		deps.CommandRunner = defaults.CommandRunner
	}
	if deps.StateRepositories == nil {
		deps.StateRepositories = defaults.StateRepositories
	}
	if deps.Paths == nil {
		deps.Paths = defaults.Paths
	}
	if deps.RunLogs == nil {
		deps.RunLogs = filesystemRunLogFactory{Paths: deps.Paths}
	}
	if deps.TemplateStores == nil {
		deps.TemplateStores = defaults.TemplateStores
	}
	if deps.FileSystem == nil {
		deps.FileSystem = defaults.FileSystem
	}
	if deps.PackageManager == nil {
		deps.PackageManager = stages.HomebrewPackageManager{Runner: deps.CommandRunner()}
	}
	if deps.UI == nil {
		deps.UI = defaults.UI
	}
	if deps.Executor == nil {
		deps.Executor = defaults.Executor
	}
	if deps.TTY == nil {
		deps.TTY = defaults.TTY
	}
	if deps.DryRunStageDelay == nil {
		deps.DryRunStageDelay = defaults.DryRunStageDelay
	}
	return deps
}
