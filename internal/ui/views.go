package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	title := lipgloss.NewStyle().Bold(true).Foreground(textColor).Render("Laptop Setup")
	fmt.Fprintf(&b, "%s\n\n", title)
	if m.resumeRun {
		fmt.Fprintf(&b, "%s\n", labelValue("Resume run", m.current.RunID.String()))
		fmt.Fprintf(&b, "%s\n\n", labelValue("Mode", m.current.Mode.String()))
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(mutedColor).Render("Review the saved plan and continue where it left off."))
	} else {
		fmt.Fprintf(&b, "Choose what this Mac needs, review the generated plan, then let the runner apply it stage by stage.\n\n")
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(mutedColor).Render("Prechecks mark work that is already complete before anything runs."))
	}
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
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Foreground(mutedColor).Render(subtitle))
	field := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Render(input.View())
	fmt.Fprintf(&b, "%s\n", field)
	if m.inputError != "" {
		fmt.Fprintf(&b, "\n%s\n", lipgloss.NewStyle().Foreground(failureColor).Render(m.inputError))
	}
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
		m.planError = m.humanizeStageReferences(err.Error())
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Bold(true).Render("Execution Plan Review"))

	if m.planError != "" {
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Bold(true).Foreground(failureColor).Render("PLAN ERROR"))
		fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Foreground(failureColor).Render(m.planError))
	}

	fmt.Fprintf(&b, "%s\n", reviewSectionTitle("Mode"))
	mode := state.ModeFromDryRun(m.effectiveDryRun()).String()
	fmt.Fprintf(&b, "%s\n", labelValue("Mode", mode))
	if m.effectiveDryRun() {
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Bold(true).Foreground(warningColor).Render("Dry-run preview: no changes will be applied."))
	}
	if m.resumeRun && m.current != nil {
		fmt.Fprintf(&b, "%s\n", labelValue("Resume run", m.current.RunID.String()))
	}

	if !m.resumeRun {
		fmt.Fprintf(&b, "\n%s\n", reviewSectionTitle("Package Summary"))
		fmt.Fprintf(&b, "%s\n", labelValue("Selected packages/apps", fmt.Sprintf("%d of %d", len(m.selectedBrewIDs()), len(m.brewEntries))))
		if selected := m.selectedBrewIDs(); len(selected) > 0 {
			fmt.Fprintf(&b, "%s\n", labelValue("Brewfile entries", summarizeReviewList(selected, 6)))
		}
	}

	decisions := m.effectiveDecisions()
	fmt.Fprintf(&b, "\n%s\n", reviewSectionTitle("Key Decisions"))
	fmt.Fprintf(&b, "%s\n", labelValue("Node toolchain", selectOptionTitle(m.nodeOptions, string(stages.NodeToolchainFromDecisions(decisions)))))
	fmt.Fprintf(&b, "%s\n", labelValue("Docker runtime", selectOptionTitle(m.dockerOptions, string(stages.DockerRuntimeFromDecisions(decisions)))))
	fmt.Fprintf(&b, "%s\n", labelValue("Shell", fmt.Sprintf("oh-my-zsh=%t, zshrc=%t, starship=%t",
		stages.ShellInstallOhMyZsh(decisions),
		stages.ShellApplyZshrcTemplate(decisions),
		stages.ShellApplyStarshipTemplate(decisions),
	)))
	if m.stageSelected(string(stages.StageGitConfig)) || stringInSlice(string(stages.StageGitConfig), m.plan) {
		name, email := stages.GitIdentityFromDecisions(decisions)
		fmt.Fprintf(&b, "%s\n", labelValue("Git identity", fmt.Sprintf("%s <%s>", name, email)))
	}

	fmt.Fprintf(&b, "\n%s\n", reviewSectionTitle("Planned Stages"))
	if len(m.plan) == 0 {
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(mutedColor).Render("No stages resolved yet."))
	}
	for _, stageID := range m.plan {
		fmt.Fprintf(&b, "- %s\n", m.stageTitle(stageID))
	}

	fmt.Fprintf(&b, "\n%s\n", reviewSectionTitle("Skipped Or Already Satisfied"))
	skippedOrSatisfied := m.reviewSkippedOrSatisfiedLines()
	if len(skippedOrSatisfied) == 0 {
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(mutedColor).Render("None"))
	}
	for _, line := range skippedOrSatisfied {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	return b.String()
}

