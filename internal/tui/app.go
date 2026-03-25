package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/watcher"
)

// Screen identifies which view is active.
type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenFinder
	ScreenNewWorktree
)

// FinderKind controls what the finder shows.
type FinderKind int

const (
	FinderAll      FinderKind = iota // sessions + projects + worktrees + marks
	FinderSessions                   // sessions only
	FinderProjects                   // projects only
	FinderWorktrees                  // worktrees only (current repo)
	FinderPanes                      // panes only (current session)
	FinderQueue                      // attention queue (urgency-sorted)
	FinderMarks                      // marks only
)

// ItemKind distinguishes session entries from project entries.
type ItemKind int

const (
	KindSession  ItemKind = iota
	KindProject
	KindWorktree
	KindPane
	KindMark
	KindQueue
)

// PostAction is an action to execute after the TUI exits.
// Used when the action must happen outside bubbletea (e.g. tmux attach).
type PostAction struct {
	Kind           ItemKind
	SessionName    string
	ProjectPath    string
	PaneID         string // direct pane switch (dashboard, queue, pane, mark)
	WorktreePath   string // for KindWorktree (switch to existing)
	WorktreeBranch string // for KindWorktree (switch to existing)
	BranchName     string // for KindWorktree (create new, from cms new)
	Priority       []string
}

// ErrMsg wraps an error for delivery to the TUI.
type ErrMsg struct {
	Err error
}

// RootModel is the top-level bubbletea model that delegates to sub-screens.
type RootModel struct {
	screen      Screen
	initial     Screen // the screen we started on
	dashboard   dashboardModel
	finder      finderModel
	newWorktree newWorktreeModel
	finderKind  FinderKind
	watcher     *watcher.Watcher
	cfg         config.Config
	width       int
	height      int
	postAction  *PostAction // action to execute after TUI exits
}

// PostAction returns the action to execute after TUI exits (if any).
func (m RootModel) PostAction() *PostAction {
	return m.postAction
}

// NewRootModel creates a new root TUI model.
func NewRootModel(initial Screen, fk FinderKind, cfg config.Config, w *watcher.Watcher) RootModel {
	dash := newDashboardModel(cfg)
	dash.watcher = w
	m := RootModel{
		screen:     initial,
		initial:    initial,
		dashboard:  dash,
		finderKind: fk,
		watcher:    w,
		cfg:        cfg,
	}
	if initial == ScreenFinder {
		m.finder = newFinderModel(cfg, w, fk, 0, 0)
	}
	if initial == ScreenNewWorktree {
		m.newWorktree = newNewWorktreeModel(cfg)
	}
	return m
}

// Init implements tea.Model.
func (m RootModel) Init() tea.Cmd {
	switch m.screen {
	case ScreenDashboard:
		return m.dashboard.Init()
	case ScreenFinder:
		return m.finder.Init()
	case ScreenNewWorktree:
		return m.newWorktree.Init()
	}
	return nil
}

// Update implements tea.Model.
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.updateActive(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Screen transitions from dashboard (only when not moving).
		if m.screen == ScreenDashboard && !m.dashboard.moving {
			switch msg.String() {
			case "/":
				return m.switchTo(ScreenFinder)
			case "a":
				return m.switchToFinder(FinderQueue)
			case "q":
				return m, tea.Quit
			}
		}
	}

	return m.updateActive(msg)
}

// View implements tea.Model.
func (m RootModel) View() string {
	switch m.screen {
	case ScreenDashboard:
		return m.dashboard.View()
	case ScreenFinder:
		return m.finder.View()
	case ScreenNewWorktree:
		return m.newWorktree.View()
	}
	return ""
}

func (m RootModel) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case ScreenDashboard:
		m.dashboard, cmd = m.dashboard.Update(msg)
		if m.dashboard.postAction != nil {
			m.postAction = m.dashboard.postAction
		}
	case ScreenFinder:
		m.finder, cmd = m.finder.Update(msg)
		if m.finder.done {
			// If an action was selected, store it for post-exit execution.
			if m.finder.action != nil {
				m.postAction = m.finder.action
				return m, tea.Quit
			}
			// Started as standalone finder with no action (esc) — just quit.
			if m.initial == ScreenFinder {
				return m, tea.Quit
			}
			if m.finder.focusSession != "" {
				m.dashboard.focusSession = m.finder.focusSession
			}
			return m.switchTo(ScreenDashboard)
		}
	case ScreenNewWorktree:
		m.newWorktree, cmd = m.newWorktree.Update(msg)
		if m.newWorktree.done {
			if m.newWorktree.action != nil {
				m.postAction = m.newWorktree.action
				return m, tea.Quit
			}
			// Cancelled — quit (standalone screen).
			return m, tea.Quit
		}
	}
	return m, cmd
}

func (m RootModel) switchTo(s Screen) (tea.Model, tea.Cmd) {
	m.screen = s
	switch s {
	case ScreenDashboard:
		return m, m.dashboard.Init()
	case ScreenFinder:
		return m, m.initFinder()
	case ScreenNewWorktree:
		return m, m.initNewWorktree()
	}
	return m, nil
}

func (m *RootModel) initNewWorktree() tea.Cmd {
	m.newWorktree = newNewWorktreeModel(m.cfg)
	return m.newWorktree.Init()
}

func (m *RootModel) initFinder() tea.Cmd {
	m.finder = newFinderModel(m.cfg, m.watcher, FinderAll, m.width, m.height)
	return m.finder.Init()
}

func (m RootModel) switchToFinder(kind FinderKind) (tea.Model, tea.Cmd) {
	m.screen = ScreenFinder
	m.finder = newFinderModel(m.cfg, m.watcher, kind, m.width, m.height)
	return m, m.finder.Init()
}
