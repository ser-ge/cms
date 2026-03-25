package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/watcher"
)

// paneEntry is a navigable item in the pane list.
type paneEntry struct {
	pane      tmux.Pane
	session   string
	window    tmux.Window
	agent     agent.AgentStatus
	hasAgent  bool
	isCurrent bool
}

type dashboardModel struct {
	// Data.
	sessions []tmux.Session
	agents   map[string]agent.AgentStatus
	current  tmux.CurrentTarget

	// Navigation.
	items  []paneEntry
	cursor int

	// Fuzzy filter.
	filtering bool
	filter    textinput.Model
	filtered  []int // indices into items matching the filter

	// Move mode: grab a pane, navigate to destination, drop it.
	moving  bool
	moveSrc string // pane ID being moved

	// Kill confirm: waiting for y/n after pressing x.
	confirmKill bool
	killTarget  string // pane ID to kill

	// Focus: set by finder to auto-select a session's first pane.
	focusSession string

	postAction *PostAction // action to run after TUI exits

	loading  bool
	width    int
	height   int
	frame    int // tick counter for spinner animation
	spinning bool
	cfg      config.DashboardConfig
	watcher  *watcher.Watcher // optional, for debug stats
}

type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(450*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func newDashboardModel(cfg config.Config) dashboardModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 64

	return dashboardModel{
		filter:  ti,
		loading: true,
		cfg:     cfg.Dashboard,
	}
}

// hasWorking reports whether any agent instance is currently working.
func (m *dashboardModel) hasWorking() bool {
	for _, cs := range m.agents {
		if cs.Running && cs.Activity == agent.ActivityWorking {
			return true
		}
	}
	return false
}

// syncSpinner starts or stops the spinner animation based on working state.
func (m *dashboardModel) syncSpinner() tea.Cmd {
	if m.hasWorking() {
		if !m.spinning {
			m.spinning = true
			return spinnerTickCmd()
		}
	} else {
		m.spinning = false
	}
	return nil
}

func (m dashboardModel) Init() tea.Cmd {
	// Watcher handles bootstrap -- no command needed.
	return nil
}

