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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/textinput"
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
	phaseDevStages     = []string{"node_toolchain", "docker_config", "shell_setup", "git_config"}
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
	Config           Config
	Store            execution.StateRepository
	Current          *state.RunState
	Catalog          []stages.Stage
	RepoRoot         string
	HomeDir          string
	Out              io.Writer
	Commander        runner.CommandRunner
	Templates        stages.TemplateStore
	ExecutionService ExecutionService
}

type screen int

const (
	screenWelcome screen = iota
	screenMacOS
	screenInstall
	screenBrew
	screenDevTools
	screenNodeToolchain
	screenDockerRuntime
	screenShellOptions
	screenGitName
	screenGitEmail
	screenManual
	screenReview
	screenExecuting
	screenInteractive
	screenFailure
	screenSummary
	screenQuitConfirm
)

type toggleOption struct {
	ID       string
	Title    string
	Selected bool
}

type selectOption struct {
	ID          string
	Title       string
	Description string
}

type stageStatusMsg struct {
	StageID string
	Status  state.StageStatus
}

type executionDoneMsg struct {
	Err error
}

type logTailTickMsg time.Time

type tailedLogLine struct {
	StageID string
	Line    string
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

type interactiveCommandRequest struct {
	Command  runner.Command
	Response chan interactiveCommandResult
}

type interactiveCommandResult struct {
	Result runner.Result
	Err    error
}

type interactiveCommandRequestMsg struct {
	Request interactiveCommandRequest
}

type interactiveCommandFinishedMsg struct {
	Request interactiveCommandRequest
	Result  interactiveCommandResult
}

type model struct {
	ctx    context.Context
	cancel context.CancelFunc

	config           Config
	store            execution.StateRepository
	current          *state.RunState
	catalog          []stages.Stage
	stageMap         map[string]stages.Stage
	runner           runner.CommandRunner
	templates        stages.TemplateStore
	executionService ExecutionService

	repoRoot string
	homeDir  string
	width    int
	height   int

	screen             screen
	cursor             int
	resumeRun          bool
	quitPreviousScreen screen

	macOSOptions   []toggleOption
	installOptions []toggleOption
	devOptions     []toggleOption
	nodeOptions    []selectOption
	dockerOptions  []selectOption
	shellOptions   []toggleOption
	manualOptions  []toggleOption

	brewEntries  []stages.BrewEntry
	brewSelected map[string]bool
	brewList     list.Model

	optionList       list.Model
	optionListScreen screen
	optionListReady  bool

	nodeSelection   int
	dockerSelection int
	gitNameInput    textinput.Model
	gitEmailInput   textinput.Model
	inputError      string

	plan              []string
	planError         string
	stageStatuses     map[string]state.StageStatus
	stageOrder        []string
	tailedLogs        []tailedLogLine
	logTailOffset     int64
	logTailCarry      string
	updates           chan tea.Msg
	failurePrompt     *failureRequest
	interactivePrompt *interactiveCommandRequest
	runState          *state.RunState
	humanLogPath      string
	eventsLogPath     string
	runErr            error
	executing         bool
	spinner           spinner.Model
	help              help.Model
	stopwatch         stopwatch.Model
}

const (
	displayedLogLineLimit = 12
	bufferedLogLineLimit  = 256
	logTailPollInterval   = 200 * time.Millisecond
	defaultViewWidth      = 120
	defaultViewHeight     = 40
)

var bootstrapTitleArtLines = []string{
	"██████╗  ██████╗  ██████╗ ████████╗███████╗████████╗██████╗  █████╗ ██████╗",
	"██╔══██╗██╔═══██╗██╔═══██╗╚══██╔══╝██╔════╝╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗",
	"██████╔╝██║   ██║██║   ██║   ██║   ███████╗   ██║   ██████╔╝███████║██████╔╝",
	"██╔══██╗██║   ██║██║   ██║   ██║   ╚════██║   ██║   ██╔══██╗██╔══██║██╔═══╝",
	"██████╔╝╚██████╔╝╚██████╔╝   ██║   ███████║   ██║   ██║  ██║██║  ██║██║",
	"╚═════╝  ╚═════╝  ╚═════╝    ╚═╝   ╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝",
}

var bootstrapCompactTitleArtLines = []string{
	"▗▄▄▖  ▗▄▖  ▗▄▖▗▄▄▄▖▗▄▄▖▗▄▄▄▖▗▄▄▖  ▗▄▖ ▗▄▄▖",
	"▐▌ ▐▌▐▌ ▐▌▐▌ ▐▌ █ ▐▌     █  ▐▌ ▐▌▐▌ ▐▌▐▌ ▐▌",
	"▐▛▀▚▖▐▌ ▐▌▐▌ ▐▌ █  ▝▀▚▖  █  ▐▛▀▚▖▐▛▀▜▌▐▛▀▘",
	"▐▙▄▞▘▝▚▄▞▘▝▚▄▞▘ █ ▗▄▄▞▘  █  ▐▌ ▐▌▐▌ ▐▌▐▌",
}

var (
	textColor        = lipgloss.AdaptiveColor{Light: "#0F172A", Dark: "#E5E7EB"}
	mutedColor       = lipgloss.AdaptiveColor{Light: "#475569", Dark: "#A1A1AA"}
	dimColor         = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#30303A"}
	borderColor      = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#D1D5DB"}
	accentColor      = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A855F7"}
	accentAltColor   = lipgloss.AdaptiveColor{Light: "#0284C7", Dark: "#22D3EE"}
	successColor     = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"}
	warningColor     = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F59E0B"}
	failureColor     = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"}
	pendingToneColor = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"}
)

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
	if options.ExecutionService == nil {
		return errors.New("execution service is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stageMap := make(map[string]stages.Stage, len(options.Catalog))
	for _, stage := range options.Catalog {
		stageMap[stage.ID.String()] = stage
	}

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	shortcutHelp := newShortcutHelp()
	elapsed := stopwatch.NewWithInterval(time.Millisecond)
	gitNameInput := textinput.New()
	gitNameInput.Placeholder = "Git user.name"
	gitNameInput.CharLimit = 128
	gitNameInput.Prompt = "> "

	gitEmailInput := textinput.New()
	gitEmailInput.Placeholder = "Git user.email"
	gitEmailInput.CharLimit = 128
	gitEmailInput.Prompt = "> "

	m := model{
		ctx:              runCtx,
		cancel:           cancel,
		config:           options.Config,
		store:            options.Store,
		current:          options.Current,
		catalog:          options.Catalog,
		stageMap:         stageMap,
		runner:           options.Commander,
		templates:        options.Templates,
		executionService: options.ExecutionService,
		repoRoot:         options.RepoRoot,
		homeDir:          options.HomeDir,
		screen:           screenWelcome,
		resumeRun:        options.Config.Resume,
		macOSOptions:     optionsForStageIDs(options.Catalog, phaseMacOSStages),
		installOptions:   optionsForStageIDs(options.Catalog, phaseInstallStages),
		devOptions:       optionsForStageIDs(options.Catalog, phaseDevStages),
		nodeOptions: []selectOption{
			{ID: string(stages.NodeToolchainVitePlus), Title: "vite+"},
			{ID: string(stages.NodeToolchainNvmPnpm), Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: string(stages.DockerRuntimeColima), Title: "colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
			{ID: stages.DecisionShellApplyZshrc, Title: "Write ~/.zshrc from template", Selected: true},
			{ID: stages.DecisionShellApplyStarship, Title: "Write starship.toml from template", Selected: true},
		},
		manualOptions: optionsForStageIDs(options.Catalog, phaseManualStages),
		brewSelected:  make(map[string]bool),
		gitNameInput:  gitNameInput,
		gitEmailInput: gitEmailInput,
		stageStatuses: detectAlreadyDoneStages(runCtx, options.Catalog, options.Commander, options.Templates, options.RepoRoot, options.HomeDir),
		spinner:       spin,
		help:          shortcutHelp,
		stopwatch:     elapsed,
	}

	if err := m.reloadBrewEntries(); err != nil && !m.resumeRun {
		return err
	}

	output := options.Out
	if output == nil {
		output = os.Stdout
	}
	program := tea.NewProgram(m, tea.WithOutput(output), tea.WithContext(runCtx), tea.WithAltScreen())
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
		m.plan = stageIDsToStrings(m.current.ResolvedPlan)
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.KeyMsg:
		return m.updateKey(message)
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.syncInputWidths()
		m.syncBrewListSize()
		m.syncOptionListSize()
		return m, nil
	case spinner.TickMsg:
		if m.executing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(message)
			return m, cmd
		}
	case stopwatch.StartStopMsg:
		var cmd tea.Cmd
		m.stopwatch, cmd = m.stopwatch.Update(message)
		return m, cmd
	case stopwatch.ResetMsg:
		var cmd tea.Cmd
		m.stopwatch, cmd = m.stopwatch.Update(message)
		return m, cmd
	case stopwatch.TickMsg:
		var cmd tea.Cmd
		m.stopwatch, cmd = m.stopwatch.Update(message)
		return m, cmd
	case logTailTickMsg:
		if m.executing {
			m.pollRunLog()
			return m, scheduleLogTailTick()
		}
	case stageStatusMsg:
		m.stageStatuses[message.StageID] = message.Status
		return m, waitForExecutionUpdate(m.updates)
	case failureRequestMsg:
		m.failurePrompt = &message.Request
		m.screen = screenFailure
		return m, nil
	case interactiveCommandRequestMsg:
		m.interactivePrompt = &message.Request
		m.screen = screenInteractive
		return m, nil
	case interactiveCommandFinishedMsg:
		select {
		case message.Request.Response <- message.Result:
		default:
		}
		m.interactivePrompt = nil
		m.screen = screenExecuting
		return m, waitForExecutionUpdate(m.updates)
	case executionDoneMsg:
		m.pollRunLog()
		m.executing = false
		m.runErr = message.Err
		m.failurePrompt = nil
		m.screen = screenSummary
		return m, m.stopwatch.Stop()
	}
	return m, nil
}

func (m model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		if m.screen == screenQuitConfirm {
			m.abortExecutionIfNeeded(execution.ActionAbort)
			return m, tea.Quit
		}
		m.quitPreviousScreen = m.screen
		m.screen = screenQuitConfirm
		return m, nil
	}

	switch m.screen {
	case screenQuitConfirm:
		if key.String() == "esc" {
			m.screen = m.quitPreviousScreen
		}
	case screenWelcome:
		switch key.String() {
		case "enter":
			if m.resumeRun {
				m.screen = screenReview
			} else {
				m.screen = screenMacOS
			}
			m.cursor = 0
		}
	case screenMacOS:
		return m.updateToggleListScreen(key, &m.macOSOptions, screenWelcome, screenInstall)
	case screenInstall:
		return m.updateToggleListScreen(key, &m.installOptions, screenMacOS, screenBrew)
	case screenBrew:
		m.ensureBrewList()
		if m.brewList.SettingFilter() || m.brewList.IsFiltered() {
			switch key.String() {
			case "enter":
				if m.brewList.SettingFilter() {
					var cmd tea.Cmd
					m.brewList, cmd = m.brewList.Update(key)
					return m, cmd
				}
			case "esc":
				var cmd tea.Cmd
				m.brewList, cmd = m.brewList.Update(key)
				m.cursor = m.brewList.GlobalIndex()
				return m, cmd
			}
		}

		switch key.String() {
		case "esc":
			m.screen = screenInstall
			m.cursor = 0
		case " ":
			selected := m.brewList.SelectedItem()
			if item, ok := selected.(brewListItem); ok {
				m.brewSelected[item.ID] = !m.brewSelected[item.ID]
				m.refreshBrewListItems()
			}
		case "enter":
			m.screen = screenDevTools
			m.cursor = 0
		default:
			var cmd tea.Cmd
			m.brewList, cmd = m.brewList.Update(key)
			m.cursor = m.brewList.GlobalIndex()
			return m, cmd
		}
	case screenDevTools:
		return m.updateToggleListScreen(key, &m.devOptions, screenBrew, screenNodeToolchain)
	case screenNodeToolchain:
		return m.updateSelectListScreen(key, &m.nodeSelection, screenDevTools, screenDockerRuntime)
	case screenDockerRuntime:
		return m.updateSelectListScreen(key, &m.dockerSelection, screenNodeToolchain, screenShellOptions)
	case screenShellOptions:
		return m.updateShellOptionsScreen(key)
	case screenGitName:
		switch key.String() {
		case "esc":
			m.inputError = ""
			m.gitNameInput.Blur()
			m.screen = screenShellOptions
			m.cursor = m.optionCursor(m.shellOptions, "git_config")
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.gitNameInput.Value())
			m.inputError = ""
			m.gitNameInput.SetValue(name)
			m.gitNameInput.Blur()
			m.gitEmailInput.Focus()
			m.screen = screenGitEmail
			return m, textinput.Blink
		}
		var cmd tea.Cmd
		m.gitNameInput, cmd = m.gitNameInput.Update(key)
		return m, cmd
	case screenGitEmail:
		switch key.String() {
		case "esc":
			m.inputError = ""
			m.gitEmailInput.Blur()
			m.gitNameInput.Focus()
			m.screen = screenGitName
			return m, textinput.Blink
		case "enter":
			email := strings.TrimSpace(m.gitEmailInput.Value())
			m.inputError = ""
			m.gitEmailInput.SetValue(email)
			m.gitEmailInput.Blur()
			m.screen = screenManual
			m.cursor = 0
			return m, nil
		}
		var cmd tea.Cmd
		m.gitEmailInput, cmd = m.gitEmailInput.Update(key)
		return m, cmd
	case screenManual:
		return m.updateToggleListScreen(key, &m.manualOptions, m.manualBackScreen(), screenReview)
	case screenReview:
		switch key.String() {
		case "esc":
			if m.resumeRun {
				m.screen = screenWelcome
			} else {
				m.screen = screenManual
			}
			m.cursor = 0
		case "enter":
			return m.startExecutionFromReview()
		}
	case screenExecuting:
	case screenInteractive:
		switch key.String() {
		case "enter":
			if m.interactivePrompt != nil {
				return m, runInteractiveCommand(*m.interactivePrompt)
			}
		}
	case screenFailure:
		switch key.String() {
		case "enter":
			m.resolveFailure(execution.ActionRetry)
			m.screen = screenExecuting
			return m, waitForExecutionUpdate(m.updates)
		case " ":
			if m.failurePrompt != nil && m.failurePrompt.CanSkip {
				m.resolveFailure(execution.ActionSkip)
				m.screen = screenExecuting
				return m, waitForExecutionUpdate(m.updates)
			}
		}
	case screenSummary:
		switch key.String() {
		case "enter":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *model) resolvePlan() ([]string, error) {
	if m.resumeRun {
		if m.current == nil {
			return nil, errors.New("resume requested but no existing state is loaded")
		}
		return stageIDsToStrings(m.current.ResolvedPlan), nil
	}

	selectedStages := m.selectedStageIDs()
	onlyIDs := selectedStages
	if len(m.config.Only) > 0 {
		onlyIDs = m.config.Only
	}

	if slices.Contains(onlyIDs, "brew_bundle") && len(m.selectedBrewIDs()) == 0 {
		return nil, fmt.Errorf("%s selected with no package/app entries; select at least one entry or deselect %s",
			m.stageTitle("brew_bundle"),
			m.stageTitle("brew_bundle"),
		)
	}

	typedPlan, err := stages.ResolvePlan(m.catalog, stages.PlanOptions{
		FromID:  m.config.From,
		OnlyIDs: onlyIDs,
		SkipIDs: m.config.Skip,
	})
	if err != nil {
		return nil, err
	}
	return stageIDsToStrings(typedPlan), nil
}

func (m *model) selectedStageIDs() []string {
	selectedSet := make(map[string]struct{})
	collectSelected(selectedSet, m.macOSOptions)
	collectSelected(selectedSet, m.installOptions)
	collectSelected(selectedSet, m.devOptions)
	collectSelected(selectedSet, m.manualOptions)

	ids := make([]string, 0, len(selectedSet))
	for _, stage := range m.catalog {
		if _, ok := selectedSet[stage.ID.String()]; ok {
			ids = append(ids, stage.ID.String())
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

func (m *model) collectDecisions() stages.DecisionSet {
	decisions := stages.DefaultDecisions()
	decisions.SelectedStageIDs = stringsToStageIDs(m.selectedStageIDs())
	decisions.NodeToolchain = stages.NodeToolchain(m.selectedNodeToolchainID())
	decisions.DockerRuntime = stages.DockerRuntime(m.selectedDockerRuntimeID())
	decisions.ShellInstallOhMyZsh = m.shellOptionEnabled(stages.DecisionShellInstallOhMyZsh)
	decisions.ShellApplyZshrc = m.shellOptionEnabled(stages.DecisionShellApplyZshrc)
	decisions.ShellApplyStarship = m.shellOptionEnabled(stages.DecisionShellApplyStarship)
	decisions.GitConfigMode = stages.GitConfigModeTemplate
	decisions.GitUserName = strings.TrimSpace(m.gitNameInput.Value())
	decisions.GitUserEmail = strings.TrimSpace(m.gitEmailInput.Value())
	return decisions
}

func (m *model) effectiveDecisions() stages.DecisionSet {
	if m.resumeRun && m.current != nil {
		return m.current.Decisions
	}
	return m.collectDecisions()
}

func (m *model) selectedNodeToolchainID() string {
	if m.nodeSelection >= 0 && m.nodeSelection < len(m.nodeOptions) {
		return m.nodeOptions[m.nodeSelection].ID
	}
	return string(stages.NodeToolchainVitePlus)
}

func (m *model) selectedDockerRuntimeID() string {
	if m.dockerSelection >= 0 && m.dockerSelection < len(m.dockerOptions) {
		return m.dockerOptions[m.dockerSelection].ID
	}
	return string(stages.DockerRuntimeColima)
}

func (m *model) shellOptionEnabled(id string) bool {
	for _, option := range m.shellOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	return true
}

func stringsToStageIDs(ids []string) []state.StageID {
	out := make([]state.StageID, 0, len(ids))
	for _, id := range ids {
		out = append(out, state.StageID(id))
	}
	return out
}

func stageIDsToStrings(ids []state.StageID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func (m *model) stageSelected(id string) bool {
	for _, option := range m.macOSOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	for _, option := range m.installOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	for _, option := range m.devOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	for _, option := range m.manualOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	return false
}

func (m *model) optionCursor(options []toggleOption, id string) int {
	for index, option := range options {
		if option.ID == id {
			return index
		}
	}
	return 0
}

func (m *model) manualBackScreen() screen {
	if m.stageSelected("git_config") {
		return screenGitEmail
	}
	return screenShellOptions
}

func (m *model) effectiveDryRun() bool {
	if m.resumeRun && m.current != nil {
		return m.current.Mode.IsDryRun()
	}
	return m.config.DryRun
}

func (m *model) syncInputWidths() {
	inputWidth := minInt(72, maxInt(24, m.width-16))
	m.gitNameInput.Width = inputWidth
	m.gitEmailInput.Width = inputWidth
}

func readGitIdentity(homeDir string) (string, string) {
	if strings.TrimSpace(homeDir) == "" {
		return "", ""
	}
	payload, err := os.ReadFile(filepath.Join(homeDir, ".gitconfig"))
	if err != nil {
		return "", ""
	}
	return parseGitIdentity(string(payload))
}

func parseGitIdentity(content string) (string, string) {
	inUser := false
	name := ""
	email := ""
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			inUser = strings.EqualFold(section, "user")
			continue
		}
		if !inUser {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "name":
			name = strings.TrimSpace(value)
		case "email":
			email = strings.TrimSpace(value)
		}
	}
	return name, email
}

func optionsForStageIDs(catalog []stages.Stage, ids []string) []toggleOption {
	stageMap := make(map[string]stages.Stage, len(catalog))
	for _, stage := range catalog {
		stageMap[stage.ID.String()] = stage
	}
	options := make([]toggleOption, 0, len(ids))
	for _, id := range ids {
		stage, ok := stageMap[id]
		if !ok {
			continue
		}
		options = append(options, toggleOption{
			ID:       stage.ID.String(),
			Title:    stage.Title,
			Selected: true,
		})
	}
	return options
}

func modeName(dryRun bool) state.Mode {
	return state.ModeFromDryRun(dryRun)
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

func stageCounts(statuses map[string]state.StageStatus) (completed int, skipped int, failed int) {
	for _, stageStatus := range statuses {
		switch stageStatus.Status {
		case stages.StatusSuccess, stages.StatusAlreadyDone, stages.StatusSimulatedSuccess:
			completed++
		case stages.StatusSkipped:
			skipped++
		case stages.StatusFailed:
			failed++
		}
	}
	return completed, skipped, failed
}

type executionProgress struct {
	CurrentStageID     string
	CurrentStageTitle  string
	CurrentStageStatus string
	CurrentStageIndex  int
	TotalStages        int
	CompletedStages    int
	TerminalStages     int
	PercentComplete    int
}

type dashboardStatus struct {
	Badge                    string
	BadgeTone                lipgloss.TerminalColor
	Heading                  string
	Summary                  string
	ConfigurationProgressPct int
	ExecutionProgressPct     int
	Hint                     string
	Spinner                  bool
}

type dashboardJourney struct {
	StageOrder  []string
	Statuses    map[string]state.StageStatus
	CurrentStep string
}

var configurationScreenOrder = []screen{
	screenWelcome,
	screenMacOS,
	screenInstall,
	screenBrew,
	screenDevTools,
	screenNodeToolchain,
	screenDockerRuntime,
	screenShellOptions,
	screenGitName,
	screenGitEmail,
	screenManual,
	screenReview,
}

func (m model) renderDocument(content string) string {
	width, height := m.viewDimensions()
	canvasWidth := maxInt(20, width-4)
	canvasHeight := maxInt(8, height-2)
	panel := m.panelStyle(canvasWidth, canvasHeight).Render(content)
	framed := lipgloss.Place(canvasWidth, canvasHeight, lipgloss.Left, lipgloss.Top, panel)
	return m.screenStyle(width, height).Render(framed)
}

func (m model) renderDashboard(status dashboardStatus, journey dashboardJourney, output string) string {
	width, height := m.viewDimensions()
	contentWidth := maxInt(20, width-4)
	contentHeight := maxInt(12, height-2)
	columnGap := 2
	shortcutHint := m.renderDashboardShortcutHint(contentWidth, status.Hint)
	shortcutHintHeight := lipgloss.Height(shortcutHint)
	if shortcutHint == "" {
		shortcutHintHeight = 0
	}
	shortcutGapHeight := 0
	if shortcutHint != "" {
		shortcutGapHeight = 1
	}
	headerHeight := maxInt(13, contentHeight/3)
	if headerHeight > contentHeight-6-shortcutHintHeight-shortcutGapHeight {
		headerHeight = maxInt(6, contentHeight-6-shortcutHintHeight-shortcutGapHeight)
	}
	bodyHeight := maxInt(6, contentHeight-headerHeight-1-shortcutHintHeight-shortcutGapHeight)

	titlePanelMinWidth := bootstrapTitleArtWidth() + 6
	statusMinWidth := 20
	titleWidth := maxInt(24, ((contentWidth-columnGap)*2)/3)
	if contentWidth >= titlePanelMinWidth+columnGap+statusMinWidth {
		titleWidth = maxInt(titlePanelMinWidth, titleWidth)
	}
	statusWidth := maxInt(statusMinWidth, contentWidth-titleWidth-columnGap)
	if titleWidth+columnGap+statusWidth > contentWidth {
		titleWidth = maxInt(20, contentWidth-statusWidth-columnGap)
	}

	journeyWidth, outputWidth := dashboardBodyWidths(contentWidth, columnGap)

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderTitlePanel(titleWidth, headerHeight),
		"  ",
		m.renderDashboardStatusPanel(statusWidth, headerHeight, status),
	)
	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.renderJourneyPanel(journeyWidth, bodyHeight, journey),
		"  ",
		m.renderOutputPanel(outputWidth, bodyHeight, output),
	)

	blocks := []string{header, "", body}
	if shortcutHint != "" {
		blocks = append(blocks, "", shortcutHint)
	}
	layout := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	framed := lipgloss.Place(contentWidth, contentHeight, lipgloss.Left, lipgloss.Top, layout)
	return m.screenStyle(width, height).Render(framed)
}

func dashboardBodyWidths(contentWidth int, columnGap int) (int, int) {
	availableWidth := contentWidth - columnGap
	journeyWidth := maxInt(24, (availableWidth*2)/5)
	outputWidth := maxInt(24, contentWidth-journeyWidth-columnGap)
	if journeyWidth+columnGap+outputWidth > contentWidth {
		journeyWidth = maxInt(20, contentWidth-outputWidth-columnGap)
	}
	return journeyWidth, outputWidth
}

func (m model) renderTitlePanel(width int, height int) string {
	innerWidth := panelInnerWidth(width)
	innerHeight := panelInnerHeight(height)
	tagline := lipgloss.NewStyle().
		Foreground(mutedColor).
		Render(truncateLine("Initiating CHAPEAUX, stand by for awesomeness...", innerWidth))

	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("BOOTSTRAP")}
	switch {
	case innerWidth >= bootstrapTitleArtWidth() && innerHeight >= len(bootstrapTitleArtLines)+2:
		lines = renderBootstrapTitleArt(bootstrapTitleArtLines)
	case innerWidth >= titleArtWidth(bootstrapCompactTitleArtLines) && innerHeight >= len(bootstrapCompactTitleArtLines)+2:
		lines = renderBootstrapTitleArt(bootstrapCompactTitleArtLines)
	}
	lines = append(lines, "", tagline)
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return m.panelStyle(width, height).
		Align(lipgloss.Left).
		AlignVertical(lipgloss.Center).
		Render(content)
}

func (m model) renderDashboardStatusPanel(width int, height int, status dashboardStatus) string {
	innerWidth := panelInnerWidth(width)
	barWidth := maxInt(10, minInt(24, innerWidth-2))
	statusBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(status.BadgeTone).
		Render(strings.ToUpper(status.Badge))
	badgeLine := statusBadge
	if status.Spinner {
		badgeLine = lipgloss.JoinHorizontal(lipgloss.Center, statusBadge, " ", lipgloss.NewStyle().Foreground(mutedColor).Render(m.spinner.View()))
	}
	badgeLine = m.renderStatusPanelTopLine(innerWidth, badgeLine)
	lines := []string{
		badgeLine,
		"",
		truncateLine(status.Heading, innerWidth),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(accentAltColor).Render("Plan"),
		renderProgressBar(barWidth, status.ConfigurationProgressPct),
		"",
		lipgloss.NewStyle().Bold(true).Foreground(accentAltColor).Render("Apply"),
		renderProgressBar(barWidth, status.ExecutionProgressPct),
	}
	return m.panelStyle(width, height).Render(strings.Join(limitLines(lines, panelInnerHeight(height)), "\n"))
}

func (m model) renderStatusPanelTopLine(width int, badgeLine string) string {
	elapsed := m.statusPanelElapsed()
	if elapsed == "" {
		return badgeLine
	}
	elapsed = lipgloss.NewStyle().Foreground(mutedColor).Render(elapsed)
	elapsedWidth := lipgloss.Width(elapsed)
	if width <= elapsedWidth {
		return truncateLine(elapsed, width)
	}
	badgeWidth := maxInt(1, width-elapsedWidth-1)
	badge := truncateLine(badgeLine, badgeWidth)
	gap := maxInt(1, width-lipgloss.Width(badge)-elapsedWidth)
	return lipgloss.JoinHorizontal(lipgloss.Center, badge, strings.Repeat(" ", gap), elapsed)
}

func (m model) statusPanelElapsed() string {
	if m.runState == nil {
		return ""
	}
	switch m.screen {
	case screenExecuting, screenInteractive, screenFailure, screenSummary:
		return formatElapsed(m.stopwatch.Elapsed())
	default:
		return ""
	}
}

func (m model) renderDashboardShortcutHint(width int, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	bindings := shortcutBindingsForHint(hint)
	if len(bindings) == 0 {
		return ""
	}
	shortcutHelp := m.help
	if shortcutHelp.ShortSeparator == "" {
		shortcutHelp = newShortcutHelp()
	}
	shortcutHelp.Width = width
	helpLine := shortcutHelp.ShortHelpView(bindings)
	return lipgloss.NewStyle().
		Width(maxInt(1, width)).
		MaxWidth(maxInt(1, width)).
		Align(lipgloss.Center).
		Foreground(mutedColor).
		Render(helpLine)
}

func newShortcutHelp() help.Model {
	h := help.New()
	h.Styles.ShortKey = h.Styles.ShortKey.Foreground(pendingToneColor)
	h.Styles.ShortDesc = h.Styles.ShortDesc.Foreground(dimColor)
	h.Styles.ShortSeparator = h.Styles.ShortSeparator.Foreground(dimColor)
	h.Styles.Ellipsis = h.Styles.Ellipsis.Foreground(dimColor)
	return h
}

func shortcutBindingsForHint(hint string) []key.Binding {
	switch hint {
	case "Enter execute  Esc back  CTRL+C quit":
		return []key.Binding{
			shortcutBinding("enter", "execute"),
			shortcutBinding("esc", "back"),
			shortcutBinding("ctrl+c", "quit"),
		}
	case "Enter continue  CTRL+C quit":
		return []key.Binding{
			shortcutBinding("enter", "continue"),
			shortcutBinding("ctrl+c", "quit"),
		}
	case "Up/down move  Space toggle  / filter  Enter continue  Esc back  CTRL+C quit":
		return []key.Binding{
			shortcutBinding("↑/k", "move"),
			shortcutBinding("space", "toggle"),
			shortcutBinding("/", "filter"),
			shortcutBinding("enter", "continue"),
			shortcutBinding("esc", "back"),
			shortcutBinding("ctrl+c", "quit"),
		}
	case "Up/down choose  Enter continue  Esc back  CTRL+C quit":
		return []key.Binding{
			shortcutBinding("↑/↓", "choose"),
			shortcutBinding("enter", "continue"),
			shortcutBinding("esc", "back"),
			shortcutBinding("ctrl+c", "quit"),
		}
	case "CTRL+C quit  Esc return":
		return []key.Binding{
			shortcutBinding("ctrl+c", "quit"),
			shortcutBinding("esc", "return"),
		}
	case "CTRL+C abort":
		return []key.Binding{
			shortcutBinding("ctrl+c", "abort"),
		}
	case "Enter retry  Space skip  CTRL+C abort":
		return []key.Binding{
			shortcutBinding("enter", "retry"),
			shortcutBinding("space", "skip"),
			shortcutBinding("ctrl+c", "abort"),
		}
	case "Enter exit  CTRL+C quit":
		return []key.Binding{
			shortcutBinding("enter", "exit"),
			shortcutBinding("ctrl+c", "quit"),
		}
	default:
		if strings.Contains(hint, "Space") {
			return []key.Binding{
				shortcutBinding("space", "toggle"),
				shortcutBinding("enter", "continue"),
				shortcutBinding("esc", "back"),
				shortcutBinding("ctrl+c", "quit"),
			}
		}
		return []key.Binding{
			shortcutBinding("enter", "continue"),
			shortcutBinding("esc", "back"),
			shortcutBinding("ctrl+c", "quit"),
		}
	}
}

func shortcutBinding(keyText string, helpText string) key.Binding {
	return key.NewBinding(
		key.WithKeys(keyText),
		key.WithHelp(keyText, helpText),
	)
}

func formatElapsed(elapsed time.Duration) string {
	return fmt.Sprintf("%.3fs", elapsed.Seconds())
}

func (m model) renderJourneyPanel(width int, height int, journey dashboardJourney) string {
	innerWidth := panelInnerWidth(width)
	lineBudget := panelInnerHeight(height)
	lines := make([]string, 0, maxInt(1, len(journey.StageOrder)))
	for _, stageID := range journey.StageOrder {
		status := normalizedStageStatus(journey.Statuses[stageID])
		lines = append(lines, m.renderJourneyLine(innerWidth, stageID, journey.CurrentStep, status))
	}
	if len(journey.StageOrder) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedColor).Render("No stages selected yet"))
	}
	lines = limitLines(lines, lineBudget)
	return m.panelStyle(width, height).Render(strings.Join(lines, "\n"))
}

