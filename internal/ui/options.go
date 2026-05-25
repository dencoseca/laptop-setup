package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dencoseca/laptop-setup/internal/stages"
)

type brewListItem struct {
	ID       string
	Selected bool
}

func (i brewListItem) Title() string {
	return fmt.Sprintf("%s %s", selectionMarker(i.Selected), i.ID)
}

func (i brewListItem) Description() string {
	return ""
}

func (i brewListItem) FilterValue() string {
	return i.ID
}

type optionListItem struct {
	ID       string
	Label    string
	Index    int
	Selected bool
	Complete bool
}

func (i optionListItem) Title() string {
	marker := selectionMarker(i.Selected)
	if i.Complete {
		marker = lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("✓")
	}
	return fmt.Sprintf("%s %s", marker, i.Label)
}

func (i optionListItem) Description() string {
	return ""
}

func (i optionListItem) FilterValue() string {
	return strings.Join([]string{i.ID, i.Label}, " ")
}

func (m *model) updateToggleListScreen(
	key tea.KeyMsg,
	options *[]toggleOption,
	backScreen screen,
	nextScreen screen,
) (tea.Model, tea.Cmd) {
	m.ensureOptionList()
	if cmd, handled := m.updateOptionListFilter(key); handled {
		return *m, cmd
	}

	switch key.String() {
	case "esc":
		m.screen = backScreen
		m.cursor = m.defaultCursorForScreen(backScreen)
	case " ":
		if item, ok := m.optionList.SelectedItem().(optionListItem); ok && item.Index >= 0 && item.Index < len(*options) {
			(*options)[item.Index].Selected = !(*options)[item.Index].Selected
			m.cursor = item.Index
			m.refreshOptionListItems()
		}
	case "enter":
		m.screen = nextScreen
		m.cursor = m.defaultCursorForScreen(nextScreen)
	default:
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		return *m, cmd
	}
	return *m, nil
}

func (m *model) updateSelectListScreen(
	key tea.KeyMsg,
	selected *int,
	backScreen screen,
	nextScreen screen,
) (tea.Model, tea.Cmd) {
	m.ensureOptionList()
	if cmd, handled := m.updateOptionListFilter(key); handled {
		return *m, cmd
	}

	switch key.String() {
	case "esc":
		m.screen = backScreen
		m.cursor = m.defaultCursorForScreen(backScreen)
	case " ":
		if item, ok := m.optionList.SelectedItem().(optionListItem); ok {
			*selected = item.Index
			m.cursor = item.Index
			m.refreshOptionListItems()
		}
	case "enter":
		if item, ok := m.optionList.SelectedItem().(optionListItem); ok {
			*selected = item.Index
			m.cursor = item.Index
		}
		m.screen = nextScreen
		m.cursor = m.defaultCursorForScreen(nextScreen)
	default:
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		*selected = m.cursor
		m.refreshOptionListItems()
		return *m, cmd
	}
	return *m, nil
}

func (m *model) updateShellOptionsScreen(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.ensureOptionList()
	if cmd, handled := m.updateOptionListFilter(key); handled {
		return *m, cmd
	}

	switch key.String() {
	case "esc":
		m.screen = screenDockerRuntime
		m.cursor = m.defaultCursorForScreen(screenDockerRuntime)
	case " ":
		if item, ok := m.optionList.SelectedItem().(optionListItem); ok && item.Index >= 0 && item.Index < len(m.shellOptions) {
			m.shellOptions[item.Index].Selected = !m.shellOptions[item.Index].Selected
			m.cursor = item.Index
			m.refreshOptionListItems()
		}
	case "enter":
		m.inputError = ""
		if m.stageSelected("git_config") {
			m.screen = screenGitName
			m.gitNameInput.Focus()
			return *m, textinput.Blink
		}
		m.screen = screenManual
		m.cursor = m.defaultCursorForScreen(screenManual)
	default:
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		return *m, cmd
	}
	return *m, nil
}

func (m *model) updateOptionListFilter(key tea.KeyMsg) (tea.Cmd, bool) {
	if key.String() == "/" {
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		return cmd, true
	}
	if !m.optionList.SettingFilter() && !m.optionList.IsFiltered() {
		return nil, false
	}

	switch key.String() {
	case "enter":
		if m.optionList.SettingFilter() {
			var cmd tea.Cmd
			m.optionList, cmd = m.optionList.Update(key)
			return cmd, true
		}
	case "esc":
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		return cmd, true
	}
	if m.optionList.SettingFilter() {
		var cmd tea.Cmd
		m.optionList, cmd = m.optionList.Update(key)
		m.cursor = m.optionList.GlobalIndex()
		return cmd, true
	}
	return nil, false
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
	return newConfigList(items, width, height)
}

