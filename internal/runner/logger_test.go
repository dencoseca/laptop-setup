package runner

import (
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
		EventType: "command_completed",
		Command:   `brew bundle install --file "/tmp/Brewfile.generated"`,
		ExitCode:  &exitCode,
		Message:   "failure details",
	})

	want := `2026-05-23T14:05:00Z | ERROR | brew_bundle | attempt=2 | command_completed | brew bundle install --file "/tmp/Brewfile.generated" | exit=23 | failure details`
	if line != want {
		t.Fatalf("formatted line mismatch:\n got: %q\nwant: %q", line, want)
	}
}
