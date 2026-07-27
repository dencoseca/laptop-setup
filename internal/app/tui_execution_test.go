package app

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
	"github.com/dencoseca/laptop-setup/internal/ui"
)

type stubStateRepository struct {
	saveErr error
	saves   int
	events  *[]string
}

func (s *stubStateRepository) Load(context.Context) (*state.RunState, error) {
	return nil, nil
}

func (s *stubStateRepository) Save(context.Context, *state.RunState) error {
	s.saves++
	if s.events != nil {
		*s.events = append(*s.events, "save")
	}
	return s.saveErr
}

func (s *stubStateRepository) Path() string {
	return ""
}

type stubRunLogFactory struct {
	logs   RunLogs
	err    error
	opens  int
	runID  state.RunID
	events *[]string
}

func (f *stubRunLogFactory) Open(runID state.RunID) (RunLogs, error) {
	f.opens++
	f.runID = runID
	if f.events != nil {
		*f.events = append(*f.events, "open")
	}
	return f.logs, f.err
}

type trackingWriteCloser struct {
	closeErr error
	closes   int
}

func (*trackingWriteCloser) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func (w *trackingWriteCloser) Close() error {
	w.closes++
	return w.closeErr
}

func TestPrepareExecutionRejectsInvalidFreshRunBeforeMutation(t *testing.T) {
	store := &stubStateRepository{}
	logFactory := &stubRunLogFactory{}
	service := testTUIExecutionService(store, logFactory)

	_, err := service.PrepareExecution(context.Background(), ui.ExecutionRequest{
		Plan:      []state.StageID{"missing"},
		Decisions: stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"missing"}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if store.saves != 0 {
		t.Fatalf("expected no state saves, got %d", store.saves)
	}
	if logFactory.opens != 0 {
		t.Fatalf("expected no log opens, got %d", logFactory.opens)
	}
}

func TestPrepareExecutionLogOpenFailurePreservesPriorState(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(statePath)
	prior := testRunState("prior-run")
	if err := store.Save(ctx, prior); err != nil {
		t.Fatalf("seed prior state: %v", err)
	}

	openErr := errors.New("open logs")
	logFactory := &stubRunLogFactory{err: openErr}
	service := testTUIExecutionService(store, logFactory)

	_, err := service.PrepareExecution(ctx, testFreshExecutionRequest())
	if !errors.Is(err, openErr) {
		t.Fatalf("expected log-open error, got %v", err)
	}

	current, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load current state: %v", err)
	}
	if current.RunID != prior.RunID {
		t.Fatalf("current state replaced: got %q want %q", current.RunID, prior.RunID)
	}
}

func TestPrepareExecutionStateSaveFailureClosesBothLogsAndRetainsArtifacts(t *testing.T) {
	saveErr := errors.New("save state")
	humanCloseErr := errors.New("close human log")
	humanLog := &trackingWriteCloser{closeErr: humanCloseErr}
	eventLog := &trackingWriteCloser{}
	store := &stubStateRepository{saveErr: saveErr}
	logFactory := &stubRunLogFactory{logs: RunLogs{
		RunDir:   "/runs/unused",
		HumanLog: humanLog,
		EventLog: eventLog,
	}}
	service := testTUIExecutionService(store, logFactory)

	_, err := service.PrepareExecution(context.Background(), testFreshExecutionRequest())
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected state-save error, got %v", err)
	}
	if !errors.Is(err, humanCloseErr) {
		t.Fatalf("expected close error to be retained, got %v", err)
	}
	if !strings.Contains(err.Error(), `unused log artifacts retained in "/runs/unused"`) {
		t.Fatalf("expected retained-artifact location, got %v", err)
	}
	if humanLog.closes != 1 {
		t.Fatalf("expected human log to close once, got %d", humanLog.closes)
	}
	if eventLog.closes != 1 {
		t.Fatalf("expected event log to close once, got %d", eventLog.closes)
	}
}

func TestPrepareExecutionOpensLogsBeforeSavingFreshAndResumedRuns(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		request ui.ExecutionRequest
	}{
		{name: "fresh", request: testFreshExecutionRequest()},
		{
			name: "resume",
			request: ui.ExecutionRequest{
				Resume:  true,
				Current: testRunState("resumed-run"),
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			events := []string{}
			humanLog := &trackingWriteCloser{}
			eventLog := &trackingWriteCloser{}
			store := &stubStateRepository{events: &events}
			logFactory := &stubRunLogFactory{
				events: &events,
				logs: RunLogs{
					RunDir:       "/runs/success",
					HumanLogPath: "/runs/success/run.log",
					EventsPath:   "/runs/success/events.jsonl",
					HumanLog:     humanLog,
					EventLog:     eventLog,
				},
			}
			service := testTUIExecutionService(store, logFactory)

			run, err := service.PrepareExecution(context.Background(), testCase.request)
			if err != nil {
				t.Fatalf("PrepareExecution returned error: %v", err)
			}
			t.Cleanup(func() {
				_ = run.HumanLog.Close()
				_ = run.EventsLog.Close()
			})

			if got, want := strings.Join(events, ","), "open,save"; got != want {
				t.Fatalf("operation order: got %q want %q", got, want)
			}
			if run.RunState == nil || logFactory.runID != run.RunState.RunID {
				t.Fatalf("logs opened for %q, run state is %+v", logFactory.runID, run.RunState)
			}
			if run.HumanLog != humanLog || run.EventsLog != eventLog {
				t.Fatal("opened log handles were not returned")
			}
			if testCase.request.Resume && run.RunState != testCase.request.Current {
				t.Fatal("resume did not retain the current run state")
			}
		})
	}
}

func testTUIExecutionService(store StateRepository, logFactory RunLogFactory) tuiExecutionService {
	return tuiExecutionService{
		deps:  Dependencies{RunLogs: logFactory},
		store: store,
		catalog: []stages.Stage{{
			ID:    "stage",
			Title: "Stage",
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return nil },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		}},
		commandRunner: noOpRunner{},
	}
}

func testFreshExecutionRequest() ui.ExecutionRequest {
	return ui.ExecutionRequest{
		Plan:        []state.StageID{"stage"},
		Decisions:   stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"stage"}),
		SelectedIDs: []string{"package"},
	}
}

func testRunState(runID state.RunID) *state.RunState {
	return &state.RunState{
		RunID:        runID,
		StartAt:      time.Now().UTC(),
		Mode:         state.ModeNormal,
		ResolvedPlan: []state.StageID{"stage"},
		Decisions:    stages.DefaultDecisions().WithSelectedStageIDs([]state.StageID{"stage"}),
		Stages:       map[state.StageID]state.StageStatus{},
	}
}

type noOpRunner struct{}

func (noOpRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	return runner.Result{}, nil
}

func (noOpRunner) LookPath(context.Context, string) (string, error) {
	return "", nil
}

var _ io.WriteCloser = (*trackingWriteCloser)(nil)
