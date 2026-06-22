package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/dencoseca/laptop-setup/internal/execution"
	"github.com/dencoseca/laptop-setup/internal/stages"
	"github.com/dencoseca/laptop-setup/internal/state"
)

func (m model) View() string {
	var content string
	if spec, ok := configurationScreenSpec(m.screen); ok {
		content = m.viewConfigurationScreen(spec)
		return m.viewConfigFlow(content)
	}

	switch m.screen {
	case screenExecuting:
		return m.viewExecuting()
	case screenInteractive:
		return m.viewInteractiveCommand()
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

func (m model) viewConfigurationScreen(spec screenSpec) string {
	switch spec.screen {
	case screenWelcome:
		return m.viewWelcome()
	case screenBrew:
		return m.viewBrewSelection(spec.title)
	case screenGitName:
		return m.viewTextInput(spec.title, spec.textInputSubtitle, m.gitNameInput)
	case screenGitEmail:
		return m.viewTextInput(spec.title, spec.textInputSubtitle, m.gitEmailInput)
	case screenReview:
		return m.viewReview()
	default:
		if options := m.toggleOptionsForList(spec.optionList); options != nil {
			return m.viewToggleOptions(spec.title, *options)
		}
		if options, selected := m.selectOptionsForList(spec.optionList); options != nil && selected != nil {
			return m.viewSelectOptions(spec.title, *options, *selected)
		}
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
	fmt.Fprintf(&b, "Press CTRL+C to quit.")
	return b.String()
}

func (m model) viewToggleOptions(title string, options []toggleOption) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	selector := m.optionListForView(m.toggleListItems(options))
	if selector.SettingFilter() {
		fmt.Fprintf(&b, "%s\n", selector.FilterInput.View())
	}
	fmt.Fprintf(&b, "%s", selector.View())
	return b.String()
}

func (m model) viewSelectOptions(title string, options []selectOption, selected int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))
	selector := m.optionListForView(selectListItems(options, selected))
	if selector.SettingFilter() {
		fmt.Fprintf(&b, "%s\n", selector.FilterInput.View())
	}
	fmt.Fprintf(&b, "%s", selector.View())
	return b.String()
}

func (m model) viewBrewSelection(title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render(title))

	if len(m.brewEntries) == 0 {
		fmt.Fprintf(&b, "No package or app entries found in the Brewfile template.\n")
		return b.String()
	}

	selector := m.brewListForView()
	if selector.SettingFilter() {
		fmt.Fprintf(&b, "%s\n", selector.FilterInput.View())
	}
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

func (m *model) viewReview() string {
	plan, err := m.resolvePlan()
	m.plan = plan
	m.planError = ""
	if err != nil {
		m.planError = err.Error()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Execution Plan Review"))
	fmt.Fprintf(&b, "Mode: %s\n", state.ModeFromDryRun(m.effectiveDryRun()))
	if !m.resumeRun {
		fmt.Fprintf(&b, "Selected packages/apps: %d\n", len(m.selectedBrewIDs()))
	}
	decisions := m.effectiveDecisions()
	fmt.Fprintf(&b, "Node toolchain: %s\n", selectOptionTitle(m.nodeOptions, string(stages.NodeToolchainFromDecisions(decisions))))
	fmt.Fprintf(&b, "Docker runtime: %s\n", selectOptionTitle(m.dockerOptions, string(stages.DockerRuntimeFromDecisions(decisions))))
	fmt.Fprintf(&b, "Shell: oh-my-zsh=%t, zshrc=%t, starship=%t\n",
		stages.ShellInstallOhMyZsh(decisions),
		stages.ShellApplyZshrcTemplate(decisions),
		stages.ShellApplyStarshipTemplate(decisions),
	)
	if m.stageSelected(string(stages.StageGitConfig)) {
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

func (m model) viewInteractiveCommand() string {
	return m.renderDashboard(m.interactiveDashboardStatus(), m.previewJourney(), m.viewInteractivePrompt())
}

func (m model) viewConfigFlow(output string) string {
	return m.renderDashboard(m.configurationDashboardStatus(), m.previewJourney(), output)
}

func (m model) viewInteractivePrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Terminal Authorization"))
	if m.interactivePrompt != nil {
		command := m.interactivePrompt.Command
		if strings.TrimSpace(command.Prompt) != "" {
			fmt.Fprintf(&b, "%s\n\n", command.Prompt)
		}
		fmt.Fprintf(&b, "Command: %s\n\n", command.String())
	}
	fmt.Fprintf(&b, "Press Enter to temporarily leave the setup UI and run this command in the terminal.\n")
	fmt.Fprintf(&b, "When the command finishes, the setup UI will resume automatically.\n\n")
	fmt.Fprintf(&b, "Press CTRL+C to abort the run.")
	return b.String()
}

func (m model) viewFailureScreen() string {
	return m.renderDashboard(m.failureDashboardStatus(), m.previewJourney(), m.viewFailure())
}

func (m model) viewSummaryScreen() string {
	return m.renderDashboard(m.summaryDashboardStatus(), m.previewJourney(), m.viewSummary())
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
