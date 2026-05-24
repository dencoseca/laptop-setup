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
	screenNodeToolchain
	screenDockerRuntime
	screenShellOptions
	screenGitName
	screenGitEmail
	screenManual
	screenReview
	screenExecuting
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

type brewListItem struct {
	ID       string
	Kind     string
	Selected bool
}

func (i brewListItem) Title() string {
	return fmt.Sprintf("%s %s", selectionMarker(i.Selected), i.ID)
}

func (i brewListItem) Description() string {
	return i.Kind
}

func (i brewListItem) FilterValue() string {
	return strings.Join([]string{i.ID, i.Kind}, " ")
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

	nodeSelection   int
	dockerSelection int
	gitNameInput    textinput.Model
	gitEmailInput   textinput.Model
	inputError      string

	plan          []string
	planError     string
	stageStatuses map[string]state.StageStatus
	stageOrder    []string
	tailedLogs    []tailedLogLine
	logTailOffset int64
	logTailCarry  string
	updates       chan tea.Msg
	failurePrompt *failureRequest
	runState      *state.RunState
	humanLogPath  string
	eventsLogPath string
	runErr        error
	executing     bool
	spinner       spinner.Model
	help          help.Model
	stopwatch     stopwatch.Model
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

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stageMap := make(map[string]stages.Stage, len(options.Catalog))
	for _, stage := range options.Catalog {
		stageMap[stage.ID] = stage
	}

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	shortcutHelp := newShortcutHelp()
	elapsed := stopwatch.New()
	currentGitName, currentGitEmail := readGitIdentity(options.HomeDir)
	gitNameInput := textinput.New()
	gitNameInput.Placeholder = "Git user.name"
	gitNameInput.CharLimit = 128
	gitNameInput.Prompt = "> "
	gitNameInput.SetValue(currentGitName)

	gitEmailInput := textinput.New()
	gitEmailInput.Placeholder = "Git user.email"
	gitEmailInput.CharLimit = 128
	gitEmailInput.Prompt = "> "
	gitEmailInput.SetValue(currentGitEmail)

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
		nodeOptions: []selectOption{
			{ID: stages.NodeToolchainVitePlus, Title: "vite+"},
			{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima"},
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
		stageStatuses: detectAlreadyDoneStages(runCtx, options.Catalog, options.Commander, options.RepoRoot, options.HomeDir),
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
		m.plan = slices.Clone(m.current.ResolvedPlan)
	}
	return m.stopwatch.Init()
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
	case executionDoneMsg:
		m.pollRunLog()
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
		return m.updateToggleScreen(key, &m.macOSOptions, screenWelcome, screenInstall)
	case screenInstall:
		return m.updateToggleScreen(key, &m.installOptions, screenMacOS, screenBrew)
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
		switch key.String() {
		case "esc":
			m.screen = screenBrew
			m.cursor = 0
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.devOptions)-1 {
				m.cursor++
			}
		case " ":
			if len(m.devOptions) > 0 {
				m.devOptions[m.cursor].Selected = !m.devOptions[m.cursor].Selected
			}
		case "enter":
			m.screen = screenNodeToolchain
			m.cursor = m.nodeSelection
		}
	case screenNodeToolchain:
		switch key.String() {
		case "esc":
			m.screen = screenDevTools
			m.cursor = 0
		case "up":
			m.updateRadioSelection(len(m.nodeOptions), &m.nodeSelection, -1)
		case "down":
			m.updateRadioSelection(len(m.nodeOptions), &m.nodeSelection, 1)
		case " ":
			if len(m.nodeOptions) > 0 {
				m.nodeSelection = m.cursor
			}
		case "enter":
			if len(m.nodeOptions) > 0 {
				m.nodeSelection = m.cursor
			}
			m.screen = screenDockerRuntime
			m.cursor = m.dockerSelection
		}
	case screenDockerRuntime:
		switch key.String() {
		case "esc":
			m.screen = screenNodeToolchain
			m.cursor = m.nodeSelection
		case "up":
			m.updateRadioSelection(len(m.dockerOptions), &m.dockerSelection, -1)
		case "down":
			m.updateRadioSelection(len(m.dockerOptions), &m.dockerSelection, 1)
		case " ":
			if len(m.dockerOptions) > 0 {
				m.dockerSelection = m.cursor
			}
		case "enter":
			if len(m.dockerOptions) > 0 {
				m.dockerSelection = m.cursor
			}
			m.screen = screenShellOptions
			m.cursor = 0
		}
	case screenShellOptions:
		switch key.String() {
		case "esc":
			m.screen = screenDockerRuntime
			m.cursor = m.dockerSelection
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.shellOptions)-1 {
				m.cursor++
			}
		case " ":
			if len(m.shellOptions) > 0 {
				m.shellOptions[m.cursor].Selected = !m.shellOptions[m.cursor].Selected
			}
		case "enter":
			m.inputError = ""
			if m.stageSelected("git_config") {
				m.screen = screenGitName
				m.gitNameInput.Focus()
				return m, textinput.Blink
			}
			m.screen = screenManual
			m.cursor = 0
		}
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
			if name == "" {
				m.inputError = "Git user.name is required."
				return m, nil
			}
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
			if email == "" {
				m.inputError = "Git user.email is required."
				return m, nil
			}
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
		return m.updateToggleScreen(key, &m.manualOptions, m.manualBackScreen(), screenReview)
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
			m.tailedLogs = nil
			m.logTailOffset = 0
			m.logTailCarry = ""
			m.updates = make(chan tea.Msg, 32)
			return m, tea.Batch(
				startExecutionWorker(m.ctx, m.updates, setup, m.store, m.catalog, m.repoRoot, m.homeDir, m.runner),
				waitForExecutionUpdate(m.updates),
				scheduleLogTailTick(),
				m.spinner.Tick,
			)
		}
	case screenExecuting:
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

