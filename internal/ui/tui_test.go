package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dencoseca/laptop-setup/internal/runner"
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

func TestWindowSizeMessageUpdatesViewState(t *testing.T) {
	m := model{
		gitNameInput:  textinput.New(),
		gitEmailInput: textinput.New(),
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 132, Height: 44})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after resize, got %T", next)
	}
	if updated.width != 132 || updated.height != 44 {
		t.Fatalf("unexpected view size: %dx%d", updated.width, updated.height)
	}
	if updated.gitNameInput.Width == 0 || updated.gitEmailInput.Width == 0 {
		t.Fatalf("expected text inputs to be resized, got name=%d email=%d", updated.gitNameInput.Width, updated.gitEmailInput.Width)
	}
}

func TestBrewViewportRange(t *testing.T) {
	start, end := brewViewportRange(30, 1, 10)
	if start != 0 || end != 10 {
		t.Fatalf("expected 0-10 for cursor near start, got %d-%d", start, end)
	}

	start, end = brewViewportRange(30, 29, 10)
	if start != 20 || end != 30 {
		t.Fatalf("expected 20-30 for cursor near end, got %d-%d", start, end)
	}

	start, end = brewViewportRange(30, 15, 10)
	if start != 10 || end != 20 {
		t.Fatalf("expected 10-20 for cursor in middle, got %d-%d", start, end)
	}
}

func TestViewBrewSelectionRendersViewportInsteadOfFullList(t *testing.T) {
	entries := make([]stages.BrewEntry, 0, 30)
	selected := map[string]bool{}
	for index := 0; index < 30; index++ {
		id := fmt.Sprintf("pkg-%02d", index)
		entries = append(entries, stages.BrewEntry{ID: id, Kind: "brew"})
	}
	selected["pkg-15"] = true

	m := model{
		width:        120,
		height:       36,
		cursor:       15,
		brewEntries:  entries,
		brewSelected: selected,
	}

	view := m.viewBrewSelection()
	if !strings.Contains(view, "> [x] pkg-15 (brew)") {
		t.Fatalf("expected current cursor row to be visible, got %q", view)
	}
	if strings.Contains(view, "pkg-00 (brew)") {
		t.Fatalf("expected early rows to be outside viewport, got %q", view)
	}
	if strings.Contains(view, "pkg-29 (brew)") {
		t.Fatalf("expected trailing rows to be outside viewport, got %q", view)
	}
	if strings.Contains(view, "More: ^") {
		t.Fatalf("expected verbose offscreen indicator to be removed, got %q", view)
	}
	if !strings.Contains(view, "Space toggles. Enter continues, b goes back.\n\n  ...\n") {
		t.Fatalf("expected top ellipsis when rows are hidden above, got %q", view)
	}
	if strings.Count(view, "  ...") < 2 {
		t.Fatalf("expected top and bottom ellipsis when viewport is in the middle, got %q", view)
	}
	if strings.Contains(view, "Showing ") || strings.Contains(view, "Selected: ") {
		t.Fatalf("expected summary footer to be removed, got %q", view)
	}
}

func TestViewBrewSelectionHidesEllipsisAtEndOfList(t *testing.T) {
	entries := make([]stages.BrewEntry, 0, 24)
	selected := map[string]bool{}
	for index := 0; index < 24; index++ {
		id := fmt.Sprintf("pkg-%02d", index)
		entries = append(entries, stages.BrewEntry{ID: id, Kind: "brew"})
		selected[id] = true
	}

	m := model{
		width:        120,
		height:       36,
		cursor:       23,
		brewEntries:  entries,
		brewSelected: selected,
	}

	view := m.viewBrewSelection()
	if !strings.Contains(view, "Space toggles. Enter continues, b goes back.\n\n  ...\n") {
		t.Fatalf("expected top ellipsis when rows are hidden above, got %q", view)
	}
	if strings.Contains(view, "pkg-23 (brew)\n  ...\n\n") {
		t.Fatalf("expected no bottom ellipsis when at list end, got %q", view)
	}
	if strings.Contains(view, "Showing ") || strings.Contains(view, "Selected: ") {
		t.Fatalf("expected end-of-list summary footer removed, got %q", view)
	}
}

func TestRenderOutputPanelCapsRenderedHeight(t *testing.T) {
	lines := make([]string, 0, 80)
	for index := 0; index < 80; index++ {
		lines = append(lines, fmt.Sprintf("line-%02d", index))
	}
	content := strings.Join(lines, "\n")

	m := model{}
	rendered := m.renderOutputPanel(56, 25, content)
	if got, want := lipgloss.Height(rendered), 25; got != want {
		t.Fatalf("expected output panel height=%d, got=%d", want, got)
	}
}

