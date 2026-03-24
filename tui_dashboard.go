package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles — initialized from config via initStyles().
var (
	sessionStyle  lipgloss.Style
	windowStyle   lipgloss.Style
	dimStyle      lipgloss.Style
	selectedStyle lipgloss.Style
	currentStyle  lipgloss.Style
	moveSrcStyle  lipgloss.Style
	workingStyle  lipgloss.Style
	waitingStyle  lipgloss.Style
	idleStyle     lipgloss.Style
	helpStyle     lipgloss.Style
	attachLabel   string

	claudeAccentStyle lipgloss.Style
	codexAccentStyle  lipgloss.Style
	claudePlanStyle   lipgloss.Style
	claudeAcceptStyle lipgloss.Style
	claudeDangerStyle lipgloss.Style
	claudeSafeStyle   lipgloss.Style
	codexPlanStyle    lipgloss.Style
	codexAcceptStyle  lipgloss.Style
	codexDangerStyle  lipgloss.Style
	codexSafeStyle    lipgloss.Style

	ctxLowStyle  lipgloss.Style
	ctxMidStyle  lipgloss.Style
	ctxHighStyle lipgloss.Style
)

func initStyles(c ColorsConfig) {
	sessionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Shared.Session))
	windowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Window))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Dim))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color(c.Shared.Selected))
	currentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Current))
	moveSrcStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.MoveSrc)).Bold(true)
	workingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Working))
	waitingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Waiting))
	idleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Idle))
	helpStyle = dimStyle
	attachLabel = dimStyle.Render(" (attached)")

	claudeAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Claude.Accent)).Bold(true)
	codexAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Codex.Accent)).Bold(true)
	claudePlanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Claude.Plan))
	claudeAcceptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Claude.Accept))
	claudeDangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Claude.Danger)).Bold(true)
	claudeSafeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Claude.Safe))
	codexPlanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Codex.Plan))
	codexAcceptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Codex.Accept))
	codexDangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Codex.Danger)).Bold(true)
	codexSafeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Codex.Safe))

	ctxLowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxLow))
	ctxMidStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxMid))
	ctxHighStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxHigh))

	// Picker styles.
	pickerSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color(c.Shared.Selected)).Foreground(lipgloss.Color(c.Shared.Current)).Bold(true)
	pickerNormalStyle = lipgloss.NewStyle()
	pickerDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Window))
	pickerMatchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Working)).Bold(true)
	pickerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Shared.Session))
	pickerCountStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Dim))
}

func contextStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 80:
		return ctxHighStyle
	case pct >= 50:
		return ctxMidStyle
	default:
		return ctxLowStyle
	}
}

// paneEntry is a navigable item in the pane list.
type paneEntry struct {
	pane      Pane
	session   string
	window    Window
	agent     AgentStatus
	hasAgent  bool
	isCurrent bool
}

type dashboardModel struct {
	// Data.
	sessions []Session
	agents   map[string]AgentStatus
	current  CurrentTarget

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

	// Hysteresis.
	prevActivity map[string]Activity
	idleCount    map[string]int

	postAction *postAction // action to run after TUI exits

	loading bool
	width   int
	height  int
	frame   int // tick counter for spinner animation
}

const idleThreshold = 10

func newDashboardModel() dashboardModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 64

	return dashboardModel{
		prevActivity: map[string]Activity{},
		idleCount:    map[string]int{},
		filter:       ti,
		loading:      true,
	}
}

type spinnerTickMsg struct{}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// hasWorking reports whether any agent instance is currently working.
func (m *dashboardModel) hasWorking() bool {
	for _, cs := range m.agents {
		if cs.Running && cs.Activity == ActivityWorking {
			return true
		}
	}
	return false
}

