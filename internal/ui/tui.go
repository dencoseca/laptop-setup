package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/runner"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

var (
	phaseMacOSStages   = []string{"xcode_clt", "macos_defaults"}
	phaseInstallStages = []string{"homebrew_install", "brew_bundle"}
	phaseDevStages     = []string{"vite_plus_install", "docker_config", "shell_setup", "git_config"}
	phaseManualStages  = []string{"manual_app_store_apps"}
)

type Config struct {
	Resume bool
	DryRun bool
	From   string
	Only   []string
	Skip   []string
}

type Options struct {
	Config    Config
	Store     *state.Store
	Current   *state.RunState
	Catalog   []stages.Stage
	RepoRoot  string
	HomeDir   string
	Out       io.Writer
	Commander runner.CommandRunner
}

type screen int

const (
	screenWelcome screen = iota
	screenMacOS
	screenInstall
	screenBrew
	screenDevTools
	screenManual
	screenReview
	screenExecuting
	screenFailure
	screenSummary
)

type toggleOption struct {
	ID       string
	Title    string
	Selected bool
}

type stageStatusMsg struct {
	StageID string
	Status  state.StageStatus
}

type logEventMsg struct {
	Line string
}

type executionDoneMsg struct {
	Err error
}

type failureRequest struct {
	StageID  string
	Title    string
	Attempt  int
	CanSkip  bool
	Message  string
	Response chan execution.FailureAction
}

type failureRequestMsg struct {
	Request failureRequest
}

type executionSetup struct {
	runState     *state.RunState
	dryRun       bool
	humanLogPath string
	eventsPath   string
	humanLog     *os.File
	eventsLog    *os.File
}

type model struct {
	ctx    context.Context
	cancel context.CancelFunc

	config   Config
	store    *state.Store
	current  *state.RunState
	catalog  []stages.Stage
	stageMap map[string]stages.Stage
	runner   runner.CommandRunner

	repoRoot string
	homeDir  string

	screen    screen
	cursor    int
	resumeRun bool

	macOSOptions   []toggleOption
	installOptions []toggleOption
	devOptions     []toggleOption
	manualOptions  []toggleOption

	brewEntries  []stages.BrewEntry
	brewSelected map[string]bool

	plan          []string
	planError     string
	stageStatuses map[string]state.StageStatus
	stageOrder    []string
	recentLogs    []string
	updates       chan tea.Msg
	failurePrompt *failureRequest
	runState      *state.RunState
	humanLogPath  string
	eventsLogPath string
	runErr        error
	executing     bool
	spinner       spinner.Model
}

func Run(ctx context.Context, options Options) error {
	if options.Store == nil {
		return errors.New("state store is required")
	}
	if len(options.Catalog) == 0 {
		return errors.New("stage catalog is required")
	}
	if options.Commander == nil {
		options.Commander = runner.NewOSCommandRunner()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stageMap := make(map[string]stages.Stage, len(options.Catalog))
	for _, stage := range options.Catalog {
		stageMap[stage.ID] = stage
	}

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	m := model{
		ctx:            runCtx,
		cancel:         cancel,
		config:         options.Config,
		store:          options.Store,
		current:        options.Current,
		catalog:        options.Catalog,
		stageMap:       stageMap,
		runner:         options.Commander,
		repoRoot:       options.RepoRoot,
		homeDir:        options.HomeDir,
		screen:         screenWelcome,
		resumeRun:      options.Config.Resume,
		macOSOptions:   optionsForStageIDs(options.Catalog, phaseMacOSStages),
		installOptions: optionsForStageIDs(options.Catalog, phaseInstallStages),
		devOptions:     optionsForStageIDs(options.Catalog, phaseDevStages),
		manualOptions:  optionsForStageIDs(options.Catalog, phaseManualStages),
		brewSelected:   make(map[string]bool),
		stageStatuses:  make(map[string]state.StageStatus),
		spinner:        spin,
	}

	if err := m.reloadBrewEntries(); err != nil && !m.resumeRun {
		return err
	}

	output := options.Out
	if output == nil {
		output = os.Stdout
	}
	program := tea.NewProgram(m, tea.WithOutput(output), tea.WithContext(runCtx))
	finalModel, err := program.Run()
	if err != nil {
		return err
	}

	finished, ok := finalModel.(model)
	if !ok {
		return errors.New("unexpected final TUI model type")
	}
	if finished.runErr != nil {
		return finished.runErr
	}
	return nil
}

func (m model) Init() tea.Cmd {
	if m.resumeRun {
		m.plan = slices.Clone(m.current.ResolvedPlan)
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.KeyMsg:
		return m.updateKey(message)
	case spinner.TickMsg:
		if m.executing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(message)
			return m, cmd
		}
	case stageStatusMsg:
		m.stageStatuses[message.StageID] = message.Status
		return m, waitForExecutionUpdate(m.updates)
	case logEventMsg:
		m.recentLogs = append(m.recentLogs, message.Line)
		if len(m.recentLogs) > 12 {
			m.recentLogs = append([]string(nil), m.recentLogs[len(m.recentLogs)-12:]...)
		}
		return m, waitForExecutionUpdate(m.updates)
	case failureRequestMsg:
		m.failurePrompt = &message.Request
		m.screen = screenFailure
		return m, nil
	case executionDoneMsg:
		m.executing = false
		m.runErr = message.Err
		m.failurePrompt = nil
		m.screen = screenSummary
		return m, nil
	}
	return m, nil
}

