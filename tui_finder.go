package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type itemKind int

const (
	kindSession itemKind = iota
	kindProject
)

type finderEntry struct {
	kind        itemKind
	sessionName string // for kindSession
	projectPath string // for kindProject
}

// postAction is an action to execute after the TUI exits.
// Used when the action must happen outside bubbletea (e.g. tmux attach).
type postAction struct {
	kind        itemKind
	sessionName string
	projectPath string
	paneID      string // direct pane switch (dashboard)
	priority    []string
	sessions    []Session
	claude      map[string]ClaudeStatus
}

type finderModel struct {
	picker  pickerModel
	entries []finderEntry // parallel to picker items

	// Async loading state.
	sessData    []Session                // raw session data for Claude enrichment
	claudeData  map[string]ClaudeStatus // latest Claude status for smart switch
	sessions    []PickerItem
	sessIdx  []finderEntry
	projects []PickerItem
	projIdx  []finderEntry
	hasSess  bool
	hasProj  bool

	done          bool
	action        *postAction // action to run after TUI exits
	focusSession  string // session name to focus in dashboard on esc
	cfg           Config
	width  int
	height int
}

func newFinderModel(cfg Config, width, height int) finderModel {
	return finderModel{
		cfg:    cfg,
		width:  width,
		height: height,
	}
}

func (m finderModel) Init() tea.Cmd {
	// Fetch sessions (fast), projects, and Claude detection all concurrently.
	return tea.Batch(
		fetchSessionsFastCmd(),
		scanProjectsCmd(m.cfg),
	)
}

// --- Messages ---

type sessionsLoadedMsg struct {
	sessions []Session
	pt       procTable
}

type claudeLoadedMsg struct {
	claude map[string]ClaudeStatus
}

type projectsScannedMsg struct {
	projects []Project
}

// Phase 1: just tmux state — instant.
func fetchSessionsFastCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, pt, err := FetchState()
		if err != nil {
			return sessionsLoadedMsg{}
		}
		return sessionsLoadedMsg{sessions, pt}
	}
}

// Phase 2a: Claude detection — can take 600ms+.
func fetchClaudeForFinderCmd(sessions []Session, pt procTable) tea.Cmd {
	return func() tea.Msg {
		return claudeLoadedMsg{detectAllClaude(sessions, pt)}
	}
}

// Phase 2b: project scan.
func scanProjectsCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		return projectsScannedMsg{ScanProjects(cfg)}
	}
}

// Periodic refresh: fetch sessions + Claude in one shot.
func fetchSessionsWithClaudeCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, pt, err := FetchState()
		if err != nil {
			return sessionsLoadedMsg{}
		}
		claude := detectAllClaude(sessions, pt)
		return sessionsWithClaudeMsg{sessions, pt, claude}
	}
}

type sessionsWithClaudeMsg struct {
	sessions []Session
	pt       procTable
	claude   map[string]ClaudeStatus
}

type finderTickMsg struct{}

func finderTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return finderTickMsg{}
	})
}

// buildSessionItems populates session picker items from raw session data.
func (m *finderModel) buildSessionItems(claude map[string]ClaudeStatus) {
	m.sessions = nil
	m.sessIdx = nil
	for _, sess := range m.sessData {
		desc := fmt.Sprintf("%d windows", len(sess.Windows))
		if cs := claudeSummary(sess, claude); cs != "" {
			desc += " · " + cs
		}
		if sess.Attached {
			desc += " · attached"
		}
		m.sessions = append(m.sessions, PickerItem{
			Title:       sess.Name,
			Description: desc,
			FilterValue: sess.Name,
			Active:      true,
		})
		m.sessIdx = append(m.sessIdx, finderEntry{
			kind:        kindSession,
			sessionName: sess.Name,
		})
	}
}

func claudeSummary(sess Session, claude map[string]ClaudeStatus) string {
	if claude == nil {
		return ""
	}

	var working, waiting, idle int
	var workingCtx, waitingCtx, idleCtx int

	for _, win := range sess.Windows {
		for _, pane := range win.Panes {
			cs, ok := claude[pane.ID]
			if !ok || !cs.Running {
				continue
			}
			switch cs.Activity {
			case ActivityWorking:
				working++
				workingCtx = max(workingCtx, cs.ContextPct)
			case ActivityWaitingInput:
				waiting++
				waitingCtx = max(waitingCtx, cs.ContextPct)
			default:
				idle++
				idleCtx = max(idleCtx, cs.ContextPct)
			}
		}
	}

	total := working + waiting + idle
	if total == 0 {
		return ""
	}

	var parts []string
	if working > 0 {
		parts = append(parts, fmt.Sprintf("⚡%d %d%%", working, workingCtx))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("❓%d %d%%", waiting, waitingCtx))
	}
	if idle > 0 {
		parts = append(parts, fmt.Sprintf("💤%d %d%%", idle, idleCtx))
	}
	return fmt.Sprintf("%d claude · %s", total, fmt.Sprintf("%s", joinParts(parts)))
}