func (m *model) updateRadioSelection(optionCount int, selected *int, delta int) {
	if delta < 0 && m.cursor > 0 {
		m.cursor--
	}
	if delta > 0 && m.cursor < optionCount-1 {
		m.cursor++
	}
	if optionCount > 0 {
		*selected = m.cursor
	}
}

func (m *model) updateToggleScreen(
	key tea.KeyMsg,
	options *[]toggleOption,
	backScreen screen,
	nextScreen screen,
) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = backScreen
		m.cursor = 0
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
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
	var content string
	switch m.screen {
	case screenWelcome:
		content = m.viewWelcome()
	case screenMacOS:
		content = m.viewToggleOptions("MacOS Setup", m.macOSOptions)
	case screenInstall:
		content = m.viewToggleOptions("Install Apps/Packages", m.installOptions)
	case screenBrew:
		content = m.viewBrewSelection()
	case screenDevTools:
		content = m.viewToggleOptions("Dev Tools Setup", m.devOptions)
	case screenNodeToolchain:
		content = m.viewSelectOptions("Dev Tools: Node Toolchain", m.nodeOptions, m.nodeSelection)
	case screenDockerRuntime:
		content = m.viewSelectOptions("Dev Tools: Docker Runtime", m.dockerOptions, m.dockerSelection)
	case screenShellOptions:
		content = m.viewToggleOptions("Dev Tools: Shell Setup Options", m.shellOptions)
	case screenGitName:
		content = m.viewTextInput("Git Identity: user.name", "Enter git user.name, then press Enter.", m.gitNameInput)
	case screenGitEmail:
		content = m.viewTextInput("Git Identity: user.email", "Enter git user.email, then press Enter.", m.gitEmailInput)
	case screenManual:
		content = m.viewToggleOptions("Manual Steps", m.manualOptions)
	case screenReview:
		content = m.viewReview()
	case screenExecuting:
		return m.viewExecuting()
	case screenFailure:
		return m.viewFailureScreen()
	case screenSummary:
		return m.viewSummaryScreen()
	case screenQuitConfirm:
		content = m.viewQuitConfirm()
	default:
		content = ""
	}
	return m.viewConfigFlow(content)
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
	fmt.Fprintf(&b, "Press CTRL+C to quit.")
	return b.String()
}

