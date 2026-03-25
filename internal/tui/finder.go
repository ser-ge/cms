package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/mark"
	"github.com/serge/cms/internal/project"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/watcher"
	"github.com/serge/cms/internal/worktree"
)

type finderEntry struct {
	kind           ItemKind
	sessionName    string // KindSession
	projectPath    string // KindProject
	worktreePath   string // KindWorktree
	worktreeBranch string // KindWorktree
	paneID         string // KindPane, KindQueue, KindMark
	markLabel      string // KindMark
	unseen         bool   // KindQueue (for markAttentionSeen)
}

type finderModel struct {
	picker  pickerModel
	entries []finderEntry // parallel to picker items

	// Session/agent state from watcher.
	sessData  []tmux.Session
	agentData map[string]agent.AgentStatus
	sessions  []PickerItem
	sessIdx   []finderEntry
	projects      []PickerItem
	projIdx       []finderEntry
	queueItems    []PickerItem
	queueIdx      []finderEntry
	worktreeItems []PickerItem
	worktreeIdx   []finderEntry
	paneItems     []PickerItem
	paneIdx       []finderEntry
	windowItems   []PickerItem
	windowIdx     []finderEntry
	markItems     []PickerItem
	markIdx       []finderEntry
	hasSess       bool
	hasProj       bool
	hasQueue      bool
	hasWorktree   bool
	hasPane       bool
	hasWindow     bool
	hasMark       bool

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
	// Most modes need session data: queue reads agents, panes flatten sessions,
	// worktrees need the current pane's working dir. Only projects mode skips.
	needsSessions := kind != FinderProjects
	if needsSessions {
		sessions, agents, _ := w.CachedState()
		if len(sessions) > 0 {
			m.sessData = sessions
			m.agentData = agents
			m.buildSessionItems(agents)
			m.hasSess = true
		}
	}

	// Mark data sources as "done" when this mode doesn't need them.
	// For FinderAll, we load everything.
	switch kind {
	case FinderSessions:
		m.hasProj = true
		m.hasQueue = true
		m.hasWorktree = true
		m.hasPane = true
		m.hasWindow = true
		m.hasMark = true
	case FinderProjects:
		m.hasSess = true
		m.hasQueue = true
		m.hasWorktree = true
		m.hasPane = true
		m.hasWindow = true
		m.hasMark = true
	case FinderQueue:
		m.hasProj = true
		m.buildQueueItems()
		m.hasQueue = true
		m.hasWorktree = true
		m.hasPane = true
		m.hasWindow = true
		m.hasMark = true
	case FinderWorktrees:
		m.hasSess = true
		m.hasProj = true
		m.hasQueue = true
		m.hasPane = true
		m.hasWindow = true
		m.hasMark = true
		// Worktrees loaded async via Init.
	case FinderPanes:
		m.hasSess = true
		m.hasProj = true
		m.hasQueue = true
		m.hasWorktree = true
		m.hasWindow = true
		m.hasMark = true
		m.buildPaneItems()
		m.hasPane = true
	case FinderWindows:
		m.hasSess = true
		m.hasProj = true
		m.hasQueue = true
		m.hasWorktree = true
		m.hasPane = true
		m.hasMark = true
		m.buildWindowItems()
		m.hasWindow = true
	case FinderMarks:
		m.hasSess = true
		m.hasProj = true
		m.hasQueue = true
		m.hasWorktree = true
		m.hasPane = true
		m.hasWindow = true
		// Marks loaded async via Init.
	case FinderAll:
		m.buildQueueItems()
		m.hasQueue = true
		m.buildPaneItems()
		m.hasPane = true
		m.hasWindow = true
		// Worktrees and marks loaded async via Init.
	}

	if m.hasSess || m.hasProj || m.hasQueue || m.hasWorktree || m.hasPane || m.hasMark {
		m.rebuildPicker()
	}

	return m
}

