package state

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file permissions: got=%#o want=%#o", got, os.FileMode(0o600))
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
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat state directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory permissions: got=%#o want=%#o", got, os.FileMode(0o700))
	}
}

func TestStoreSaveRejectsInvalidRunStateBeforeCreatingFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cases := []struct {
		name   string
		mutate func(*RunState)
		want   string
	}{
		{
			name: "invalid run id",
			mutate: func(run *RunState) {
				run.RunID = "../run"
			},
			want: "field run_id",
		},
		{
			name: "invalid mode",
			mutate: func(run *RunState) {
				run.Mode = "turbo"
			},
			want: "field mode",
		},
		{
			name: "empty plan",
			mutate: func(run *RunState) {
				run.ResolvedPlan = nil
			},
			want: "field resolved_plan",
		},
		{
			name: "invalid plan stage id",
			mutate: func(run *RunState) {
				run.ResolvedPlan = []StageID{" "}
			},
			want: "field resolved_plan[0]",
		},
		{
			name: "duplicate plan stage id",
			mutate: func(run *RunState) {
				run.ResolvedPlan = []StageID{"stage_one", "stage_one"}
			},
			want: "field resolved_plan[1]",
		},
		{
			name: "invalid decision",
			mutate: func(run *RunState) {
				run.Decisions.NodeToolchain = "invalid"
			},
			want: "field decisions: dev.node_toolchain",
		},
		{
			name: "invalid stage map id",
			mutate: func(run *RunState) {
				run.Stages = map[StageID]StageStatus{" ": {Status: StageStatusPending}}
			},
			want: "field stages",
		},
		{
			name: "missing stage status",
			mutate: func(run *RunState) {
				run.Stages["stage_one"] = StageStatus{}
			},
			want: "field stages.stage_one.status",
		},
		{
			name: "invalid stage status",
			mutate: func(run *RunState) {
				run.Stages["stage_one"] = StageStatus{Status: "unknown"}
			},
			want: "field stages.stage_one.status",
		},
		{
			name: "negative stage attempts",
			mutate: func(run *RunState) {
				run.Stages["stage_one"] = StageStatus{Status: StageStatusFailed, Attempts: -1}
			},
			want: "field stages.stage_one.attempts",
		},
		{
			name: "generated file outside run directory",
			mutate: func(run *RunState) {
				run.GeneratedFile = filepath.Join(homeDir, "outside", "generated")
			},
			want: "field generated_file",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := filepath.Join(t.TempDir(), "missing")
			store := NewStore(filepath.Join(stateDir, "state.json"))
			run := validRunState()
			tc.mutate(run)

			err := store.Save(context.Background(), run)
			if err == nil {
				t.Fatal("expected Save to reject invalid state")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %v", tc.want, err)
			}
			if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
				t.Fatalf("invalid Save created state directory: err=%v", err)
			}
		})
	}
}

func TestStoreSaveNormalizesCopyWithoutMutatingCaller(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	run := validRunState()
	run.Decisions = stages.DecisionSet{}
	wantCaller := cloneRunState(run)

	if err := store.Save(context.Background(), run); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if !reflect.DeepEqual(run, wantCaller) {
		t.Fatalf("Save mutated caller:\n got: %+v\nwant: %+v", run, wantCaller)
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error after successful Save: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected saved state")
	}
	if loaded.Decisions.IsZero() {
		t.Fatal("expected persisted state to contain normalized decisions")
	}
	if !reflect.DeepEqual(loaded.Decisions.SelectedStageIDs, run.ResolvedPlan) {
		t.Fatalf("normalized selected stage ids: got=%v want=%v", loaded.Decisions.SelectedStageIDs, run.ResolvedPlan)
	}
}

func TestStoreInvalidSaveLeavesExistingStateUntouched(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	existing := []byte("existing state bytes\n")
	if err := os.WriteFile(store.Path(), existing, 0o600); err != nil {
		t.Fatalf("write existing state: %v", err)
	}

	run := validRunState()
	run.Mode = "invalid"
	if err := store.Save(context.Background(), run); err == nil {
		t.Fatal("expected Save to reject invalid state")
	}

	got, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read existing state: %v", err)
	}
	if !reflect.DeepEqual(got, existing) {
		t.Fatalf("existing state changed:\n got: %q\nwant: %q", got, existing)
	}
}

func validRunState() *RunState {
	return &RunState{
		RunID:        "run-1",
		Mode:         ModeNormal,
		ResolvedPlan: []StageID{"stage_one"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]StageID{"stage_one"}),
		Stages: map[StageID]StageStatus{
			"stage_one": {Status: StageStatusPending},
		},
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