func (m model) viewToggleOptions(title string, options []toggleOption) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "Toggle stages with Space. Enter to continue, Esc to go back.\n\n")
	for index, option := range options {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s %s\n", prefix, m.toggleOptionMarker(option), option.Title)
	}
	return b.String()
}

func (m model) viewSelectOptions(title string, options []selectOption, selected int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "Use Up/Down to choose. Enter to continue, Esc to go back.\n\n")
	for index, option := range options {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%s %s\n", prefix, radioMarker(selected == index), option.Title)
	}
	return b.String()
}

func (m model) viewBrewSelection() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Brew Catalog Selection"))

	if len(m.brewEntries) == 0 {
		fmt.Fprintf(&b, "No Brew entries found in templates/Brewfile.\n")
		return b.String()
	}

	selector := m.brewListForView()
	fmt.Fprintf(&b, "%s", selector.View())
	return b.String()
}

func (m model) viewTextInput(title string, subtitle string, input textinput.Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "%s\n\n", subtitle)
	fmt.Fprintf(&b, "%s\n", input.View())
	if m.inputError != "" {
		fmt.Fprintf(&b, "\n%s\n", m.inputError)
	}
	fmt.Fprintf(&b, "\nPress Enter to continue, Esc to go back.")
	return b.String()
}

func (m model) viewQuitConfirm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Quit Laptop Setup"))
	fmt.Fprintf(&b, "Press `CTRL + C` again to quit.\n\n")
	fmt.Fprintf(&b, "Press Esc to return.")
	return b.String()
}

func selectionMarker(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("●")
	}
	return lipgloss.NewStyle().Foreground(mutedColor).Render("○")
}

func (m model) toggleOptionMarker(option toggleOption) string {
	status := m.stageStatuses[option.ID].Status
	if isCompleteStageStatus(status) {
		return lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("✓")
	}
	return selectionMarker(option.Selected)
}

func radioMarker(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("●")
	}
	return lipgloss.NewStyle().Foreground(mutedColor).Render("○")
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
	decisions := m.effectiveDecisions()
	fmt.Fprintf(&b, "Node toolchain: %s\n", selectOptionTitle(m.nodeOptions, stages.NodeToolchainFromDecisions(decisions)))
	fmt.Fprintf(&b, "Docker runtime: %s\n", selectOptionTitle(m.dockerOptions, stages.DockerRuntimeFromDecisions(decisions)))
	fmt.Fprintf(&b, "Shell: oh-my-zsh=%t, zshrc=%t, starship=%t\n",
		stages.ShellInstallOhMyZsh(decisions),
		stages.ShellApplyZshrcTemplate(decisions),
		stages.ShellApplyStarshipTemplate(decisions),
	)
	if m.stageSelected("git_config") {
		name, email := stages.GitIdentityFromDecisions(decisions)
		fmt.Fprintf(&b, "Git identity: %s <%s>\n", name, email)
	}
	fmt.Fprintf(&b, "\nStages:\n")
	for _, stageID := range m.plan {
		fmt.Fprintf(&b, "- %s\n", m.stageTitle(stageID))
	}
	if m.planError != "" {
		fmt.Fprintf(&b, "\nPlan error: %s\n", m.planError)
	}
	fmt.Fprintf(&b, "\nPress Enter to execute, Esc to go back.")
	return b.String()
}

func selectOptionTitle(options []selectOption, id string) string {
	for _, option := range options {
		if option.ID == id && strings.TrimSpace(option.Title) != "" {
			return option.Title
		}
	}
	return id
}

