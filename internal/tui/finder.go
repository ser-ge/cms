package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/serge/cms/internal/agent"
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
	unseen         bool   // KindQueue (for markAttentionSeen + "unseen" sort key)
	queueStateRank int    // KindQueue: stateRank for "state" sort key
	queueSortTime  int64  // KindQueue: timestamp for "oldest"/"newest" sort keys
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
	branchItems   []PickerItem
	branchIdx     []finderEntry
	hasSess       bool
	hasProj       bool
	hasQueue      bool
	hasWorktree   bool
	hasBranch     bool
	hasPane       bool
	hasWindow     bool
	hasMark       bool

	sections        []string
	done            bool
	action          *PostAction // action to run after TUI exits
	focusSession    string      // session name to focus in dashboard on esc
	lastSessionName string      // cached tmux last session (updated on focus change)
	watcher         *watcher.Watcher
	cfg             config.Config
	width           int
	height          int
}

func newFinderModel(cfg config.Config, w *watcher.Watcher, sections []string, width, height int) finderModel {
	m := finderModel{
		sections: sections,
		cfg:      cfg,
		watcher:  w,
		width:    width,
		height:   height,
	}

	want := sectionSet(sections)

	// Cache the last session name once at init (avoid subprocess per rebuild).
	if sortKeysContain(cfg.Finder.SortKeys("sessions"), "recent") {
		m.lastSessionName = tmux.FetchLastSession()
	}

	// Pre-populate sessions from watcher cache. Most sections need session
	// data (queue reads agents, panes flatten sessions, worktrees need
	// current pane's working dir). Only "projects" alone can skip.
	needsSessions := len(want) != 1 || !want["projects"]
	if needsSessions {
		sessions, agents, _ := w.CachedState()
		if len(sessions) > 0 {
			m.sessData = sessions
			m.agentData = agents
			m.buildSessionItems(agents)
			m.hasSess = true
		}
	}

	// Mark unwanted data sources as "done" so rebuildPicker doesn't wait.
	if !want["sessions"] {
		m.hasSess = true
	}
	if !want["projects"] {
		m.hasProj = true
	}
	if !want["queue"] {
		m.hasQueue = true
	}
	if !want["worktrees"] {
		m.hasWorktree = true
	}
	if !want["branches"] {
		m.hasBranch = true
	}
	if !want["panes"] {
		m.hasPane = true
	}
	if !want["windows"] {
		m.hasWindow = true
	}
	if !want["marks"] {
		m.hasMark = true
	}

	// Build sync data sources that are wanted.
	if want["queue"] {
		m.buildQueueItems()
		m.hasQueue = true
	}
	if want["panes"] {
		m.buildPaneItems()
		m.hasPane = true
	}
	if want["windows"] {
		m.buildWindowItems()
		m.hasWindow = true
	}

	// When watcher has cached state (BootstrapSync was called), run cheap
	// scans synchronously so they appear on first render with no pop-in.
	// Worktrees and branches involve expensive git operations (merge-status
	// checks) and remain async to avoid blocking the TUI.
	if m.hasSess && len(m.sessData) > 0 {
		if want["projects"] && !m.hasProj {
			msg := projectsScannedMsg{project.Scan(cfg)}
			m, _ = m.Update(msg)
		}
		if want["marks"] && !m.hasMark {
			msg := loadMarksCmd(m.sessData)()
			m, _ = m.Update(msg)
		}
	}

	if m.hasSess || m.hasProj || m.hasQueue || m.hasWorktree || m.hasBranch || m.hasPane || m.hasMark {
		m.rebuildPicker()
	}

	return m
}

// sectionSet builds a lookup map from a sections slice.
func sectionSet(sections []string) map[string]bool {
	m := make(map[string]bool, len(sections))
	for _, s := range sections {
		m[s] = true
	}
	return m
}