func (m model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.abortExecutionIfNeeded(execution.ActionAbort)
		return m, tea.Quit
	}

	switch m.screen {
	case screenWelcome:
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "enter":
			if m.resumeRun {
				m.screen = screenReview
			} else {
				m.screen = screenMacOS
			}
			m.cursor = 0
		}
	case screenMacOS:
		return m.updateToggleScreen(key, &m.macOSOptions, screenWelcome, screenInstall)
	case screenInstall:
		return m.updateToggleScreen(key, &m.installOptions, screenMacOS, screenBrew)
	case screenBrew:
		switch key.String() {
		case "b":
			m.screen = screenInstall
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.brewEntries)-1 {
				m.cursor++
			}
		case " ":
			if len(m.brewEntries) > 0 {
				id := m.brewEntries[m.cursor].ID
				m.brewSelected[id] = !m.brewSelected[id]
			}
		case "enter":
			m.screen = screenDevTools
			m.cursor = 0
		case "q":
			return m, tea.Quit
		}
	case screenDevTools:
		return m.updateToggleScreen(key, &m.devOptions, screenBrew, screenManual)
	case screenManual:
		return m.updateToggleScreen(key, &m.manualOptions, screenDevTools, screenReview)
	case screenReview:
		switch key.String() {
		case "b":
			if m.resumeRun {
				m.screen = screenWelcome
			} else {
				m.screen = screenManual
			}
			m.cursor = 0
		case "q":
			return m, tea.Quit
		case "enter":
			setup, err := m.prepareExecutionSetup()
			if err != nil {
				m.planError = err.Error()
				return m, nil
			}

			m.planError = ""
			m.runState = setup.runState
			m.humanLogPath = setup.humanLogPath
			m.eventsLogPath = setup.eventsPath
			m.stageOrder = slices.Clone(setup.runState.ResolvedPlan)
			m.initialiseStageStatuses()

			m.screen = screenExecuting
			m.executing = true
			m.updates = make(chan tea.Msg, 32)
			return m, tea.Batch(
				startExecutionWorker(m.ctx, m.updates, setup, m.store, m.catalog, m.repoRoot, m.homeDir, m.runner),
				waitForExecutionUpdate(m.updates),
				m.spinner.Tick,
			)
		}
	case screenExecuting:
		switch key.String() {
		case "q":
			m.abortExecutionIfNeeded(execution.ActionAbort)
			return m, tea.Quit
		}
	case screenFailure:
		switch key.String() {
		case "r":
			m.resolveFailure(execution.ActionRetry)
			m.screen = screenExecuting
			return m, waitForExecutionUpdate(m.updates)
		case "s":
			if m.failurePrompt != nil && m.failurePrompt.CanSkip {
				m.resolveFailure(execution.ActionSkip)
				m.screen = screenExecuting
				return m, waitForExecutionUpdate(m.updates)
			}
		case "a", "q":
			m.resolveFailure(execution.ActionAbort)
			m.screen = screenExecuting
			return m, waitForExecutionUpdate(m.updates)
		}
	case screenSummary:
		switch key.String() {
		case "enter", "q":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *model) updateToggleScreen(
	key tea.KeyMsg,
	options *[]toggleOption,
	backScreen screen,
	nextScreen screen,
) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		return m, tea.Quit
	case "b":
		m.screen = backScreen
		m.cursor = 0
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(*options)-1 {
			m.cursor++
		}
	case " ":
		if len(*options) > 0 {
			(*options)[m.cursor].Selected = !(*options)[m.cursor].Selected
		}
	case "enter":
		m.screen = nextScreen
		m.cursor = 0
	}
	return *m, nil
}