func (m dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinnerTickMsg:
		if m.hasWorking() {
			m.frame++
			m.spinning = true
			return m, spinnerTickCmd()
		}
		m.spinning = false
		return m, nil

	case watcher.StateMsg:
		// Full state snapshot from watcher (bootstrap + structural changes).
		m.sessions = msg.Sessions
		m.agents = msg.Agents
		m.current = msg.Current
		m.loading = false
		m.rebuildItems()
		return m, m.syncSpinner()

	case watcher.AgentUpdateMsg:
		// Incremental agent status update from watcher.
		m.agents = agent.ApplyUpdates(m.agents, msg.Updates)
		m.rebuildItems()
		return m, m.syncSpinner()

	case watcher.FocusChangedMsg:
		// User switched pane/window/session externally.
		m.current = msg.Current
		m.rebuildItems()
		return m, nil

	case watcher.GitUpdateMsg:
		// Update git info on panes.
		for i := range m.items {
			if info, ok := msg.GitInfo[m.items[i].pane.WorkingDir]; ok {
				m.items[i].pane.Git = info
			}
		}
		return m, nil

	case watcher.ErrMsg:
		return m, nil

	case paneKilledMsg:
		return m, nil

	case paneMovedMsg:
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFiltering(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m dashboardModel) updateNormal(msg tea.KeyMsg) (dashboardModel, tea.Cmd) {
	// Kill confirm: waiting for y/n.
	if m.confirmKill {
		switch msg.String() {
		case "y":
			cmd := killPaneCmd(m.killTarget)
			m.confirmKill = false
			m.killTarget = ""
			return m, cmd
		default:
			m.confirmKill = false
			m.killTarget = ""
		}
		return m, nil
	}

	// Move mode: navigate to destination and drop.
	if m.moving {
		switch msg.String() {
		case "j", "down":
			m.moveCursor(1)
		case "k", "up":
			m.moveCursor(-1)
		case "enter":
			dst := m.selectedEntry()
			if dst != nil && dst.pane.ID != m.moveSrc {
				cmd := movePaneCmd(m.moveSrc, dst.pane.ID)
				m.moving = false
				m.moveSrc = ""
				return m, cmd
			}
			m.moving = false
			m.moveSrc = ""
			return m, nil
		case "esc":
			m.moving = false
			m.moveSrc = ""
		}
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		if len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
	case "m":
		entry := m.selectedEntry()
		if entry != nil {
			m.moving = true
			m.moveSrc = entry.pane.ID
		}
	case "x":
		entry := m.selectedEntry()
		if entry != nil && !entry.isCurrent {
			m.confirmKill = true
			m.killTarget = entry.pane.ID
		}
	case "enter":
		entry := m.selectedEntry()
		if entry != nil && !entry.isCurrent {
			m.postAction = &PostAction{PaneID: entry.pane.ID}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m dashboardModel) updateFiltering(msg tea.KeyMsg) (dashboardModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filtered = nil
		return m, nil
	case "enter":
		entry := m.selectedEntry()
		if entry != nil && !entry.isCurrent {
			m.postAction = &PostAction{PaneID: entry.pane.ID}
			return m, tea.Quit
		}
		return m, nil
	case "up", "ctrl+p":
		m.moveCursor(-1)
		return m, nil
	case "down", "ctrl+n":
		m.moveCursor(1)
		return m, nil
	}

	// Pass to text input.
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m *dashboardModel) moveCursor(delta int) {
	count := m.visibleCount()
	if count == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= count {
		m.cursor = count - 1
	}
}

func (m *dashboardModel) visibleCount() int {
	if m.filtered != nil {
		return len(m.filtered)
	}
	return len(m.items)
}

func (m *dashboardModel) selectedEntry() *paneEntry {
	if m.filtered != nil {
		if m.cursor >= 0 && m.cursor < len(m.filtered) {
			return &m.items[m.filtered[m.cursor]]
		}
		return nil
	}
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return &m.items[m.cursor]
	}
	return nil
}

func (m *dashboardModel) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = nil
		return
	}

	m.filtered = nil
	for i, entry := range m.items {
		searchable := strings.ToLower(
			entry.session + " " + entry.pane.Git.RepoName + " " + entry.pane.WorkingDir + " " + entry.pane.Command + " " + entry.pane.Git.Branch,
		)
		if strings.Contains(searchable, query) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m *dashboardModel) rebuildItems() {
	m.items = nil
	for _, sess := range m.sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				isCurrent := sess.Name == m.current.Session &&
					win.Index == m.current.Window &&
					pane.Index == m.current.Pane

				cs, hasAgent := m.agents[pane.ID]
				m.items = append(m.items, paneEntry{
					pane:      pane,
					session:   sess.Name,
					window:    win,
					agent:     cs,
					hasAgent:  hasAgent && cs.Running,
					isCurrent: isCurrent,
				})
			}
		}
	}

	// Focus a specific session if requested by the finder.
	if m.focusSession != "" {
		for i, entry := range m.items {
			if entry.session == m.focusSession {
				m.cursor = i
				m.focusSession = ""
				break
			}
		}
	}

	// Clamp cursor.
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}

	// Re-apply filter if active.
	if m.filtering {
		m.applyFilter()
	}
}