func joinParts(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += " · "
		}
		s += p
	}
	return s
}

func (m finderModel) Update(msg tea.Msg) (finderModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		// Sessions arrived — show immediately, fire Claude detection.
		m.sessData = msg.sessions
		m.buildSessionItems(nil)
		m.hasSess = true
		m.rebuildPicker()
		return m, fetchClaudeForFinderCmd(msg.sessions, msg.pt)

	case claudeLoadedMsg:
		// Claude status arrived — update session descriptions, schedule refresh.
		m.claudeData = msg.claude
		m.buildSessionItems(msg.claude)
		m.rebuildPicker()
		return m, finderTickCmd()

	case finderTickMsg:
		// Periodic refresh: re-fetch sessions + Claude status.
		return m, fetchSessionsWithClaudeCmd()

	case sessionsWithClaudeMsg:
		// Refresh arrived — update sessions with latest Claude status.
		m.sessData = msg.sessions
		m.claudeData = msg.claude
		m.buildSessionItems(msg.claude)
		m.hasSess = true
		m.rebuildPicker()
		return m, finderTickCmd()

	case projectsScannedMsg:
		m.projects = nil
		m.projIdx = nil
		for _, p := range msg.projects {
			desc := shortenHome(p.Path)
			if p.Git.Branch != "" {
				g := p.Git.Branch
				if p.Git.Dirty {
					g += "*"
				}
				desc += " · " + g
			}
			m.projects = append(m.projects, PickerItem{
				Title:       p.Name,
				Description: desc,
				FilterValue: p.Name + " " + p.Path,
			})
			m.projIdx = append(m.projIdx, finderEntry{
				kind:        kindProject,
				projectPath: p.Path,
			})
		}
		m.hasProj = true
		m.rebuildPicker()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.width = msg.Width
		m.picker.height = msg.Height
		return m, nil
	}

	if !m.hasSess && !m.hasProj {
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)

	if m.picker.done {
		if m.picker.chosen >= 0 && m.picker.chosen < len(m.entries) {
			entry := m.entries[m.picker.chosen]

			// Check if Enter was pressed (item was explicitly selected).
			if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "enter" {
				m.action = &postAction{
					kind:        entry.kind,
					sessionName: entry.sessionName,
					projectPath: entry.projectPath,
					priority:    m.cfg.SwitchPriority,
					sessions:    m.sessData,
					claude:      m.claudeData,
				}
			}

			// Esc: pass the selected session name to dashboard for focus.
			if entry.kind == kindSession {
				m.focusSession = entry.sessionName
			}
		}
		m.done = true
	}
	return m, cmd
}

// rebuildPicker merges sessions + projects into the picker.
// Sessions come first (lower indices = bottom of fzf-style display, near input).
// Projects that already have a matching session are excluded.
func (m *finderModel) rebuildPicker() {
	// Build set of active session names for dedup.
	activeNames := map[string]bool{}
	for _, e := range m.sessIdx {
		activeNames[NormalizeSessionName(e.sessionName)] = true
	}

	var items []PickerItem
	var entries []finderEntry

	// Sessions first (appear at bottom, nearest input).
	items = append(items, m.sessions...)
	entries = append(entries, m.sessIdx...)

	// Projects, excluding those that already have an active session.
	for i, p := range m.projects {
		normName := NormalizeSessionName(p.Title)
		if activeNames[normName] {
			continue
		}
		items = append(items, p)
		entries = append(entries, m.projIdx[i])
	}

	m.entries = entries

	// Preserve picker state across rebuilds.
	query := m.picker.Value()
	cursor := m.picker.cursor
	mode := m.picker.mode

	m.picker = newPicker("", items, m.cfg.EscapeChord, m.cfg.EscapeChordMs)
	m.picker.width = m.width
	m.picker.height = m.height
	m.picker.mode = mode
	if mode == pickerNormal {
		m.picker.input.Blur()
	}
	if query != "" {
		m.picker.input.SetValue(query)
		m.picker.applyFilter()
	}
	if cursor < m.picker.visibleCount() {
		m.picker.cursor = cursor
	}
}

func (m finderModel) View() string {
	if !m.hasSess && !m.hasProj {
		return "  Loading...\n"
	}
	return m.picker.View()
}
