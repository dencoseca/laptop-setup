package ui

import (
	"context"
	"fmt"
	"io"
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

func TestReviewEnterPassesPhaseDecisionsToExecutionService(t *testing.T) {
	catalog := []stages.Stage{
		{ID: "node_toolchain"},
		{ID: "docker_config"},
		{ID: "shell_setup"},
		{ID: "git_config"},
	}
	service := &recordingExecutionService{}
	m := model{
		ctx:              context.Background(),
		screen:           screenReview,
		catalog:          catalog,
		config:           Config{},
		stageStatuses:    make(map[string]state.StageStatus),
		executionService: service,
		devOptions: []toggleOption{
			{ID: "node_toolchain", Title: "Node Toolchain", Selected: true},
			{ID: "docker_config", Title: "Docker Configuration", Selected: true},
			{ID: "shell_setup", Title: "Shell Setup", Selected: true},
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
			{ID: string(stages.NodeToolchainNvmPnpm), Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: string(stages.DockerRuntimeColima), Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: false},
			{ID: stages.DecisionShellApplyZshrc, Title: "Write zshrc", Selected: true},
			{ID: stages.DecisionShellApplyStarship, Title: "Write starship", Selected: false},
		},
		nodeSelection:   1,
		dockerSelection: 0,
	}
	m.gitNameInput = textinput.New()
	m.gitEmailInput = textinput.New()
	m.gitNameInput.SetValue("Alice")
	m.gitEmailInput.SetValue("alice@example.com")

	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := next.(model); !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}

	decisions := service.request.Decisions
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
	if got := stages.GitConfigModeFromDecisions(decisions); got != stages.GitConfigModeTemplate {
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

func TestDetectAlreadyDoneStagesUsesDefaultDecisions(t *testing.T) {
	catalog := []stages.Stage{
		{
			ID: "docker_config",
			Precheck: func(_ context.Context, execCtx stages.ExecutionContext) (stages.CheckResult, error) {
				if got := stages.DockerRuntimeFromDecisions(execCtx.Decisions); got != stages.DockerRuntimeColima {
					t.Fatalf("expected default docker runtime decision, got %q", got)
				}
				return stages.CheckResult{Satisfied: true}, nil
			},
		},
	}

	statuses := detectAlreadyDoneStages(context.Background(), catalog, &noOpRunner{}, nil, "/repo", "/home")
	if got := statuses["docker_config"].Status; got != stages.StatusAlreadyDone {
		t.Fatalf("expected docker_config to be marked already done, got %q", got)
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
		"xcode_clt":   {Status: stages.StatusSuccess},
		"brew_bundle": {Status: stages.StatusRunning},
		"git_config":  {Status: stages.StatusPending},
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
			"xcode_clt":             {Status: stages.StatusSuccess},
			"manual_app_store_apps": {Status: stages.StatusSkipped},
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
		kind := "brew"
		if index%2 == 0 {
			kind = "cask"
		}
		entries = append(entries, stages.BrewEntry{ID: id, Kind: kind})
	}
	selected["pkg-15"] = true

	m := model{
		width:        120,
		height:       36,
		cursor:       15,
		brewEntries:  entries,
		brewSelected: selected,
	}

	view := ansi.Strip(m.viewBrewSelection())
	if !strings.Contains(view, "│ ● pkg-15") {
		t.Fatalf("expected current cursor row to be visible, got %q", view)
	}
	if strings.Contains(view, "Packages and apps") || strings.Contains(view, "30 items") {
		t.Fatalf("expected list title and item count to be hidden, got %q", view)
	}
	if strings.Contains(view, "brew") || strings.Contains(view, "cask") {
		t.Fatalf("expected package manager implementation details to be hidden, got %q", view)
	}
	if strings.Contains(view, "pkg-00") {
		t.Fatalf("expected early rows to be outside viewport, got %q", view)
	}
	if strings.Contains(view, "pkg-29") {
		t.Fatalf("expected trailing rows to be outside viewport, got %q", view)
	}
	if strings.Contains(view, "Showing ") || strings.Contains(view, "Selected: ") {
		t.Fatalf("expected summary footer to be removed, got %q", view)
	}
}

func TestViewSelectOptionsUsesRadioMarkersAndOmitsDescriptions(t *testing.T) {
	m := model{cursor: 0}
	view := m.viewSelectOptions("Dev Tools: Node Toolchain", []selectOption{
		{ID: string(stages.NodeToolchainVitePlus), Title: "vite+", Description: "Install Vite+ toolchain via official installer"},
		{ID: string(stages.NodeToolchainNvmPnpm), Title: "pnpm + nvm", Description: "Install nvm and pnpm using official install scripts"},
	}, 0)

	for _, fragment := range []string{
		fmt.Sprintf("│ %s vite+", radioMarker(true)),
		fmt.Sprintf("  %s pnpm + nvm", radioMarker(false)),
	} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("expected select view to contain %q, got %q", fragment, view)
		}
	}
	for _, fragment := range []string{
		"Install Vite+ toolchain via official installer",
		"Install nvm and pnpm using official install scripts",
		"Use Up/Down to choose.",
		"Filter with /.",
		"Enter to continue",
		"Esc to go back",
		"✓",
	} {
		if strings.Contains(view, fragment) {
			t.Fatalf("expected select view to omit %q, got %q", fragment, view)
		}
	}
}