func (m dashboardModel) View() string {
	if m.loading {
		return "  Loading...\n"
	}

	// Determine which items are visible.
	visible := make(map[int]bool)
	visibleList := m.filtered
	if visibleList == nil {
		for i := range m.items {
			visible[i] = true
		}
	} else {
		for _, idx := range visibleList {
			visible[idx] = true
		}
	}

	// Pass 1: compute column values and max widths for all visible panes.
	allCols := make([]paneColumns, len(m.items))
	widths := fixedPaneColumnWidths()
	colIndices := dashboardColumnIndexes(m.cfg.Columns)
	for i, entry := range m.items {
		if !visible[i] {
			continue
		}
		pc := m.paneLineCols(entry)
		allCols[i] = pc
		for c := 0; c < numPaneCols; c++ {
			if len(pc.cols[c]) > widths[c] {
				widths[c] = len(pc.cols[c])
			}
		}
	}

	// Pass 2: render with aligned columns.
	lastSession := ""
	lastWindow := -1
	visibleIdx := 0
	selectedLine := -1
	var lines []string

	for i, entry := range m.items {
		if !visible[i] {
			continue
		}

		// Session header.
		if entry.session != lastSession {
			if lastSession != "" {
				lines = append(lines, "")
			}
			label := ""
			for _, sess := range m.sessions {
				if sess.Name == entry.session && sess.Attached {
					label = attachLabel
				}
			}
			lines = append(lines, fmt.Sprintf(" %s%s", sessionStyle.Render(entry.session), label))
			lastSession = entry.session
			lastWindow = -1
		}

		// Window header.
		if entry.window.Index != lastWindow {
			if showWindowHeader(m.cfg, m.sessions, entry.session) {
				active := ""
				if entry.window.Active {
					active = "*"
				}
				label := fmt.Sprintf("%s%s", entry.window.Name, active)
				lines = append(lines, fmt.Sprintf("   %s", windowStyle.Render(label)))
			}
			lastWindow = entry.window.Index
		}

		// Pane line.
		line := renderPaneLine(allCols[i], widths, colIndices)

		isSelected := visibleIdx == m.cursor
		isMoveSrc := m.moving && entry.pane.ID == m.moveSrc
		if isMoveSrc {
			line = moveSrcStyle.Render(line)
		} else if isSelected {
			line = selectedStyle.Render(line)
			selectedLine = len(lines)
		} else if entry.isCurrent {
			line = currentStyle.Render(line)
		}

		lines = append(lines, line)
		visibleIdx++
	}

	// Status bar.
	var help string
	if m.confirmKill {
		help = waitingStyle.Render(" kill pane? (y/n)")
	} else if m.filtering {
		help = " " + m.filter.View()
	} else if m.moving {
		help = moveSrcStyle.Render(" MOVE: j/k navigate  enter: drop here  esc: cancel")
	} else {
		help = helpStyle.Render(" j/k: navigate  enter: jump  m: move  x: kill  /: find  q: quit")
	}

	// In debug mode, append hook status indicator.
	if os.Getenv("CMS_DEBUG") != "" && m.watcher != nil {
		hookCount, hookListening := m.watcher.HookStats()
		var tag string
		if !hookListening {
			tag = dimStyle.Render(" [hooks: off]")
		} else if hookCount > 0 {
			tag = workingStyle.Render(fmt.Sprintf(" [hooks: %d active]", hookCount))
		} else {
			tag = dimStyle.Render(" [hooks: listening]")
		}
		help += tag
	}

	return renderDashboardViewport(lines, selectedLine, help, m.width, m.height, m.cfg)
}

func showWindowHeader(cfg config.DashboardConfig, sessions []tmux.Session, sessionName string) bool {
	switch cfg.WindowHeaders {
	case "always":
		return true
	case "never":
		return false
	}
	for _, sess := range sessions {
		if sess.Name != sessionName {
			continue
		}
		if len(sess.Windows) != 1 {
			return true
		}
		return len(sess.Windows[0].Panes) > 1
	}
	return true
}

// paneColumns holds the column values for a single pane line.
// Columns: name, branch, command, activity, context, mode
const numPaneCols = 6

type paneColumns struct {
	indicator string
	cols      [numPaneCols]string // plain text for width calculation
	styled    [numPaneCols]string // styled text for rendering
}

func (m dashboardModel) paneLineCols(entry paneEntry) paneColumns {
	pc := paneColumns{}

	// Indicator.
	pc.indicator = "  "
	if entry.hasAgent {
		switch entry.agent.Activity {
		case agent.ActivityWorking:
			frame := workingFramesUI[m.frame%len(workingFramesUI)]
			style := workingStyle
			if entry.agent.Mode == agent.ModeBypassPermissions || entry.agent.Mode == agent.ModeDangerFullAccess {
				style = ModeStyle(entry.agent)
			}
			pc.indicator = style.Render(frame) + " "
		case agent.ActivityWaitingInput:
			pc.indicator = waitingStyle.Render(waitingIndicator) + " "
		case agent.ActivityIdle:
			pc.indicator = idleStyle.Render(idleIndicator) + " "
		case agent.ActivityCompleted:
			pc.indicator = idleStyle.Render(idleIndicator) + " "
		default:
			pc.indicator = dimStyle.Render(unknownIndicator) + " "
		}
	}

	// Col 0: name (repo or dir).
	if entry.pane.Git.IsRepo {
		pc.cols[0] = entry.pane.Git.RepoName
		pc.styled[0] = entry.pane.Git.RepoName
	} else if dir := ShortenHome(entry.pane.WorkingDir); dir != "" {
		pc.cols[0] = dir
		pc.styled[0] = dir
	}

	// Col 1: branch/git.
	if entry.pane.Git.IsRepo {
		g := entry.pane.Git.Branch
		if entry.pane.Git.Dirty {
			g += "*"
		}
		if entry.pane.Git.Ahead > 0 {
			g += fmt.Sprintf("\u2191%d", entry.pane.Git.Ahead)
		}
		if entry.pane.Git.Behind > 0 {
			g += fmt.Sprintf("\u2193%d", entry.pane.Git.Behind)
		}
		pc.cols[1] = g
		pc.styled[1] = g
	}

	// Col 2: command (if not shell).
	cmd := entry.pane.Command
	if !proc.IsShellCommand(cmd) {
		pc.cols[2] = cmd
		pc.styled[2] = cmd
	}

	// Col 3: activity.
	if entry.hasAgent {
		if label, styled := renderAgentActivity(entry.agent); label != "" {
			pc.cols[3] = label
			pc.styled[3] = styled
		}
	}

	// Col 4: context %.
	if m.cfg.ShowContextPercentage && entry.hasAgent && entry.agent.ContextSet {
		txt := fmt.Sprintf("%d%%", entry.agent.ContextPct)
		pc.cols[4] = txt
		pc.styled[4] = ContextStyle(entry.agent.ContextPct).Render(txt)
	}

	// Col 5: mode.
	if entry.hasAgent {
		if modeStr := RenderMode(entry.agent); modeStr != "" {
			pc.cols[5] = entry.agent.ModeLabel
			pc.styled[5] = modeStr
		}
	}

	return pc
}