func (m finderModel) Init() tea.Cmd {
	var cmds []tea.Cmd

	switch m.kind {
	case FinderSessions, FinderQueue, FinderPanes, FinderWindows:
		// No async work needed.
	case FinderWorktrees:
		cmds = append(cmds, scanWorktreesCmd(m.sessData, m.agentData, m.watcher))
	case FinderMarks:
		cmds = append(cmds, loadMarksCmd(m.sessData))
	case FinderAll:
		cmds = append(cmds, scanProjectsCmd(m.cfg))
		cmds = append(cmds, scanWorktreesCmd(m.sessData, m.agentData, m.watcher))
		cmds = append(cmds, loadMarksCmd(m.sessData))
	case FinderProjects:
		cmds = append(cmds, scanProjectsCmd(m.cfg))
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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

type worktreesScannedMsg struct {
	worktrees []git.Worktree
	repoRoot  string
}

// scanWorktreesCmd discovers worktrees for the current pane's repo.
func scanWorktreesCmd(sessions []tmux.Session, agents map[string]agent.AgentStatus, w *watcher.Watcher) tea.Cmd {
	return func() tea.Msg {
		_, _, current := w.CachedState()
		cwd := currentPaneWorkingDir(sessions, current)
		if cwd == "" {
			// Fallback to process working directory (e.g. at startup before
			// watcher has populated current target).
			cwd, _ = os.Getwd()
		}
		if cwd == "" {
			return worktreesScannedMsg{}
		}
		root, err := worktree.FindRepoRoot(cwd)
		if err != nil {
			return worktreesScannedMsg{}
		}
		wts, err := git.ListWorktrees(root)
		if err != nil {
			return worktreesScannedMsg{}
		}
		return worktreesScannedMsg{worktrees: wts, repoRoot: root}
	}
}

type marksLoadedMsg struct {
	marks map[string]mark.Mark
}

func loadMarksCmd(sessions []tmux.Session) tea.Cmd {
	return func() tea.Msg {
		marks, _ := mark.Load()
		return marksLoadedMsg{marks: marks}
	}
}

// currentPaneWorkingDir resolves the working directory of the current pane.
func currentPaneWorkingDir(sessions []tmux.Session, current tmux.CurrentTarget) string {
	for _, sess := range sessions {
		if sess.Name != current.Session {
			continue
		}
		for _, win := range sess.Windows {
			if win.Index != current.Window {
				continue
			}
			for _, pane := range win.Panes {
				if pane.Index == current.Pane {
					return pane.WorkingDir
				}
			}
		}
	}
	return ""
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
		m.buildQueueItems()
		m.buildPaneItems()
		m.buildWindowItems()
		m.hasSess = true
		m.rebuildPicker()
		return m, nil

	case watcher.AgentUpdateMsg:
		// Incremental agent update from watcher.
		debug.Logf("finder: agent update panes=%d", len(msg.Updates))
		m.agentData = agent.ApplyUpdates(m.agentData, msg.Updates)
		m.buildSessionItems(m.agentData)
		m.buildQueueItems()
		m.rebuildPicker()
		return m, nil

	case watcher.AttentionUpdateMsg:
		m.buildQueueItems()
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

	case worktreesScannedMsg:
		m.worktreeItems = nil
		m.worktreeIdx = nil
		defBranch := ""
		if msg.repoRoot != "" {
			defBranch, _ = worktree.DefaultBranch(msg.repoRoot)
		}
		for _, wt := range msg.worktrees {
			if wt.IsBare {
				continue
			}
			branch := wt.Branch
			if branch == "" {
				branch = "(detached)"
			}
			desc := ShortenHome(wt.Path)
			if !wt.IsMain && defBranch != "" && wt.Branch != "" && wt.Branch != defBranch {
				if integrated, reason := worktree.IsBranchIntegrated(msg.repoRoot, wt.Branch, defBranch); integrated {
					desc += " [merged: " + reason + "]"
				}
			}
			m.worktreeItems = append(m.worktreeItems, PickerItem{
				Title:       branch,
				Description: desc,
				FilterValue: branch + " " + wt.Path,
				Active:      wt.IsMain,
			})
			m.worktreeIdx = append(m.worktreeIdx, finderEntry{
				kind:           KindWorktree,
				worktreePath:   wt.Path,
				worktreeBranch: wt.Branch,
			})
		}
		m.hasWorktree = true
		m.rebuildPicker()
		return m, nil

	case marksLoadedMsg:
		m.markItems = nil
		m.markIdx = nil
		for label, mk := range msg.marks {
			alive := mark.IsAlive(mk, m.sessData)
			desc := mk.Session + ":" + mk.Window
			if !alive {
				desc += " (dead)"
			}
			m.markItems = append(m.markItems, PickerItem{
				Title:       label,
				Description: desc,
				FilterValue: label + " " + mk.Session + " " + mk.Window,
				Active:      alive,
			})
			m.markIdx = append(m.markIdx, finderEntry{
				kind:      KindMark,
				paneID:    mk.PaneID,
				markLabel: label,
			})
		}
		m.hasMark = true
		m.rebuildPicker()
		return m, nil

	case markRemovedMsg:
		// Reload marks after deletion.
		return m, loadMarksCmd(m.sessData)

	case sessionKilledMsg, paneKilledMsg:
		// Watcher will send a StateMsg to refresh, nothing to do here.
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.width = msg.Width
		m.picker.height = msg.Height
		return m, nil
	}

	if !m.hasSess && !m.hasProj && !m.hasQueue && !m.hasWorktree && !m.hasPane && !m.hasWindow && !m.hasMark {
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)

	// Handle normal-mode actions (e.g. x to close).
	if m.picker.action != PickerNoAction && m.picker.chosen >= 0 && m.picker.chosen < len(m.entries) {
		entry := m.entries[m.picker.chosen]
		action := m.picker.action
		m.picker.action = PickerNoAction
		m.picker.chosen = -1

		if action == PickerActionDelete {
			switch entry.kind {
			case KindSession:
				cmd = killSessionCmd(entry.sessionName)
			case KindPane, KindQueue, KindWindow:
				if entry.paneID != "" {
					cmd = killPaneCmd(entry.paneID)
				}
			case KindMark:
				cmd = removeMarkCmd(entry.markLabel)
			}
		}
		return m, cmd
	}

	if m.picker.done {
		if m.picker.chosen >= 0 && m.picker.chosen < len(m.entries) {
			entry := m.entries[m.picker.chosen]

			// Check if Enter was pressed (item was explicitly selected).
			if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "enter" {
				switch entry.kind {
				case KindQueue:
					if entry.unseen {
						cmd = markAttentionSeenCmd(&m.watcher.Attention, entry.paneID)
					}
					m.action = &PostAction{PaneID: entry.paneID}
				case KindPane, KindMark, KindWindow:
					m.action = &PostAction{PaneID: entry.paneID}
				case KindWorktree:
					m.action = &PostAction{
						Kind:           entry.kind,
						WorktreePath:   entry.worktreePath,
						WorktreeBranch: entry.worktreeBranch,
					}
				default:
					m.action = &PostAction{
						Kind:        entry.kind,
						SessionName: entry.sessionName,
						ProjectPath: entry.projectPath,
						Priority:    m.cfg.General.SwitchPriority,
					}
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

	// Determine which sections to include.
	// Dedicated modes show their single type; FinderAll uses config include order.
	sections := m.sectionsForKind()

	for _, section := range sections {
		si, se := m.sectionItems(section)
		items = append(items, si...)
		entries = append(entries, se...)
	}

	m.entries = entries
	m.picker = m.picker.resetWith(items, m.cfg.General.EscapeChord, m.cfg.General.EscapeChordMs)
}

// sectionsForKind returns which item type sections to include.
func (m *finderModel) sectionsForKind() []string {
	switch m.kind {
	case FinderSessions:
		return []string{"sessions"}
	case FinderProjects:
		return []string{"projects"}
	case FinderQueue:
		return []string{"queue"}
	case FinderWorktrees:
		return []string{"worktrees"}
	case FinderPanes:
		return []string{"panes"}
	case FinderWindows:
		return []string{"windows"}
	case FinderMarks:
		return []string{"marks"}
	default:
		return m.cfg.Finder.Include
	}
}

// sectionItems returns sorted picker items + entries for a given section name.
func (m *finderModel) sectionItems(section string) ([]PickerItem, []finderEntry) {
	switch section {
	case "sessions":
		return m.sortedSectionItems(m.sessions, m.sessIdx, "sessions", m.sessionIsCurrent, m.sessionIsRecent)
	case "queue":
		// Queue has its own urgency sort — no generic reordering.
		return m.queueItems, m.queueIdx
	case "worktrees":
		items, idx := m.worktreeItemsWithOpenStatus()
		return m.sortedSectionItems(items, idx, "worktrees", m.worktreeIsCurrent, nil)
	case "marks":
		return m.sortedSectionItems(m.markItems, m.markIdx, "marks", nil, nil)
	case "windows":
		return m.sortedSectionItems(m.windowItems, m.windowIdx, "windows", m.windowIsCurrent, nil)
	case "panes":
		return m.sortedSectionItems(m.paneItems, m.paneIdx, "panes", m.paneIsCurrent, nil)
	case "projects":
		return m.filteredProjectItems()
	}
	return nil, nil
}

// sortedSectionItems applies generic demote_current/promote_recent sorting.
type currentFn func(int) bool
type recentFn func(int) bool

func (m *finderModel) sortedSectionItems(
	items []PickerItem, idx []finderEntry, pickerType string,
	isCurrent currentFn, isRecent recentFn,
) ([]PickerItem, []finderEntry) {
	if len(items) == 0 {
		return nil, nil
	}

	demote := m.cfg.Finder.ShouldDemoteCurrent(pickerType)
	promote := m.cfg.Finder.ShouldPromoteRecent(pickerType)
	promoteOpen := m.cfg.Finder.ShouldPromoteOpen(pickerType)

	if !demote && !promote && !promoteOpen {
		return items, idx
	}

	// Build index list and stable-sort.
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		// Open items first (Active == true means open).
		if promoteOpen {
			oa, ob := items[ia].Active, items[ib].Active
			if oa != ob {
				return oa
			}
		}
		if promote && isRecent != nil {
			ra, rb := isRecent(ia), isRecent(ib)
			if ra != rb {
				return ra
			}
		}
		if demote && isCurrent != nil {
			ca, cb := isCurrent(ia), isCurrent(ib)
			if ca != cb {
				return !ca // non-current before current
			}
		}
		return false
	})

	sortedItems := make([]PickerItem, len(items))
	sortedIdx := make([]finderEntry, len(idx))
	for i, o := range order {
		sortedItems[i] = items[o]
		sortedIdx[i] = idx[o]
	}
	return sortedItems, sortedIdx
}

// --- "is current" predicates per item type ---

func (m *finderModel) sessionIsCurrent(i int) bool {
	name := m.sessIdx[i].sessionName
	for _, sess := range m.sessData {
		if sess.Name == name {
			return sess.Attached
		}
	}
	return false
}

func (m *finderModel) sessionIsRecent(i int) bool {
	return m.lastSessionName != "" && m.sessIdx[i].sessionName == m.lastSessionName
}

func (m *finderModel) worktreeIsCurrent(i int) bool {
	_, _, current := m.watcher.CachedState()
	cwd := currentPaneWorkingDir(m.sessData, current)
	return cwd != "" && strings.HasPrefix(cwd, m.worktreeIdx[i].worktreePath)
}

func (m *finderModel) windowIsCurrent(i int) bool {
	return m.windowItems[i].Active
}

func (m *finderModel) paneIsCurrent(i int) bool {
	return m.paneItems[i].Active
}

// worktreeItemsWithOpenStatus returns worktree items with Active set to true
// for worktrees that have a tmux window open. With promote_open, open
// worktrees sort first via the Active field in sortedSectionItems.
func (m *finderModel) worktreeItemsWithOpenStatus() ([]PickerItem, []finderEntry) {
	if len(m.worktreeItems) == 0 {
		return nil, nil
	}

	promoteOpen := m.cfg.Finder.ShouldPromoteOpen("worktrees")
	if !promoteOpen {
		return m.worktreeItems, m.worktreeIdx
	}

	// Build set of worktree paths that have tmux panes inside them.
	openPaths := map[string]bool{}
	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				openPaths[pane.WorkingDir] = true
			}
		}
	}

	items := make([]PickerItem, len(m.worktreeItems))
	copy(items, m.worktreeItems)
	for i := range items {
		wtPath := m.worktreeIdx[i].worktreePath
		for openDir := range openPaths {
			if strings.HasPrefix(openDir, wtPath) {
				items[i].Active = true
				break
			}
		}
	}
	return items, m.worktreeIdx
}

