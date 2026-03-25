package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/session"
	"github.com/serge/cms/internal/tmux"
)

// Result messages returned by action commands.

type paneKilledMsg struct{ PaneID string }
type paneMovedMsg struct{ SrcID, DstID string }
type paneSwitchedMsg struct{ PaneID string }
type sessionSwitchedMsg struct{ Name string }
type projectOpenedMsg struct{ Path string }
type attentionMarkedSeenMsg struct{ PaneID string }

// Action command factories — each returns a tea.Cmd that performs the
// operation and sends a result message back to the TUI.

func killPaneCmd(paneID string) tea.Cmd {
	return func() tea.Msg {
		session.KillPane(paneID)
		return paneKilledMsg{PaneID: paneID}
	}
}

func movePaneCmd(srcID, dstID string) tea.Cmd {
	return func() tea.Msg {
		session.MovePane(srcID, dstID)
		return paneMovedMsg{SrcID: srcID, DstID: dstID}
	}
}

func switchToPaneCmd(paneID string) tea.Cmd {
	return func() tea.Msg {
		session.SwitchToPane(paneID)
		return paneSwitchedMsg{PaneID: paneID}
	}
}

func smartSwitchCmd(name string, priority []string, sessions []tmux.Session, agents map[string]agent.AgentStatus) tea.Cmd {
	return func() tea.Msg {
		session.SmartSwitch(name, priority, sessions, agents)
		return sessionSwitchedMsg{Name: name}
	}
}

func openProjectCmd(path string) tea.Cmd {
	return func() tea.Msg {
		session.OpenProject(path)
		return projectOpenedMsg{Path: path}
	}
}

func markAttentionSeenCmd(q *attention.Queue, paneID string) tea.Cmd {
	return func() tea.Msg {
		q.MarkSeen(paneID)
		return attentionMarkedSeenMsg{PaneID: paneID}
	}
}