func TestSelectionMarkerUsesCircleInsteadOfCompletionTick(t *testing.T) {
	if got, want := ansi.Strip(selectionMarker(true)), "●"; got != want {
		t.Fatalf("expected selected marker %q, got %q", want, got)
	}
	if got, want := ansi.Strip(selectionMarker(false)), "○"; got != want {
		t.Fatalf("expected unselected marker %q, got %q", want, got)
	}
	if strings.Contains(ansi.Strip(selectionMarker(true)), "✓") {
		t.Fatalf("expected selected marker not to use completion tick")
	}
}

func TestViewToggleOptionsUsesCompletionTickForAlreadyDoneStage(t *testing.T) {
	m := model{
		cursor: 0,
		stageStatuses: map[string]state.StageStatus{
			"xcode_clt": {Status: stages.StatusAlreadyDone},
		},
	}

	view := ansi.Strip(m.viewToggleOptions("MacOS Setup", []toggleOption{
		{ID: "xcode_clt", Title: "Xcode Command Line Tools", Selected: true},
		{ID: "macos_defaults", Title: "macOS Defaults", Selected: true},
	}))

	if !strings.Contains(view, "│ ✓ Xcode Command Line Tools") {
		t.Fatalf("expected already-installed xcode row to use completion tick, got %q", view)
	}
	if !strings.Contains(view, "  ● macOS Defaults") {
		t.Fatalf("expected selected unfinished row to use selected circle, got %q", view)
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

	view := ansi.Strip(m.viewBrewSelection())
	if !strings.Contains(view, "│ ● pkg-23") {
		t.Fatalf("expected last row to be visible and selected, got %q", view)
	}
	if strings.Contains(view, "...") {
		t.Fatalf("expected Bubbles pagination instead of manual ellipsis rows, got %q", view)
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
			"xcode_clt":   {Status: stages.StatusSuccess},
			"brew_bundle": {Status: stages.StatusRunning},
			"git_config":  {Status: stages.StatusPending},
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
		"Plan",
		"Apply",
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
	if strings.Contains(view, "2 of 3") || strings.Contains(view, "finished") {
		t.Fatalf("expected execution view to omit status summary counts, got %q", view)
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

func TestRenderJourneyLineUsesCompletionTickForFinishedStages(t *testing.T) {
	m := model{
		stageMap: map[string]stages.Stage{
			"xcode_clt": {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
		},
	}

	successLine := ansi.Strip(m.renderJourneyLine(36, "xcode_clt", "brew_bundle", string(stages.StatusSuccess)))
	if !strings.HasPrefix(successLine, "✓ Xcode Command Line Tools") {
		t.Fatalf("expected completed stage to use tick prefix, got %q", successLine)
	}

	alreadyDoneLine := ansi.Strip(m.renderJourneyLine(36, "xcode_clt", "xcode_clt", string(stages.StatusAlreadyDone)))
	if !strings.HasPrefix(alreadyDoneLine, "✓ Xcode Command Line Tools") {
		t.Fatalf("expected already-done current stage to keep tick prefix, got %q", alreadyDoneLine)
	}
}

func TestPreviewJourneyCarriesPrecheckStatuses(t *testing.T) {
	m := model{
		catalog: []stages.Stage{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			{ID: "macos_defaults", Title: "macOS Defaults"},
		},
		stageMap: map[string]stages.Stage{
			"xcode_clt":      {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			"macos_defaults": {ID: "macos_defaults", Title: "macOS Defaults"},
		},
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode Command Line Tools", Selected: true},
			{ID: "macos_defaults", Title: "macOS Defaults", Selected: true},
		},
		stageStatuses: map[string]state.StageStatus{
			"xcode_clt": {Status: stages.StatusAlreadyDone},
		},
	}

	journey := m.previewJourney()

	if got := journey.Statuses["xcode_clt"].Status; got != stages.StatusAlreadyDone {
		t.Fatalf("expected preview journey to carry xcode already-done status, got %q", got)
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
		Badge:                    "Configuring",
		BadgeTone:                accentAltColor,
		Heading:                  "Package & App Selection",
		Summary:                  "4 of 13",
		ConfigurationProgressPct: 30,
		ExecutionProgressPct:     0,
		Hint:                     "Enter continue  Esc back",
	}))
	lines := strings.Split(view, "\n")

	badgeLineIndex := -1
	headingLineIndex := -1
	configLineIndex := -1
	executionLineIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "CONFIGURING") {
			badgeLineIndex = index
		}
		if strings.Contains(line, "Package & App Selection") {
			headingLineIndex = index
		}
		if strings.Contains(line, "Plan") {
			configLineIndex = index
		}
		if strings.Contains(line, "Apply") {
			executionLineIndex = index
		}
	}

	if badgeLineIndex == -1 || headingLineIndex == -1 || configLineIndex == -1 || executionLineIndex == -1 {
		t.Fatalf("expected badge, heading, and both progress labels in status panel, got %q", view)
	}
	if got, want := strings.Index(lines[badgeLineIndex], "CONFIGURING"), strings.Index(lines[headingLineIndex], "Package & App Selection"); got != want {
		t.Fatalf("expected badge and heading to start in same column, got badge=%d heading=%d view=%q", got, want, view)
	}
	if headingLineIndex != badgeLineIndex+2 {
		t.Fatalf("expected one spacer line between badge and heading, got view=%q", view)
	}
	if strings.Trim(lines[badgeLineIndex+1], " │") != "" {
		t.Fatalf("expected spacer line between badge and heading, got %q", lines[badgeLineIndex+1])
	}
	if strings.Trim(lines[headingLineIndex+1], " │") != "" {
		t.Fatalf("expected spacer line after heading, got %q", lines[headingLineIndex+1])
	}
	if configLineIndex != headingLineIndex+2 {
		t.Fatalf("expected config progress label directly below heading spacer, got view=%q", view)
	}
	if executionLineIndex != configLineIndex+3 {
		t.Fatalf("expected execution progress label below config progress bar, got view=%q", view)
	}
	if strings.Trim(lines[configLineIndex+2], " │") != "" {
		t.Fatalf("expected spacer line between progress bars, got %q", lines[configLineIndex+2])
	}
	if strings.Contains(view, "4 of 13") {
		t.Fatalf("expected status panel to omit stage count summary, got %q", view)
	}
	if strings.Contains(view, "stages selected") {
		t.Fatalf("expected status panel to omit selected stage count, got %q", view)
	}
	if strings.Contains(view, "% complete") {
		t.Fatalf("expected status panel to omit textual progress percentage, got %q", view)
	}
	if strings.Contains(view, "Enter continue") {
		t.Fatalf("expected status panel to omit keyboard shortcuts, got %q", view)
	}
}

func TestRenderProgressBarUsesSuccessColorAtComplete(t *testing.T) {
	inProgress := renderProgressBar(10, 90)
	complete := renderProgressBar(10, 100)

	if got, want := lipgloss.Width(inProgress), 10; got != want {
		t.Fatalf("expected in-progress bar width=%d, got %d rendered=%q", want, got, inProgress)
	}
	if got, want := lipgloss.Width(complete), 10; got != want {
		t.Fatalf("expected complete bar width=%d, got %d rendered=%q", want, got, complete)
	}
	if stripped := ansi.Strip(inProgress); strings.ContainsAny(stripped, "[]=") || !strings.Contains(stripped, "90%") {
		t.Fatalf("expected Bubbles progress output with percent and no bracket bar, got %q", stripped)
	}
	if stripped := ansi.Strip(complete); strings.ContainsAny(stripped, "[]=") || !strings.Contains(stripped, "100%") {
		t.Fatalf("expected complete Bubbles progress output with percent and no bracket bar, got %q", stripped)
	}
}

func TestDashboardStatusSplitsConfigurationAndExecutionProgress(t *testing.T) {
	configStatus := model{screen: screenBrew}.configurationDashboardStatus()
	if configStatus.ConfigurationProgressPct == 0 {
		t.Fatalf("expected configuration progress to advance during config flow")
	}
	if configStatus.ExecutionProgressPct != 0 {
		t.Fatalf("expected execution progress to stay at zero before execution, got %d", configStatus.ExecutionProgressPct)
	}

	m := model{
		stageOrder: []string{"xcode_clt", "brew_bundle", "git_config"},
		stageMap: map[string]stages.Stage{
			"xcode_clt":   {ID: "xcode_clt", Title: "Xcode Command Line Tools"},
			"brew_bundle": {ID: "brew_bundle", Title: "Brew Bundle"},
			"git_config":  {ID: "git_config", Title: "Git Configuration"},
		},
		stageStatuses: map[string]state.StageStatus{
			"xcode_clt":   {Status: stages.StatusSuccess},
			"brew_bundle": {Status: stages.StatusRunning},
			"git_config":  {Status: stages.StatusPending},
		},
	}
	executionStatus := m.executionDashboardStatus(m.executionProgress())
	if executionStatus.ConfigurationProgressPct != 100 {
		t.Fatalf("expected configuration progress complete during execution, got %d", executionStatus.ConfigurationProgressPct)
	}
	if executionStatus.ExecutionProgressPct != 33 {
		t.Fatalf("expected execution progress to reflect terminal stages, got %d", executionStatus.ExecutionProgressPct)
	}
}

func TestConfigurationProgressStartsAtZeroAndEndsAtComplete(t *testing.T) {
	welcomeStatus := model{screen: screenWelcome}.configurationDashboardStatus()
	if welcomeStatus.ConfigurationProgressPct != 0 {
		t.Fatalf("expected configuration progress to start at zero, got %d", welcomeStatus.ConfigurationProgressPct)
	}

	reviewStatus := model{screen: screenReview}.configurationDashboardStatus()
	if reviewStatus.ConfigurationProgressPct != 100 {
		t.Fatalf("expected configuration progress to be complete on review, got %d", reviewStatus.ConfigurationProgressPct)
	}
}

func TestRenderDashboardPlacesShortcutHintBelowBody(t *testing.T) {
	m := model{
		width:  96,
		height: 28,
	}
	hint := "Enter continue  Esc back  CTRL+C quit"
	view := ansi.Strip(m.renderDashboard(dashboardStatus{
		Badge:                    "Configuring",
		BadgeTone:                accentAltColor,
		Heading:                  "Package & App Selection",
		Summary:                  "4 of 13",
		ConfigurationProgressPct: 30,
		ExecutionProgressPct:     0,
		Hint:                     hint,
	}, dashboardJourney{}, ""))
	lines := strings.Split(view, "\n")

	lastPanelBorderIndex := -1
	hintLineIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "└") {
			lastPanelBorderIndex = index
		}
		if strings.Contains(line, "enter continue") {
			hintLineIndex = index
		}
	}

	if hintLineIndex == -1 {
		t.Fatalf("expected dashboard footer to contain shortcut hint, got %q", view)
	}
	if hintLineIndex <= lastPanelBorderIndex {
		t.Fatalf("expected shortcut hint below body panels, got hint line=%d border line=%d view=%q", hintLineIndex, lastPanelBorderIndex, view)
	}
	if hintLineIndex != lastPanelBorderIndex+2 {
		t.Fatalf("expected one spacer line before shortcut hint, got hint line=%d border line=%d view=%q", hintLineIndex, lastPanelBorderIndex, view)
	}
	if strings.TrimSpace(lines[lastPanelBorderIndex+1]) != "" {
		t.Fatalf("expected blank spacer line before shortcut hint, got %q", lines[lastPanelBorderIndex+1])
	}
	if strings.ContainsAny(lines[hintLineIndex], "│┌┐└┘─") {
		t.Fatalf("expected shortcut hint without a border, got %q", lines[hintLineIndex])
	}
	if !strings.Contains(lines[hintLineIndex], "esc back") || !strings.Contains(lines[hintLineIndex], "ctrl+c quit") {
		t.Fatalf("expected Bubbles help bindings in footer, got line %q", lines[hintLineIndex])
	}
	if strings.Contains(lines[hintLineIndex], "Elapsed") || strings.Contains(lines[hintLineIndex], "0.000s") {
		t.Fatalf("expected elapsed time to be omitted from footer, got line %q", lines[hintLineIndex])
	}
	if got := strings.Index(lines[hintLineIndex], "enter continue"); got <= 2 {
		t.Fatalf("expected shortcut hint to be centered, got line %q", lines[hintLineIndex])
	}
}

