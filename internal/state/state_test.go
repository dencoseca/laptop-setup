package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencoseca/laptop-setup/internal/stages"
)

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC().Truncate(time.Second)
	run := &RunState{
		RunID:        NewRunID(now),
		StartAt:      now,
		Mode:         "normal",
		ResolvedPlan: []StageID{"a", "b"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]StageID{"a", "b"}),
		SelectedIDs:  []string{"go", "jq"},
		Stages: map[StageID]StageStatus{
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
	if len(loaded.Decisions.SelectedStageIDs) != 2 {
		t.Fatalf("decision mismatch: %+v", loaded.Decisions)
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

func TestStoreContractSaveLoadUpdateAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	store := NewStore(path)
	if store.Path() != path {
		t.Fatalf("Path mismatch: got=%q want=%q", store.Path(), path)
	}

	run := &RunState{
		RunID:        "run-1",
		Mode:         "normal",
		ResolvedPlan: []StageID{"stage_one"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]StageID{"stage_one"}),
		Stages: map[StageID]StageStatus{
			"stage_one": {Status: StageStatusPending},
		},
	}
	if err := store.Save(context.Background(), run); err != nil {
		t.Fatalf("initial Save returned error: %v", err)
	}

	run.Stages["stage_one"] = StageStatus{Status: StageStatusSuccess, Attempts: 1}
	if err := store.Save(context.Background(), run); err != nil {
		t.Fatalf("update Save returned error: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected saved state")
	}
	if status := loaded.Stages["stage_one"]; status.Status != StageStatusSuccess || status.Attempts != 1 {
		t.Fatalf("loaded status mismatch: %+v", status)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected temporary state file to be absent after commit, got err=%v", err)
	}
}

func TestStoreLoadRejectsInvalidPersistedState(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "invalid json",
			payload: `{`,
			want:    "invalid JSON",
		},
		{
			name: "invalid mode",
			payload: `{
				"run_id": "run-1",
				"mode": "turbo",
				"resolved_plan": ["a"],
				"stages": {}
			}`,
			want: "field mode",
		},
		{
			name: "unknown status",
			payload: `{
				"run_id": "run-1",
				"mode": "normal",
				"resolved_plan": ["a"],
				"stages": {
					"a": {"status": "mystery", "attempts": 1}
				}
			}`,
			want: "field stages.a.status",
		},
		{
			name: "invalid decision value",
			payload: `{
				"run_id": "run-1",
				"mode": "normal",
				"decisions": {
					"dev.node_toolchain": "invalid"
				},
				"resolved_plan": ["a"],
				"stages": {}
			}`,
			want: "dev.node_toolchain",
		},
		{
			name: "path traversal run id",
			payload: `{
				"run_id": "../x",
				"mode": "normal",
				"resolved_plan": ["a"],
				"stages": {}
			}`,
			want: "field run_id",
		},
		{
			name: "empty plan",
			payload: `{
				"run_id": "run-1",
				"mode": "normal",
				"resolved_plan": [],
				"stages": {}
			}`,
			want: "field resolved_plan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore(filepath.Join(t.TempDir(), "state.json"))
			if err := os.WriteFile(store.Path(), []byte(tc.payload), 0o644); err != nil {
				t.Fatalf("write state payload: %v", err)
			}

			loaded, err := store.Load(context.Background())
			if err == nil {
				t.Fatalf("expected load error, got state %+v", loaded)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStoreLoadRejectsGeneratedFileOutsideRunDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	payload := `{
		"run_id": "run-1",
		"mode": "normal",
		"resolved_plan": ["brew_bundle"],
		"stages": {},
		"generated_file": "/tmp/Brewfile.generated"
	}`
	if err := os.WriteFile(store.Path(), []byte(payload), 0o644); err != nil {
		t.Fatalf("write state payload: %v", err)
	}

	loaded, err := store.Load(context.Background())
	if err == nil {
		t.Fatalf("expected load error, got state %+v", loaded)
	}
	if !strings.Contains(err.Error(), "field generated_file") {
		t.Fatalf("expected generated_file validation error, got %v", err)
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
		if _, exists := seen[runID.String()]; exists {
			t.Fatalf("NewRunID collided for same instant: %s", runID)
		}
		seen[runID.String()] = struct{}{}

		if !strings.HasPrefix(runID.String(), "20260525T123456000000789Z-") {
			t.Fatalf("generated run id missing UTC nanosecond timestamp prefix: %q", runID)
		}
		if _, err := RunDir(runID); err != nil {
			t.Fatalf("generated run id rejected by RunDir: id=%q err=%v", runID, err)
		}
		if strings.ContainsAny(runID.String(), `/\`) {
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
			if _, err := RunDir(RunID(runID)); err == nil {
				t.Fatalf("expected RunDir to reject %q", runID)
			}
		})
	}
}
