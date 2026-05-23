package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

type config struct {
	yes         bool
	resume      bool
	dryRun      bool
	environment string
	from        string
	only        []string
	skip        []string
	statePath   string
}

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

	var runState *state.RunState
	catalog := stages.DefaultCatalog()
	stageIndex := make(map[string]stages.Stage, len(catalog))
	for _, stage := range catalog {
		stageIndex[stage.ID] = stage
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	effectiveDryRun := cfg.dryRun

	if cfg.resume {
		if current == nil {
			return errors.New("no previous run state found for --resume")
		}
		runState = current
		if cfg.dryRun && runState.Mode != "dry-run" {
			return errors.New("cannot resume a normal run as dry-run")
		}
		effectiveDryRun = runState.Mode == "dry-run"
	} else {
		if err = stages.ValidateEnvironment(cfg.environment); err != nil {
			return err
		}
		plan, resolveErr := stages.ResolvePlan(catalog, stages.PlanOptions{
			FromID:  cfg.from,
			OnlyIDs: cfg.only,
			SkipIDs: cfg.skip,
		})
		if resolveErr != nil {
			return resolveErr
		}

		selectedIDs, selectErr := stages.ResolveSelectedBrewIDs(repoRoot, cfg.environment)
		if selectErr != nil {
			return selectErr
		}

		runState = &state.RunState{
			RunID:        state.NewRunID(time.Now()),
			StartAt:      time.Now().UTC(),
			Mode:         modeName(effectiveDryRun),
			ResolvedPlan: plan,
			Decisions: map[string]any{
				stages.DecisionEnvironment: cfg.environment,
			},
			SelectedIDs: selectedIDs,
			Stages:      make(map[string]state.StageStatus, len(catalog)),
		}
	}

	if runState.Decisions == nil {
		runState.Decisions = map[string]any{}
	}
	environment := strings.TrimSpace(cfg.environment)
	if environment == "" {
		if existing, ok := runState.Decisions[stages.DecisionEnvironment].(string); ok {
			environment = existing
		}
	}
	if err = stages.ValidateEnvironment(environment); err != nil {
		return err
	}
	runState.Decisions[stages.DecisionEnvironment] = environment
	if len(runState.SelectedIDs) == 0 {
		selectedIDs, selectErr := stages.ResolveSelectedBrewIDs(repoRoot, environment)
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
	commandRunner := runner.NewOSCommandRunner()

	if err = logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_started",
		Message:   "Starting stage execution",
	}); err != nil {
		return err
	}

	promptEnabled, err := canPrompt()
	if err != nil {
		return err
	}

	for _, stageID := range runState.ResolvedPlan {
		stage, ok := stageIndex[stageID]
		if !ok {
			return fmt.Errorf("unknown stage in plan: %s", stageID)
		}

		progress := runState.Stages[stageID]
		if progress.Status == string(stages.StatusSuccess) ||
			progress.Status == string(stages.StatusAlreadyDone) ||
			progress.Status == string(stages.StatusSimulatedSuccess) ||
			progress.Status == string(stages.StatusSkipped) {
			continue
		}

		if !cfg.yes && stage.CanSkip {
			shouldRun, confirmErr := confirmStage(stdout, promptEnabled, stage)
			if confirmErr != nil {
				return confirmErr
			}
			if !shouldRun {
				progress.Status = string(stages.StatusSkipped)
				runState.Stages[stageID] = progress
				if err = store.Save(ctx, runState); err != nil {
					return err
				}
				if err = logger.Log(runner.Event{
					RunID:     runState.RunID,
					StageID:   stageID,
					Attempt:   progress.Attempts,
					Mode:      runState.Mode,
					EventType: "stage_skipped",
					Message:   "user skipped stage",
				}); err != nil {
					return err
				}
				continue
			}
		}

		progress.Status = string(stages.StatusRunning)
		progress.Attempts++
		runState.Stages[stageID] = progress
		if err = store.Save(ctx, runState); err != nil {
			return err
		}

		if err = logger.Log(runner.Event{
			RunID:     runState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      runState.Mode,
			EventType: "stage_started",
			Message:   stage.Title,
		}); err != nil {
			return err
		}

		execCtx := stages.ExecutionContext{
			DryRun:                effectiveDryRun,
			Runner:                commandRunner,
			Logger:                logger,
			RunID:                 runState.RunID,
			Mode:                  runState.Mode,
			StageID:               stageID,
			Attempt:               progress.Attempts,
			RunDir:                runDir,
			RepoRoot:              repoRoot,
			HomeDir:               homeDir,
			Environment:           environment,
			SelectedBrewIDs:       runState.SelectedIDs,
			GeneratedBrewfilePath: runState.GeneratedFile,
			SetGeneratedBrewfilePath: func(path string) {
				runState.GeneratedFile = path
			},
		}

		checkResult, err := stage.Precheck(ctx, execCtx)
		if err != nil {
			progress.Status = string(stages.StatusFailed)
			progress.LastError = err.Error()
			runState.Stages[stageID] = progress
			runState.LastFailure = err.Error()
			_ = store.Save(ctx, runState)
			return fmt.Errorf("precheck failed for %s: %w", stageID, err)
		}
		if checkResult.Satisfied {
			progress.Status = string(stages.StatusAlreadyDone)
			runState.Stages[stageID] = progress
			if err = store.Save(ctx, runState); err != nil {
				return err
			}
			if err = logger.Log(runner.Event{
				RunID:     runState.RunID,
				StageID:   stageID,
				Attempt:   progress.Attempts,
				Mode:      runState.Mode,
				EventType: "stage_already_done",
				Message:   checkResult.Message,
			}); err != nil {
				return err
			}
			continue
		}

		if effectiveDryRun {
			if err = stage.Simulate(ctx, execCtx); err != nil {
				progress.Status = string(stages.StatusFailed)
				progress.LastError = err.Error()
				runState.Stages[stageID] = progress
				runState.LastFailure = err.Error()
				_ = store.Save(ctx, runState)
				return fmt.Errorf("simulate failed for %s: %w", stageID, err)
			}
			progress.Status = string(stages.StatusSimulatedSuccess)
		} else {
			if err = stage.Run(ctx, execCtx); err != nil {
				progress.Status = string(stages.StatusFailed)
				progress.LastError = err.Error()
				runState.Stages[stageID] = progress
				runState.LastFailure = err.Error()
				_ = store.Save(ctx, runState)
				return fmt.Errorf("stage failed for %s: %w", stageID, err)
			}
			progress.Status = string(stages.StatusSuccess)
		}

		runState.Stages[stageID] = progress
		if err = store.Save(ctx, runState); err != nil {
			return err
		}

		if err = logger.Log(runner.Event{
			RunID:     runState.RunID,
			StageID:   stageID,
			Attempt:   progress.Attempts,
			Mode:      runState.Mode,
			EventType: "stage_completed",
			Message:   progress.Status,
		}); err != nil {
			return err
		}
	}

	endAt := time.Now().UTC()
	runState.EndAt = &endAt
	if err = store.Save(ctx, runState); err != nil {
		return err
	}

	if err = logger.Log(runner.Event{
		RunID:     runState.RunID,
		Mode:      runState.Mode,
		EventType: "run_completed",
		Message:   "All planned stages processed",
	}); err != nil {
		return err
	}

	return nil
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("laptop-setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.BoolVar(&cfg.yes, "yes", false, "Run non-interactively")
	fs.BoolVar(&cfg.yes, "y", false, "Run non-interactively")
	fs.BoolVar(&cfg.resume, "resume", false, "Resume from existing run state")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Simulate execution without machine changes")
	fs.StringVar(&cfg.environment, "environment", "", "Environment profile to apply (home|work)")
	fs.StringVar(&cfg.environment, "e", "", "Environment profile to apply (home|work)")
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
	cfg.environment = strings.ToLower(strings.TrimSpace(cfg.environment))

	if cfg.statePath == "" {
		defaultPath, err := state.DefaultPath()
		if err != nil {
			return config{}, err
		}
		cfg.statePath = defaultPath
	}

	if !cfg.resume && cfg.environment == "" {
		return config{}, errors.New("environment is required (use --environment home|work)")
	}

	return cfg, nil
}

func modeName(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "normal"
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

func confirmStage(stdout io.Writer, promptEnabled bool, stage stages.Stage) (bool, error) {
	if !promptEnabled {
		return false, errors.New("interactive confirmation required but no TTY detected; rerun with --yes")
	}

	if _, err := fmt.Fprintf(stdout, "\nRun stage %s (%s)? [y/N]: ", stage.ID, stage.Title); err != nil {
		return false, fmt.Errorf("write prompt: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read stage confirmation: %w", err)
	}
	normalized := strings.ToLower(strings.TrimSpace(line))
	return normalized == "y" || normalized == "yes", nil
}
