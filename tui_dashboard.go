package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles.
var (
	sessionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	windowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	currentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	moveSrcStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	workingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // orange
	waitingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	idleStyle     = dimStyle
	helpStyle     = dimStyle
	attachLabel   = dimStyle.Render(" (attached)")

	// Mode styles.
	modePlanStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))  // blue
	modeAcceptStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))  // yellow
	modeYoloStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red bold
)

func contextStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 80:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	case pct >= 50:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	}
}

// paneEntry is a navigable item in the pane list.
type paneEntry struct {
	pane      Pane
	session   string
	window    Window
	claude    ClaudeStatus
	hasClaude bool
	isCurrent bool
}

type dashboardModel struct {
	// Data.
	sessions []Session
	claude   map[string]ClaudeStatus
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

func (m dashboardModel) Init() tea.Cmd {
	return fetchFastStateCmd()
}

func (m dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickStartMsg:
		return m, fetchFastStateCmd()

	case fastStateMsg:
		m.sessions = msg.sessions
		m.current = msg.current
		m.loading = false
		m.frame++
		m.rebuildItems()
		// Kick off Claude detection in background (can take 600ms+).
		return m, fetchClaudeCmd(msg.sessions, msg.pt)

	case claudeMsg:
		m.claude = msg.claude
		m.applyHysteresis()
		m.rebuildItems()
		return m, tickCmd()

	case errMsg:
		return m, tickCmd()

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
			return m, fetchFastStateCmd()
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
			// Refresh immediately.
			return m, fetchFastStateCmd()
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

				cs, hasClaude := m.claude[pane.ID]
				m.items = append(m.items, paneEntry{
					pane:      pane,
					session:   sess.Name,
					window:    win,
					claude:    cs,
					hasClaude: hasClaude && cs.Running,
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
	for id, cs := range m.claude {
		if cs.Activity == ActivityWorking {
			m.idleCount[id] = 0
		} else if m.prevActivity[id] == ActivityWorking && cs.Activity == ActivityIdle {
			m.idleCount[id]++
			if m.idleCount[id] < idleThreshold {
				cs.Activity = ActivityWorking
				m.claude[id] = cs
			}
		}
		m.prevActivity[id] = m.claude[id].Activity
	}
}

func (m dashboardModel) View() string {
	if m.loading {
		return "  Loading...\n"
	}

	var b strings.Builder

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

	// Track which session/window headers we've rendered.
	lastSession := ""
	lastWindow := -1
	visibleIdx := 0

	for i, entry := range m.items {
		if !visible[i] {
			// Still track headers for filtered items.
			continue
		}

		// Session header.
		if entry.session != lastSession {
			if lastSession != "" {
				b.WriteString("\n")
			}
			label := ""
			for _, sess := range m.sessions {
				if sess.Name == entry.session && sess.Attached {
					label = attachLabel
				}
			}
			fmt.Fprintf(&b, " %s%s\n", sessionStyle.Render(entry.session), label)
			lastSession = entry.session
			lastWindow = -1
		}

		// Window header.
		if entry.window.Index != lastWindow {
			active := ""
			if entry.window.Active {
				active = "*"
			}
			fmt.Fprintf(&b, "   %s\n", windowStyle.Render(fmt.Sprintf("%s%s", entry.window.Name, active)))
			lastWindow = entry.window.Index
		}

		// Pane line.
		line := m.renderPaneLine(entry)

		isSelected := visibleIdx == m.cursor
		isMoveSrc := m.moving && entry.pane.ID == m.moveSrc
		if isMoveSrc {
			line = moveSrcStyle.Render(line)
		} else if isSelected {
			line = selectedStyle.Render(line)
		} else if entry.isCurrent {
			line = currentStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
		visibleIdx++
	}

	content := b.String()

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

	return pinBottom(content, help, m.height)
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m dashboardModel) renderPaneLine(entry paneEntry) string {
	// Left-side indicator for Claude panes.
	indicator := "  " // no claude
	if entry.hasClaude {
		switch entry.claude.Activity {
		case ActivityWorking:
			frame := spinnerFrames[m.frame%len(spinnerFrames)]
			indicator = workingStyle.Render(frame) + " "
		case ActivityWaitingInput:
			indicator = waitingStyle.Render("?") + " "
		case ActivityIdle:
			indicator = dimStyle.Render("●") + " "
		default:
			indicator = dimStyle.Render("·") + " "
		}
	}

	parts := []string{}

	if entry.pane.Git.IsRepo {
		parts = append(parts, entry.pane.Git.RepoName)

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
		parts = append(parts, g)
	} else if dir := shortenHome(entry.pane.WorkingDir); dir != "" {
		parts = append(parts, dir)
	}

	cmd := entry.pane.Command
	if cmd != "fish" && cmd != "bash" && cmd != "zsh" {
		parts = append(parts, cmd)
	}

	if entry.hasClaude {
		parts = append(parts, renderClaude(entry.claude))
		if m := renderMode(entry.claude.Mode); m != "" {
			parts = append(parts, m)
		}
	}

	return fmt.Sprintf("   %s%d │ %s", indicator, entry.pane.Index, strings.Join(parts, " │ "))
}

func renderClaude(cs ClaudeStatus) string {
	var style lipgloss.Style
	switch cs.Activity {
	case ActivityWorking:
		style = workingStyle
	case ActivityWaitingInput:
		style = waitingStyle
	default:
		style = idleStyle
	}

	c := style.Render(cs.Activity.String())
	if cs.ContextPct > 0 {
		c += " " + contextStyle(cs.ContextPct).Render(fmt.Sprintf("%d%%", cs.ContextPct))
	}
	return c
}

func renderMode(mode ClaudeMode) string {
	switch mode {
	case ModePlan:
		return modePlanStyle.Render("plan")
	case ModeAcceptEdits:
		return modeAcceptStyle.Render("auto-edit")
	case ModeYolo:
		return modeYoloStyle.Render("yolo")
	default:
		return ""
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickStartMsg(t)
	})
}

// fastStateMsg delivers tmux state without Claude detection (instant).
type fastStateMsg struct {
	sessions []Session
	pt       procTable
	current  CurrentTarget
}

// claudeMsg delivers Claude detection results (can take 600ms+).
type claudeMsg struct {
	claude map[string]ClaudeStatus
}

func fetchFastStateCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, pt, err := FetchState()
		if err != nil {
			return errMsg{err}
		}
		current, _ := FetchCurrentTarget()
		return fastStateMsg{sessions: sessions, pt: pt, current: current}
	}
}

func fetchClaudeCmd(sessions []Session, pt procTable) tea.Cmd {
	return func() tea.Msg {
		claude := detectAllClaude(sessions, pt)
		return claudeMsg{claude: claude}
	}
}