func reviewSectionTitle(title string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(strings.ToUpper(title))
}

func summarizeReviewList(items []string, limit int) string {
	if limit <= 0 || len(items) == 0 {
		return ""
	}
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	visible := strings.Join(items[:limit], ", ")
	return fmt.Sprintf("%s, +%d more", visible, len(items)-limit)
}

func stringInSlice(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func (m model) reviewSkippedOrSatisfiedLines() []string {
	plannedSet := make(map[string]struct{}, len(m.plan))
	for _, stageID := range m.plan {
		plannedSet[stageID] = struct{}{}
	}
	selectedSet := make(map[string]struct{})
	if !m.resumeRun {
		collectSelected(selectedSet, m.macOSOptions)
		collectSelected(selectedSet, m.installOptions)
		collectSelected(selectedSet, m.devOptions)
		collectSelected(selectedSet, m.manualOptions)
	}
	skipSet := make(map[string]struct{}, len(m.config.Skip))
	for _, stageID := range m.config.Skip {
		skipSet[strings.TrimSpace(stageID)] = struct{}{}
	}

	lines := []string{}
	for _, stage := range m.catalog {
		stageID := stage.ID.String()
		title := m.stageTitle(stageID)
		status := normalizedStageStatus(m.stageStatuses[stageID])
		switch status {
		case string(stages.StatusAlreadyDone):
			lines = append(lines, fmt.Sprintf("Already satisfied: %s", title))
			continue
		case string(stages.StatusSkipped):
			lines = append(lines, fmt.Sprintf("Skipped: %s", title))
			continue
		}

		if _, planned := plannedSet[stageID]; planned {
			continue
		}
		if _, skipped := skipSet[stageID]; skipped {
			lines = append(lines, fmt.Sprintf("Skipped: %s", title))
			continue
		}
		if m.resumeRun {
			continue
		}
		if _, selected := selectedSet[stageID]; !selected {
			lines = append(lines, fmt.Sprintf("Not selected: %s", title))
			continue
		}
		if strings.TrimSpace(m.config.From) != "" || len(m.config.Only) > 0 {
			lines = append(lines, fmt.Sprintf("Omitted by plan filters: %s", title))
		}
	}
	return lines
}

func (m model) humanizeStageReferences(value string) string {
	out := value
	for _, stage := range m.catalog {
		stageID := stage.ID.String()
		title := strings.TrimSpace(stage.Title)
		if stageID == "" || title == "" {
			continue
		}
		out = strings.ReplaceAll(out, stageID, title)
	}
	return out
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
	return m.renderConfigurationFlow(m.configurationDashboardStatus(), m.previewJourney(), output)
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
		fmt.Fprintf(&b, "%s\n", labelValue("Stage", m.failurePrompt.Title))
		fmt.Fprintf(&b, "%s\n", labelValue("Attempt", fmt.Sprintf("%d", m.failurePrompt.Attempt)))
		fmt.Fprintf(&b, "%s\n\n", lipgloss.NewStyle().Foreground(failureColor).Render("Error: "+m.failurePrompt.Message))
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

	status := m.summaryStatus()
	fmt.Fprintf(&b, "%s\n", labelValue("Status", status.label))
	fmt.Fprintf(&b, "%s\n", status.message)
	if status.failed && m.runErr != nil {
		fmt.Fprintf(&b, "%s\n", lipgloss.NewStyle().Foreground(failureColor).Render("Error: "+m.runErr.Error()))
	}

	completed, skipped, failed := stageCounts(m.stageStatuses)
	fmt.Fprintf(&b, "\n%s\n", labelValue("Stage counts", fmt.Sprintf("completed=%d skipped=%d failed=%d", completed, skipped, failed)))
	m.writeSummaryStageSection(&b, "Skipped stages", []stages.Status{stages.StatusSkipped})
	m.writeSummaryStageSection(&b, "Failed stages", []stages.Status{stages.StatusFailed})
	if m.humanLogPath != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("Run log", m.humanLogPath))
	}
	if m.eventsLogPath != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("Events log", m.eventsLogPath))
	}
	fmt.Fprintf(&b, "\nManual App Store reminders:\n")
	manualApps := stages.ManualAppStoreApps()
	for _, item := range manualApps {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	m.writeSummaryNextSteps(&b)
	fmt.Fprintf(&b, "\nPress Enter to exit.")
	return b.String()
}

