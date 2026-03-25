package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// screen identifies which view is active.
type screen int

const (
	screenDashboard screen = iota
	screenFinder
	screenQueue
)

// rootModel is the top-level bubbletea model that delegates to sub-screens.
type rootModel struct {
	screen     screen
	initial    screen // the screen we started on
	dashboard  dashboardModel
	finder     finderModel
	queue      queueModel
	finderKind finderKind
	watcher    *Watcher
	cfg        Config
	width      int
	height     int
	postAction *postAction // action to execute after TUI exits
}

func newRootModel(initial screen, fk finderKind, cfg Config, watcher *Watcher) rootModel {
	dash := newDashboardModel(cfg)
	dash.watcher = watcher
	m := rootModel{
		screen:     initial,
		initial:    initial,
		dashboard:  dash,
		finderKind: fk,
		watcher:    watcher,
		cfg:        cfg,
	}
	if initial == screenFinder {
		m.finder = newFinderModel(cfg, watcher, fk, 0, 0)
	}
	if initial == screenQueue {
		m.queue = newQueueModel(cfg, watcher, 0, 0)
	}
	return m
}

func (m rootModel) Init() tea.Cmd {
	switch m.screen {
	case screenDashboard:
		return m.dashboard.Init()
	case screenFinder:
		return m.finder.Init()
	case screenQueue:
		return m.queue.Init()
	}
	return nil
}

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if m.screen == screenDashboard && !m.dashboard.moving {
			switch msg.String() {
			case "/":
				return m.switchTo(screenFinder)
			case "a":
				return m.switchTo(screenQueue)
			case "q":
				return m, tea.Quit
			}
		}
	}

	return m.updateActive(msg)
}

func (m rootModel) View() string {
	switch m.screen {
	case screenDashboard:
		return m.dashboard.View()
	case screenFinder:
		return m.finder.View()
	case screenQueue:
		return m.queue.View()
	}
	return ""
}

func (m rootModel) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case screenDashboard:
		m.dashboard, cmd = m.dashboard.Update(msg)
		if m.dashboard.postAction != nil {
			m.postAction = m.dashboard.postAction
		}
	case screenFinder:
		m.finder, cmd = m.finder.Update(msg)
		if m.finder.done {
			// If an action was selected, store it for post-exit execution.
			if m.finder.action != nil {
				m.postAction = m.finder.action
				return m, tea.Quit
			}
			// Started as standalone finder with no action (esc) → just quit.
			if m.initial == screenFinder {
				return m, tea.Quit
			}
			if m.finder.focusSession != "" {
				m.dashboard.focusSession = m.finder.focusSession
			}
			return m.switchTo(screenDashboard)
		}
	case screenQueue:
		m.queue, cmd = m.queue.Update(msg)
		if m.queue.done {
			if m.queue.action != nil {
				m.postAction = m.queue.action
				return m, tea.Quit
			}
			// Started as standalone queue with no action (esc) → just quit.
			if m.initial == screenQueue {
				return m, tea.Quit
			}
			return m.switchTo(screenDashboard)
		}
	}
	return m, cmd
}

func (m rootModel) switchTo(s screen) (tea.Model, tea.Cmd) {
	m.screen = s
	switch s {
	case screenDashboard:
		return m, m.dashboard.Init()
	case screenFinder:
		return m, m.initFinder()
	case screenQueue:
		return m, m.initQueue()
	}
	return m, nil
}

func (m *rootModel) initFinder() tea.Cmd {
	m.finder = newFinderModel(m.cfg, m.watcher, finderAll, m.width, m.height)
	return m.finder.Init()
}

func (m *rootModel) initQueue() tea.Cmd {
	m.queue = newQueueModel(m.cfg, m.watcher, m.width, m.height)
	return m.queue.Init()
}


// Shared message types.

type errMsg struct {
	err error
}
