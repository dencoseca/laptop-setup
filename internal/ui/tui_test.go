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
	"github.com/charmbracelet/x/ansi"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func TestPrepareExecutionSetupPersistsPhaseDecisions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	catalog := []stages.Stage{
		{ID: "node_toolchain"},
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
			{ID: "node_toolchain", Title: "Node Toolchain", Selected: true},
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
	if !strings.Contains(view, fmt.Sprintf("> %s pkg-15 (brew)", selectionMarker(true))) {
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
	if !strings.Contains(view, "Space toggles. Enter continues, Esc goes back.\n\n  ...\n") {
		t.Fatalf("expected top ellipsis when rows are hidden above, got %q", view)
	}
	if strings.Count(view, "  ...") < 2 {
		t.Fatalf("expected top and bottom ellipsis when viewport is in the middle, got %q", view)
	}
	if strings.Contains(view, "Showing ") || strings.Contains(view, "Selected: ") {
		t.Fatalf("expected summary footer to be removed, got %q", view)
	}
}

func TestViewSelectOptionsUsesRadioMarkersAndOmitsDescriptions(t *testing.T) {
	m := model{cursor: 0}
	view := m.viewSelectOptions("Dev Tools: Node Toolchain", []selectOption{
		{ID: stages.NodeToolchainVitePlus, Title: "vite+", Description: "Install Vite+ toolchain via official installer"},
		{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm", Description: "Install nvm and pnpm using official install scripts"},
	}, 0)

	for _, fragment := range []string{
		fmt.Sprintf("> %s vite+", radioMarker(true)),
		fmt.Sprintf("  %s pnpm + nvm", radioMarker(false)),
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected select view to contain %q, got %q", fragment, view)
		}
	}
	for _, fragment := range []string{
		"Install Vite+ toolchain via official installer",
		"Install nvm and pnpm using official install scripts",
		"✓",
	} {
		if strings.Contains(view, fragment) {
			t.Fatalf("expected select view to omit %q, got %q", fragment, view)
		}
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
	if !strings.Contains(view, "Space toggles. Enter continues, Esc goes back.\n\n  ...\n") {
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
			{StageID: "brew_bundle", Line: "2026-05-23T12:30:00Z | INFO | brew_bundle | stage_started | Brew Bundle"},
			{StageID: "brew_bundle", Line: "installing docker"},
		},
	}

	view := m.View()

	for _, fragment := range []string{
		"██████╗  ██████╗",
		"Initiating CHAPEAUX, stand by for awesomeness...",
		"2 of 3",
		"Overall Progress",
		"Stage: Brew Bundle",
		"Brew Bundle",
		"installing docker",
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected execution view to contain %q, got %q", fragment, view)
		}
	}
	for _, fragment := range []string{"LIVE STATUS", "RUN STATUS", "JOURNEY", "STANDARD OUTPUT"} {
		if strings.Contains(view, fragment) {
			t.Fatalf("expected execution view to omit panel title %q, got %q", fragment, view)
		}
	}
	if strings.Contains(view, "brew_bundle") {
		t.Fatalf("expected execution view to hide internal stage id, got %q", view)
	}
	if !strings.Contains(view, "┌") {
		t.Fatalf("expected bordered execution view, got %q", view)
	}
	if strings.Contains(view, "completed xcode") {
		t.Fatalf("expected execution log view to filter to current stage, got %q", view)
	}
}

func TestDashboardBodyWidthsUseFortySixtySplit(t *testing.T) {
	journeyWidth, outputWidth := dashboardBodyWidths(116, 2)

	if got, want := journeyWidth, 45; got != want {
		t.Fatalf("expected journey width=%d, got=%d", want, got)
	}
	if got, want := outputWidth, 69; got != want {
		t.Fatalf("expected output width=%d, got=%d", want, got)
	}
	if got, want := journeyWidth+2+outputWidth, 116; got != want {
		t.Fatalf("expected widths plus gap=%d, got=%d", want, got)
	}
}