func (m model) viewExecuting() string {
	progress := m.executionProgress()
	return m.renderDashboard(
		m.executionDashboardStatus(progress),
		dashboardJourney{
			StageOrder:  m.stageOrder,
			Statuses:    m.stageStatuses,
			CurrentStep: progress.CurrentStageID,
		},
		m.executionOutput(progress.CurrentStageID),
	)
}

func (m model) viewConfigFlow(output string) string {
	return m.renderDashboard(m.configurationDashboardStatus(), m.previewJourney(), m.standardOutputContent(output))
}

func (m model) viewFailureScreen() string {
	return m.renderDashboard(m.failureDashboardStatus(), m.previewJourney(), m.standardOutputContent(m.viewFailure()))
}

func (m model) viewSummaryScreen() string {
	return m.renderDashboard(m.summaryDashboardStatus(), m.previewJourney(), m.standardOutputContent(m.viewSummary()))
}

func (m model) viewFailure() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Stage Failure"))
	if m.failurePrompt != nil {
		fmt.Fprintf(&b, "Stage: %s\n", m.failurePrompt.Title)
		fmt.Fprintf(&b, "Attempt: %d\n", m.failurePrompt.Attempt)
		fmt.Fprintf(&b, "Error: %s\n\n", m.failurePrompt.Message)
	}
	fmt.Fprintf(&b, "Actions:\n")
	fmt.Fprintf(&b, "- Enter: Retry stage\n")
	if m.failurePrompt != nil && m.failurePrompt.CanSkip {
		fmt.Fprintf(&b, "- Space: Skip stage\n")
	}
	fmt.Fprintf(&b, "- CTRL+C: Abort run\n")
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
	fmt.Fprintf(&b, "\nManual App Store reminders:\n")
	manualApps := stages.ManualAppStoreApps()
	for _, item := range manualApps {
		fmt.Fprintf(&b, "- %s\n", item)
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
	m.brewList = newBrewList(m.brewListItems(), m.brewListWidth(), m.brewListHeight())
	return nil
}

func newBrewList(items []list.Item, width int, height int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(textColor)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(mutedColor)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		BorderForeground(accentColor).
		Foreground(accentColor)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		BorderForeground(accentColor).
		Foreground(mutedColor)
	delegate.Styles.FilterMatch = delegate.Styles.FilterMatch.
		Foreground(accentAltColor).
		Underline(true)

	selector := list.New(items, delegate, maxInt(1, width), maxInt(1, height))
	selector.Title = "Brew entries"
	selector.SetStatusBarItemName("entry", "entries")
	selector.SetShowHelp(false)
	selector.DisableQuitKeybindings()
	selector.Styles.Title = selector.Styles.Title.
		Background(successColor).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#FFFFFF"}).
		Bold(true)
	selector.Styles.TitleBar = selector.Styles.TitleBar.PaddingLeft(0)
	selector.Styles.StatusBar = selector.Styles.StatusBar.Foreground(mutedColor)
	selector.Styles.PaginationStyle = selector.Styles.PaginationStyle.Foreground(mutedColor)
	selector.Styles.ActivePaginationDot = selector.Styles.ActivePaginationDot.Foreground(accentColor)
	selector.Styles.InactivePaginationDot = selector.Styles.InactivePaginationDot.Foreground(dimColor)
	return selector
}

func (m model) brewListItems() []list.Item {
	items := make([]list.Item, 0, len(m.brewEntries))
	for _, entry := range m.brewEntries {
		items = append(items, brewListItem{
			ID:       entry.ID,
			Kind:     entry.Kind,
			Selected: m.brewSelected[entry.ID],
		})
	}
	return items
}

func (m *model) refreshBrewListItems() {
	m.ensureBrewList()
	index := m.brewList.GlobalIndex()
	_ = m.brewList.SetItems(m.brewListItems())
	if len(m.brewEntries) > 0 {
		m.brewList.Select(minInt(index, len(m.brewEntries)-1))
		m.cursor = m.brewList.GlobalIndex()
	}
}