func TestRenderDashboardStatusPanelShowsExecutionTimerTopRight(t *testing.T) {
	m := model{
		screen:   screenExecuting,
		runState: &state.RunState{},
	}
	view := ansi.Strip(m.renderDashboardStatusPanel(34, 13, dashboardStatus{
		Badge:                    "Running",
		BadgeTone:                accentAltColor,
		Heading:                  "Brew Bundle",
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     30,
	}))
	lines := strings.Split(view, "\n")

	for _, line := range lines {
		if !strings.Contains(line, "RUNNING") {
			continue
		}
		if strings.Contains(line, "Elapsed") {
			t.Fatalf("expected timer without label, got %q", line)
		}
		if !strings.Contains(line, "0.000s") {
			t.Fatalf("expected timer on badge line, got %q", line)
		}
		if !strings.HasSuffix(strings.TrimRight(line, " │"), "0.000s") {
			t.Fatalf("expected timer right-aligned in status panel, got %q", line)
		}
		return
	}
	t.Fatalf("expected status panel badge line, got %q", view)
}

func TestRenderDashboardStatusPanelHidesTimerBeforeExecutionStarts(t *testing.T) {
	m := model{screen: screenReview}
	view := ansi.Strip(m.renderDashboardStatusPanel(34, 13, dashboardStatus{
		Badge:                    "Ready",
		BadgeTone:                successColor,
		Heading:                  "Execution Plan Review",
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     0,
	}))

	if strings.Contains(view, "0.000s") || strings.Contains(view, "Elapsed") {
		t.Fatalf("expected no timer before apply execution starts, got %q", view)
	}
}

