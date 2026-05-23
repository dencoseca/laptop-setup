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
	screenNodeToolchain
	screenDockerRuntime
	screenShellOptions
	screenGitConfig
	screenGitName
	screenGitEmail
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

type selectOption struct {
	ID          string
	Title       string
	Description string
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
	nodeOptions    []selectOption
	dockerOptions  []selectOption
	shellOptions   []toggleOption
	gitModeOptions []selectOption
	manualOptions  []toggleOption

	brewEntries  []stages.BrewEntry
	brewSelected map[string]bool

	nodeSelection    int
	dockerSelection  int
	gitModeSelection int
	gitNameInput     textinput.Model
	gitEmailInput    textinput.Model
	gitCurrentName   string
	gitCurrentEmail  string
	inputError       string

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
			{ID: stages.NodeToolchainVitePlus, Title: "vite+", Description: "Install Vite+ toolchain via official installer"},
			{ID: stages.NodeToolchainNvmPnpm, Title: "pnpm + nvm", Description: "Install nvm and pnpm using official install scripts"},
		},
		dockerOptions: []selectOption{
			{ID: stages.DockerRuntimeColima, Title: "colima", Description: "Configure Docker client context for Colima"},
		},
		shellOptions: []toggleOption{
			{ID: stages.DecisionShellInstallOhMyZsh, Title: "Install oh-my-zsh", Selected: true},
			{ID: stages.DecisionShellApplyZshrc, Title: "Write ~/.zshrc from template", Selected: true},
			{ID: stages.DecisionShellApplyStarship, Title: "Write starship.toml from template", Selected: true},
		},
		gitModeOptions: []selectOption{
			{ID: stages.GitConfigModeTemplate, Title: "Use template git config", Description: "Write templates/gitconfig as ~/.gitconfig"},
			{ID: stages.GitConfigModeExisting, Title: "Keep existing git config", Description: "Keep ~/.gitconfig when present"},
			{ID: stages.GitConfigModeCustom, Title: "Set custom identity", Description: "Write template config and override user.name/user.email"},
		},
		manualOptions:   optionsForStageIDs(options.Catalog, phaseManualStages),
		brewSelected:    make(map[string]bool),
		gitNameInput:    gitNameInput,
		gitEmailInput:   gitEmailInput,
		gitCurrentName:  currentGitName,
		gitCurrentEmail: currentGitEmail,
		stageStatuses:   make(map[string]state.StageStatus),
		spinner:         spin,
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
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenBrew
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
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
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenDevTools
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.nodeOptions)-1 {
				m.cursor++
			}
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
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenNodeToolchain
			m.cursor = m.nodeSelection
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.dockerOptions)-1 {
				m.cursor++
			}
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
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenDockerRuntime
			m.cursor = m.dockerSelection
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.shellOptions)-1 {
				m.cursor++
			}
		case " ":
			if len(m.shellOptions) > 0 {
				m.shellOptions[m.cursor].Selected = !m.shellOptions[m.cursor].Selected
			}
		case "enter":
			m.screen = screenGitConfig
			m.cursor = m.gitModeSelection
		}
	case screenGitConfig:
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "b":
			m.screen = screenShellOptions
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.gitModeOptions)-1 {
				m.cursor++
			}
		case " ":
			if len(m.gitModeOptions) > 0 {
				m.gitModeSelection = m.cursor
			}
		case "enter":
			if len(m.gitModeOptions) > 0 {
				m.gitModeSelection = m.cursor
			}
			m.inputError = ""
			if m.selectedGitModeID() == stages.GitConfigModeCustom {
				m.screen = screenGitName
				m.gitNameInput.Focus()
				return m, textinput.Blink
			}
			m.screen = screenManual
			m.cursor = 0
		}
	case screenGitName:
		switch key.String() {
		case "q":
			return m, tea.Quit
		case "b":
			m.inputError = ""
			m.gitNameInput.Blur()
			m.screen = screenGitConfig
			m.cursor = m.gitModeSelection
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
		case "q":
			return m, tea.Quit
		case "b":
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
		return m.updateToggleScreen(key, &m.manualOptions, screenGitConfig, screenReview)
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
	case screenNodeToolchain:
		return m.viewSelectOptions("Dev Tools: Node Toolchain", m.nodeOptions, m.nodeSelection)
	case screenDockerRuntime:
		return m.viewSelectOptions("Dev Tools: Docker Runtime", m.dockerOptions, m.dockerSelection)
	case screenShellOptions:
		return m.viewToggleOptions("Dev Tools: Shell Setup Options", m.shellOptions)
	case screenGitConfig:
		return m.viewGitConfigMode()
	case screenGitName:
		return m.viewTextInput("Git Identity: user.name", "Enter git user.name, then press Enter.", m.gitNameInput)
	case screenGitEmail:
		return m.viewTextInput("Git Identity: user.email", "Enter git user.email, then press Enter.", m.gitEmailInput)
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

func (m model) viewSelectOptions(title string, options []selectOption, selected int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	fmt.Fprintf(&b, "Select one option with space. Enter to continue, b to go back.\n\n")
	for index, option := range options {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		current := " "
		if selected == index {
			current = "x"
		}
		fmt.Fprintf(&b, "%s[%s] %s\n", prefix, current, option.Title)
		if option.Description != "" {
			fmt.Fprintf(&b, "    %s\n", option.Description)
		}
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

func (m model) viewGitConfigMode() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Dev Tools: Git Configuration"))
	currentName := strings.TrimSpace(m.gitCurrentName)
	if currentName == "" {
		currentName = "(not set)"
	}
	currentEmail := strings.TrimSpace(m.gitCurrentEmail)
	if currentEmail == "" {
		currentEmail = "(not set)"
	}
	fmt.Fprintf(&b, "Current identity: %s <%s>\n", currentName, currentEmail)
	fmt.Fprintf(&b, "Choose how git config should be handled.\n\n")
	for index, option := range m.gitModeOptions {
		prefix := "  "
		if m.cursor == index {
			prefix = "> "
		}
		selected := " "
		if m.gitModeSelection == index {
			selected = "x"
		}
		fmt.Fprintf(&b, "%s[%s] %s\n", prefix, selected, option.Title)
		if option.Description != "" {
			fmt.Fprintf(&b, "    %s\n", option.Description)
		}
	}
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
	fmt.Fprintf(&b, "\nPress Enter to continue, b to go back.")
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
	decisions := m.effectiveDecisions()
	fmt.Fprintf(&b, "Node toolchain: %s\n", stages.NodeToolchainFromDecisions(decisions))
	fmt.Fprintf(&b, "Docker runtime: %s\n", stages.DockerRuntimeFromDecisions(decisions))
	fmt.Fprintf(&b, "Shell: oh-my-zsh=%t, zshrc=%t, starship=%t\n",
		stages.ShellInstallOhMyZsh(decisions),
		stages.ShellApplyZshrcTemplate(decisions),
		stages.ShellApplyStarshipTemplate(decisions),
	)
	gitMode := stages.GitConfigModeFromDecisions(decisions)
	fmt.Fprintf(&b, "Git config mode: %s\n", gitMode)
	if gitMode == stages.GitConfigModeCustom {
		name, email := stages.GitIdentityFromDecisions(decisions)
		fmt.Fprintf(&b, "Git identity: %s <%s>\n", name, email)
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
	decisions[stages.DecisionGitConfigMode] = m.selectedGitModeID()
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

func (m *model) selectedGitModeID() string {
	if m.gitModeSelection >= 0 && m.gitModeSelection < len(m.gitModeOptions) {
		return m.gitModeOptions[m.gitModeSelection].ID
	}
	return stages.GitConfigModeTemplate
}

func (m *model) shellOptionEnabled(id string) bool {
	for _, option := range m.shellOptions {
		if option.ID == id {
			return option.Selected
		}
	}
	return true
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