func (m model) renderJourneyLine(width int, stageID string, currentStep string, status string) string {
	if width <= 0 {
		return ""
	}

	prefix := lipgloss.NewStyle().Foreground(statusTone(status)).Render(stageStatusMarker(status))
	if stageID == currentStep && !isCompleteStageStatus(status) {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(accentAltColor).Render(">")
	}
	state := lipgloss.NewStyle().Foreground(statusTone(status)).Render(statusLabel(status))
	stateWidth := lipgloss.Width(state)
	if width <= stateWidth+1 {
		return truncateLine(statusLabel(status), width)
	}

	leftFixedWidth := lipgloss.Width(prefix) + 1
	titleWidth := maxInt(1, width-leftFixedWidth-stateWidth-1)
	title := lipgloss.NewStyle().Foreground(textColor).Render(truncateLine(m.stageTitle(stageID), titleWidth))
	left := lipgloss.JoinHorizontal(lipgloss.Center, prefix, " ", title)
	gap := maxInt(1, width-lipgloss.Width(left)-stateWidth)
	return lipgloss.JoinHorizontal(lipgloss.Center, left, strings.Repeat(" ", gap), state)
}

func stageStatusMarker(status string) string {
	if isCompleteStageStatus(status) {
		return "✓"
	}
	return "•"
}