func TestViewExecutingRendersDashboardLayout(t *testing.T) {
	m := model{
		screen: screenExecuting,
		width:  120,
		height: 36,
		stageOrder: []string{
			"xcode_clt",
			"brew_bundle",
			"git_config",
		},
		stageMap: map[string]stages.Stage{
			"xcode_clt":   {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			"brew_bundle": {ID: "brew_bundle", Title: "Brew Bundle"},
			"git_config":  {ID: "git_config", Title: "Git Configuration"},
		},
		stageStatuses: map[string]state.StageStatus{
			"xcode_clt":   {Status: string(stages.StatusSuccess)},
			"brew_bundle": {Status: string(stages.StatusRunning)},
			"git_config":  {Status: string(stages.StatusPending)},
		},
		tailedLogs: []tailedLogLine{
			{StageID: "xcode_clt", Line: "completed xcode"},
			{StageID: "brew_bundle", Line: "installing go"},
			{StageID: "brew_bundle", Line: "installing docker"},
		},
	}

	view := m.View()

	for _, fragment := range []string{
		"██████╗  ██████╗",
		"Initiating CHAPEAUX, stand by for awesomeness...",
		"LIVE STATUS",
		"2 of 3",
		"Overall Progress",
		"JOURNEY",
		"STANDARD OUTPUT",
		"Stage brew_bundle",
		"Brew Bundle",
		"installing go",
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected execution view to contain %q, got %q", fragment, view)
		}
	}
	if !strings.Contains(view, "┌") {
		t.Fatalf("expected bordered execution view, got %q", view)
	}
	if strings.Contains(view, "completed xcode") {
		t.Fatalf("expected execution log view to filter to current stage, got %q", view)
	}
}

func TestViewConfigurationUsesDashboardLayoutWithJourneyPreview(t *testing.T) {
	m := model{
		screen: screenGitConfig,
		width:  120,
		height: 36,
		catalog: []stages.Stage{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			{ID: "brew_bundle", Title: "Brew Bundle"},
			{ID: "git_config", Title: "Git Configuration"},
		},
		stageMap: map[string]stages.Stage{
			"xcode_clt":   {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			"brew_bundle": {ID: "brew_bundle", Title: "Brew Bundle"},
			"git_config":  {ID: "git_config", Title: "Git Configuration"},
		},
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools", Selected: true},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries: []stages.BrewEntry{
			{ID: "go", Kind: "brew"},
		},
		brewSelected: map[string]bool{
			"go": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "Use template git config", Description: "Write templates/gitconfig as ~/.gitconfig"},
			{ID: stages.GitConfigModeCustom, Title: "Set custom identity", Description: "Write template config and override user.name/user.email"},
		},
	}

	view := m.View()

	for _, fragment := range []string{
		"██████╗  ██████╗",
		"Initiating CHAPEAUX, stand by for awesomeness...",
		"CONFIGURATION",
		"JOURNEY",
		"STANDARD OUTPUT",
		"Dev Tools: Git Configuration",
		"Choose how git config should be handled.",
		"Xcode Command Line Tools",
		"Brew Bundle",
		"Git Configuration",
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected configuration view to contain %q, got %q", fragment, view)
		}
	}
}

func TestViewConfigurationMatchesWindowDimensions(t *testing.T) {
	m := model{
		screen: screenGitConfig,
		width:  117,
		height: 41,
		catalog: []stages.Stage{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			{ID: "brew_bundle", Title: "Brew Bundle"},
			{ID: "git_config", Title: "Git Configuration"},
		},
		stageMap: map[string]stages.Stage{
			"xcode_clt":   {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			"brew_bundle": {ID: "brew_bundle", Title: "Brew Bundle"},
			"git_config":  {ID: "git_config", Title: "Git Configuration"},
		},
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools", Selected: true},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries: []stages.BrewEntry{
			{ID: "go", Kind: "brew"},
		},
		brewSelected: map[string]bool{
			"go": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "Use template git config", Description: "Write templates/gitconfig as ~/.gitconfig"},
			{ID: stages.GitConfigModeCustom, Title: "Set custom identity", Description: "Write template config and override user.name/user.email"},
		},
	}

	view := m.View()
	if got, want := lipgloss.Width(view), 117; got != want {
		t.Fatalf("expected view width=%d, got=%d", want, got)
	}
	if got, want := lipgloss.Height(view), 41; got != want {
		t.Fatalf("expected view height=%d, got=%d", want, got)
	}
}

type noOpRunner struct{}

func (r *noOpRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	return runner.Result{}, nil
}

