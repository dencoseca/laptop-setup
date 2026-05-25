package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
)

type config struct {
	yes       bool
	resume    bool
	dryRun    bool
	from      string
	only      []string
	skip      []string
	statePath string
}

var (
	defaultCatalogFn       = stages.DefaultCatalog
	resolveSelectedBrewIDs = stages.ResolveSelectedBrewIDs
	newCommandRunner       = func() runner.CommandRunner { return runner.NewOSCommandRunner() }
	uiRun                  = ui.Run
	executeRun             = execution.Execute
	getwd                  = os.Getwd
	userHomeDirectory      = os.UserHomeDir
	dryRunStageDelay       = execution.RandomDryRunStageDelay(2*time.Second, 5*time.Second)
)

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}

	if cfg.resume {
		if len(cfg.only) > 0 || len(cfg.skip) > 0 || strings.TrimSpace(cfg.from) != "" {
			return errors.New("--resume cannot be combined with --from, --only, or --skip")
		}
	}

	store := state.NewStore(cfg.statePath)
	current, err := store.Load(ctx)
	if err != nil {
		return err
	}

	if cfg.resume && current == nil {
		return errors.New("no previous run state found for --resume")
	}
	if cfg.resume {
		catalog := defaultCatalogFn()
		if err := validateResumeRequest(current, catalog, cfg.dryRun); err != nil {
			return err
		}
	}

	promptEnabled, err := canPrompt()
	if err != nil {
		return err
	}

	if !cfg.yes {
		if !promptEnabled {
			return errors.New("interactive mode requires a TTY; rerun with --yes for non-interactive execution")
		}

		repoRoot, err := getwd()
		if err != nil {
			return fmt.Errorf("resolve repository root: %w", err)
		}
		homeDir, err := userHomeDirectory()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}

		return uiRun(ctx, ui.Options{
			Config: ui.Config{
				Resume: cfg.resume,
				DryRun: cfg.dryRun,
				From:   cfg.from,
				Only:   cfg.only,
				Skip:   cfg.skip,
			},
			Store:     store,
			Current:   current,
			Catalog:   defaultCatalogFn(),
			RepoRoot:  repoRoot,
			HomeDir:   homeDir,
			Out:       stdout,
			Commander: newCommandRunner(),
		})
	}

	return runNonInteractive(ctx, cfg, store, current, stdout)
}

func runNonInteractive(
	ctx context.Context,
	cfg config,
	store *state.Store,
	current *state.RunState,
	stdout io.Writer,
) error {
	catalog := defaultCatalogFn()
	repoRoot, err := getwd()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	homeDir, err := userHomeDirectory()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	effectiveDryRun := cfg.dryRun
	var runState *state.RunState

	if cfg.resume {
		runState = current
		effectiveDryRun = runState.Mode.IsDryRun()
		if err := validateResumeRequest(runState, catalog, cfg.dryRun); err != nil {
			return err
		}
	} else {
		plan, resolveErr := stages.ResolvePlan(catalog, stages.PlanOptions{
			FromID:  cfg.from,
			OnlyIDs: cfg.only,
			SkipIDs: cfg.skip,
		})
		if resolveErr != nil {
			return resolveErr
		}
		selectedIDs, selectErr := resolveSelectedBrewIDs(repoRoot)
		if selectErr != nil {
			return selectErr
		}
		runState = &state.RunState{
			RunID:        state.NewRunID(time.Now()),
			StartAt:      time.Now().UTC(),
			Mode:         modeName(effectiveDryRun),
			ResolvedPlan: plan,
			Decisions:    stages.DefaultDecisions().WithSelectedStageIDs(plan),
			SelectedIDs:  selectedIDs,
			Stages:       make(map[state.StageID]state.StageStatus, len(catalog)),
		}
	}

	if runState.Decisions.IsZero() {
		runState.Decisions = stages.DefaultDecisions().WithSelectedStageIDs(runState.ResolvedPlan)
	}
	if len(runState.Decisions.SelectedStageIDs) == 0 {
		runState.Decisions = runState.Decisions.WithSelectedStageIDs(runState.ResolvedPlan)
	}
	if err = runState.Decisions.Validate(); err != nil {
		return fmt.Errorf("validate decisions: %w", err)
	}
	if len(runState.SelectedIDs) == 0 {
		selectedIDs, selectErr := resolveSelectedBrewIDs(repoRoot)
		if selectErr != nil {
			return selectErr
		}
		runState.SelectedIDs = selectedIDs
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

	return executeRun(ctx, execution.Options{
		Store:         store,
		RunState:      runState,
		Catalog:       catalog,
		DryRun:        effectiveDryRun,
		DryRunDelay:   dryRunStageDelay,
		RepoRoot:      repoRoot,
		HomeDir:       homeDir,
		RunDir:        runDir,
		CommandRunner: newCommandRunner(),
		Logger:        logger,
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
	fs := flag.NewFlagSet("laptop-setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.BoolVar(&cfg.yes, "yes", false, "Run non-interactively")
	fs.BoolVar(&cfg.yes, "y", false, "Run non-interactively")
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
		defaultPath, err := state.DefaultPath()
		if err != nil {
			return config{}, err
		}
		cfg.statePath = defaultPath
	}

	return cfg, nil
}

func modeName(dryRun bool) state.Mode {
	return state.ModeFromDryRun(dryRun)
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

func canPrompt() (bool, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect stdin: %w", err)
	}
	return (stat.Mode() & os.ModeCharDevice) != 0, nil
}
