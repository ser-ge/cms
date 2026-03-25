package tui

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/project"
	"github.com/serge/cms/internal/session"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/watcher"
)

type finderEntry struct {
	kind        ItemKind
	sessionName string // for KindSession
	projectPath string // for KindProject
}

type finderModel struct {
	picker  pickerModel
	entries []finderEntry // parallel to picker items

	// Session/agent state from watcher.
	sessData  []tmux.Session
	agentData map[string]agent.AgentStatus
	sessions  []PickerItem
	sessIdx   []finderEntry
	projects  []PickerItem
	projIdx   []finderEntry
	hasSess   bool
	hasProj   bool

	kind            FinderKind
	done            bool
	action          *PostAction // action to run after TUI exits
	focusSession    string      // session name to focus in dashboard on esc
	lastSessionName string      // cached tmux last session (updated on focus change)
	watcher         *watcher.Watcher
	cfg             config.Config
	width           int
	height          int
}

func newFinderModel(cfg config.Config, w *watcher.Watcher, kind FinderKind, width, height int) finderModel {
	m := finderModel{
		kind:    kind,
		cfg:     cfg,
		watcher: w,
		width:   width,
		height:  height,
	}

	// Cache the last session name once at init (avoid subprocess per rebuild).
	if cfg.General.LastSessionFirst {
		m.lastSessionName = tmux.FetchLastSession()
	}

	// Pre-populate sessions from watcher cache if this mode needs them.
	if kind != FinderProjects {
		sessions, agents, _ := w.CachedState()
		if len(sessions) > 0 {
			m.sessData = sessions
			m.agentData = agents
			m.buildSessionItems(agents)
			m.hasSess = true
		}
	}

	// For projects-only mode, mark sessions as "done" so we don't wait for them.
	if kind == FinderProjects {
		m.hasSess = true
	}
	// For sessions-only mode, mark projects as "done" so we don't wait for them.
	if kind == FinderSessions {
		m.hasProj = true
	}

	if m.hasSess || m.hasProj {
		m.rebuildPicker()
	}

	return m
}

func (m finderModel) Init() tea.Cmd {
	if m.kind == FinderSessions {
		return nil // sessions already loaded from cache
	}
	// Scan projects from disk (async).
	return scanProjectsCmd(m.cfg)
}

// --- Messages ---

type projectsScannedMsg struct {
	projects []project.Project
}

func scanProjectsCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		return projectsScannedMsg{project.Scan(cfg)}
	}
}

type providerSummary struct {
	total   int
	working int
	waiting int
	idle    int
	maxCtx  int
	hasCtx  bool
}

// buildSessionItems populates session picker items from raw session data.
func (m *finderModel) buildSessionItems(agents map[string]agent.AgentStatus) {
	m.sessions = nil
	m.sessIdx = nil
	for _, sess := range m.sessData {
		desc := fmt.Sprintf("%d windows", len(sess.Windows))
		if cs := m.agentSummary(sess, agents); cs != "" {
			desc += " \u00b7 " + cs
		}
		if sess.Attached {
			desc += " \u00b7 attached"
		}
		m.sessions = append(m.sessions, PickerItem{
			Title:       sess.Name,
			Description: desc,
			FilterValue: sess.Name,
			Active:      true,
		})
		m.sessIdx = append(m.sessIdx, finderEntry{
			kind:        KindSession,
			sessionName: sess.Name,
		})
	}
}

func (m finderModel) agentSummary(sess tmux.Session, agents map[string]agent.AgentStatus) string {
	if agents == nil {
		return ""
	}
	if len(m.cfg.Finder.ProviderOrder) == 0 {
		return ""
	}

	summaries := map[agent.Provider]*providerSummary{}

	for _, win := range sess.Windows {
		for _, pane := range win.Panes {
			cs, ok := agents[pane.ID]
			if !ok || !cs.Running {
				continue
			}
			if summaries[cs.Provider] == nil {
				summaries[cs.Provider] = &providerSummary{}
			}
			s := summaries[cs.Provider]
			s.total++
			switch cs.Activity {
			case agent.ActivityWorking:
				s.working++
			case agent.ActivityWaitingInput:
				s.waiting++
			default:
				s.idle++
			}
			if cs.ContextSet {
				s.maxCtx = max(s.maxCtx, cs.ContextPct)
				s.hasCtx = true
			}
		}
	}

	var parts []string
	for _, provider := range orderedProviders(m.cfg.Finder.ProviderOrder) {
		s := summaries[provider]
		if s == nil {
			continue
		}
		if s.total == 0 {
			continue
		}
		parts = append(parts, renderProviderSummary(provider, *s, m.cfg.Finder))
	}
	return JoinParts(parts)
}

func renderProviderSummary(provider agent.Provider, s providerSummary, cfg config.FinderConfig) string {
	label := ProviderAccent(provider).Render(provider.String())
	var states []string
	for _, state := range cfg.StateOrder {
		switch state {
		case "total":
			states = append(states, ProviderAccent(provider).Render(fmt.Sprintf("%d", s.total)))
		case "idle":
			if s.idle > 0 {
				states = append(states, idleStyle.Render(fmt.Sprintf("%s %d", idleIndicator, s.idle)))
			}
		case "working":
			if s.working > 0 {
				states = append(states, workingStyle.Render(fmt.Sprintf("\u26a1%d", s.working)))
			}
		case "waiting":
			if s.waiting > 0 {
				states = append(states, waitingStyle.Render(fmt.Sprintf("%s%d", waitingIndicator, s.waiting)))
			}
		}
	}
	state := JoinParts(states)
	if cfg.ShowContextPercentage && s.hasCtx {
		if state == "" {
			return fmt.Sprintf("%s %s", label, ContextStyle(s.maxCtx).Render(fmt.Sprintf("%d%%", s.maxCtx)))
		}
		return fmt.Sprintf("%s %s %s", label, state, ContextStyle(s.maxCtx).Render(fmt.Sprintf("%d%%", s.maxCtx)))
	}
	if state == "" {
		return label
	}
	return fmt.Sprintf("%s %s", label, state)
}

