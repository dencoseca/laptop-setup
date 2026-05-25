package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC().Truncate(time.Second)
	run := &RunState{
		RunID:        NewRunID(now),
		StartAt:      now,
		Mode:         "normal",
		ResolvedPlan: []string{"a", "b"},
		Decisions: map[string]any{
			"selected_stage_ids": []string{"a", "b"},
		},
		SelectedIDs: []string{"go", "jq"},
		Stages: map[string]StageStatus{
			"a": {Status: "success", Attempts: 1},
			"b": {Status: "pending", Attempts: 0},
		},
	}

	if err := store.Save(context.Background(), run); err != nil {
		t.Fatalf("save state: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected persisted state, got nil")
	}
	if loaded.RunID != run.RunID {
		t.Fatalf("run id mismatch: got=%s want=%s", loaded.RunID, run.RunID)
	}
	if loaded.Mode != "normal" {
		t.Fatalf("mode mismatch: %s", loaded.Mode)
	}
	if got := loaded.Decisions["selected_stage_ids"]; got == nil {
		t.Fatalf("decision mismatch: %v", got)
	}
	if loaded.Stages["a"].Status != "success" {
		t.Fatalf("stage status mismatch for a: %+v", loaded.Stages["a"])
	}
}

func TestStoreLoadMissingStateReturnsNil(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load missing state returned error: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil state for missing file, got %+v", loaded)
	}
}

func TestRunDirRequiresRunID(t *testing.T) {
	_, err := RunDir("")
	if err == nil {
		t.Fatal("expected RunDir to fail with empty run id")
	}
}

func TestNewRunIDDoesNotCollideForSameInstant(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 34, 56, 789, time.UTC)
	seen := make(map[string]struct{})

	for range 1000 {
		runID := NewRunID(now)
		if _, exists := seen[runID]; exists {
			t.Fatalf("NewRunID collided for same instant: %s", runID)
		}
		seen[runID] = struct{}{}

		if !strings.HasPrefix(runID, "20260525T123456000000789Z-") {
			t.Fatalf("generated run id missing UTC nanosecond timestamp prefix: %q", runID)
		}
		if _, err := RunDir(runID); err != nil {
			t.Fatalf("generated run id rejected by RunDir: id=%q err=%v", runID, err)
		}
		if strings.ContainsAny(runID, `/\`) {
			t.Fatalf("generated run id contains path separator: %q", runID)
		}
	}
}

func TestRunDirRejectsInvalidRunIDs(t *testing.T) {
	invalidIDs := []string{
		"",
		"../x",
		filepath.Join(string(os.PathSeparator), "tmp", "x"),
		"a/b",
		`a\b`,
		".",
		"..",
		"run id",
		"run:id",
		"-starts-with-dash",
	}

	for _, runID := range invalidIDs {
		t.Run(runID, func(t *testing.T) {
			if _, err := RunDir(runID); err == nil {
				t.Fatalf("expected RunDir to reject %q", runID)
			}
		})
	}
}