func (m *model) ensureBrewList() {
	if m.brewList.Width() > 0 || m.brewList.Height() > 0 || len(m.brewList.Items()) > 0 {
		return
	}
	m.brewList = newBrewList(m.brewListItems(), m.brewListWidth(), m.brewListHeight())
	if len(m.brewEntries) > 0 {
		m.brewList.Select(minInt(maxInt(0, m.cursor), len(m.brewEntries)-1))
	}
}

func (m model) brewListForView() list.Model {
	selector := m.brewList
	if selector.Width() <= 0 && selector.Height() <= 0 && len(selector.Items()) == 0 {
		selector = newBrewList(m.brewListItems(), m.brewListWidth(), m.brewListHeight())
		if len(m.brewEntries) > 0 {
			selector.Select(minInt(maxInt(0, m.cursor), len(m.brewEntries)-1))
		}
	}
	selector.SetSize(m.brewListWidth(), m.brewListHeight())
	return selector
}

func (m *model) syncBrewListSize() {
	m.ensureBrewList()
	m.brewList.SetSize(m.brewListWidth(), m.brewListHeight())
}

func (m model) brewListWidth() int {
	width, _ := m.outputPanelInnerSize()
	return width
}

func (m model) brewListHeight() int {
	_, height := m.outputPanelInnerSize()
	return maxInt(1, height-2)
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
		return nil, fmt.Errorf("%s selected with no Brew entries; select at least one entry or deselect %s",
			m.stageTitle("brew_bundle"),
			m.stageTitle("brew_bundle"),
		)
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
			Decisions:    m.collectDecisions(),
			SelectedIDs:  m.selectedBrewIDs(),
			Stages:       make(map[string]state.StageStatus, len(m.catalog)),
		}
	}

	runState.Decisions = stages.NormalizeDecisions(runState.Decisions)
	if !m.resumeRun {
		runState.SelectedIDs = m.selectedBrewIDs()
		runState.ResolvedPlan = plan
		runState.Decisions = m.collectDecisions()
	} else if _, ok := runState.Decisions[stages.DecisionSelectedStageIDs]; !ok {
		runState.Decisions[stages.DecisionSelectedStageIDs] = append([]string(nil), runState.ResolvedPlan...)
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

func (m *model) collectDecisions() map[string]any {
	decisions := stages.DefaultDecisions()
	decisions[stages.DecisionSelectedStageIDs] = m.selectedStageIDs()
	decisions[stages.DecisionNodeToolchain] = m.selectedNodeToolchainID()
	decisions[stages.DecisionDockerRuntime] = m.selectedDockerRuntimeID()
	decisions[stages.DecisionShellInstallOhMyZsh] = m.shellOptionEnabled(stages.DecisionShellInstallOhMyZsh)
	decisions[stages.DecisionShellApplyZshrc] = m.shellOptionEnabled(stages.DecisionShellApplyZshrc)
	decisions[stages.DecisionShellApplyStarship] = m.shellOptionEnabled(stages.DecisionShellApplyStarship)
	decisions[stages.DecisionGitConfigMode] = stages.GitConfigModeTemplate
	decisions[stages.DecisionGitUserName] = strings.TrimSpace(m.gitNameInput.Value())
	decisions[stages.DecisionGitUserEmail] = strings.TrimSpace(m.gitEmailInput.Value())
	return stages.NormalizeDecisions(decisions)
}

func (m *model) effectiveDecisions() map[string]any {
	if m.resumeRun && m.current != nil {
		return stages.NormalizeDecisions(m.current.Decisions)
	}
	return m.collectDecisions()
}

func (m *model) selectedNodeToolchainID() string {
	if m.nodeSelection >= 0 && m.nodeSelection < len(m.nodeOptions) {
		return m.nodeOptions[m.nodeSelection].ID
	}
	return stages.NodeToolchainVitePlus
}

func (m *model) selectedDockerRuntimeID() string {
	if m.dockerSelection >= 0 && m.dockerSelection < len(m.dockerOptions) {
		return m.dockerOptions[m.dockerSelection].ID
	}
	return stages.DockerRuntimeColima
}

func (m *model) shellOptionEnabled(id string) bool {
	for _, option := range m.shellOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	return true
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
		return m.current.Mode == "dry-run"
	}
	return m.config.DryRun
}

