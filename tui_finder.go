package main

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

type itemKind int

const (
	kindSession itemKind = iota
	kindProject
)

// finderKind controls what the finder shows.
type finderKind int

const (
	finderAll      finderKind = iota // sessions + projects
	finderSessions                   // sessions only (cms switch)
	finderProjects                   // projects only (cms open)
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
	agents      map[string]AgentStatus
}

type finderModel struct {
	picker  pickerModel
	entries []finderEntry // parallel to picker items

	// Session/agent state from watcher.
	sessData  []Session
	agentData map[string]AgentStatus
	sessions  []PickerItem
	sessIdx   []finderEntry
	projects  []PickerItem
	projIdx   []finderEntry
	hasSess   bool
	hasProj   bool

	kind         finderKind
	done         bool
	action       *postAction // action to run after TUI exits
	focusSession string      // session name to focus in dashboard on esc
	watcher      *Watcher
	cfg          Config
	width        int
	height       int
}

func newFinderModel(cfg Config, watcher *Watcher, kind finderKind, width, height int) finderModel {
	m := finderModel{
		kind:    kind,
		cfg:     cfg,
		watcher: watcher,
		width:   width,
		height:  height,
	}

	// Pre-populate sessions from watcher cache if this mode needs them.
	if kind != finderProjects {
		sessions, agents, _ := watcher.CachedState()
		if len(sessions) > 0 {
			m.sessData = sessions
			m.agentData = agents
			m.buildSessionItems(agents)
			m.hasSess = true
		}
	}

	// For projects-only mode, mark sessions as "done" so we don't wait for them.
	if kind == finderProjects {
		m.hasSess = true
	}
	// For sessions-only mode, mark projects as "done" so we don't wait for them.
	if kind == finderSessions {
		m.hasProj = true
	}

	if m.hasSess || m.hasProj {
		m.rebuildPicker()
	}

	return m
}

func (m finderModel) Init() tea.Cmd {
	if m.kind == finderSessions {
		return nil // sessions already loaded from cache
	}
	// Scan projects from disk (async).
	return scanProjectsCmd(m.cfg)
}

// --- Messages ---

type projectsScannedMsg struct {
	projects []Project
}

func scanProjectsCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		return projectsScannedMsg{ScanProjects(cfg)}
	}
}

type providerSummary struct {
	total   int
	working int
	waiting int
	idle    int
	maxCtx  int
}

// buildSessionItems populates session picker items from raw session data.
func (m *finderModel) buildSessionItems(agents map[string]AgentStatus) {
	m.sessions = nil
	m.sessIdx = nil
	for _, sess := range m.sessData {
		desc := fmt.Sprintf("%d windows", len(sess.Windows))
		if cs := agentSummary(sess, agents); cs != "" {
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

func agentSummary(sess Session, agents map[string]AgentStatus) string {
	if agents == nil {
		return ""
	}

	summaries := map[Provider]*providerSummary{}

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
			case ActivityWorking:
				s.working++
			case ActivityWaitingInput:
				s.waiting++
			default:
				s.idle++
			}
			s.maxCtx = max(s.maxCtx, cs.ContextPct)
		}
	}

	var parts []string
	for _, provider := range knownProviders() {
		s := summaries[provider]
		if s == nil {
			continue
		}
		if s.total == 0 {
			continue
		}
		parts = append(parts, renderProviderSummary(provider, *s))
	}
	return joinParts(parts)
}

func renderProviderSummary(provider Provider, s providerSummary) string {
	label := providerAccent(provider).Render(provider.String())
	var states []string
	if s.waiting > 0 {
		states = append(states, waitingStyle.Render(fmt.Sprintf("❓%d", s.waiting)))
	}
	if s.working > 0 {
		states = append(states, workingStyle.Render(fmt.Sprintf("⚡%d", s.working)))
	}
	if s.idle > 0 {
		states = append(states, idleStyle.Render(fmt.Sprintf("●%d", s.idle)))
	}
	state := joinParts(states)
	return fmt.Sprintf("%s %d %s %s", label, s.total, state, contextStyle(s.maxCtx).Render(fmt.Sprintf("%d%%", s.maxCtx)))
}

func (m finderModel) Update(msg tea.Msg) (finderModel, tea.Cmd) {
	switch msg := msg.(type) {
	case stateMsg:
		// Full state snapshot from watcher — update sessions + agents.
		debugf("finder: full state sessions=%d agents=%d", len(msg.sessions), len(msg.agents))
		m.sessData = msg.sessions
		m.agentData = msg.agents
		m.buildSessionItems(msg.agents)
		m.hasSess = true
		m.rebuildPicker()
		return m, nil

	case agentUpdateMsg:
		// Incremental agent update from watcher.
		debugf("finder: agent update panes=%d", len(msg.updates))
		if m.agentData == nil {
			m.agentData = map[string]AgentStatus{}
		}
		for id, status := range msg.updates {
			debugf("finder: pane=%s provider=%s running=%v activity=%s", id, status.Provider.String(), status.Running, status.Activity.String())
			if status.Running {
				m.agentData[id] = status
			} else {
				delete(m.agentData, id)
			}
		}
		m.buildSessionItems(m.agentData)
		m.rebuildPicker()
		return m, nil

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
					agents:      m.agentData,
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
// The attached session is placed last among sessions (furthest from input).
// Projects that already have a matching session are excluded.
func (m *finderModel) rebuildPicker() {
	var items []PickerItem
	var entries []finderEntry

	// Sessions: sorted by recency (most recent closest to input),
	// with the attached session pushed to end (furthest from input).
	if m.kind != finderProjects && len(m.sessions) > 0 {
		// Build index pairs so we can sort sessions + entries together.
		type indexedSession struct {
			idx      int
			attached bool
		}
		ordered := make([]indexedSession, len(m.sessions))
		for i := range m.sessions {
			name := m.sessIdx[i].sessionName
			var attached bool
			for _, sess := range m.sessData {
				if sess.Name == name {
					attached = sess.Attached
					break
				}
			}
			ordered[i] = indexedSession{idx: i, attached: attached}
		}

		// Sort: attached last and preserve existing tmux order within each bucket.
		sort.SliceStable(ordered, func(a, b int) bool {
			if ordered[a].attached != ordered[b].attached {
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
	if m.kind != finderSessions {
		activeNames := map[string]bool{}
		for _, e := range m.sessIdx {
			activeNames[NormalizeSessionName(e.sessionName)] = true
		}
		for i, p := range m.projects {
			normName := NormalizeSessionName(p.Title)
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