// filteredProjectItems returns projects. With promote_open, projects that
// already have a tmux session are marked Active and sorted first.
// Without promote_open, those projects are excluded (legacy behavior).
func (m *finderModel) filteredProjectItems() ([]PickerItem, []finderEntry) {
	activeNames := map[string]bool{}
	for _, e := range m.sessIdx {
		activeNames[normalizeName(e.sessionName)] = true
	}

	promoteOpen := m.cfg.Finder.ShouldPromoteOpen("projects")

	var items []PickerItem
	var entries []finderEntry
	for i, p := range m.projects {
		isOpen := activeNames[normalizeName(p.Title)]
		if isOpen && !promoteOpen {
			// Legacy: exclude projects that already have a session.
			continue
		}
		item := p
		if isOpen {
			item.Active = true
		}
		items = append(items, item)
		entries = append(entries, m.projIdx[i])
	}

	// With promote_open, sort open projects first.
	if promoteOpen && len(items) > 0 {
		order := make([]int, len(items))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			return items[order[a]].Active && !items[order[b]].Active
		})
		sorted := make([]PickerItem, len(items))
		sortedE := make([]finderEntry, len(entries))
		for i, o := range order {
			sorted[i] = items[o]
			sortedE[i] = entries[o]
		}
		items = sorted
		entries = sortedE
	}

	return items, entries
}