func orderedProviders(ordered []string) []agent.Provider {
	if len(ordered) == 0 {
		return nil
	}
	var providers []agent.Provider
	for _, name := range ordered {
		switch name {
		case "claude":
			providers = append(providers, agent.ProviderClaude)
		case "codex":
			providers = append(providers, agent.ProviderCodex)
		}
	}
	return providers
}

func (m finderModel) Update(msg tea.Msg) (finderModel, tea.Cmd) {
	switch msg := msg.(type) {
	case watcher.StateMsg:
		// Full state snapshot from watcher -- update sessions + agents.
		debug.Logf("finder: full state sessions=%d agents=%d", len(msg.Sessions), len(msg.Agents))
		m.sessData = msg.Sessions
		m.agentData = msg.Agents
		m.buildSessionItems(msg.Agents)
		m.hasSess = true
		m.rebuildPicker()
		return m, nil

	case watcher.AgentUpdateMsg:
		// Incremental agent update from watcher.
		debug.Logf("finder: agent update panes=%d", len(msg.Updates))
		m.agentData = agent.ApplyUpdates(m.agentData, msg.Updates)
		m.buildSessionItems(m.agentData)
		m.rebuildPicker()
		return m, nil

	case watcher.FocusChangedMsg:
		// User switched session -- refresh cached last session name.
		if m.cfg.General.LastSessionFirst {
			m.lastSessionName = tmux.FetchLastSession()
		}
		m.rebuildPicker()
		return m, nil

	case projectsScannedMsg:
		m.projects = nil
		m.projIdx = nil
		for _, p := range msg.projects {
			desc := ShortenHome(p.Path)
			if p.Git.Branch != "" {
				g := p.Git.Branch
				if p.Git.Dirty {
					g += "*"
				}
				desc += " \u00b7 " + g
			}
			m.projects = append(m.projects, PickerItem{
				Title:       p.Name,
				Description: desc,
				FilterValue: p.Name + " " + p.Path,
			})
			m.projIdx = append(m.projIdx, finderEntry{
				kind:        KindProject,
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
				m.action = &PostAction{
					Kind:        entry.kind,
					SessionName: entry.sessionName,
					ProjectPath: entry.projectPath,
					Priority:    m.cfg.General.SwitchPriority,
				}
			}

			// Esc: pass the selected session name to dashboard for focus.
			if entry.kind == KindSession {
				m.focusSession = entry.sessionName
			}
		}
		m.done = true
	}
	return m, cmd
}

// rebuildPicker merges sessions + projects into the picker.
// Sessions come first (lower indices = bottom of fzf-style display, near input).
// The attached session is placed last among sessions (furthest from input).
// Projects that already have a matching session are excluded.
func (m *finderModel) rebuildPicker() {
	var items []PickerItem
	var entries []finderEntry

	// Sessions: preserve tmux order, with optional last-session promotion and
	// attached-session demotion controlled by config.
	if m.kind != FinderProjects && len(m.sessions) > 0 {
		// Build index pairs so we can sort sessions + entries together.
		type indexedSession struct {
			idx         int
			attached    bool
			lastSession bool
		}
		ordered := make([]indexedSession, len(m.sessions))
		lastSessionName := m.lastSessionName
		for i := range m.sessions {
			name := m.sessIdx[i].sessionName
			var attached bool
			for _, sess := range m.sessData {
				if sess.Name == name {
					attached = sess.Attached
					break
				}
			}
			ordered[i] = indexedSession{
				idx:         i,
				attached:    attached,
				lastSession: lastSessionName != "" && name == lastSessionName,
			}
		}

		// Sort: optionally promote the tmux last session and optionally demote attached.
		sort.SliceStable(ordered, func(a, b int) bool {
			if ordered[a].lastSession != ordered[b].lastSession {
				return ordered[a].lastSession
			}
			if m.cfg.General.AttachedLast && ordered[a].attached != ordered[b].attached {
				return !ordered[a].attached // unattached before attached
			}
			return false
		})

		for _, o := range ordered {
			items = append(items, m.sessions[o.idx])
			entries = append(entries, m.sessIdx[o.idx])
		}
	}

	// Projects, excluding those that already have an active session.
	if m.kind != FinderSessions {
		activeNames := map[string]bool{}
		for _, e := range m.sessIdx {
			activeNames[session.NormalizeName(e.sessionName)] = true
		}
		for i, p := range m.projects {
			normName := session.NormalizeName(p.Title)
			if activeNames[normName] {
				continue
			}
			items = append(items, p)
			entries = append(entries, m.projIdx[i])
		}
	}

	m.entries = entries

	// Preserve picker state across rebuilds.
	query := m.picker.Value()
	cursor := m.picker.cursor
	mode := m.picker.mode

	m.picker = newPicker("", items, m.cfg.General.EscapeChord, m.cfg.General.EscapeChordMs)
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