type summaryStatus struct {
	label   string
	message string
	failed  bool
}

func (m model) summaryStatus() summaryStatus {
	if m.runErr == nil {
		if m.effectiveDryRun() {
			return summaryStatus{
				label:   "dry-run completed",
				message: lipgloss.NewStyle().Foreground(warningColor).Render("No system changes were applied."),
			}
		}
		return summaryStatus{
			label:   "completed",
			message: lipgloss.NewStyle().Foreground(successColor).Render("All runnable stages reached a terminal state."),
		}
	}
	if errors.Is(m.runErr, execution.ErrAborted) || errors.Is(m.runErr, context.Canceled) {
		return summaryStatus{
			label:   "aborted",
			message: lipgloss.NewStyle().Foreground(warningColor).Render("The run stopped before the remaining stages could finish."),
		}
	}
	return summaryStatus{
		label:   "failed",
		message: lipgloss.NewStyle().Foreground(failureColor).Render("A stage failed before the run could complete."),
		failed:  true,
	}
}

func (m model) writeSummaryStageSection(b *strings.Builder, title string, matching []stages.Status) {
	lines := m.summaryStageLines(matching)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, line := range lines {
		fmt.Fprintf(b, "- %s\n", line)
	}
}

func (m model) summaryStageLines(matching []stages.Status) []string {
	matches := make(map[stages.Status]struct{}, len(matching))
	for _, status := range matching {
		matches[status] = struct{}{}
	}

	lines := []string{}
	for _, stageID := range m.summaryStageOrder() {
		stageStatus := m.stageStatuses[stageID]
		status := stages.Status(normalizedStageStatus(stageStatus))
		if _, ok := matches[status]; !ok {
			continue
		}
		line := m.stageTitle(stageID)
		if strings.TrimSpace(stageStatus.LastError) != "" {
			line = fmt.Sprintf("%s: %s", line, stageStatus.LastError)
		}
		lines = append(lines, line)
	}
	return lines
}

func (m model) summaryStageOrder() []string {
	if len(m.stageOrder) > 0 {
		return slices.Clone(m.stageOrder)
	}
	if m.runState != nil && len(m.runState.ResolvedPlan) > 0 {
		return stageIDsToStrings(m.runState.ResolvedPlan)
	}
	if len(m.catalog) > 0 {
		ids := make([]string, 0, len(m.catalog))
		for _, stage := range m.catalog {
			ids = append(ids, stage.ID.String())
		}
		return ids
	}
	ids := make([]string, 0, len(m.stageStatuses))
	for stageID := range m.stageStatuses {
		ids = append(ids, stageID)
	}
	slices.Sort(ids)
	return ids
}

func (m model) writeSummaryNextSteps(b *strings.Builder) {
	nextSteps := m.summaryNextSteps()
	if len(nextSteps) == 0 {
		return
	}
	fmt.Fprintf(b, "\nNext steps:\n")
	for _, step := range nextSteps {
		fmt.Fprintf(b, "- %s\n", step)
	}
}

func (m model) summaryNextSteps() []string {
	if m.effectiveDryRun() && m.runErr == nil {
		return []string{"Run again without --dry-run when you are ready to apply these changes."}
	}

	steps := []string{}
	if m.stageChangedMachine(stages.StageHomebrewInstall) || m.stageChangedMachine(stages.StageShellSetup) {
		steps = append(steps, "Restart your terminal or run exec zsh so shell changes are loaded.")
	}
	if len(m.summaryStageLines([]stages.Status{stages.StatusFailed})) > 0 {
		steps = append(steps, "Review the run log, fix the failed stage, then resume the run.")
	}
	if len(steps) == 0 && m.runErr == nil {
		steps = append(steps, "Review the manual App Store reminders above.")
	}
	return steps
}

func (m model) stageChangedMachine(stageID stages.StageID) bool {
	status := stages.Status(normalizedStageStatus(m.stageStatuses[stageID.String()]))
	return status == stages.StatusSuccess
}