func (m model) View() string {
	switch m.screen {
	case screenWelcome:
		return m.viewWelcome()
	case screenMacOS:
		return m.viewToggleOptions("Phase: MacOS Setup", m.macOSOptions)
	case screenInstall:
		return m.viewToggleOptions("Phase: Install Apps/Packages", m.installOptions)
	case screenBrew:
		return m.viewBrewSelection()
	case screenDevTools:
		return m.viewToggleOptions("Phase: Dev Tools Setup", m.devOptions)
	case screenManual:
		return m.viewToggleOptions("Phase: Manual Steps", m.manualOptions)
	case screenReview:
		return m.viewReview()
	case screenExecuting:
		return m.viewExecuting()
	case screenFailure:
		return m.viewFailure()
	case screenSummary:
		return m.viewSummary()
	default:
		return ""
	}
}

func (m model) viewWelcome() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Render("Laptop Setup")
	fmt.Fprintf(&b, "%s\n\n", title)
	if m.resumeRun {
		fmt.Fprintf(&b, "Resume run: %s\n", m.current.RunID)
		fmt.Fprintf(&b, "Mode: %s\n\n", m.current.Mode)
		fmt.Fprintf(&b, "Press Enter to review and continue.\n")
	} else {
		fmt.Fprintf(&b, "Interactive setup will collect phase decisions, show plan review, and execute stages.\n\n")
		fmt.Fprintf(&b, "Press Enter to continue.\n")
	}
	fmt.Fprintf(&b, "Press q to quit.")
	return b.String()
}

func (m model) viewToggleOptions(title string, options []toggleOption) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "Toggle stages with space. Enter to continue, b to go back.\n\n")
	for index, option := range options {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		selected := " "
		if option.Selected {
			selected = "x"
		}
		fmt.Fprintf(&b, "%s[%s] %s (%s)\n", prefix, selected, option.Title, option.ID)
	}
	return b.String()
}

func (m model) viewBrewSelection() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Phase: Brew Catalog Selection"))
	fmt.Fprintf(&b, "Toggle entries with space. Enter to continue, b to go back.\n\n")

	if len(m.brewEntries) == 0 {
		fmt.Fprintf(&b, "No Brew entries found in templates/Brewfile.\n")
		return b.String()
	}

	for index, entry := range m.brewEntries {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		selected := " "
		if m.brewSelected[entry.ID] {
			selected = "x"
		}
		fmt.Fprintf(&b, "%s[%s] %s (%s)\n", prefix, selected, entry.ID, entry.Kind)
	}
	fmt.Fprintf(&b, "\nSelected: %d of %d", len(m.selectedBrewIDs()), len(m.brewEntries))
	return b.String()
}