func (m finderModel) Init() tea.Cmd {
	want := sectionSet(m.sections)
	var cmds []tea.Cmd

	// Skip scans already completed synchronously in newFinderModel.
	if want["projects"] && !m.hasProj {
		cmds = append(cmds, scanProjectsCmd(m.cfg))
	}
	if (want["worktrees"] || want["branches"]) && !m.hasWorktree {
		cmds = append(cmds, scanWorktreesCmd(m.sessData, m.agentData, m.watcher))
	}
	if want["branches"] && !m.hasBranch {
		cmds = append(cmds, scanBranchesCmd(m.sessData, m.watcher))
	}
	if want["marks"] && !m.hasMark {
		cmds = append(cmds, loadMarksCmd(m.sessData))
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

// scanBranchesCmd lists local branches and cross-references with worktrees.
func scanBranchesCmd(sessions []tmux.Session, w *watcher.Watcher) tea.Cmd {
	return func() tea.Msg {
		_, _, current := w.CachedState()
		cwd := currentPaneWorkingDir(sessions, current)
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		if cwd == "" {
			return branchesScannedMsg{}
		}
		root, err := worktree.FindRepoRoot(cwd)
		if err != nil {
			return branchesScannedMsg{}
		}
		branches, err := git.ListLocalBranches(root)
		if err != nil {
			return branchesScannedMsg{}
		}
		// Build set of branches that have worktrees.
		wtBranches := map[string]bool{}
		wts, _ := git.ListWorktrees(root)
		for _, wt := range wts {
			if wt.Branch != "" {
				wtBranches[wt.Branch] = true
			}
		}
		return branchesScannedMsg{branches: branches, repoRoot: root, worktreeBranches: wtBranches}
	}
}

type branchesScannedMsg struct {
	branches []string
	repoRoot string
	// Branches that have worktrees (for Active marking).
	worktreeBranches map[string]bool
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

// buildSessionItems populates session picker items from raw session data.
func (m *finderModel) buildSessionItems(agents map[string]agent.AgentStatus) {
	m.sessions = make([]PickerItem, 0, len(m.sessData))
	m.sessIdx = make([]finderEntry, 0, len(m.sessData))
	for _, sess := range m.sessData {
		var parts []string
		parts = append(parts, fmt.Sprintf("%dw", len(sess.Windows)))
		if summary := m.agentSummary(sess, agents); summary != "" {
			parts = append(parts, summary)
		}
		if sess.Attached {
			parts = append(parts, "attached")
		}
		desc := JoinParts(parts)

		// Icon color: green if session exists (all listed sessions are open).
		iconStyle := activeStyle

		m.sessions = append(m.sessions, PickerItem{
			Title:       sess.Name,
			Description: desc,
			FilterValue: sess.Name,
			Active:      sess.Attached,
			Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Sessions, iconStyle),
		})
		m.sessIdx = append(m.sessIdx, finderEntry{
			kind:        KindSession,
			sessionName: sess.Name,
		})
	}
}

// renderStateCounts renders non-zero activity counts as icon+number.
// Example: "?2 · ✓1 · ●3" instead of "2 waiting · 1 completed · 3 idle".
func renderStateCounts(counts map[agent.Activity]int) string {
	// Order: waiting, completed, working, idle (matches default state_order).
	order := []agent.Activity{
		agent.ActivityWaitingInput,
		agent.ActivityCompleted,
		agent.ActivityWorking,
		agent.ActivityIdle,
	}
	var parts []string
	for _, a := range order {
		if n := counts[a]; n > 0 {
			parts = append(parts, ActivityStyle(a).Render(fmt.Sprintf("%s %d", activityIndicator(a), n)))
		}
	}
	return JoinParts(parts)
}

// activityIndicator returns the icon for an agent activity state.
func activityIndicator(a agent.Activity) string {
	switch a {
	case agent.ActivityWaitingInput:
		return waitingIndicator
	case agent.ActivityCompleted:
		return completedIndicator
	case agent.ActivityWorking:
		return "\u26a1"
	case agent.ActivityIdle:
		return idleIndicator
	default:
		return unknownIndicator
	}
}

func (m finderModel) agentSummary(sess tmux.Session, agents map[string]agent.AgentStatus) string {
	if agents == nil {
		return ""
	}

	counts := map[agent.Activity]int{}
	var maxCtx int
	var hasCtx bool
	for _, win := range sess.Windows {
		for _, pane := range win.Panes {
			cs, ok := agents[pane.ID]
			if !ok || !cs.Running {
				continue
			}
			counts[cs.Activity]++
			if cs.ContextSet {
				maxCtx = max(maxCtx, cs.ContextPct)
				hasCtx = true
			}
		}
	}

	if len(counts) == 0 {
		return ""
	}

	state := renderStateCounts(counts)
	if m.cfg.Finder.ShowContextPercentage && hasCtx {
		ctx := ContextStyle(maxCtx).Render(fmt.Sprintf("%d%%", maxCtx))
		if state == "" {
			return ctx
		}
		return state + " " + ctx
	}
	return state
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
		if sortKeysContain(m.cfg.Finder.SortKeys("sessions"), "recent") {
			m.lastSessionName = tmux.FetchLastSession()
		}
		m.rebuildPicker()
		return m, nil

	case projectsScannedMsg:
		m.projects = nil
		m.projIdx = nil
		for _, p := range msg.projects {
			var parts []string
			parts = append(parts, CompactPath(ShortenHome(p.Path), 25))
			if p.Git.Branch != "" {
				g := p.Git.Branch
				if p.Git.Dirty {
					g += "*"
				}
				parts = append(parts, g)
			}
			// Icon: dim by default; Active set later by filteredProjectItems.
			m.projects = append(m.projects, PickerItem{
				Title:       p.Name,
				Description: JoinParts(parts),
				FilterValue: p.Name + " " + p.Path,
				Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Projects, dimStyle),
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
		projectName := filepath.Base(msg.repoRoot)

		for _, wt := range msg.worktrees {
			if wt.IsBare {
				continue
			}
			branch := wt.Branch
			if branch == "" {
				branch = "(detached)"
			}

			// Title: project/branch (same style as queue).
			title := projectName + "/" + branch

			// Static description: merged status (expensive git check, done once at scan).
			desc := ""
			if !wt.IsMain && defBranch != "" && wt.Branch != "" && wt.Branch != defBranch {
				if integrated, reason := worktree.IsBranchIntegrated(msg.repoRoot, wt.Branch, defBranch); integrated {
					desc = "[merged: " + reason + "]"
				}
			}

			// Icon and Active are set to defaults here; worktreeItemsWithOpenStatus()
			// recomputes them on every rebuildPicker() using live agent data.
			m.worktreeItems = append(m.worktreeItems, PickerItem{
				Title:       title,
				Description: desc,
				FilterValue: branch + " " + wt.Path,
				Active:      wt.IsMain,
				Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Worktrees, dimStyle),
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

	case branchesScannedMsg:
		m.branchItems = nil
		m.branchIdx = nil
		for _, branch := range msg.branches {
			desc := "local branch"
			hasWT := msg.worktreeBranches[branch]
			if hasWT {
				desc = "has worktree"
			}
			iconStyle := dimStyle
			if hasWT {
				iconStyle = activeStyle
			}
			m.branchItems = append(m.branchItems, PickerItem{
				Title:       branch,
				Description: desc,
				FilterValue: branch,
				Active:      hasWT,
				Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Branches, iconStyle),
			})
			m.branchIdx = append(m.branchIdx, finderEntry{
				kind:           KindBranch,
				worktreeBranch: branch,
			})
		}
		m.hasBranch = true
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
			iconStyle := dimStyle
			if alive {
				iconStyle = activeStyle
			}
			m.markItems = append(m.markItems, PickerItem{
				Title:       label,
				Description: desc,
				FilterValue: label + " " + mk.Session + " " + mk.Window,
				Active:      alive,
				Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Marks, iconStyle),
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

	if !m.hasSess && !m.hasProj && !m.hasQueue && !m.hasWorktree && !m.hasBranch && !m.hasPane && !m.hasWindow && !m.hasMark {
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
				case KindBranch:
					// Create/switch to worktree for this branch.
					m.action = &PostAction{
						Kind:       KindWorktree,
						BranchName: entry.worktreeBranch,
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

	// Track section boundaries for per-section title padding.
	type sectionRange struct{ start, end int }
	var ranges []sectionRange

	sections := m.activeSections()
	for _, section := range sections {
		startIdx := len(items)
		si, se := m.sectionItems(section)
		items = append(items, si...)
		entries = append(entries, se...)
		ranges = append(ranges, sectionRange{startIdx, len(items)})
	}

	// Pad titles within each section for columnar alignment.
	for _, r := range ranges {
		maxW := 0
		for i := r.start; i < r.end; i++ {
			if w := lipgloss.Width(items[i].Title); w > maxW {
				maxW = w
			}
		}
		for i := r.start; i < r.end; i++ {
			if w := lipgloss.Width(items[i].Title); w < maxW {
				items[i].Title += strings.Repeat(" ", maxW-w)
			}
		}
	}

	m.entries = entries
	m.picker = m.picker.resetWith(items, m.cfg.General.EscapeChord, m.cfg.General.EscapeChordMs)
	m.picker.width = m.width
	m.picker.height = m.height
}

// activeSections returns which item type sections to include.
func (m *finderModel) activeSections() []string {
	return m.sections
}

// sectionItems returns sorted picker items + entries for a given section name.
func (m *finderModel) sectionItems(section string) ([]PickerItem, []finderEntry) {
	switch section {
	case "sessions":
		return m.sortedSectionItems(m.sessions, m.sessIdx, "sessions", m.sessionIsCurrent, m.sessionIsRecent)
	case "queue":
		return m.sortedSectionItems(m.queueItems, m.queueIdx, "queue", nil, nil)
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
	case "branches":
		return m.filteredBranchItems()
	}
	return nil, nil
}

// sortedSectionItems applies config-driven sort keys to a section's items.
// Sort keys are evaluated left-to-right; the first key that distinguishes
// two items wins. Prefix "-" demotes matching items to the bottom.
type currentFn func(int) bool
type recentFn func(int) bool

func (m *finderModel) sortedSectionItems(
	items []PickerItem, idx []finderEntry, section string,
	isCurrent currentFn, isRecent recentFn,
) ([]PickerItem, []finderEntry) {
	if len(items) == 0 {
		return nil, nil
	}

	keys := m.cfg.Finder.SortKeys(section)
	if len(keys) == 0 {
		return items, idx
	}

	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		for _, key := range keys {
			demote := strings.HasPrefix(key, "-")
			name := strings.TrimPrefix(key, "-")
			va, vb := evalSortKey(name, ia, ib, items, idx, isCurrent, isRecent)
			if va != vb {
				if demote {
					return !va
				}
				return va
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

// evalSortKey evaluates a single sort key for two items.
// Returns (true, false) when item a should sort before b,
// (false, true) when b before a, (false, false) when equal.
func evalSortKey(
	name string, ia, ib int,
	items []PickerItem, idx []finderEntry,
	isCurrent currentFn, isRecent recentFn,
) (bool, bool) {
	switch name {
	case "active":
		return items[ia].Active, items[ib].Active
	case "current":
		if isCurrent == nil {
			return false, false
		}
		return isCurrent(ia), isCurrent(ib)
	case "recent":
		if isRecent == nil {
			return false, false
		}
		return isRecent(ia), isRecent(ib)
	case "state":
		ka, kb := idx[ia].queueStateRank, idx[ib].queueStateRank
		return ka < kb, ka > kb
	case "unseen":
		return idx[ia].unseen, idx[ib].unseen
	case "oldest":
		ta, tb := idx[ia].queueSortTime, idx[ib].queueSortTime
		return ta < tb, ta > tb
	case "newest":
		ta, tb := idx[ia].queueSortTime, idx[ib].queueSortTime
		return ta > tb, ta < tb
	}
	return false, false
}

// sortKeysContain returns true if any key in keys matches name
// (ignoring the "-" prefix).
func sortKeysContain(keys []string, name string) bool {
	for _, k := range keys {
		if strings.TrimPrefix(k, "-") == name {
			return true
		}
	}
	return false
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

// worktreeItemsWithOpenStatus returns worktree items with Active and agent
// state summaries computed from live session/agent data. Called on every
// rebuildPicker() so updates are reflected when agents change state.
func (m *finderModel) worktreeItemsWithOpenStatus() ([]PickerItem, []finderEntry) {
	if len(m.worktreeItems) == 0 {
		return nil, nil
	}

	// For each worktree, collect agent states and git repo status.
	type wtInfo struct {
		hasPane     bool
		dirty       bool // uncommitted changes in any pane
		ahead       bool // unpushed commits in any pane
		stateCounts map[agent.Activity]int
	}
	info := make([]wtInfo, len(m.worktreeItems))
	for i := range info {
		info[i].stateCounts = map[agent.Activity]int{}
	}

	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				for i, entry := range m.worktreeIdx {
					if !strings.HasPrefix(pane.WorkingDir, entry.worktreePath) {
						continue
					}
					info[i].hasPane = true
					if pane.Git.Dirty {
						info[i].dirty = true
					}
					if pane.Git.Ahead > 0 {
						info[i].ahead = true
					}
					if cs, ok := m.agentData[pane.ID]; ok && cs.Running {
						info[i].stateCounts[cs.Activity]++
					}
				}
			}
		}
	}

	items := make([]PickerItem, len(m.worktreeItems))
	copy(items, m.worktreeItems)
	for i := range items {
		wi := info[i]

		// Merged status from scan time; suppressed if worktree has new work.
		isMerged := strings.Contains(m.worktreeItems[i].Description, "[merged:")
		hasDiverged := wi.dirty || wi.ahead

		// Build description: agent counts + static suffix (merged status).
		var parts []string
		if counts := renderStateCounts(wi.stateCounts); counts != "" {
			parts = append(parts, counts)
		}
		// Show merged label only when the worktree hasn't diverged since.
		if !hasDiverged && m.worktreeItems[i].Description != "" {
			parts = append(parts, m.worktreeItems[i].Description)
		}
		items[i].Description = JoinParts(parts)

		// Icon color reflects repo state:
		//   red (waiting)  — dirty or ahead: needs attention
		//   green (active) — clean, diverged: in flight
		//   gray (dim)     — merged or no pane: done
		switch {
		case hasDiverged:
			items[i].Active = true
			items[i].Icon = RenderSectionIcon(m.cfg.Finder.SectionIcons.Worktrees, waitingStyle)
		case wi.hasPane && !isMerged:
			items[i].Active = true
			items[i].Icon = RenderSectionIcon(m.cfg.Finder.SectionIcons.Worktrees, activeStyle)
		}
	}
	return items, m.worktreeIdx
}

// filteredProjectItems returns projects with Active marking. When the sessions
// section is also visible, projects with open sessions are hidden (deduped).
func (m *finderModel) filteredProjectItems() ([]PickerItem, []finderEntry) {
	activeNames := map[string]bool{}
	for _, e := range m.sessIdx {
		activeNames[normalizeName(e.sessionName)] = true
	}

	dedupSessions := sectionSet(m.sections)["sessions"]

	var items []PickerItem
	var entries []finderEntry
	for i, p := range m.projects {
		isOpen := activeNames[normalizeName(p.Title)]
		if isOpen && dedupSessions {
			continue // shown in sessions section
		}
		item := p
		item.Active = isOpen
		if isOpen {
			item.Icon = RenderSectionIcon(m.cfg.Finder.SectionIcons.Projects, activeStyle)
		}
		items = append(items, item)
		entries = append(entries, m.projIdx[i])
	}

	return m.sortedSectionItems(items, entries, "projects", nil, nil)
}

// filteredBranchItems returns branches with Active marking. When the worktrees
// section is also visible, branches with worktrees are hidden (deduped).
func (m *finderModel) filteredBranchItems() ([]PickerItem, []finderEntry) {
	dedupWorktrees := sectionSet(m.sections)["worktrees"]

	var items []PickerItem
	var entries []finderEntry
	for i, b := range m.branchItems {
		if b.Active && dedupWorktrees {
			continue // shown in worktrees section
		}
		items = append(items, b)
		entries = append(entries, m.branchIdx[i])
	}

	// isCurrent needs to work on the filtered entries, not m.branchIdx.
	isCurrent := func(i int) bool {
		_, _, current := m.watcher.CachedState()
		cwd := currentPaneWorkingDir(m.sessData, current)
		if cwd == "" {
			return false
		}
		branch, err := git.Cmd(cwd, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return false
		}
		return strings.TrimSpace(branch) == entries[i].worktreeBranch
	}

	return m.sortedSectionItems(items, entries, "branches", isCurrent, nil)
}

// buildWindowItems populates window picker items from session data.
func (m *finderModel) buildWindowItems() {
	winCap := 0
	for _, s := range m.sessData {
		winCap += len(s.Windows)
	}
	m.windowItems = make([]PickerItem, 0, winCap)
	m.windowIdx = make([]finderEntry, 0, winCap)
	stateOrder := m.cfg.Finder.GetStateOrder("windows")
	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			title := fmt.Sprintf("%s:%s", sess.Name, win.Name)

			// Count agents per activity state in this window.
			stateCounts := map[agent.Activity]int{}
			var activities []agent.Activity
			for _, pane := range win.Panes {
				if cs, ok := m.agentData[pane.ID]; ok && cs.Running {
					stateCounts[cs.Activity]++
					activities = append(activities, cs.Activity)
				}
			}

			var parts []string
			parts = append(parts, fmt.Sprintf("%dp", len(win.Panes)))
			if counts := renderStateCounts(stateCounts); counts != "" {
				parts = append(parts, counts)
			}
			desc := JoinParts(parts)

			// Target the first pane in the window for switching.
			var paneID string
			if len(win.Panes) > 0 {
				paneID = win.Panes[0].ID
			}

			// Icon color: most urgent agent activity, or dim.
			iconStyle := dimStyle
			if len(activities) > 0 {
				iconStyle = ActivityStyle(MostUrgentActivity(activities, stateOrder))
			}

			m.windowItems = append(m.windowItems, PickerItem{
				Title:       title,
				Description: desc,
				FilterValue: sess.Name + " " + win.Name,
				Active:      len(activities) > 0,
				Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Windows, iconStyle),
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
// Agent panes get columnar descriptions: path  provider  context%  activity  mode.
func (m *finderModel) buildPaneItems() {
	paneCap := 0
	for _, s := range m.sessData {
		for _, w := range s.Windows {
			paneCap += len(w.Panes)
		}
	}
	m.paneItems = make([]PickerItem, 0, paneCap)
	m.paneIdx = make([]finderEntry, 0, paneCap)

	// First pass: collect items and find max path width for alignment.
	type paneEntry struct {
		title, filterVal string
		path             string
		cs               agent.AgentStatus
		hasAgent         bool
		command          string
		paneID           string
	}
	var pending []paneEntry

	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				title := fmt.Sprintf("%s:%s.%d", sess.Name, win.Name, pane.Index)
				cs, hasAgent := m.agentData[pane.ID]
				hasAgent = hasAgent && cs.Running
				pending = append(pending, paneEntry{
					title:    title,
					filterVal: sess.Name + " " + win.Name + " " + pane.Command + " " + pane.WorkingDir,
					path:     CompactPath(ShortenHome(pane.WorkingDir), 25),
					cs:       cs,
					hasAgent: hasAgent,
					command:  pane.Command,
					paneID:   pane.ID,
				})
			}
		}
	}

	// Find max path width for columnar alignment.
	maxPathW := 0
	for _, pe := range pending {
		if w := len(pe.path); w > maxPathW {
			maxPathW = w
		}
	}

	for _, pe := range pending {
		var desc string
		iconStyle := dimStyle

		if pe.hasAgent {
			iconStyle = ActivityStyle(pe.cs.Activity)
			desc = buildPaneDescription(pe.path, pe.cs, maxPathW)
		} else {
			paddedPath := fmt.Sprintf("%-*s", maxPathW, pe.path)
			if pe.command != "" {
				desc = paddedPath + "  " + pe.command
			} else {
				desc = pe.path
			}
		}

		m.paneItems = append(m.paneItems, PickerItem{
			Title:       pe.title,
			Description: desc,
			FilterValue: pe.filterVal,
			Active:      pe.hasAgent,
			Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Panes, iconStyle),
		})
		m.paneIdx = append(m.paneIdx, finderEntry{
			kind:   KindPane,
			paneID: pe.paneID,
		})
	}
	m.hasPane = true
}

// buildPaneDescription returns a columnar description for an agent pane:
// path  provider  context%  activity  mode
func buildPaneDescription(path string, cs agent.AgentStatus, maxPathW int) string {
	const (
		providerW = 6
		contextW  = 4
		activityW = 9
		modeW     = 15
	)

	paddedPath := fmt.Sprintf("%-*s", maxPathW, path)

	provider := ""
	if cs.Provider != agent.ProviderUnknown {
		provider = cs.Provider.String()
	}

	context := ""
	if cs.ContextSet {
		context = fmt.Sprintf("%d%%", cs.ContextPct)
	}

	activity := RenderActivity(cs.Activity)
	activity += strings.Repeat(" ", max(0, activityW-len(cs.Activity.String())))

	mode := ""
	modePad := ""
	if cs.ModeLabel != "" {
		mode = RenderMode(cs)
		modePad = strings.Repeat(" ", max(0, modeW-len(cs.ModeLabel)))
	}

	return fmt.Sprintf("%s  %-*s  %*s  %s  %s%s",
		paddedPath,
		providerW, provider,
		contextW, context,
		activity,
		mode, modePad,
	)
}

// buildQueueItems constructs agent pane items for the queue view.
// Sorting is handled by sortedSectionItems via config-driven sort keys;
// this method only builds items and populates sort data on finderEntry.
// stateRank returns the sort priority for an activity based on the configured
// state order. Lower rank = higher priority. States not in the order list
// get max rank.
func stateRank(a agent.Activity, order []string) int {
	name := a.String()
	for i, s := range order {
		if s == name {
			return i
		}
	}
	return len(order)
}

func (m *finderModel) buildQueueItems() {
	paneCap := 0
	for _, s := range m.sessData {
		for _, w := range s.Windows {
			paneCap += len(w.Panes)
		}
	}
	m.queueItems = make([]PickerItem, 0, paneCap)
	m.queueIdx = make([]finderEntry, 0, paneCap)

	actSince := m.watcher.ActivitySince()
	attnEvents := m.watcher.Attention.Snapshot()

	// Build unseen lookup: paneID -> most urgent reason.
	unseenSet := map[string]bool{}
	for _, ev := range attnEvents {
		if !ev.Seen {
			unseenSet[ev.PaneID] = true
		}
	}

	stateOrder := m.cfg.Finder.GetStateOrder("queue")
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
				rank := stateRank(cs.Activity, stateOrder)

				var sortTime int64
				if !hasSince {
					sortTime = 1<<62 - 1
				} else {
					sortTime = since.Unix()
				}

				if debug.Enabled {
					desc += fmt.Sprintf("  [rank=%d t=%d]", rank, sortTime)
				}

				// Icon: activity-colored, bold if unseen, faint if seen.
				iconStyle := ActivityStyle(cs.Activity)
				if unseen {
					iconStyle = iconStyle.Bold(true)
				} else {
					iconStyle = iconStyle.Faint(true)
				}

				m.queueItems = append(m.queueItems, PickerItem{
					Title:       title,
					Description: desc,
					FilterValue: filterVal,
					Active:      unseen,
					Icon:        RenderSectionIcon(m.cfg.Finder.SectionIcons.Queue, iconStyle),
				})
				m.queueIdx = append(m.queueIdx, finderEntry{
					kind:           KindQueue,
					paneID:         pane.ID,
					unseen:         unseen,
					queueStateRank: rank,
					queueSortTime:  sortTime,
				})
			}
		}
	}

	// Pad titles to uniform width so description columns align.
	maxTitle := 0
	for _, item := range m.queueItems {
		if w := len(item.Title); w > maxTitle {
			maxTitle = w
		}
	}
	for i := range m.queueItems {
		m.queueItems[i].Title = fmt.Sprintf("%-*s", maxTitle, m.queueItems[i].Title)
	}
	m.hasQueue = true
}

// buildQueueDescription returns a fixed-width columnar description.
// Columns: provider  context%  activity  mode  duration
func buildQueueDescription(cs agent.AgentStatus, elapsed time.Duration, hasSince bool) string {
	const (
		providerW = 6  // "claude"
		contextW  = 4  // "100%"
		activityW = 9  // "completed"
		modeW     = 15 // "workspace-write"
		durationW = 4  // "999m"
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

	// Mode is ANSI-styled — pad after rendering based on plain text width.
	mode := ""
	if cs.ModeLabel != "" {
		mode = RenderMode(cs)
		mode += strings.Repeat(" ", max(0, modeW-len(cs.ModeLabel)))
	} else {
		mode = strings.Repeat(" ", modeW)
	}

	duration := ""
	if hasSince {
		duration = formatDuration(elapsed)
	}

	return fmt.Sprintf("%-*s  %*s  %s  %s  %*s",
		providerW, provider,
		contextW, context,
		activity,
		mode,
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
	if !m.hasSess && !m.hasProj && !m.hasQueue && !m.hasWorktree && !m.hasBranch && !m.hasPane && !m.hasWindow && !m.hasMark {
		return "  Loading...\n"
	}
	want := sectionSet(m.sections)
	if len(want) == 1 && want["queue"] && len(m.queueItems) == 0 {
		return "  No agent sessions\n"
	}
	return m.picker.View()
}

// --- Headless / plain-text support ---

// PlainRow is a single item for plain-text output.
type PlainRow struct {
	Section string // section name (e.g. "sessions", "queue")
	Title   string // unstyled title
	Desc    string // ANSI-stripped description
	Active  bool
}

// PlainSnapshot returns the current finder items as plain-text rows,
// in the exact order they would appear in the TUI picker.
// This shares the build+sort pipeline — no separate code path.
func (m *finderModel) PlainSnapshot() []PlainRow {
	rows := make([]PlainRow, len(m.entries))
	for i, entry := range m.entries {
		item := m.picker.items[i]
		rows[i] = PlainRow{
			Section: entry.kind.SectionName(),
			Title:   ansi.Strip(item.Title),
			Desc:    ansi.Strip(item.Description),
			Active:  item.Active,
		}
	}
	return rows
}

// HeadlessFinder is an opaque handle for driving the finder without bubbletea.
type HeadlessFinder struct {
	m finderModel
}

// RunHeadless creates a finderModel, bootstraps it with watcher state,
// executes Init scan commands synchronously, and returns a headless handle.
// The items are built via the same pipeline as the TUI — single code path.
func RunHeadless(cfg config.Config, w *watcher.Watcher, sections []string) *HeadlessFinder {
	m := newFinderModel(cfg, w, sections, 120, 50)
	want := sectionSet(sections)

	// Run the same scans that Init() would schedule, but synchronously.
	// Each scan function + Update handler is identical to the TUI path.
	if want["projects"] {
		m, _ = m.Update(projectsScannedMsg{project.Scan(cfg)})
	}
	if want["worktrees"] || want["branches"] {
		msg := scanWorktreesCmd(m.sessData, m.agentData, w)()
		m, _ = m.Update(msg)
	}
	if want["branches"] {
		msg := scanBranchesCmd(m.sessData, w)()
		m, _ = m.Update(msg)
	}
	if want["marks"] {
		msg := loadMarksCmd(m.sessData)()
		m, _ = m.Update(msg)
	}

	return &HeadlessFinder{m: m}
}

// PlainSnapshot returns the current items as plain-text rows.
func (h *HeadlessFinder) PlainSnapshot() []PlainRow {
	return h.m.PlainSnapshot()
}

// UpdateFromWatcher feeds a watcher message into the finder and returns
// whether the items changed (requiring a re-render).
func (h *HeadlessFinder) UpdateFromWatcher(msg tea.Msg) bool {
	switch msg.(type) {
	case watcher.StateMsg, watcher.AgentUpdateMsg, watcher.AttentionUpdateMsg,
		watcher.FocusChangedMsg, watcher.GitUpdateMsg:
		h.m, _ = h.m.Update(msg)
		return true
	}
	return false
}
