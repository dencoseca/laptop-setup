package runner

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	RunID     string    `json:"run_id,omitempty"`
	StageID   string    `json:"stage_id,omitempty"`
	Attempt   int       `json:"attempt,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	EventType string    `json:"event_type"`
	Command   string    `json:"command,omitempty"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type EventLogger struct {
	human      io.Writer
	structured io.Writer
	mu         sync.Mutex
}

func NewEventLogger(human io.Writer, structured io.Writer) *EventLogger {
	return &EventLogger{
		human:      human,
		structured: structured,
	}
}

func (l *EventLogger) Log(event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "info"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.human != nil {
		if _, err := fmt.Fprintln(l.human, formatHuman(event)); err != nil {
			return fmt.Errorf("write human log: %w", err)
		}
	}

	if l.structured != nil {
		record, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		if _, err = fmt.Fprintf(l.structured, "%s\n", record); err != nil {
			return fmt.Errorf("write event log: %w", err)
		}
	}

	return nil
}

func formatHuman(event Event) string {
	parts := []string{
		event.Timestamp.Format(time.RFC3339),
		strings.ToUpper(event.Level),
	}
	if event.StageID != "" {
		parts = append(parts, event.StageID)
	}
	if event.Attempt > 0 {
		parts = append(parts, fmt.Sprintf("attempt=%d", event.Attempt))
	}
	if event.EventType != "" {
		parts = append(parts, event.EventType)
	}
	if event.Command != "" {
		parts = append(parts, event.Command)
	}
	if event.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit=%d", *event.ExitCode))
	}
	if event.Message != "" {
		parts = append(parts, event.Message)
	}
	return strings.Join(parts, " | ")
}
