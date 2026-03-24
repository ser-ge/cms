package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// screen identifies which view is active.
type screen int

const (
	screenDashboard screen = iota
	screenFinder
)

// rootModel is the top-level bubbletea model that delegates to sub-screens.
type rootModel struct {
	screen    screen
	initial   screen // the screen we started on
	dashboard dashboardModel
	finder    finderModel
	cfg       Config
	width     int
	height    int
	postAction *postAction // action to execute after TUI exits
}

func newRootModel(initial screen, cfg Config) rootModel {
	m := rootModel{
		screen:    initial,
		initial:   initial,
		dashboard: newDashboardModel(),
		cfg:       cfg,
	}
	if initial == screenFinder {
		m.finder = newFinderModel(cfg, 0, 0)
	}
	return m
}

func (m rootModel) Init() tea.Cmd {
	switch m.screen {
	case screenDashboard:
		return m.dashboard.Init()
	case screenFinder:
		return m.finder.Init()
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
	}
	return m, nil
}

func (m *rootModel) initFinder() tea.Cmd {
	m.finder = newFinderModel(m.cfg, m.width, m.height)
	return m.finder.Init()
}

// pinBottom pads content so that the footer line is pinned to the terminal bottom.
func pinBottom(content, footer string, height int) string {
	if height <= 0 {
		return content + footer
	}
	// Total output must be exactly `height` lines.
	// Content lines + pad lines + 1 footer line = height.
	contentHeight := strings.Count(content, "\n")
	// Strip trailing newline from content so we control spacing exactly.
	content = strings.TrimRight(content, "\n")
	contentHeight = strings.Count(content, "\n") + 1 // lines = newlines + 1

	pad := height - contentHeight - 1 // -1 for footer
	if pad < 0 {
		pad = 0
	}
	return content + "\n" + strings.Repeat("\n", pad) + footer
}

// Shared message types.

type tickStartMsg time.Time

type errMsg struct {
	err error
}