func (m *model) syncInputWidths() {
	inputWidth := minInt(72, maxInt(24, m.width-16))
	m.gitNameInput.Width = inputWidth
	m.gitEmailInput.Width = inputWidth
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
				DryRunDelay:   execution.RandomDryRunStageDelay(2*time.Second, 5*time.Second),
				RepoRoot:      repoRoot,
				HomeDir:       homeDir,
				RunDir:        filepath.Dir(setup.humanLogPath),
				CommandRunner: commandRunner,
				Logger:        logger,
				Hooks: execution.Hooks{
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

func scheduleLogTailTick() tea.Cmd {
	return tea.Tick(logTailPollInterval, func(at time.Time) tea.Msg {
		return logTailTickMsg(at)
	})
}

func (m *model) pollRunLog() {
	if strings.TrimSpace(m.humanLogPath) == "" {
		return
	}

	file, err := os.Open(m.humanLogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}
	if info.Size() < m.logTailOffset {
		m.logTailOffset = 0
		m.logTailCarry = ""
	}
	if _, err = file.Seek(m.logTailOffset, io.SeekStart); err != nil {
		return
	}

	buffer := make([]byte, 4096)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			m.consumeLogTailChunk(string(buffer[:count]))
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return
			}
			break
		}
	}

	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}
	m.logTailOffset = offset
}

func (m *model) consumeLogTailChunk(chunk string) {
	if chunk == "" {
		return
	}
	payload := m.logTailCarry + chunk
	lines := strings.Split(payload, "\n")
	if strings.HasSuffix(payload, "\n") {
		m.logTailCarry = ""
	} else {
		m.logTailCarry = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		parsed := parseRunLogLine(line)
		if strings.TrimSpace(parsed.Line) == "" {
			continue
		}
		m.tailedLogs = append(m.tailedLogs, parsed)
	}
	if len(m.tailedLogs) > bufferedLogLineLimit {
		m.tailedLogs = append([]tailedLogLine(nil), m.tailedLogs[len(m.tailedLogs)-bufferedLogLineLimit:]...)
	}
}

func parseRunLogLine(raw string) tailedLogLine {
	line := strings.TrimSpace(raw)
	if line == "" {
		return tailedLogLine{}
	}

	parsed := tailedLogLine{Line: line}
	parts := strings.Split(line, " | ")
	if len(parts) < 4 {
		return parsed
	}
	if !isLogLevel(parts[1]) {
		return parsed
	}

	candidate := strings.TrimSpace(parts[2])
	if candidate == "" || isEventToken(candidate) {
		return parsed
	}

	parsed.StageID = candidate
	return parsed
}

func isLogLevel(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "INFO", "WARN", "ERROR", "DEBUG":
		return true
	default:
		return false
	}
}

func isEventToken(value string) bool {
	switch strings.TrimSpace(value) {
	case "run_started",
		"run_completed",
		"stage_started",
		"stage_already_done",
		"stage_completed",
		"stage_failed",
		"stage_retry",
		"stage_skipped",
		"command_started",
		"command_completed",
		"command_stdout",
		"command_stderr",
		"simulation",
		"stage_message":
		return true
	default:
		return false
	}
}

func currentLogStageID(stageOrder []string, statuses map[string]state.StageStatus) string {
	for _, stageID := range stageOrder {
		if statuses[stageID].Status == string(stages.StatusRunning) {
			return stageID
		}
	}
	for idx := len(stageOrder) - 1; idx >= 0; idx-- {
		stageID := stageOrder[idx]
		status := statuses[stageID].Status
		if status != "" && status != string(stages.StatusPending) {
			return stageID
		}
	}
	return ""
}