// renderAgentActivity returns the plain label and styled label for an agent's activity.
func renderAgentActivity(cs agent.AgentStatus) (string, string) {
	var style lipgloss.Style
	label := ""
	switch cs.Activity {
	case agent.ActivityWorking:
		style = workingStyle
		label = "working"
	case agent.ActivityWaitingInput:
		style = waitingStyle
		label = "waiting"
	case agent.ActivityIdle:
		style = idleStyle
		label = "idle"
	case agent.ActivityCompleted:
		style = idleStyle
		label = "completed"
	default:
		return "", ""
	}
	return label, style.Render(label)
}

func renderPaneLine(pc paneColumns, widths [numPaneCols]int, indices []int) string {
	var parts []string
	for _, i := range indices {
		if widths[i] == 0 {
			continue
		}
		if pc.cols[i] == "" {
			continue
		}
		cell := pc.styled[i]
		pad := widths[i] - len(pc.cols[i])
		if pad > 0 {
			cell += strings.Repeat(" ", pad)
		}
		parts = append(parts, cell)
	}
	return fmt.Sprintf("   %s%s", pc.indicator, strings.Join(parts, separatorStyle.Render(columnSeparatorUI)))
}

func dashboardColumnIndexes(ordered []string) []int {
	if len(ordered) == 0 {
		ordered = config.DefaultDashboardConfig().Columns
	}
	var idx []int
	for _, col := range ordered {
		switch col {
		case "name":
			idx = append(idx, 0)
		case "branch":
			idx = append(idx, 1)
		case "command":
			idx = append(idx, 2)
		case "activity":
			idx = append(idx, 3)
		case "context":
			idx = append(idx, 4)
		case "mode":
			idx = append(idx, 5)
		}
	}
	return idx
}

func fixedPaneColumnWidths() [numPaneCols]int {
	var widths [numPaneCols]int
	widths[3] = len("completed")
	widths[4] = len("100%")
	widths[5] = len("danger-full-access")
	return widths
}

// renderScrolledContent writes visible lines into b, scrolled to keep selectedLine visible.
func renderScrolledContent(b *strings.Builder, lines []string, selectedLine, contentHeight int) {
	start := 0
	if selectedLine >= contentHeight {
		start = selectedLine - contentHeight + 1
	}
	if maxStart := max(0, len(lines)-contentHeight); start > maxStart {
		start = maxStart
	}
	end := min(len(lines), start+contentHeight)
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	for i := end - start; i < contentHeight; i++ {
		b.WriteString("\n")
	}
}

func renderDashboardViewport(lines []string, selectedLine int, help string, width, height int, cfg config.DashboardConfig) string {
	if height <= 1 {
		if len(lines) == 0 {
			return help
		}
		return lines[0] + "\n" + help
	}

	var b strings.Builder
	if height <= 3 {
		renderScrolledContent(&b, lines, selectedLine, max(0, height-1))
		b.WriteString(help)
		return b.String()
	}

	footerRows := 1
	if cfg.FooterPadding {
		footerRows++
	}
	if cfg.FooterSeparator {
		footerRows++
	}
	contentHeight := max(0, height-footerRows)
	renderScrolledContent(&b, lines, selectedLine, contentHeight)
	if cfg.FooterPadding {
		b.WriteString("\n")
	}
	if cfg.FooterSeparator {
		b.WriteString(renderDashboardFooterBorder(width))
		b.WriteString("\n")
	}
	b.WriteString(help)
	return b.String()
}

func renderDashboardFooterBorder(width int) string {
	if width <= 0 {
		width = 24
	}
	return footerRuleStyle.Render(strings.Repeat(footerSeparatorUI, width))
}