// buildWindowItems populates window picker items from session data.
func (m *finderModel) buildWindowItems() {
	m.windowItems = nil
	m.windowIdx = nil
	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			title := fmt.Sprintf("%s:%s", sess.Name, win.Name)
			desc := fmt.Sprintf("%d panes", len(win.Panes))

			// Summarize agent activity in this window.
			var agentParts []string
			for _, pane := range win.Panes {
				if cs, ok := m.agentData[pane.ID]; ok && cs.Running {
					agentParts = append(agentParts, cs.Provider.String()+" "+RenderActivity(cs.Activity))
				}
			}
			if len(agentParts) > 0 {
				desc += " · " + JoinParts(agentParts)
			}

			// Use first pane's working dir for context.
			if len(win.Panes) > 0 {
				desc += " · " + ShortenHome(win.Panes[0].WorkingDir)
			}

			// Target the first pane in the window for switching.
			var paneID string
			if len(win.Panes) > 0 {
				paneID = win.Panes[0].ID
			}

			m.windowItems = append(m.windowItems, PickerItem{
				Title:       title,
				Description: desc,
				FilterValue: sess.Name + " " + win.Name,
				Active:      win.Active && sess.Attached,
			})
			m.windowIdx = append(m.windowIdx, finderEntry{
				kind:   KindWindow,
				paneID: paneID,
			})
		}
	}
	m.hasWindow = true
}

