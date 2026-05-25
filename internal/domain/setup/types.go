package setup

import (
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var (
	validRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	runIDFallbackSeq  atomic.Uint64
)

type RunID string

func NewRunID(now time.Time) RunID {
	utc := now.UTC()
	timestamp := fmt.Sprintf("%s%09dZ", utc.Format("20060102T150405"), utc.Nanosecond())
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return RunID(fmt.Sprintf("%s-%016x", timestamp, runIDFallbackSeq.Add(1)))
	}
	return RunID(fmt.Sprintf("%s-%x", timestamp, suffix))
}

func (id RunID) String() string {
	return string(id)
}

func (id RunID) Validate() error {
	value := string(id)
	if value == "" {
		return errors.New("run id is required")
	}
	if filepath.IsAbs(value) {
		return fmt.Errorf("run id must be relative: %q", value)
	}
	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return fmt.Errorf("run id must not contain path separators: %q", value)
	}
	if !validRunIDPattern.MatchString(value) {
		return fmt.Errorf("run id contains invalid characters: %q", value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("run id must be clean: %q", value)
	}
	return nil
}

type Mode string

const (
	ModeNormal Mode = "normal"
	ModeDryRun Mode = "dry-run"
)

func ModeFromDryRun(dryRun bool) Mode {
	if dryRun {
		return ModeDryRun
	}
	return ModeNormal
}

func (mode Mode) String() string {
	return string(mode)
}

func (mode Mode) IsDryRun() bool {
	return mode == ModeDryRun
}

func (mode Mode) Validate() error {
	switch mode {
	case ModeNormal, ModeDryRun:
		return nil
	default:
		return fmt.Errorf("unknown value %q", mode)
	}
}

type StageID string

func (id StageID) String() string {
	return string(id)
}

func (id StageID) Validate() error {
	value := string(id)
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return errors.New("stage id is required")
	}
	if normalized != value {
		return errors.New("stage id must not have surrounding whitespace")
	}
	return nil
}

type StageStatus string

const (
	StageStatusPending          StageStatus = "pending"
	StageStatusRunning          StageStatus = "running"
	StageStatusSuccess          StageStatus = "success"
	StageStatusSkipped          StageStatus = "skipped"
	StageStatusFailed           StageStatus = "failed"
	StageStatusAlreadyDone      StageStatus = "already_done"
	StageStatusSimulatedSuccess StageStatus = "simulated_success"
)

func (status StageStatus) String() string {
	return string(status)
}

func (status StageStatus) Validate() error {
	switch status {
	case StageStatusPending,
		StageStatusRunning,
		StageStatusSuccess,
		StageStatusSkipped,
		StageStatusFailed,
		StageStatusAlreadyDone,
		StageStatusSimulatedSuccess:
		return nil
	default:
		return fmt.Errorf("unknown value %q", status)
	}
}

func IsTerminalStatus(status StageStatus) bool {
	switch status {
	case StageStatusSuccess,
		StageStatusAlreadyDone,
		StageStatusSimulatedSuccess,
		StageStatusSkipped:
		return true
	default:
		return false
	}
}
