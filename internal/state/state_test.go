package state

import (
	"context"
	"path/filepath"
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
