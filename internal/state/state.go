package state

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	relativeStateFile = ".laptop-setup/state.json"
	relativeRunsDir   = ".laptop-setup/runs"
)

var (
	validRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	runIDFallbackSeq  atomic.Uint64
)

type StageStatus struct {
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
}

type RunState struct {
	RunID         string                 `json:"run_id"`
	StartAt       time.Time              `json:"start_at"`
	EndAt         *time.Time             `json:"end_at,omitempty"`
	Mode          string                 `json:"mode"`
	Decisions     map[string]any         `json:"decisions,omitempty"`
	SelectedIDs   []string               `json:"selected_ids,omitempty"`
	ResolvedPlan  []string               `json:"resolved_plan"`
	Stages        map[string]StageStatus `json:"stages"`
	LastFailure   string                 `json:"last_failure,omitempty"`
	GeneratedFile string                 `json:"generated_file,omitempty"`
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

func RunDir(runID string) (string, error) {
	if err := validateRunID(runID); err != nil {
		return "", err
	}
	runsDir, err := RunsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(runsDir, runID), nil
}

func NewRunID(now time.Time) string {
	utc := now.UTC()
	timestamp := fmt.Sprintf("%s%09dZ", utc.Format("20060102T150405"), utc.Nanosecond())
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("%s-%016x", timestamp, runIDFallbackSeq.Add(1))
	}
	return fmt.Sprintf("%s-%x", timestamp, suffix)
}

func validateRunID(runID string) error {
	if runID == "" {
		return errors.New("run id is required")
	}
	if filepath.IsAbs(runID) {
		return fmt.Errorf("run id must be relative: %q", runID)
	}
	if strings.Contains(runID, "/") || strings.Contains(runID, `\`) {
		return fmt.Errorf("run id must not contain path separators: %q", runID)
	}
	if !validRunIDPattern.MatchString(runID) {
		return fmt.Errorf("run id contains invalid characters: %q", runID)
	}
	if filepath.Clean(runID) != runID {
		return fmt.Errorf("run id must be clean: %q", runID)
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
		return nil, fmt.Errorf("decode state file: %w", err)
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

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	payload, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tempPath := s.path + ".tmp"
	if err = os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write temporary state file: %w", err)
	}
	if err = os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("commit state file: %w", err)
	}

	return nil
}