func (m *model) viewReview() string {
	plan, err := m.resolvePlan()
	m.plan = plan
	m.planError = ""
	if err != nil {
		m.planError = err.Error()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Execution Plan Review"))
	fmt.Fprintf(&b, "Mode: %s\n", modeName(m.effectiveDryRun()))
	if !m.resumeRun {
		fmt.Fprintf(&b, "Selected Brew entries: %d\n", len(m.selectedBrewIDs()))
	}
	fmt.Fprintf(&b, "\nStages:\n")
	for _, stageID := range m.plan {
		stage := m.stageMap[stageID]
		fmt.Fprintf(&b, "- %s (%s)\n", stage.Title, stageID)
	}
	if m.planError != "" {
		fmt.Fprintf(&b, "\nPlan error: %s\n", m.planError)
	}
	fmt.Fprintf(&b, "\nPress Enter to execute, b to go back.")
	return b.String()
}

func (m model) viewExecuting() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", m.spinner.View(), lipgloss.NewStyle().Bold(true).Render("Executing Stages"))
	fmt.Fprintf(&b, "Press q to abort.\n\n")
	for _, stageID := range m.stageOrder {
		stage := m.stageMap[stageID]
		status := m.stageStatuses[stageID]
		if status.Status == "" {
			status.Status = string(stages.StatusPending)
		}
		fmt.Fprintf(&b, "%s %s (%s)\n", statusGlyph(status.Status), stage.Title, status.Status)
	}
	fmt.Fprintf(&b, "\nRecent logs:\n")
	if len(m.recentLogs) == 0 {
		fmt.Fprintf(&b, "  (waiting for events)\n")
	}
	for _, line := range m.recentLogs {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}

func (m model) viewFailure() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Stage Failure"))
	if m.failurePrompt != nil {
		fmt.Fprintf(&b, "Stage: %s (%s)\n", m.failurePrompt.Title, m.failurePrompt.StageID)
		fmt.Fprintf(&b, "Attempt: %d\n", m.failurePrompt.Attempt)
		fmt.Fprintf(&b, "Error: %s\n\n", m.failurePrompt.Message)
	}
	fmt.Fprintf(&b, "Actions:\n")
	fmt.Fprintf(&b, "- r: Retry stage\n")
	if m.failurePrompt != nil && m.failurePrompt.CanSkip {
		fmt.Fprintf(&b, "- s: Skip stage\n")
	}
	fmt.Fprintf(&b, "- a: Abort run\n")
	return b.String()
}

func (m model) viewSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Run Summary"))

	if m.runErr == nil {
		fmt.Fprintf(&b, "Status: completed\n")
	} else if errors.Is(m.runErr, execution.ErrAborted) || errors.Is(m.runErr, context.Canceled) {
		fmt.Fprintf(&b, "Status: aborted\n")
	} else {
		fmt.Fprintf(&b, "Status: failed\n")
		fmt.Fprintf(&b, "Error: %s\n", m.runErr)
	}

	completed, skipped, failed := stageCounts(m.stageStatuses)
	fmt.Fprintf(&b, "\nStage counts: completed=%d skipped=%d failed=%d\n", completed, skipped, failed)
	if m.humanLogPath != "" {
		fmt.Fprintf(&b, "Run log: %s\n", m.humanLogPath)
	}
	if m.eventsLogPath != "" {
		fmt.Fprintf(&b, "Events log: %s\n", m.eventsLogPath)
	}
	fmt.Fprintf(&b, "\nPress Enter to exit.")
	return b.String()
}

func (m *model) reloadBrewEntries() error {
	entries, err := stages.LoadBrewEntries(filepath.Join(m.repoRoot, "templates", "Brewfile"))
	if err != nil {
		return err
	}

	previous := make(map[string]bool, len(m.brewSelected))
	for key, value := range m.brewSelected {
		previous[key] = value
	}

	m.brewEntries = entries
	m.brewSelected = make(map[string]bool, len(entries))
	for _, entry := range entries {
		if value, ok := previous[entry.ID]; ok {
			m.brewSelected[entry.ID] = value
		} else {
			m.brewSelected[entry.ID] = true
		}
	}
	return nil
}

func (m *model) resolvePlan() ([]string, error) {
	if m.resumeRun {
		if m.current == nil {
			return nil, errors.New("resume requested but no existing state is loaded")
		}
		return slices.Clone(m.current.ResolvedPlan), nil
	}

	selectedStages := m.selectedStageIDs()
	onlyIDs := selectedStages
	if len(m.config.Only) > 0 {
		onlyIDs = m.config.Only
	}

	if slices.Contains(onlyIDs, "brew_bundle") && len(m.selectedBrewIDs()) == 0 {
		return nil, errors.New("brew_bundle selected with no Brew entries; select at least one entry or deselect brew_bundle")
	}

	return stages.ResolvePlan(m.catalog, stages.PlanOptions{
		FromID:  m.config.From,
		OnlyIDs: onlyIDs,
		SkipIDs: m.config.Skip,
	})
}

