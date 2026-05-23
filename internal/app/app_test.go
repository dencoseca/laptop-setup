package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseConfigAllowsMissingEnvironmentForInteractive(t *testing.T) {
	cfg, err := parseConfig([]string{"--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.environment != "" {
		t.Fatalf("expected empty environment, got %q", cfg.environment)
	}
}

func TestParseConfigResumeAllowsMissingEnvironment(t *testing.T) {
	cfg, err := parseConfig([]string{"--resume", "--state-file", "/tmp/state.json"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.resume {
		t.Fatal("expected resume=true")
	}
}

func TestParseConfigParsesSelectionFlags(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--environment", "work",
		"--from", "brew_bundle",
		"--only", "homebrew_install,brew_bundle",
		"--skip", "brew_bundle",
		"--state-file", "/tmp/state.json",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.environment != "work" {
		t.Fatalf("environment mismatch: %q", cfg.environment)
	}
	if cfg.from != "brew_bundle" {
		t.Fatalf("from mismatch: %q", cfg.from)
	}
	if len(cfg.only) != 2 || cfg.only[0] != "homebrew_install" || cfg.only[1] != "brew_bundle" {
		t.Fatalf("unexpected only list: %v", cfg.only)
	}
	if len(cfg.skip) != 1 || cfg.skip[0] != "brew_bundle" {
		t.Fatalf("unexpected skip list: %v", cfg.skip)
	}
}

func TestParseConfigRejectsUnexpectedPositionalArgs(t *testing.T) {
	_, err := parseConfig([]string{"--state-file", "/tmp/state.json", "extra"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected positional argument parsing error")
	}
	if !strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseCSVDeduplicatesAndTrims(t *testing.T) {
	got := parseCSV("a, b,a, ,c")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got=%v want=%v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("value mismatch at index %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}
