package runner

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatHumanIncludesStageAttemptCommandAndExitCode(t *testing.T) {
	exitCode := 23
	line := formatHuman(Event{
		Timestamp: time.Date(2026, time.May, 23, 14, 5, 0, 0, time.UTC),
		Level:     "error",
		StageID:   "brew_bundle",
		Attempt:   2,
		EventType: EventTypeCommandCompleted,
		Command:   `brew bundle install --file "/tmp/Brewfile.generated"`,
		ExitCode:  &exitCode,
		Message:   "failure details",
	})

	want := `2026-05-23T14:05:00Z | ERROR | brew_bundle | attempt=2 | command_completed | brew bundle install --file "/tmp/Brewfile.generated" | exit=23 | failure details`
	if line != want {
		t.Fatalf("formatted line mismatch:\n got: %q\nwant: %q", line, want)
	}
}

func TestEventLoggerKeepsStructuredEventTypeJSONCompatible(t *testing.T) {
	var structured bytes.Buffer
	logger := NewEventLogger(nil, &structured)

	if err := logger.Log(Event{
		Timestamp: time.Date(2026, time.May, 23, 14, 5, 0, 0, time.UTC),
		EventType: EventTypeCommandCompleted,
		Message:   "ok",
	}); err != nil {
		t.Fatalf("log event: %v", err)
	}

	line := structured.String()
	for _, fragment := range []string{
		`"event_type":"command_completed"`,
		`"level":"info"`,
		`"message":"ok"`,
	} {
		if !strings.Contains(line, fragment) {
			t.Fatalf("expected structured log to contain %s, got %q", fragment, line)
		}
	}
}
