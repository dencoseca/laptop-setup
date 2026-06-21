package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
)

type config struct {
	resume    bool
	dryRun    bool
	from      string
	only      []string
	skip      []string
	statePath string
}

type App struct {
	deps Dependencies
}

func New(deps Dependencies) *App {
	return &App{deps: deps.withDefaults()}
}

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	return New(DefaultDependencies()).Run(ctx, args, stdout, stderr)
}

func (a *App) Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseConfigWithDefaultPath(args, stderr, a.deps.Paths.DefaultStatePath)
	if err != nil {
		return err
	}

	if cfg.resume {
		if len(cfg.only) > 0 || len(cfg.skip) > 0 || strings.TrimSpace(cfg.from) != "" {
			return errors.New("--resume cannot be combined with --from, --only, or --skip")
		}
	}

	store := a.deps.StateRepositories.Open(cfg.statePath)
	current, err := store.Load(ctx)
	if err != nil {
		return err
	}

	if cfg.resume && current == nil {
		return errors.New("no previous run state found for --resume")
	}
	if cfg.resume {
		catalog := a.deps.Catalog()
		if err := validateResumeRequest(current, catalog, cfg.dryRun); err != nil {
			return err
		}
	}

	promptEnabled, err := a.deps.TTY.CanPrompt()
	if err != nil {
		return err
	}

	if !promptEnabled {
		return errors.New("laptop-setup requires an interactive TTY")
	}

	repoRoot, err := a.deps.Paths.WorkingDir()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	homeDir, err := a.deps.Paths.HomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	catalog := a.deps.Catalog()
	commandRunner := a.deps.CommandRunner()
	templateStore := a.deps.TemplateStores.New(repoRoot, a.deps.FileSystem)

	return a.deps.UI.Run(ctx, ui.Options{
		Config: ui.Config{
			Resume: cfg.resume,
			DryRun: cfg.dryRun,
			From:   cfg.from,
			Only:   cfg.only,
			Skip:   cfg.skip,
		},
		Store:     store,
		Current:   current,
		Catalog:   catalog,
		RepoRoot:  repoRoot,
		HomeDir:   homeDir,
		Out:       stdout,
		Commander: commandRunner,
		Templates: templateStore,
		ExecutionService: tuiExecutionService{
			deps:          a.deps,
			store:         store,
			catalog:       catalog,
			repoRoot:      repoRoot,
			homeDir:       homeDir,
			commandRunner: commandRunner,
			templateStore: templateStore,
		},
	})
}

func validateResumeRequest(runState *state.RunState, catalog []stages.Stage, requestedDryRun bool) error {
	if runState == nil {
		return errors.New("no previous run state found for --resume")
	}
	if requestedDryRun && !runState.Mode.IsDryRun() {
		return errors.New("cannot resume a normal run as dry-run")
	}
	effectiveDryRun := runState.Mode.IsDryRun()
	return execution.ValidateRunStateForCatalog(runState, catalog, effectiveDryRun)
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	return parseConfigWithDefaultPath(args, stderr, state.DefaultPath)
}

func parseConfigWithDefaultPath(args []string, stderr io.Writer, defaultStatePath func() (string, error)) (config, error) {
	fs := flag.NewFlagSet("laptop-setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.BoolVar(&cfg.resume, "resume", false, "Resume from existing run state")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate execution without machine changes")
	fs.StringVar(&cfg.from, "from", "", "Start execution from stage id")
	var onlyRaw string
	var skipRaw string
	fs.StringVar(&onlyRaw, "only", "", "Run only these stage ids (comma-separated)")
	fs.StringVar(&skipRaw, "skip", "", "Skip these stage ids (comma-separated)")
	fs.StringVar(&cfg.statePath, "state-file", "", "Override state file path")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(rest, " "))
	}

	cfg.only = parseCSV(onlyRaw)
	cfg.skip = parseCSV(skipRaw)

	if cfg.statePath == "" {
		defaultPath, err := defaultStatePath()
		if err != nil {
			return config{}, err
		}
		cfg.statePath = defaultPath
	}

	return cfg, nil
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