func (m dashboardModel) Init() tea.Cmd {
	// Watcher handles bootstrap — no command needed.
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
			return m, spinnerTickCmd()
		}
		return m, nil

	case stateMsg:
		// Full state snapshot from watcher (bootstrap + structural changes).
		m.sessions = msg.sessions
		m.agents = msg.agents
		m.current = msg.current
		m.loading = false
		m.applyHysteresis()
		m.rebuildItems()
		if m.hasWorking() {
			return m, spinnerTickCmd()
		}
		return m, nil

	case agentUpdateMsg:
		// Incremental agent status update from watcher.
		if m.agents == nil {
			m.agents = map[string]AgentStatus{}
		}
		for id, status := range msg.updates {
			if status.Running {
				m.agents[id] = status
			} else {
				delete(m.agents, id)
			}
		}
		m.applyHysteresis()
		m.rebuildItems()
		if m.hasWorking() {
			return m, spinnerTickCmd()
		}
		return m, nil

	case focusChangedMsg:
		// User switched pane/window/session externally.
		m.current = msg.current
		m.rebuildItems()
		return m, nil

	case gitUpdateMsg:
		// Update git info on panes.
		for i := range m.items {
			if info, ok := msg.gitInfo[m.items[i].pane.WorkingDir]; ok {
				m.items[i].pane.Git = info
			}
		}
		return m, nil

	case errMsg:
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
			KillPane(m.killTarget)
			m.confirmKill = false
			m.killTarget = ""
			// Watcher will detect the structural change via control mode events.
			return m, nil
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
				MovePane(m.moveSrc, dst.pane.ID)
			}
			m.moving = false
			m.moveSrc = ""
			// Watcher will detect the structural change via control mode events.
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
			m.postAction = &postAction{paneID: entry.pane.ID}
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
			m.postAction = &postAction{paneID: entry.pane.ID}
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

func (m *dashboardModel) applyHysteresis() {
	for id, cs := range m.agents {
		if cs.Activity == ActivityWorking {
			m.idleCount[id] = 0
		} else if shouldHoldWorking(cs) && m.prevActivity[id] == ActivityWorking && cs.Activity == ActivityIdle {
			m.idleCount[id]++
			if m.idleCount[id] < idleThreshold {
				cs.Activity = ActivityWorking
				m.agents[id] = cs
			}
		}
		m.prevActivity[id] = m.agents[id].Activity
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
	var widths [numPaneCols]int
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
			active := ""
			if entry.window.Active {
				active = "*"
			}
			lines = append(lines, fmt.Sprintf("   %s", windowStyle.Render(fmt.Sprintf("%s%s", entry.window.Name, active))))
			lastWindow = entry.window.Index
		}

		// Pane line.
		line := renderPaneLine(allCols[i], widths)

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
		help = waitingStyle.Render("  kill pane? (y/n)")
	} else if m.filtering {
		help = "  " + m.filter.View()
	} else if m.moving {
		help = moveSrcStyle.Render("  MOVE: j/k navigate  enter: drop here  esc: cancel")
	} else {
		help = helpStyle.Render("  j/k: navigate  enter: jump  m: move  x: kill  /: find  q: quit")
	}

	return renderDashboardViewport(lines, selectedLine, help, m.height)
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// paneColumns holds the column values for a single pane line.
// Columns: name, branch, command, activity, context, mode
const numPaneCols = 6

type paneColumns struct {
	indicator string
	paneIdx   int
	cols      [numPaneCols]string // plain text for width calculation
	styled    [numPaneCols]string // styled text for rendering
}