func TestRenderJourneyLineLeftAlignsNameAndRightAlignsStatus(t *testing.T) {
	m := model{
		stageMap: map[string]stages.Stage{
			"xcode_clt": {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
		},
	}

	line := ansi.Strip(m.renderJourneyLine(36, "xcode_clt", "xcode_clt", string(stages.StatusRunning)))

	if !strings.HasPrefix(line, "> Xcode Command Line Tools") {
		t.Fatalf("expected stage title to be left-aligned without a number, got %q", line)
	}
	if strings.Contains(line, "01") {
		t.Fatalf("expected journey line to omit sequence number, got %q", line)
	}
	if !strings.HasSuffix(line, "live") {
		t.Fatalf("expected status to be right-aligned at row end, got %q", line)
	}
	if got, want := lipgloss.Width(line), 36; got != want {
		t.Fatalf("expected rendered line width=%d, got=%d line=%q", want, got, line)
	}
}

func TestRenderTitlePanelUsesCompactFallback(t *testing.T) {
	var m model
	width := maxInt(
		titleArtWidth(bootstrapCompactTitleArtLines),
		lipgloss.Width("Initiating CHAPEAUX, stand by for awesomeness..."),
	) + 6

	view := m.renderTitlePanel(width, len(bootstrapCompactTitleArtLines)+6)

	if !strings.Contains(view, "▗▄▄▖  ▗▄▖") {
		t.Fatalf("expected compact fallback title art, got %q", view)
	}
	if strings.Contains(view, "██████╗") {
		t.Fatalf("expected compact fallback instead of large title art, got %q", view)
	}
	if !strings.Contains(view, "Initiating CHAPEAUX, stand by for awesomeness...") {
		t.Fatalf("expected tagline in compact fallback, got %q", view)
	}
}

func TestRenderDashboardStatusPanelLeftAlignsBadgeWithContent(t *testing.T) {
	var m model
	view := ansi.Strip(m.renderDashboardStatusPanel(34, 13, dashboardStatus{
		Badge:       "Configuring",
		BadgeTone:   accentAltColor,
		Heading:     "Brew Catalog Selection",
		Summary:     "4 of 13  9 stages selected",
		ProgressPct: 30,
		Hint:        "Enter continue  Esc back",
	}))
	lines := strings.Split(view, "\n")

	badgeLineIndex := -1
	headingLineIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "CONFIGURING") {
			badgeLineIndex = index
		}
		if strings.Contains(line, "Brew Catalog Selection") {
			headingLineIndex = index
		}
	}

	if badgeLineIndex == -1 || headingLineIndex == -1 {
		t.Fatalf("expected badge and heading in status panel, got %q", view)
	}
	if got, want := strings.Index(lines[badgeLineIndex], "CONFIGURING"), strings.Index(lines[headingLineIndex], "Brew Catalog Selection"); got != want {
		t.Fatalf("expected badge and heading to start in same column, got badge=%d heading=%d view=%q", got, want, view)
	}
	if headingLineIndex != badgeLineIndex+2 {
		t.Fatalf("expected one spacer line between badge and heading, got view=%q", view)
	}
	if strings.Trim(lines[badgeLineIndex+1], " │") != "" {
		t.Fatalf("expected spacer line between badge and heading, got %q", lines[badgeLineIndex+1])
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
	for _, fragment := range []string{"CONFIGURATION", "JOURNEY", "STANDARD OUTPUT"} {
		if strings.Contains(view, fragment) {
			t.Fatalf("expected configuration view to omit panel title %q, got %q", fragment, view)
		}
	}
}

func TestViewPhaseConfigurationOmitsPhasePrefix(t *testing.T) {
	m := model{
		screen: screenDevTools,
		width:  120,
		height: 36,
		catalog: []stages.Stage{
			{ID: "node_toolchain", Title: "Node Toolchain"},
		},
		stageMap: map[string]stages.Stage{
			"node_toolchain": {ID: "node_toolchain", Title: "Node Toolchain"},
		},
		devOptions: []toggleOption{
			{ID: "node_toolchain", Title: "Node Toolchain", Selected: true},
		},
	}

	view := m.View()
	if strings.Contains(view, "Phase:") {
		t.Fatalf("expected phase prefix to be omitted, got %q", view)
	}
	if got := strings.Count(view, "Dev Tools Setup"); got < 2 {
		t.Fatalf("expected status and output panels to show Dev Tools Setup, count=%d view=%q", got, view)
	}
	if strings.Contains(view, "node_toolchain") {
		t.Fatalf("expected internal stage id to be hidden, got %q", view)
	}
}

