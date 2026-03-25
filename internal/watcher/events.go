package watcher

import (
	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
)

// StateMsg delivers a full state snapshot (bootstrap + structural changes).
type StateMsg struct {
	Sessions []tmux.Session
	Agents   map[string]agent.AgentStatus
	Current  tmux.CurrentTarget
}

// AgentUpdateMsg delivers incremental agent status updates for specific panes.
type AgentUpdateMsg struct {
	Updates map[string]agent.AgentStatus
}

// FocusChangedMsg indicates the user switched pane/window/session externally.
type FocusChangedMsg struct {
	Current tmux.CurrentTarget
}

// GitUpdateMsg delivers updated git info for pane working directories.
type GitUpdateMsg struct {
	GitInfo map[string]git.Info // workingDir -> Info
}

// AttentionUpdateMsg notifies the TUI that the attention queue changed.
type AttentionUpdateMsg struct{}

// ErrMsg wraps an error for delivery to the TUI.
type ErrMsg struct {
	Err error
}