func newConfigList(items []list.Item, width int, height int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
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
	selector.SetShowTitle(false)
	selector.SetShowFilter(false)
	selector.SetShowStatusBar(false)
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

func (m model) toggleListItems(options []toggleOption) []list.Item {
	items := make([]list.Item, 0, len(options))
	for index, option := range options {
		status := m.stageStatuses[option.ID].Status
		items = append(items, optionListItem{
			ID:       option.ID,
			Label:    option.Title,
			Index:    index,
			Selected: option.Selected,
			Complete: isCompleteStageStatus(status.String()),
		})
	}
	return items
}

func selectListItems(options []selectOption, selected int) []list.Item {
	items := make([]list.Item, 0, len(options))
	for index, option := range options {
		items = append(items, optionListItem{
			ID:       option.ID,
			Label:    option.Title,
			Index:    index,
			Selected: selected == index,
		})
	}
	return items
}

func (m model) optionListItemsForScreen(current screen) []list.Item {
	switch current {
	case screenMacOS:
		return m.toggleListItems(m.macOSOptions)
	case screenInstall:
		return m.toggleListItems(m.installOptions)
	case screenDevTools:
		return m.toggleListItems(m.devOptions)
	case screenNodeToolchain:
		return selectListItems(m.nodeOptions, m.nodeSelection)
	case screenDockerRuntime:
		return selectListItems(m.dockerOptions, m.dockerSelection)
	case screenShellOptions:
		return m.toggleListItems(m.shellOptions)
	case screenManual:
		return m.toggleListItems(m.manualOptions)
	default:
		return nil
	}
}

func (m *model) ensureOptionList() {
	if !isOptionListScreen(m.screen) {
		return
	}
	if m.optionListReady && m.optionListScreen == m.screen {
		if len(m.optionList.Items()) > 0 {
			m.optionList.Select(minInt(maxInt(0, m.cursor), len(m.optionList.Items())-1))
		}
		return
	}
	m.optionList = newConfigList(m.optionListItemsForScreen(m.screen), m.optionListWidth(), m.optionListHeight())
	m.optionList.Select(m.defaultCursorForScreen(m.screen))
	m.optionListScreen = m.screen
	m.optionListReady = true
	m.cursor = m.optionList.GlobalIndex()
}

func (m model) optionListForView(items []list.Item) list.Model {
	selector := m.optionList
	if !m.optionListReady || m.optionListScreen != m.screen {
		selector = newConfigList(items, m.optionListWidth(), m.optionListHeight())
		if len(items) > 0 {
			selector.Select(minInt(maxInt(0, m.cursor), len(items)-1))
		}
	}
	selector.SetSize(m.optionListWidth(), m.optionListHeight())
	return selector
}

func (m *model) refreshOptionListItems() {
	m.ensureOptionList()
	index := m.optionList.GlobalIndex()
	_ = m.optionList.SetItems(m.optionListItemsForScreen(m.screen))
	items := m.optionList.Items()
	if len(items) > 0 {
		m.optionList.Select(minInt(maxInt(0, index), len(items)-1))
		m.cursor = m.optionList.GlobalIndex()
	}
}

func (m *model) syncOptionListSize() {
	if !m.optionListReady {
		return
	}
	m.optionList.SetSize(m.optionListWidth(), m.optionListHeight())
}

func (m model) optionListWidth() int {
	width, _ := m.outputPanelInnerSize()
	return width
}

func (m model) optionListHeight() int {
	_, height := m.outputPanelInnerSize()
	return maxInt(1, height-5)
}

func isOptionListScreen(current screen) bool {
	switch current {
	case screenMacOS, screenInstall, screenDevTools, screenNodeToolchain, screenDockerRuntime, screenShellOptions, screenManual:
		return true
	default:
		return false
	}
}

func (m model) defaultCursorForScreen(current screen) int {
	switch current {
	case screenNodeToolchain:
		return m.nodeSelection
	case screenDockerRuntime:
		return m.dockerSelection
	default:
		return 0
	}
}

func (m model) brewListItems() []list.Item {
	items := make([]list.Item, 0, len(m.brewEntries))
	for _, entry := range m.brewEntries {
		items = append(items, brewListItem{
			ID:       entry.ID,
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

func (m model) brewVisibleCount(total int) int {
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

func brewViewportRange(total int, cursor int, visible int) (int, int) {
	if total <= 0 || visible <= 0 {
		return 0, 0
	}
	if visible >= total {
		return 0, total
	}
	cursor = minInt(maxInt(0, cursor), total-1)
	half := visible / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}