func (m dashboardModel) paneLineCols(entry paneEntry) paneColumns {
	pc := paneColumns{paneIdx: entry.pane.Index}

	// Indicator.
	pc.indicator = "  "
	if entry.hasAgent {
		switch entry.agent.Activity {
		case ActivityWorking:
			frame := spinnerFrames[m.frame%len(spinnerFrames)]
			style := workingStyle
			if entry.agent.Mode == ModeBypassPermissions || entry.agent.Mode == ModeDangerFullAccess {
				style = modeStyle(entry.agent)
			}
			pc.indicator = style.Render(frame) + " "
		case ActivityWaitingInput:
			pc.indicator = waitingStyle.Render("?") + " "
		case ActivityIdle:
			pc.indicator = dimStyle.Render("●") + " "
		default:
			pc.indicator = dimStyle.Render("·") + " "
		}
	}

	// Col 0: name (repo or dir).
	if entry.pane.Git.IsRepo {
		pc.cols[0] = entry.pane.Git.RepoName
		pc.styled[0] = entry.pane.Git.RepoName
	} else if dir := shortenHome(entry.pane.WorkingDir); dir != "" {
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
			g += fmt.Sprintf("↑%d", entry.pane.Git.Ahead)
		}
		if entry.pane.Git.Behind > 0 {
			g += fmt.Sprintf("↓%d", entry.pane.Git.Behind)
		}
		pc.cols[1] = g
		pc.styled[1] = g
	}

	// Col 2: command (if not shell).
	cmd := entry.pane.Command
	if !isShellCommand(cmd) {
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
	if entry.hasAgent && entry.agent.ContextSet {
		txt := fmt.Sprintf("%d%%", entry.agent.ContextPct)
		pc.cols[4] = txt
		pc.styled[4] = contextStyle(entry.agent.ContextPct).Render(txt)
	}

	// Col 5: mode.
	if entry.hasAgent {
		if modeStr := renderMode(entry.agent); modeStr != "" {
			pc.cols[5] = entry.agent.ModeLabel
			pc.styled[5] = modeStr
		}
	}

	return pc
}

func renderPaneLine(pc paneColumns, widths [numPaneCols]int) string {
	last := -1
	for i := 0; i < numPaneCols; i++ {
		if pc.cols[i] != "" {
			last = i
		}
	}

	var parts []string
	for i := 0; i <= last; i++ {
		if widths[i] == 0 {
			continue
		}
		cell := pc.styled[i]
		pad := widths[i] - len(pc.cols[i])
		if pad > 0 {
			cell += strings.Repeat(" ", pad)
		}
		parts = append(parts, cell)
	}
	return fmt.Sprintf("   %s%d │ %s", pc.indicator, pc.paneIdx, strings.Join(parts, " │ "))
}

func renderAgentActivity(cs AgentStatus) (string, string) {
	var style lipgloss.Style
	label := ""
	switch cs.Activity {
	case ActivityWorking:
		style = workingStyle
		label = "working"
	case ActivityWaitingInput:
		style = waitingStyle
		label = "waiting"
	case ActivityIdle:
		style = idleStyle
		label = "idle"
	default:
		return "", ""
	}
	return label, style.Render(label)
}

func renderMode(status AgentStatus) string {
	if status.ModeLabel == "" {
		return ""
	}
	return modeStyle(status).Render(status.ModeLabel)
}

func providerAccent(provider Provider) lipgloss.Style {
	switch provider {
	case ProviderClaude:
		return claudeAccentStyle
	case ProviderCodex:
		return codexAccentStyle
	default:
		return dimStyle
	}
}

func modeStyle(status AgentStatus) lipgloss.Style {
	switch status.Provider {
	case ProviderCodex:
		switch status.Mode {
		case ModePlan:
			return codexPlanStyle
		case ModeAcceptEdits:
			return codexAcceptStyle
		case ModeReadOnly, ModeWorkspaceWrite:
			return codexSafeStyle
		case ModeBypassPermissions, ModeDangerFullAccess:
			return codexDangerStyle
		default:
			return codexAccentStyle
		}
	case ProviderClaude:
		switch status.Mode {
		case ModePlan:
			return claudePlanStyle
		case ModeAcceptEdits:
			return claudeAcceptStyle
		case ModeBypassPermissions, ModeDangerFullAccess:
			return claudeDangerStyle
		case ModeReadOnly, ModeWorkspaceWrite:
			return claudeSafeStyle
		default:
			return claudeAccentStyle
		}
	default:
		return dimStyle
	}
}

func renderDashboardViewport(lines []string, selectedLine int, help string, height int) string {
	if height <= 1 {
		if len(lines) == 0 {
			return help
		}
		return lines[0] + "\n" + help
	}

	contentHeight := height - 1
	start := 0
	if selectedLine >= contentHeight {
		start = selectedLine - contentHeight + 1
	}
	if maxStart := max(0, len(lines)-contentHeight); start > maxStart {
		start = maxStart
	}
	end := min(len(lines), start+contentHeight)

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	for i := end - start; i < contentHeight; i++ {
		b.WriteString("\n")
	}
	b.WriteString(help)
	return b.String()
}