func sendEnter(t *testing.T, m model) model {
	t.Helper()
	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	return updated
}

func TestPromptFlowReachesReviewScreen(t *testing.T) {
	m := model{
		screen: screenWelcome,
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode", Selected: true},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries: []stages.BrewEntry{{ID: "go", Kind: "brew"}},
		brewSelected: map[string]bool{
			"go": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
			{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
		},
		manualOptions: []toggleOption{
			{ID: "manual_app_store_apps", Title: "Manual", Selected: true},
		},
	}

	m = sendEnter(t, m)
	if m.screen != screenMacOS {
		t.Fatalf("expected macOS screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenInstall {
		t.Fatalf("expected install screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenBrew {
		t.Fatalf("expected brew screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenDevTools {
		t.Fatalf("expected dev tools screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenNodeToolchain {
		t.Fatalf("expected node screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenDockerRuntime {
		t.Fatalf("expected docker screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenShellOptions {
		t.Fatalf("expected shell options screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenGitConfig {
		t.Fatalf("expected git config screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenManual {
		t.Fatalf("expected manual screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenReview {
		t.Fatalf("expected review screen, got %v", m.screen)
	}
}

func TestReviewEnterBlocksExecutionWhenPlanInvalid(t *testing.T) {
	m := model{
		ctx:    context.Background(),
		screen: screenReview,
		catalog: []stages.Stage{
			{ID: "brew_bundle", Title: "Brew Bundle"},
		},
		stageMap: map[string]stages.Stage{
			"brew_bundle": {ID: "brew_bundle", Title: "Brew Bundle"},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries:   []stages.BrewEntry{},
		brewSelected:  map[string]bool{},
		nodeOptions:   []selectOption{{ID: stages.NodeToolchainVitePlus, Title: "vite+"}},
		dockerOptions: []selectOption{{ID: stages.DockerRuntimeColima, Title: "colima"}},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
		},
		stageStatuses: make(map[string]state.StageStatus),
	}

	m = sendEnter(t, m)

	if m.screen != screenReview {
		t.Fatalf("expected to stay on review screen, got %v", m.screen)
	}
	if m.executing {
		t.Fatalf("expected execution not to start")
	}
	if !strings.Contains(m.planError, "brew_bundle selected with no Brew entries") {
		t.Fatalf("expected plan validation error, got %q", m.planError)
	}
}

func TestReviewEnterConfirmsPlanAndStartsExecution(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	catalog := []stages.Stage{
		{
			ID:      "xcode_clt",
			Title:   "Xcode",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{Satisfied: true, Message: "already done"}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return nil },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
		{
			ID:      "brew_bundle",
			Title:   "Brew Bundle",
			CanSkip: true,
			Precheck: func(context.Context, stages.ExecutionContext) (stages.CheckResult, error) {
				return stages.CheckResult{Satisfied: true, Message: "already done"}, nil
			},
			Run:      func(context.Context, stages.ExecutionContext) error { return nil },
			Simulate: func(context.Context, stages.ExecutionContext) error { return nil },
		},
	}
	stageMap := map[string]stages.Stage{}
	for _, stage := range catalog {
		stageMap[stage.ID] = stage
	}

	m := model{
		ctx:      context.Background(),
		screen:   screenReview,
		store:    store,
		catalog:  catalog,
		stageMap: stageMap,
		runner:   &noOpRunner{},
		repoRoot: t.TempDir(),
		homeDir:  homeDir,
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode", Selected: true},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries: []stages.BrewEntry{{ID: "go", Kind: "brew"}},
		brewSelected: map[string]bool{
			"go": true,
		},
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
		},
		stageStatuses: make(map[string]state.StageStatus),
	}

	next, cmd := m.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}

	if updated.screen != screenExecuting {
		t.Fatalf("expected executing screen, got %v", updated.screen)
	}
	if !updated.executing {
		t.Fatalf("expected executing=true")
	}
	if updated.runState == nil {
		t.Fatal("expected run state to be initialized")
	}
	if len(updated.stageOrder) == 0 {
		t.Fatal("expected stage order to be set from plan")
	}
	if cmd == nil {
		t.Fatal("expected execution command to be created")
	}
	startup := cmd()
	batch, ok := startup.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected batch startup command, got %#v", startup)
	}
	if len(batch) == 0 {
		t.Fatal("expected non-empty startup command batch")
	}
	if msg := batch[0](); msg != nil {
		t.Fatalf("expected worker start command to return nil, got %#v", msg)
	}

	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-updated.updates:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("execution worker did not finish in time")
		}
	}
}