// buildPaneItems populates pane picker items from session data.
func (m *finderModel) buildPaneItems() {
	m.paneItems = nil
	m.paneIdx = nil
	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				title := fmt.Sprintf("%s:%s.%d", sess.Name, win.Name, pane.Index)
				desc := ShortenHome(pane.WorkingDir)
				if cs, ok := m.agentData[pane.ID]; ok && cs.Running {
					desc += " · " + cs.Provider.String() + " " + RenderActivity(cs.Activity)
				} else if pane.Command != "" {
					desc += " · " + pane.Command
				}
				m.paneItems = append(m.paneItems, PickerItem{
					Title:       title,
					Description: desc,
					FilterValue: sess.Name + " " + win.Name + " " + pane.Command + " " + pane.WorkingDir,
					Active:      pane.Active,
				})
				m.paneIdx = append(m.paneIdx, finderEntry{
					kind:   KindPane,
					paneID: pane.ID,
				})
			}
		}
	}
	m.hasPane = true
}

// buildQueueItems constructs urgency-sorted agent pane items for the queue view.
// Ported from the former queueModel.rebuildPicker().
func (m *finderModel) buildQueueItems() {
	m.queueItems = nil
	m.queueIdx = nil

	actSince := m.watcher.ActivitySince()
	attnEvents := m.watcher.Attention.Snapshot()

	// Build unseen lookup: paneID -> most urgent reason.
	unseenReason := map[string]attention.Reason{}
	unseenSet := map[string]bool{}
	for _, ev := range attnEvents {
		if ev.Seen {
			continue
		}
		unseenSet[ev.PaneID] = true
		if prev, ok := unseenReason[ev.PaneID]; !ok || ev.Reason < prev {
			unseenReason[ev.PaneID] = ev.Reason
		}
	}

	type queueItem struct {
		item     PickerItem
		entry    finderEntry
		sortKey  int
		sortTime int64
	}

	var items []queueItem
	now := time.Now()

	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				cs, ok := m.agentData[pane.ID]
				if !ok || !cs.Running {
					continue
				}

				since := actSince[pane.ID]
				elapsed := time.Duration(0)
				hasSince := !since.IsZero()
				if hasSince {
					elapsed = now.Sub(since)
				}

				// Title: session/branch combined.
				title := sess.Name
				if pane.Git.IsRepo && pane.Git.Branch != "" {
					b := pane.Git.Branch
					if pane.Git.Dirty {
						b += "*"
					}
					title += "/" + b
				}

				desc := buildQueueDescription(cs, elapsed, hasSince)

				filterVal := sess.Name
				if pane.Git.Branch != "" {
					filterVal += " " + pane.Git.Branch
				}
				filterVal += " " + cs.Provider.String()

				unseen := unseenSet[pane.ID]
				reason, hasReason := unseenReason[pane.ID]

				// Sort key: unseen waiting (0), unseen finished (1), working (2), idle (3).
				sortKey := 3
				if unseen && hasReason {
					sortKey = int(reason)
				} else {
					switch cs.Activity {
					case agent.ActivityWaitingInput:
						sortKey = 0
					case agent.ActivityCompleted:
						sortKey = 1
					case agent.ActivityWorking:
						sortKey = 2
					case agent.ActivityIdle:
						sortKey = 3
					}
				}

				var sortTime int64
				if !hasSince {
					sortTime = 1<<62 - 1
				} else {
					switch {
					case sortKey <= 1:
						sortTime = since.Unix()
					case sortKey == 2:
						sortTime = since.Unix()
					default:
						sortTime = -since.Unix()
					}
				}

				items = append(items, queueItem{
					item: PickerItem{
						Title:       title,
						Description: desc,
						FilterValue: filterVal,
						Active:      unseen,
					},
					entry: finderEntry{
						kind:   KindQueue,
						paneID: pane.ID,
						unseen: unseen,
					},
					sortKey:  sortKey,
					sortTime: sortTime,
				})
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].sortKey != items[j].sortKey {
			return items[i].sortKey < items[j].sortKey
		}
		return items[i].sortTime < items[j].sortTime
	})

	// Pad titles to uniform width so description columns align.
	maxTitle := 0
	for _, qi := range items {
		if w := len(qi.item.Title); w > maxTitle {
			maxTitle = w
		}
	}

	m.queueItems = make([]PickerItem, len(items))
	m.queueIdx = make([]finderEntry, len(items))
	for i, qi := range items {
		qi.item.Title = fmt.Sprintf("%-*s", maxTitle, qi.item.Title)
		m.queueItems[i] = qi.item
		m.queueIdx[i] = qi.entry
	}
	m.hasQueue = true
}