func isCompleteStageStatus(status string) bool {
	switch status {
	case string(stages.StatusSuccess), string(stages.StatusAlreadyDone), string(stages.StatusSimulatedSuccess):
		return true
	default:
		return false
	}
}

func (m model) renderOutputPanel(width int, height int, content string) string {
	lines := strings.Split(content, "\n")
	visible := limitLines(lines, panelInnerHeight(height))
	return m.panelStyle(width, height).Render(strings.Join(visible, "\n"))
}

func (m model) standardOutputContent(content string) string {
	if strings.TrimSpace(content) != "" {
		return content
	}
	return ""
}

func (m model) executionOutput(currentStageID string) string {
	lines := []string{}
	if currentStageID != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedColor).Render("Stage: "+m.stageTitle(currentStageID)))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedColor).Render("Stage waiting"))
	}

	logLines := m.filteredDisplayLogLines(currentStageID, displayedLogLineLimit)
	if len(logLines) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(mutedColor).Render("(waiting for events)"))
	} else {
		for _, line := range logLines {
			lines = append(lines, lipgloss.NewStyle().Foreground(textColor).Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) filteredDisplayLogLines(stageID string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	displayLines := make([]string, 0, limit)
	for _, line := range m.tailedLogs {
		if stageID != "" && line.StageID != stageID {
			continue
		}
		displayLines = append(displayLines, m.displayLogLine(line))
	}
	if len(displayLines) <= limit {
		return displayLines
	}
	return append([]string(nil), displayLines[len(displayLines)-limit:]...)
}

func (m model) displayLogLine(line tailedLogLine) string {
	if line.StageID == "" {
		return line.Line
	}
	title := m.stageTitle(line.StageID)
	parts := strings.Split(line.Line, " | ")
	if len(parts) >= 4 && strings.TrimSpace(parts[2]) == line.StageID {
		parts[2] = title
		return strings.Join(parts, " | ")
	}
	return strings.ReplaceAll(line.Line, line.StageID, title)
}

func (m model) panelStyle(width int, height int) lipgloss.Style {
	style := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Foreground(textColor)
	return style.
		Width(maxInt(1, width-2)).
		Height(maxInt(1, height-2))
}

func (m model) executionProgress() executionProgress {
	progress := executionProgress{
		CurrentStageStatus: string(stages.StatusPending),
		TotalStages:        len(m.stageOrder),
	}
	if len(m.stageOrder) == 0 {
		progress.CurrentStageTitle = "Waiting for execution plan"
		return progress
	}

	firstPendingIndex := -1
	for index, stageID := range m.stageOrder {
		status := normalizedStageStatus(m.stageStatuses[stageID])
		if isTerminalStageStatus(status) {
			progress.TerminalStages++
		}
		if status == string(stages.StatusSuccess) || status == string(stages.StatusAlreadyDone) || status == string(stages.StatusSimulatedSuccess) {
			progress.CompletedStages++
		}
		if status == string(stages.StatusRunning) {
			progress.CurrentStageID = stageID
			progress.CurrentStageTitle = m.stageTitle(stageID)
			progress.CurrentStageStatus = status
			progress.CurrentStageIndex = index + 1
		}
		if firstPendingIndex == -1 && status == string(stages.StatusPending) {
			firstPendingIndex = index
		}
	}

	if progress.CurrentStageID == "" {
		switch {
		case firstPendingIndex >= 0:
			stageID := m.stageOrder[firstPendingIndex]
			progress.CurrentStageID = stageID
			progress.CurrentStageTitle = m.stageTitle(stageID)
			progress.CurrentStageIndex = firstPendingIndex + 1
		default:
			lastIndex := len(m.stageOrder) - 1
			stageID := m.stageOrder[lastIndex]
			progress.CurrentStageID = stageID
			progress.CurrentStageTitle = m.stageTitle(stageID)
			progress.CurrentStageStatus = normalizedStageStatus(m.stageStatuses[stageID])
			progress.CurrentStageIndex = lastIndex + 1
		}
	}

	if progress.CurrentStageStatus == "" {
		progress.CurrentStageStatus = normalizedStageStatus(m.stageStatuses[progress.CurrentStageID])
	}
	if progress.CurrentStageStatus == "" {
		progress.CurrentStageStatus = string(stages.StatusPending)
	}
	progress.PercentComplete = progress.TerminalStages * 100 / maxInt(1, progress.TotalStages)
	return progress
}

func (m model) executionDashboardStatus(progress executionProgress) dashboardStatus {
	return dashboardStatus{
		Badge:                    humanizeStatus(progress.CurrentStageStatus),
		BadgeTone:                statusTone(progress.CurrentStageStatus),
		Heading:                  progress.CurrentStageTitle,
		Summary:                  fmt.Sprintf("%d of %d  %d finished", progress.CurrentStageIndex, progress.TotalStages, progress.CompletedStages),
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     progress.PercentComplete,
		Hint:                     "CTRL+C abort",
		Spinner:                  true,
	}
}

func (m model) interactiveDashboardStatus() dashboardStatus {
	progress := m.executionProgress()
	return dashboardStatus{
		Badge:                    "Action",
		BadgeTone:                warningColor,
		Heading:                  "Terminal authorization",
		Summary:                  "run command with terminal input",
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     progress.PercentComplete,
		Hint:                     "Enter continue  CTRL+C abort",
	}
}

func (m model) configurationDashboardStatus() dashboardStatus {
	stepIndex, totalSteps := configurationStepPosition(m.screen)
	badge := "Configuring"
	badgeTone := accentAltColor
	hint := configurationShortcutHint(m.screen)
	if m.screen == screenReview {
		badge = "Ready"
		badgeTone = successColor
	} else if m.screen == screenWelcome {
		badge = "Ready"
		badgeTone = accentColor
	} else if m.screen == screenQuitConfirm {
		badge = "Confirm"
		badgeTone = warningColor
	}
	return dashboardStatus{
		Badge:                    badge,
		BadgeTone:                badgeTone,
		Heading:                  screenTitle(m.screen),
		Summary:                  fmt.Sprintf("%d of %d", stepIndex, totalSteps),
		ConfigurationProgressPct: configurationProgressPercent(m.screen),
		ExecutionProgressPct:     m.executionProgress().PercentComplete,
		Hint:                     hint,
	}
}

func configurationShortcutHint(current screen) string {
	switch current {
	case screenWelcome:
		return "Enter continue  CTRL+C quit"
	case screenMacOS, screenInstall, screenDevTools, screenShellOptions, screenManual:
		return "Up/down move  Space toggle  / filter  Enter continue  Esc back  CTRL+C quit"
	case screenBrew:
		return "Up/down move  Space toggle  / filter  Enter continue  Esc back  CTRL+C quit"
	case screenNodeToolchain, screenDockerRuntime:
		return "Up/down choose  / filter  Enter continue  Esc back  CTRL+C quit"
	case screenReview:
		return "Enter execute  Esc back  CTRL+C quit"
	case screenQuitConfirm:
		return "CTRL+C quit  Esc return"
	default:
		return "Enter continue  Esc back  CTRL+C quit"
	}
}

func (m model) failureDashboardStatus() dashboardStatus {
	stageID := ""
	attempt := 0
	if m.failurePrompt != nil {
		stageID = m.failurePrompt.StageID
		attempt = m.failurePrompt.Attempt
	}
	heading := "Stage failure"
	if stageID != "" {
		heading = m.stageTitle(stageID)
	}
	return dashboardStatus{
		Badge:                    "Failed",
		BadgeTone:                failureColor,
		Heading:                  heading,
		Summary:                  fmt.Sprintf("attempt %d  choose retry, skip, or abort", attempt),
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     m.executionProgress().PercentComplete,
		Hint:                     "Enter retry  Space skip  CTRL+C abort",
	}
}

func (m model) summaryDashboardStatus() dashboardStatus {
	completed, skipped, failed := stageCounts(m.stageStatuses)
	badge := "Completed"
	badgeTone := successColor
	heading := "Run finished"
	if m.runErr != nil {
		if errors.Is(m.runErr, execution.ErrAborted) || errors.Is(m.runErr, context.Canceled) {
			badge = "Aborted"
			badgeTone = warningColor
			heading = "Run aborted"
		} else {
			badge = "Failed"
			badgeTone = failureColor
			heading = "Run failed"
		}
	}
	return dashboardStatus{
		Badge:                    badge,
		BadgeTone:                badgeTone,
		Heading:                  heading,
		Summary:                  fmt.Sprintf("%d completed  %d skipped  %d failed", completed, skipped, failed),
		ConfigurationProgressPct: 100,
		ExecutionProgressPct:     m.executionProgress().PercentComplete,
		Hint:                     "Enter exit  CTRL+C quit",
	}
}

func (m model) previewJourney() dashboardJourney {
	if len(m.stageOrder) > 0 {
		currentStep := ""
		if m.executing || m.screen == screenInteractive || m.screen == screenFailure || m.screen == screenSummary {
			currentStep = m.executionProgress().CurrentStageID
		}
		return dashboardJourney{
			StageOrder:  slices.Clone(m.stageOrder),
			Statuses:    cloneStatuses(m.stageStatuses),
			CurrentStep: currentStep,
		}
	}

	if m.resumeRun && m.current != nil && len(m.current.ResolvedPlan) > 0 {
		return dashboardJourney{
			StageOrder: stageIDsToStrings(m.current.ResolvedPlan),
			Statuses:   stageStatusMapToStrings(m.current.Stages),
		}
	}

	plan, err := m.resolvePlan()
	if err != nil {
		plan = m.selectedStageIDs()
	}
	return dashboardJourney{
		StageOrder: plan,
		Statuses:   cloneStatuses(m.stageStatuses),
	}
}

func detectAlreadyDoneStages(
	ctx context.Context,
	catalog []stages.Stage,
	commandRunner runner.CommandRunner,
	templateStore stages.TemplateStore,
	repoRoot string,
	homeDir string,
) map[string]state.StageStatus {
	statuses := make(map[string]state.StageStatus)
	if commandRunner == nil {
		commandRunner = runner.NewOSCommandRunner()
	}
	for _, stage := range catalog {
		if stage.Precheck == nil {
			continue
		}
		result, err := stage.Precheck(ctx, stages.ExecutionContext{
			Runner:        commandRunner,
			StageID:       stage.ID,
			RepoRoot:      repoRoot,
			HomeDir:       homeDir,
			TemplateStore: templateStore,
			Decisions:     stages.DefaultDecisions(),
		})
		if err != nil || !result.Satisfied {
			continue
		}
		statuses[stage.ID.String()] = state.StageStatus{
			Status: stages.StatusAlreadyDone,
		}
	}
	return statuses
}

func cloneStatuses(statuses map[string]state.StageStatus) map[string]state.StageStatus {
	if len(statuses) == 0 {
		return map[string]state.StageStatus{}
	}
	cloned := make(map[string]state.StageStatus, len(statuses))
	for key, value := range statuses {
		cloned[key] = value
	}
	return cloned
}

func stageStatusMapToStrings(statuses map[state.StageID]state.StageStatus) map[string]state.StageStatus {
	if len(statuses) == 0 {
		return map[string]state.StageStatus{}
	}
	out := make(map[string]state.StageStatus, len(statuses))
	for stageID, status := range statuses {
		out[stageID.String()] = status
	}
	return out
}

func configurationStepPosition(current screen) (int, int) {
	total := len(configurationScreenOrder)
	for index, candidate := range configurationScreenOrder {
		if current == candidate {
			return index + 1, total
		}
	}
	return total, total
}

func configurationProgressPercent(current screen) int {
	totalTransitions := maxInt(1, len(configurationScreenOrder)-1)
	for index, candidate := range configurationScreenOrder {
		if current == candidate {
			return index * 100 / totalTransitions
		}
	}
	return 100
}

func screenTitle(current screen) string {
	switch current {
	case screenWelcome:
		return "Interactive Setup"
	case screenMacOS:
		return "MacOS Setup"
	case screenInstall:
		return "Install Apps/Packages"
	case screenBrew:
		return "Package & App Selection"
	case screenDevTools:
		return "Dev Tools Setup"
	case screenNodeToolchain:
		return "Dev Tools: Node Toolchain"
	case screenDockerRuntime:
		return "Dev Tools: Docker Runtime"
	case screenShellOptions:
		return "Dev Tools: Shell Setup Options"
	case screenGitName:
		return "Git Identity: user.name"
	case screenGitEmail:
		return "Git Identity: user.email"
	case screenManual:
		return "Manual Steps"
	case screenReview:
		return "Execution Plan Review"
	case screenExecuting:
		return "Executing Plan"
	case screenInteractive:
		return "Terminal Authorization"
	case screenFailure:
		return "Stage Failure"
	case screenSummary:
		return "Run Summary"
	case screenQuitConfirm:
		return "Quit Confirmation"
	default:
		return "Laptop Setup"
	}
}

func (m model) stageTitle(stageID string) string {
	if stage, ok := m.stageMap[stageID]; ok && strings.TrimSpace(stage.Title) != "" {
		return stage.Title
	}
	return stageID
}

func (m model) screenStyle(width int, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Padding(1, 2).
		Foreground(textColor)
}

func (m model) viewDimensions() (int, int) {
	width := m.width
	height := m.height
	if width <= 0 {
		width = defaultViewWidth
	}
	if height <= 0 {
		height = defaultViewHeight
	}
	return maxInt(60, width), maxInt(20, height)
}

func normalizedStageStatus(status state.StageStatus) string {
	if status.Status == "" {
		return string(stages.StatusPending)
	}
	return status.Status.String()
}

func isTerminalStageStatus(status string) bool {
	switch status {
	case string(stages.StatusSuccess),
		string(stages.StatusAlreadyDone),
		string(stages.StatusSimulatedSuccess),
		string(stages.StatusSkipped),
		string(stages.StatusFailed):
		return true
	default:
		return false
	}
}

func humanizeStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return "Pending"
	}
	words := strings.Fields(strings.ReplaceAll(status, "_", " "))
	for index, word := range words {
		if len(word) == 0 {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func statusTone(status string) lipgloss.TerminalColor {
	switch status {
	case string(stages.StatusSuccess), string(stages.StatusAlreadyDone), string(stages.StatusSimulatedSuccess):
		return successColor
	case string(stages.StatusRunning):
		return accentAltColor
	case string(stages.StatusSkipped):
		return warningColor
	case string(stages.StatusFailed):
		return failureColor
	default:
		return pendingToneColor
	}
}

func statusLabel(status string) string {
	switch status {
	case string(stages.StatusSuccess):
		return "done"
	case string(stages.StatusSimulatedSuccess):
		return "sim"
	case string(stages.StatusSkipped):
		return "skip"
	case string(stages.StatusFailed):
		return "fail"
	case string(stages.StatusAlreadyDone):
		return "ready"
	case string(stages.StatusRunning):
		return "live"
	default:
		return "next"
	}
}

func renderProgressBar(width int, percent int) string {
	width = maxInt(10, width)
	percent = maxInt(0, minInt(100, percent))
	bar := progress.New(
		progress.WithWidth(width),
		progress.WithDefaultGradient(),
	)
	bar.EmptyColor = "#30303A"
	bar.PercentageStyle = lipgloss.NewStyle().Foreground(mutedColor)
	if percent == 100 {
		bar.FullColor = "#34D399"
		bar = progress.New(
			progress.WithWidth(width),
			progress.WithSolidFill("#34D399"),
		)
		bar.EmptyColor = "#30303A"
		bar.PercentageStyle = lipgloss.NewStyle().Foreground(mutedColor)
	}
	return bar.ViewAs(float64(percent) / 100)
}

func limitLines(lines []string, maxLines int) []string {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines
	}
	return append([]string(nil), lines[:maxLines]...)
}

func truncateLine(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func bootstrapTitleArtWidth() int {
	return titleArtWidth(bootstrapTitleArtLines)
}

func titleArtWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = maxInt(width, lipgloss.Width(line))
	}
	return width
}

func renderBootstrapTitleArt(lines []string) []string {
	palette := []lipgloss.TerminalColor{
		accentColor,
		accentAltColor,
		successColor,
		warningColor,
		failureColor,
		accentAltColor,
	}
	rendered := make([]string, 0, len(lines))
	for index, line := range lines {
		rendered = append(rendered, lipgloss.NewStyle().
			Bold(true).
			Foreground(palette[index%len(palette)]).
			Render(line))
	}
	return rendered
}

func (m model) outputPanelLineBudget() int {
	_, height := m.outputPanelInnerSize()
	return height
}

func (m model) outputPanelInnerSize() (int, int) {
	width, height := m.viewDimensions()
	contentWidth := maxInt(20, width-4)
	contentHeight := maxInt(12, height-2)
	columnGap := 2
	shortcutHintHeight := 1
	shortcutGapHeight := 1
	headerHeight := maxInt(13, contentHeight/3)
	if headerHeight > contentHeight-6-shortcutHintHeight-shortcutGapHeight {
		headerHeight = maxInt(6, contentHeight-6-shortcutHintHeight-shortcutGapHeight)
	}
	bodyHeight := maxInt(6, contentHeight-headerHeight-1-shortcutHintHeight-shortcutGapHeight)
	_, outputWidth := dashboardBodyWidths(contentWidth, columnGap)
	return panelInnerWidth(outputWidth), panelInnerHeight(bodyHeight)
}

func panelInnerWidth(width int) int {
	return maxInt(1, width-6)
}

func panelInnerHeight(height int) int {
	return maxInt(1, height-4)
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