func filteredLogLines(lines []tailedLogLine, stageID string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	filtered := make([]string, 0, limit)
	for _, line := range lines {
		if stageID != "" && line.StageID != stageID {
			continue
		}
		filtered = append(filtered, line.Line)
	}
	if len(filtered) <= limit {
		return filtered
	}
	return append([]string(nil), filtered[len(filtered)-limit:]...)
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

func (m model) renderDashboardShortcutHint(width int, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	bindings := shortcutBindingsForHint(hint)
	if len(bindings) == 0 {
		return ""
	}
	elapsed := lipgloss.NewStyle().
		Foreground(mutedColor).
		Render("Elapsed: " + formatElapsed(m.stopwatch.Elapsed()))

	shortcutHelp := m.help
	if shortcutHelp.ShortSeparator == "" {
		shortcutHelp = newShortcutHelp()
	}
	helpWidth := maxInt(1, width-lipgloss.Width(elapsed)-3)
	shortcutHelp.Width = helpWidth
	helpLine := shortcutHelp.ShortHelpView(bindings)
	line := lipgloss.JoinHorizontal(lipgloss.Center, elapsed, "   ", helpLine)
	return lipgloss.NewStyle().
		Width(maxInt(1, width)).
		MaxWidth(maxInt(1, width)).
		Align(lipgloss.Center).
		Foreground(mutedColor).
		Render(line)
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
	if elapsed < time.Second {
		return "0s"
	}
	elapsed = elapsed.Round(time.Second)
	hours := int(elapsed.Hours())
	minutes := int(elapsed.Minutes()) % 60
	seconds := int(elapsed.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
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
		ConfigurationProgressPct: stepIndex * 100 / maxInt(1, totalSteps),
		ExecutionProgressPct:     m.executionProgress().PercentComplete,
		Hint:                     hint,
	}
}

func configurationShortcutHint(current screen) string {
	switch current {
	case screenWelcome:
		return "Enter continue  CTRL+C quit"
	case screenMacOS, screenInstall, screenDevTools, screenShellOptions, screenManual:
		return "Space toggle  Enter continue  Esc back  CTRL+C quit"
	case screenBrew:
		return "Up/down move  Space toggle  / filter  Enter continue  Esc back  CTRL+C quit"
	case screenNodeToolchain, screenDockerRuntime:
		return "Up/down choose  Enter continue  Esc back  CTRL+C quit"
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
		if m.executing || m.screen == screenFailure || m.screen == screenSummary {
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
			StageOrder: slices.Clone(m.current.ResolvedPlan),
			Statuses:   cloneStatuses(m.current.Stages),
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
			Runner:   commandRunner,
			StageID:  stage.ID,
			RepoRoot: repoRoot,
			HomeDir:  homeDir,
		})
		if err != nil || !result.Satisfied {
			continue
		}
		statuses[stage.ID] = state.StageStatus{
			Status: string(stages.StatusAlreadyDone),
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

func configurationStepPosition(current screen) (int, int) {
	total := len(configurationScreenOrder)
	for index, candidate := range configurationScreenOrder {
		if current == candidate {
			return index + 1, total
		}
	}
	return total, total
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
		return "Brew Catalog Selection"
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
	return status.Status
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

func (m model) brewVisibleCount(total int) int {
	// Line budget in output panel:
	// - Brew title + instruction + spacer = 3
	// - Optional top/bottom overflow markers + trailing spacer = 3
	const reservedLines = 6

	visible := m.outputPanelLineBudget() - reservedLines
	if visible < 1 {
		visible = 1
	}
	if total > 0 && visible > total {
		return total
	}
	return visible
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

func brewViewportRange(total int, cursor int, visible int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if visible <= 0 || visible >= total {
		return 0, total
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	start := cursor - (visible / 2)
	if start < 0 {
		start = 0
	}
	maxStart := total - visible
	if start > maxStart {
		start = maxStart
	}
	return start, start + visible
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