func TestFormatElapsedUsesSecondsWithMilliseconds(t *testing.T) {
	for _, testCase := range []struct {
		elapsed time.Duration
		want    string
	}{
		{elapsed: 0, want: "0.000s"},
		{elapsed: 1234 * time.Millisecond, want: "1.234s"},
		{elapsed: time.Minute + 2345*time.Millisecond, want: "62.345s"},
	} {
		if got := formatElapsed(testCase.elapsed); got != testCase.want {
			t.Fatalf("formatElapsed(%s)=%q want %q", testCase.elapsed, got, testCase.want)
		}
	}
}

func TestViewConfigurationUsesDashboardLayoutWithJourneyPreview(t *testing.T) {
	m := model{
		screen: screenDevTools,
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
			{ID: "jq", Kind: "brew"},
		},
		brewSelected: map[string]bool{
			"jq": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git Configuration", Selected: true},
		},
	}

	view := m.View()

	for _, fragment := range []string{
		"██████╗  ██████╗",
		"Initiating CHAPEAUX, stand by for awesomeness...",
		"Dev Tools Setup",
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
	for _, fragment := range []string{"Toggle with Space.", "Filter with /.", "Enter to continue", "Esc to go back"} {
		if strings.Contains(view, fragment) {
			t.Fatalf("expected configuration view to omit shortcut hint %q, got %q", fragment, view)
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
			{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
		},
		dockerOptions: []selectOption{
			{ID: string(stages.DockerRuntimeColima), Title: "colima"},
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
		screen: screenDevTools,
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
			{ID: "jq", Kind: "brew"},
		},
		brewSelected: map[string]bool{
			"jq": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git Configuration", Selected: true},
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

func (r *noOpRunner) LookPath(context.Context, string) (string, error) {
	return "/usr/local/bin/test-command", nil
}

type recordingExecutionService struct {
	request ExecutionRequest
}

func (s *recordingExecutionService) PrepareExecution(_ context.Context, request ExecutionRequest) (ExecutionRun, error) {
	s.request = request
	runState := &state.RunState{
		RunID:        state.RunID("test-run"),
		StartAt:      time.Now().UTC(),
		Mode:         modeName(request.DryRun),
		ResolvedPlan: request.Plan,
		Decisions:    request.Decisions,
		SelectedIDs:  request.SelectedIDs,
		Stages:       make(map[state.StageID]state.StageStatus, len(request.Plan)),
	}
	if request.Resume {
		runState = request.Current
	}
	return ExecutionRun{
		RunState:     runState,
		DryRun:       request.DryRun,
		RunDir:       tTempRunDir,
		HumanLogPath: filepath.Join(tTempRunDir, "run.log"),
		EventsPath:   filepath.Join(tTempRunDir, "events.jsonl"),
		HumanLog:     discardWriteCloser{Writer: io.Discard},
		EventsLog:    discardWriteCloser{Writer: io.Discard},
	}, nil
}

func (s *recordingExecutionService) Execute(context.Context, ExecutionRun, ExecutionHooks) error {
	return nil
}

type discardWriteCloser struct {
	io.Writer
}

func (discardWriteCloser) Close() error {
	return nil
}

const tTempRunDir = "/tmp/laptop-setup-test-run"

func sendEnter(t *testing.T, m model) model {
	t.Helper()
	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	return updated
}

func sendKey(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()
	next, _ := m.updateKey(key)
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	return updated
}

func TestRadioSelectionFollowsArrowNavigation(t *testing.T) {
	t.Run("node toolchain", func(t *testing.T) {
		m := model{
			screen:        screenNodeToolchain,
			cursor:        0,
			nodeSelection: 0,
			nodeOptions: []selectOption{
				{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
				{ID: string(stages.NodeToolchainNvmPnpm), Title: "pnpm + nvm"},
			},
		}

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if m.cursor != 1 || m.nodeSelection != 1 {
			t.Fatalf("expected down arrow to select node option 1, got cursor=%d selection=%d", m.cursor, m.nodeSelection)
		}

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
		if m.cursor != 0 || m.nodeSelection != 0 {
			t.Fatalf("expected up arrow to select node option 0, got cursor=%d selection=%d", m.cursor, m.nodeSelection)
		}
	})

	t.Run("docker runtime", func(t *testing.T) {
		m := model{
			screen:          screenDockerRuntime,
			cursor:          0,
			dockerSelection: 0,
			dockerOptions: []selectOption{
				{ID: "colima", Title: "colima"},
				{ID: "future-runtime", Title: "future runtime"},
			},
		}

		m = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if m.cursor != 1 || m.dockerSelection != 1 {
			t.Fatalf("expected down arrow to select docker option 1, got cursor=%d selection=%d", m.cursor, m.dockerSelection)
		}
	})

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

func TestGitIdentityInputsAllowBlankEnter(t *testing.T) {
	t.Run("blank name continues to email", func(t *testing.T) {
		m := model{
			screen:        screenGitName,
			gitNameInput:  textinput.New(),
			gitEmailInput: textinput.New(),
		}

		m = sendEnter(t, m)
		if m.screen != screenGitEmail {
			t.Fatalf("expected blank git name to continue to email screen, got %v", m.screen)
		}
		if m.inputError != "" {
			t.Fatalf("expected no input error, got %q", m.inputError)
		}
	})

	t.Run("blank email continues to manual", func(t *testing.T) {
		m := model{
			screen:        screenGitEmail,
			gitEmailInput: textinput.New(),
		}

		m = sendEnter(t, m)
		if m.screen != screenManual {
			t.Fatalf("expected blank git email to continue to manual screen, got %v", m.screen)
		}
		if m.inputError != "" {
			t.Fatalf("expected no input error, got %q", m.inputError)
		}
	})
}

func TestShellOptionsEnterSkipsOrCollectsGitIdentity(t *testing.T) {
	t.Run("git stage skipped", func(t *testing.T) {
		m := model{
			screen: screenShellOptions,
			devOptions: []toggleOption{
				{ID: "git_config", Title: "Git Configuration", Selected: false},
			},
		}

		m = sendEnter(t, m)
		if m.screen != screenManual {
			t.Fatalf("expected skipped git stage to continue to manual screen, got %v", m.screen)
		}
	})

	t.Run("git stage selected", func(t *testing.T) {
		m := model{
			screen:       screenShellOptions,
			gitNameInput: textinput.New(),
			devOptions: []toggleOption{
				{ID: "git_config", Title: "Git Configuration", Selected: true},
			},
		}

		m = sendEnter(t, m)
		if m.screen != screenGitName {
			t.Fatalf("expected selected git stage to collect git name, got %v", m.screen)
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

func TestInteractiveCommandRequestShowsAuthorizationScreen(t *testing.T) {
	response := make(chan interactiveCommandResult, 1)
	m := model{
		screen:        screenExecuting,
		executing:     true,
		stageOrder:    []string{"homebrew_install"},
		stageStatuses: map[string]state.StageStatus{"homebrew_install": {Status: stages.StatusRunning}},
		stageMap:      map[string]stages.Stage{"homebrew_install": {ID: "homebrew_install", Title: "Homebrew Install"}},
	}

	next, cmd := m.Update(interactiveCommandRequestMsg{Request: interactiveCommandRequest{
		Command:  runner.Command{Name: "brew", Args: []string{"bundle", "install"}, Interactive: true, Prompt: "Homebrew may ask for your password."},
		Response: response,
	}})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	if cmd != nil {
		t.Fatal("expected request to render prompt without starting command")
	}
	if updated.screen != screenInteractive {
		t.Fatalf("expected interactive screen, got %v", updated.screen)
	}
	if updated.interactivePrompt == nil {
		t.Fatal("expected interactive prompt to be stored")
	}
	view := updated.View()
	if !strings.Contains(view, "Terminal Authorization") || !strings.Contains(view, "brew bundle install") {
		t.Fatalf("expected interactive authorization view, got %q", view)
	}

	next, cmd = updated.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok = next.(model); !ok {
		t.Fatalf("expected model type after enter, got %T", next)
	}
	if cmd == nil {
		t.Fatal("expected enter to start interactive command")
	}
}

func TestInteractiveCommandFinishedRespondsToExecutionWorker(t *testing.T) {
	response := make(chan interactiveCommandResult, 1)
	updates := make(chan tea.Msg)
	request := interactiveCommandRequest{
		Command:  runner.Command{Name: "brew", Args: []string{"bundle", "install"}, Interactive: true},
		Response: response,
	}
	m := model{
		screen:            screenInteractive,
		updates:           updates,
		interactivePrompt: &request,
	}
	want := interactiveCommandResult{Result: runner.Result{ExitCode: 0}}

	next, cmd := m.Update(interactiveCommandFinishedMsg{Request: request, Result: want})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("expected model type after update, got %T", next)
	}
	if updated.screen != screenExecuting {
		t.Fatalf("expected executing screen, got %v", updated.screen)
	}
	if updated.interactivePrompt != nil {
		t.Fatal("expected interactive prompt to be cleared")
	}
	if cmd == nil {
		t.Fatal("expected command to resume execution updates")
	}

	select {
	case got := <-response:
		if got.Result.ExitCode != want.Result.ExitCode || got.Err != nil {
			t.Fatalf("unexpected interactive result: %+v", got)
		}
	default:
		t.Fatal("expected interactive result to be sent to execution worker")
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
		brewEntries: []stages.BrewEntry{{ID: "jq", Kind: "brew"}},
		brewSelected: map[string]bool{
			"jq": true,
		},
		devOptions: []toggleOption{
			{ID: "git_config", Title: "Git", Selected: true},
		},
		nodeOptions: []selectOption{
			{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
			{ID: string(stages.NodeToolchainNvmPnpm), Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: string(stages.DockerRuntimeColima), Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
		},
		manualOptions: []toggleOption{
			{ID: "manual_app_store_apps", Title: "Manual", Selected: true},
		},
	}
	m.gitNameInput = textinput.New()
	m.gitEmailInput = textinput.New()
	m.gitNameInput.SetValue("Alice")
	m.gitEmailInput.SetValue("alice@example.com")

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
	if m.screen != screenGitName {
		t.Fatalf("expected git name screen, got %v", m.screen)
	}
	m = sendEnter(t, m)
	if m.screen != screenGitEmail {
		t.Fatalf("expected git email screen, got %v", m.screen)
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

func TestCriticalToggleOptionCannotBeDeselected(t *testing.T) {
	m := model{
		screen: screenMacOS,
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode", Selected: true, Critical: true},
		},
		stageStatuses: map[string]state.StageStatus{},
	}

	next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeySpace})
	updated := next.(model)

	if !updated.macOSOptions[0].Selected {
		t.Fatal("expected critical option to remain selected")
	}
}

func TestLimitedOutputBufferTruncatesCapturedInteractiveOutput(t *testing.T) {
	buffer := newLimitedOutputBuffer(4)

	if _, err := buffer.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	output := buffer.String()
	if !strings.HasPrefix(output, "abcd\n") {
		t.Fatalf("expected output to keep prefix within limit, got %q", output)
	}
	if !strings.Contains(output, "<output truncated>") {
		t.Fatalf("expected truncation marker, got %q", output)
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
		nodeOptions:   []selectOption{{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"}},
		dockerOptions: []selectOption{{ID: string(stages.DockerRuntimeColima), Title: "colima"}},
		stageStatuses: make(map[string]state.StageStatus),
	}

	m = sendEnter(t, m)

	if m.screen != screenReview {
		t.Fatalf("expected to stay on review screen, got %v", m.screen)
	}
	if m.executing {
		t.Fatalf("expected execution not to start")
	}
	if !strings.Contains(m.planError, "Brew Bundle selected with no package/app entries") {
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
		stageMap[stage.ID.String()] = stage
	}

	m := model{
		ctx:              context.Background(),
		screen:           screenReview,
		store:            store,
		catalog:          catalog,
		stageMap:         stageMap,
		runner:           &noOpRunner{},
		repoRoot:         t.TempDir(),
		homeDir:          homeDir,
		executionService: &recordingExecutionService{},
		macOSOptions: []toggleOption{
			{ID: "xcode_clt", Title: "Xcode", Selected: true},
		},
		installOptions: []toggleOption{
			{ID: "brew_bundle", Title: "Brew Bundle", Selected: true},
		},
		brewEntries: []stages.BrewEntry{{ID: "jq", Kind: "brew"}},
		brewSelected: map[string]bool{
			"jq": true,
		},
		nodeOptions: []selectOption{
			{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
		},
		dockerOptions: []selectOption{
			{ID: string(stages.DockerRuntimeColima), Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
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