func (m *model) prepareExecutionSetup() (executionSetup, error) {
	plan, err := m.resolvePlan()
	if err != nil {
		return executionSetup{}, err
	}

	var (
		runState *state.RunState
		dryRun   bool
	)

	if m.resumeRun {
		if m.current == nil {
			return executionSetup{}, errors.New("resume requested but no existing run state found")
		}
		runState = m.current
		dryRun = runState.Mode == "dry-run"
	} else {
		dryRun = m.config.DryRun
		runState = &state.RunState{
			RunID:        state.NewRunID(time.Now()),
			StartAt:      time.Now().UTC(),
			Mode:         modeName(dryRun),
			ResolvedPlan: plan,
			Decisions: map[string]any{
				"selected_stage_ids": m.selectedStageIDs(),
			},
			SelectedIDs: m.selectedBrewIDs(),
			Stages:      make(map[string]state.StageStatus, len(m.catalog)),
		}
	}

	if runState.Decisions == nil {
		runState.Decisions = make(map[string]any)
	}
	if !m.resumeRun {
		runState.SelectedIDs = m.selectedBrewIDs()
		runState.ResolvedPlan = plan
	}

	if err = m.store.Save(m.ctx, runState); err != nil {
		return executionSetup{}, err
	}

	runDir, err := state.RunDir(runState.RunID)
	if err != nil {
		return executionSetup{}, err
	}
	if err = os.MkdirAll(runDir, 0o755); err != nil {
		return executionSetup{}, fmt.Errorf("create run directory: %w", err)
	}

	humanLogPath := filepath.Join(runDir, "run.log")
	eventsPath := filepath.Join(runDir, "events.jsonl")
	humanLog, err := os.OpenFile(humanLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return executionSetup{}, fmt.Errorf("open run log: %w", err)
	}
	eventsLog, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = humanLog.Close()
		return executionSetup{}, fmt.Errorf("open events log: %w", err)
	}

	return executionSetup{
		runState:     runState,
		dryRun:       dryRun,
		humanLogPath: humanLogPath,
		eventsPath:   eventsPath,
		humanLog:     humanLog,
		eventsLog:    eventsLog,
	}, nil
}

func (m *model) selectedStageIDs() []string {
	selectedSet := make(map[string]struct{})
	collectSelected(selectedSet, m.macOSOptions)
	collectSelected(selectedSet, m.installOptions)
	collectSelected(selectedSet, m.devOptions)
	collectSelected(selectedSet, m.manualOptions)

	ids := make([]string, 0, len(selectedSet))
	for _, stage := range m.catalog {
		if _, ok := selectedSet[stage.ID]; ok {
			ids = append(ids, stage.ID)
		}
	}
	return ids
}

func collectSelected(set map[string]struct{}, options []toggleOption) {
	for _, option := range options {
		if option.Selected {
			set[option.ID] = struct{}{}
		}
	}
}