// buildQueueDescription returns a fixed-width columnar description.
// Columns: provider  context%  activity  duration
func buildQueueDescription(cs agent.AgentStatus, elapsed time.Duration, hasSince bool) string {
	const (
		providerW = 6 // "claude"
		contextW  = 4 // "100%"
		activityW = 9 // "completed"
		durationW = 4 // "999m"
	)

	provider := ""
	if cs.Provider != agent.ProviderUnknown {
		provider = cs.Provider.String()
	}

	context := ""
	if cs.ContextSet {
		context = fmt.Sprintf("%d%%", cs.ContextPct)
	}

	// Activity is ANSI-styled — pad after rendering based on plain text width.
	activity := RenderActivity(cs.Activity)
	activity += strings.Repeat(" ", max(0, activityW-len(cs.Activity.String())))

	duration := ""
	if hasSince {
		duration = formatDuration(elapsed)
	}

	return fmt.Sprintf("%-*s  %*s  %s  %*s",
		providerW, provider,
		contextW, context,
		activity,
		durationW, duration,
	)
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "<1s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func (m finderModel) View() string {
	if !m.hasSess && !m.hasProj && !m.hasQueue && !m.hasWorktree && !m.hasPane && !m.hasWindow && !m.hasMark {
		return "  Loading...\n"
	}
	if m.kind == FinderQueue && len(m.queueItems) == 0 {
		return "  No agent sessions\n"
	}
	return m.picker.View()
}
