package ui

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func TestPrepareExecutionSetupPersistsPhaseDecisions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	catalog := []stages.Stage{
		{ID: "vite_plus_install"},
		{ID: "docker_config"},
		{ID: "shell_setup"},
		{ID: "git_config"},
	}
	m := model{
		ctx:     context.Background(),
		store:   store,
		catalog: catalog,
		config:  Config{},
		devOptions: []toggleOption{
			{ID: "vite_plus_install", Title: "Vite+ Install", Selected: true},
			{ID: "docker_config", Title: "Docker Configuration", Selected: true},
			{ID: "shell_setup", Title: "Shell Setup", Selected: true},
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
			{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: false},
			{ID: stages.DecisionShellApplyZshrc, Title: "Write zshrc", Selected: true},
			{ID: stages.DecisionShellApplyStarship, Title: "Write starship", Selected: false},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
			{ID: stages.GitConfigModeExisting, Title: "existing"},
			{ID: stages.GitConfigModeCustom, Title: "custom"},
		},
		nodeSelection:    1,
		dockerSelection:  0,
		gitModeSelection: 2,
	}
	m.gitNameInput = textinput.New()
	m.gitEmailInput = textinput.New()
	m.gitNameInput.SetValue("Alice")
	m.gitEmailInput.SetValue("alice@example.com")

	setup, err := m.prepareExecutionSetup()
	if err != nil {
		t.Fatalf("prepareExecutionSetup returned error: %v", err)
	}
	defer setup.humanLog.Close()
	defer setup.eventsLog.Close()

	decisions := setup.runState.Decisions
	if got := stages.NodeToolchainFromDecisions(decisions); got != stages.NodeToolchainNvmPnpm {
		t.Fatalf("node toolchain mismatch: got=%s", got)
	}
	if got := stages.DockerRuntimeFromDecisions(decisions); got != stages.DockerRuntimeColima {
		t.Fatalf("docker runtime mismatch: got=%s", got)
	}
	if stages.ShellInstallOhMyZsh(decisions) {
		t.Fatalf("expected oh-my-zsh decision=false")
	}
	if !stages.ShellApplyZshrcTemplate(decisions) {
		t.Fatalf("expected zshrc decision=true")
	}
	if stages.ShellApplyStarshipTemplate(decisions) {
		t.Fatalf("expected starship decision=false")
	}
	if got := stages.GitConfigModeFromDecisions(decisions); got != stages.GitConfigModeCustom {
		t.Fatalf("git mode mismatch: got=%s", got)
	}
	name, email := stages.GitIdentityFromDecisions(decisions)
	if name != "Alice" || email != "alice@example.com" {
		t.Fatalf("git identity mismatch: got=%q <%s>", name, email)
	}
}

func TestParseGitIdentity(t *testing.T) {
	name, email := parseGitIdentity(`
[core]
  autocrlf = input
[user]
  name = Ada Lovelace
  email = ada@example.com
`)
	if name != "Ada Lovelace" {
		t.Fatalf("name mismatch: %q", name)
	}
	if email != "ada@example.com" {
		t.Fatalf("email mismatch: %q", email)
	}
}

func TestParseRunLogLineExtractsStageID(t *testing.T) {
	line := "2026-05-23T12:30:00Z | INFO | brew_bundle | stage_started | Brew Bundle"
	parsed := parseRunLogLine(line)
	if parsed.StageID != "brew_bundle" {
		t.Fatalf("expected stage id brew_bundle, got %q", parsed.StageID)
	}
	if parsed.Line != line {
		t.Fatalf("line mismatch: %q", parsed.Line)
	}
}

func TestParseRunLogLineWithoutStageID(t *testing.T) {
	line := "2026-05-23T12:30:00Z | INFO | run_started | Starting stage execution"
	parsed := parseRunLogLine(line)
	if parsed.StageID != "" {
		t.Fatalf("expected empty stage id, got %q", parsed.StageID)
	}
	if parsed.Line != line {
		t.Fatalf("line mismatch: %q", parsed.Line)
	}
}

func TestCurrentLogStageIDPrefersRunningStage(t *testing.T) {
	stageOrder := []string{"xcode_clt", "brew_bundle", "git_config"}
	statuses := map[string]state.StageStatus{
		"xcode_clt":   {Status: string(stages.StatusSuccess)},
		"brew_bundle": {Status: string(stages.StatusRunning)},
		"git_config":  {Status: string(stages.StatusPending)},
	}

	if got := currentLogStageID(stageOrder, statuses); got != "brew_bundle" {
		t.Fatalf("current stage mismatch: got=%q", got)
	}
}

func TestFilteredLogLinesByStage(t *testing.T) {
	lines := []tailedLogLine{
		{StageID: "xcode_clt", Line: "a"},
		{StageID: "brew_bundle", Line: "b"},
		{StageID: "brew_bundle", Line: "c"},
		{StageID: "git_config", Line: "d"},
	}

	got := filteredLogLines(lines, "brew_bundle", 10)
	want := []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered lines mismatch: got=%v want=%v", got, want)
	}
}

func TestConsumeLogTailChunkHandlesPartialLines(t *testing.T) {
	var m model

	m.consumeLogTailChunk("2026-05-23T12:30:00Z | INFO | xcode_clt | stage_started | Xcode")
	if m.logTailCarry == "" {
		t.Fatalf("expected partial line to be buffered")
	}
	if len(m.tailedLogs) != 0 {
		t.Fatalf("expected no completed lines yet")
	}

	m.consumeLogTailChunk(" CLT\n2026-05-23T12:30:05Z | INFO | xcode_clt | stage_completed | success\n")
	if m.logTailCarry != "" {
		t.Fatalf("expected buffered fragment to be flushed")
	}
	if len(m.tailedLogs) != 2 {
		t.Fatalf("expected two log lines, got %d", len(m.tailedLogs))
	}
	if m.tailedLogs[0].StageID != "xcode_clt" || m.tailedLogs[1].StageID != "xcode_clt" {
		t.Fatalf("unexpected stage ids: %+v", m.tailedLogs)
	}
}

func TestViewSummaryIncludesManualAppStoreReminders(t *testing.T) {
	m := model{
		stageStatuses: map[string]state.StageStatus{
			"xcode_clt":             {Status: string(stages.StatusSuccess)},
			"manual_app_store_apps": {Status: string(stages.StatusSkipped)},
		},
		humanLogPath:  "/tmp/run.log",
		eventsLogPath: "/tmp/events.jsonl",
	}

	summary := m.viewSummary()
	if !strings.Contains(summary, "Stage counts: completed=1 skipped=1 failed=0") {
		t.Fatalf("expected stage counts in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Run log: /tmp/run.log") {
		t.Fatalf("expected run log path in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Events log: /tmp/events.jsonl") {
		t.Fatalf("expected events log path in summary, got %q", summary)
	}
	if !strings.Contains(summary, "Manual App Store reminders:") {
		t.Fatalf("expected manual app reminder section in summary, got %q", summary)
	}
	for _, item := range stages.ManualAppStoreApps() {
		if !strings.Contains(summary, "- "+item) {
			t.Fatalf("expected manual app %q in summary, got %q", item, summary)
		}
	}
}
