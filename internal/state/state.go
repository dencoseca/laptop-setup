package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dencoseca/laptop-setup/internal/domain/setup"
	"github.com/dencoseca/laptop-setup/internal/stages"
)

const (
	relativeStateFile = ".laptop-setup/state.json"
	relativeRunsDir   = ".laptop-setup/runs"
	privateDirPerm    = 0o700
	privateFilePerm   = 0o600
)

type RunID = setup.RunID
type Mode = setup.Mode
type StageID = setup.StageID
type StageStatusValue = setup.StageStatus

const (
	ModeNormal Mode = setup.ModeNormal
	ModeDryRun Mode = setup.ModeDryRun

	StageStatusPending          StageStatusValue = setup.StageStatusPending
	StageStatusRunning          StageStatusValue = setup.StageStatusRunning
	StageStatusSuccess          StageStatusValue = setup.StageStatusSuccess
	StageStatusSkipped          StageStatusValue = setup.StageStatusSkipped
	StageStatusFailed           StageStatusValue = setup.StageStatusFailed
	StageStatusAlreadyDone      StageStatusValue = setup.StageStatusAlreadyDone
	StageStatusSimulatedSuccess StageStatusValue = setup.StageStatusSimulatedSuccess
)

type StageStatus struct {
	Status    StageStatusValue `json:"status"`
	Attempts  int              `json:"attempts"`
	LastError string           `json:"last_error,omitempty"`
}

type RunState struct {
	RunID         RunID                   `json:"run_id"`
	StartAt       time.Time               `json:"start_at"`
	EndAt         *time.Time              `json:"end_at,omitempty"`
	Mode          Mode                    `json:"mode"`
	Decisions     stages.DecisionSet      `json:"decisions,omitempty"`
	SelectedIDs   []string                `json:"selected_ids,omitempty"`
	ResolvedPlan  []StageID               `json:"resolved_plan"`
	Stages        map[StageID]StageStatus `json:"stages"`
	LastFailure   string                  `json:"last_failure,omitempty"`
	GeneratedFile string                  `json:"generated_file,omitempty"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, relativeStateFile), nil
}

func RunsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, relativeRunsDir), nil
}

func RunDir(runID RunID) (string, error) {
	if err := runID.Validate(); err != nil {
		return "", err
	}
	runsDir, err := RunsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(runsDir, runID.String()), nil
}

func NewRunID(now time.Time) RunID {
	return setup.NewRunID(now)
}

func ModeFromDryRun(dryRun bool) Mode {
	return setup.ModeFromDryRun(dryRun)
}

func IsTerminalStatus(status StageStatusValue) bool {
	return setup.IsTerminalStatus(status)
}

func NormalizeRunState(run *RunState) error {
	if run == nil {
		return errors.New("field run_state: is required")
	}
	if run.Decisions.IsZero() {
		run.Decisions = stages.DefaultDecisions().WithSelectedStageIDs(run.ResolvedPlan)
	}
	if len(run.Decisions.SelectedStageIDs) == 0 {
		run.Decisions = run.Decisions.WithSelectedStageIDs(run.ResolvedPlan)
	}
	return nil
}

func ValidateRunState(run *RunState) error {
	if run == nil {
		return errors.New("field run_state: is required")
	}
	if err := run.RunID.Validate(); err != nil {
		return fmt.Errorf("field run_id: %w", err)
	}
	if err := run.Mode.Validate(); err != nil {
		return fmt.Errorf("field mode: %w", err)
	}
	if len(run.ResolvedPlan) == 0 {
		return errors.New("field resolved_plan: must contain at least one stage id")
	}
	seenPlanIDs := make(map[StageID]struct{}, len(run.ResolvedPlan))
	for index, stageID := range run.ResolvedPlan {
		if err := stageID.Validate(); err != nil {
			return fmt.Errorf("field resolved_plan[%d]: %w", index, err)
		}
		if _, seen := seenPlanIDs[stageID]; seen {
			return fmt.Errorf("field resolved_plan[%d]: duplicate stage id %q", index, stageID)
		}
		seenPlanIDs[stageID] = struct{}{}
	}
	if err := run.Decisions.Validate(); err != nil {
		return fmt.Errorf("field decisions: %w", err)
	}
	for stageID, status := range run.Stages {
		if err := stageID.Validate(); err != nil {
			return fmt.Errorf("field stages: %w", err)
		}
		if strings.TrimSpace(status.Status.String()) == "" {
			return fmt.Errorf("field stages.%s.status: status is required", stageID)
		}
		if err := status.Status.Validate(); err != nil {
			return fmt.Errorf("field stages.%s.status: %w", stageID, err)
		}
		if status.Attempts < 0 {
			return fmt.Errorf("field stages.%s.attempts: must be non-negative", stageID)
		}
	}
	if strings.TrimSpace(run.GeneratedFile) != "" {
		if err := validateGeneratedFile(run.RunID, run.GeneratedFile); err != nil {
			return fmt.Errorf("field generated_file: %w", err)
		}
	}
	return nil
}

func validateGeneratedFile(runID RunID, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("must be an absolute path inside the run directory: %q", path)
	}
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return fmt.Errorf("must be clean: %q", path)
	}
	runDir, err := RunDir(runID)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(runDir, cleanPath)
	if err != nil {
		return fmt.Errorf("compare to run directory: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("must be inside run directory %q: %q", runDir, path)
	}
	return nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load(ctx context.Context) (*RunState, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if s.path == "" {
		return nil, errors.New("state path is empty")
	}

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state RunState
	if err = json.Unmarshal(payload, &state); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, fmt.Errorf("decode state file: invalid JSON at byte %d: %w", syntaxErr.Offset, err)
		}
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			field := typeErr.Field
			if field == "" {
				field = typeErr.Value
			}
			return nil, fmt.Errorf("decode state file: invalid field %s at byte %d: %w", field, typeErr.Offset, err)
		}
		return nil, fmt.Errorf("decode state file: %w", err)
	}
	if err = NormalizeRunState(&state); err != nil {
		return nil, fmt.Errorf("normalize state file: %w", err)
	}
	if err = ValidateRunState(&state); err != nil {
		return nil, fmt.Errorf("validate state file: %w", err)
	}
	return &state, nil
}

func (s *Store) Save(ctx context.Context, run *RunState) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if s.path == "" {
		return errors.New("state path is empty")
	}
	if run == nil {
		return errors.New("run state is nil")
	}

	stateDir := filepath.Dir(s.path)
	if err := os.MkdirAll(stateDir, privateDirPerm); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	payload, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tempFile, err := os.CreateTemp(stateDir, "."+filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if err = tempFile.Chmod(privateFilePerm); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("secure temporary state file: %w", err)
	}
	if _, err = tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err = tempFile.Close(); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err = os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("commit state file: %w", err)
	}

	return nil
}