func (m *model) selectedBrewIDs() []string {
	ids := make([]string, 0, len(m.brewEntries))
	for _, entry := range m.brewEntries {
		if m.brewSelected[entry.ID] {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

func (m *model) effectiveDryRun() bool {
	if m.resumeRun && m.current != nil {
		return m.current.Mode == "dry-run"
	}
	return m.config.DryRun
}

func (m *model) initialiseStageStatuses() {
	if m.runState == nil {
		return
	}
	for _, stageID := range m.stageOrder {
		status := m.runState.Stages[stageID]
		if status.Status == "" {
			status.Status = string(stages.StatusPending)
		}
		m.stageStatuses[stageID] = status
	}
}

func (m *model) abortExecutionIfNeeded(action execution.FailureAction) {
	if m.failurePrompt != nil {
		m.resolveFailure(action)
	}
	if m.executing {
		m.cancel()
	}
}

func (m *model) resolveFailure(action execution.FailureAction) {
	if m.failurePrompt == nil {
		return
	}
	select {
	case m.failurePrompt.Response <- action:
	default:
	}
	m.failurePrompt = nil
}

func startExecutionWorker(
	ctx context.Context,
	updates chan<- tea.Msg,
	setup executionSetup,
	store *state.Store,
	catalog []stages.Stage,
	repoRoot string,
	homeDir string,
	commandRunner runner.CommandRunner,
) tea.Cmd {
	return func() tea.Msg {
		go func() {
			defer close(updates)
			defer setup.humanLog.Close()
			defer setup.eventsLog.Close()

			logger := runner.NewEventLogger(setup.humanLog, setup.eventsLog)

			err := execution.Execute(ctx, execution.Options{
				Store:         store,
				RunState:      setup.runState,
				Catalog:       catalog,
				DryRun:        setup.dryRun,
				RepoRoot:      repoRoot,
				HomeDir:       homeDir,
				RunDir:        filepath.Dir(setup.humanLogPath),
				CommandRunner: commandRunner,
				Logger:        logger,
				Hooks: execution.Hooks{
					OnEvent: func(event runner.Event) {
						line := formatEventLine(event)
						select {
						case updates <- logEventMsg{Line: line}:
						case <-ctx.Done():
						}
					},
					OnStageStatus: func(stageID string, status state.StageStatus) {
						select {
						case updates <- stageStatusMsg{StageID: stageID, Status: status}:
						case <-ctx.Done():
						}
					},
					OnFailure: func(inner context.Context, failure execution.Failure) (execution.FailureAction, error) {
						response := make(chan execution.FailureAction, 1)
						request := failureRequest{
							StageID:  failure.Stage.ID,
							Title:    failure.Stage.Title,
							Attempt:  failure.Attempt,
							CanSkip:  failure.Stage.CanSkip,
							Message:  failure.Err.Error(),
							Response: response,
						}
						select {
						case updates <- failureRequestMsg{Request: request}:
						case <-inner.Done():
							return execution.ActionAbort, inner.Err()
						}

						select {
						case action := <-response:
							return action, nil
						case <-inner.Done():
							return execution.ActionAbort, inner.Err()
						}
					},
				},
			})

			select {
			case updates <- executionDoneMsg{Err: err}:
			case <-ctx.Done():
			}
		}()
		return nil
	}
}

func waitForExecutionUpdate(updates <-chan tea.Msg) tea.Cmd {
	if updates == nil {
		return nil
	}
	return func() tea.Msg {
		message, ok := <-updates
		if !ok {
			return nil
		}
		return message
	}
}

func optionsForStageIDs(catalog []stages.Stage, ids []string) []toggleOption {
	stageMap := make(map[string]stages.Stage, len(catalog))
	for _, stage := range catalog {
		stageMap[stage.ID] = stage
	}
	options := make([]toggleOption, 0, len(ids))
	for _, id := range ids {
		stage, ok := stageMap[id]
		if !ok {
			continue
		}
		options = append(options, toggleOption{
			ID:       stage.ID,
			Title:    stage.Title,
			Selected: true,
		})
	}
	return options
}

func modeName(dryRun bool) string {
	if dryRun {
		return "dry-run"
	}
	return "normal"
}

func statusGlyph(status string) string {
	switch status {
	case string(stages.StatusSuccess):
		return "[ok]"
	case string(stages.StatusSimulatedSuccess):
		return "[sim]"
	case string(stages.StatusSkipped):
		return "[skip]"
	case string(stages.StatusFailed):
		return "[fail]"
	case string(stages.StatusAlreadyDone):
		return "[done]"
	case string(stages.StatusRunning):
		return "[run]"
	default:
		return "[...]"
	}
}

func formatEventLine(event runner.Event) string {
	timestamp := event.Timestamp.UTC().Format("15:04:05")
	parts := []string{timestamp, strings.ToUpper(event.Level)}
	if event.StageID != "" {
		parts = append(parts, event.StageID)
	}
	if event.EventType != "" {
		parts = append(parts, event.EventType)
	}
	if event.Message != "" {
		parts = append(parts, event.Message)
	}
	return strings.Join(parts, " | ")
}

func stageCounts(statuses map[string]state.StageStatus) (completed int, skipped int, failed int) {
	for _, stageStatus := range statuses {
		switch stageStatus.Status {
		case string(stages.StatusSuccess), string(stages.StatusAlreadyDone), string(stages.StatusSimulatedSuccess):
			completed++
		case string(stages.StatusSkipped):
			skipped++
		case string(stages.StatusFailed):
			failed++
		}
	}
	return completed, skipped, failed
}