func TestViewReviewHidesInternalStageIDs(t *testing.T) {
	m := model{
		screen: screenReview,
		width:  120,
		height: 36,
		catalog: []stages.Stage{
			{ID: "node_toolchain", Title: "Node Toolchain"},
			{ID: "docker_config", Title: "Docker Configuration"},
		},
		stageMap: map[string]stages.Stage{
			"node_toolchain": {ID: "node_toolchain", Title: "Node Toolchain"},
			"docker_config":  {ID: "docker_config", Title: "Docker Configuration"},
		},
		devOptions: []toggleOption{
			{ID: "node_toolchain", Title: "Node Toolchain", Selected: true},
			{ID: "docker_config", Title: "Docker Configuration", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "template"},
		},
	}

	view := m.View()
	for _, fragment := range []string{"Node Toolchain", "Docker Configuration"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected review to contain %q, got %q", fragment, view)
		}
	}
	for _, internalID := range []string{"node_toolchain", "docker_config"} {
		if strings.Contains(view, internalID) {
			t.Fatalf("expected review to hide internal stage id %q, got %q", internalID, view)
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

func TestGitIdentityInputsAcceptAlphanumericCharacters(t *testing.T) {
	t.Run("name", func(t *testing.T) {
		input := textinput.New()
		input.Focus()

		m := model{
			screen:       screenGitName,
			gitNameInput: input,
		}

		next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		updated, ok := next.(model)
		if !ok {
			t.Fatalf("expected model type after update, got %T", next)
		}

		if updated.screen != screenGitName {
			t.Fatalf("expected to stay on git name input, got %v", updated.screen)
		}
		if got := updated.gitNameInput.Value(); got != "b" {
			t.Fatalf("expected typed character to be inserted, got %q", got)
		}
	})

	t.Run("email", func(t *testing.T) {
		input := textinput.New()
		input.Focus()

		m := model{
			screen:        screenGitEmail,
			gitEmailInput: input,
		}

		next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
		updated, ok := next.(model)
		if !ok {
			t.Fatalf("expected model type after update, got %T", next)
		}

		if updated.screen != screenGitEmail {
			t.Fatalf("expected to stay on git email input, got %v", updated.screen)
		}
		if got := updated.gitEmailInput.Value(); got != "b" {
			t.Fatalf("expected typed character to be inserted, got %q", got)
		}
	})
}

func TestCtrlCRequiresConfirmationBeforeQuit(t *testing.T) {
	m := model{screen: screenGitName}

	next, cmd := m.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	if cmd != nil {
		t.Fatal("expected first CTRL+C to show confirmation without quitting")
	}
	if updated.screen != screenQuitConfirm {
		t.Fatalf("expected quit confirmation screen, got %v", updated.screen)
	}
	if !strings.Contains(updated.View(), "Press `CTRL + C` again to quit.") {
		t.Fatalf("expected quit confirmation message, got %q", updated.View())
	}

	next, cmd = updated.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok = next.(model); !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	if cmd == nil {
		t.Fatal("expected second CTRL+C to quit")
	}
	if _, ok = cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected quit command from second CTRL+C")
	}
}

func TestEscapeReturnsFromQuitConfirmation(t *testing.T) {
	m := model{screen: screenGitName}

	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	confirm, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}

	next, _ = confirm.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	if updated.screen != screenGitName {
		t.Fatalf("expected Esc to return to previous screen, got %v", updated.screen)
	}
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
	if !strings.Contains(m.planError, "Brew Bundle selected with no Brew entries") {
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
