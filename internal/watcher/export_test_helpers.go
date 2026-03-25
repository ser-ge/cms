package watcher

import (
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/tmux"
)

// UpdateCacheForTest exposes updateCache for use in external test packages.
func (w *Watcher) UpdateCacheForTest(sessions []tmux.Session, agents map[string]agent.AgentStatus, current tmux.CurrentTarget) {
	w.updateCache(sessions, agents, current)
}

// SetActivitySinceForTest sets an activity timestamp for a pane (for test harnesses).
func (w *Watcher) SetActivitySinceForTest(paneID string, t time.Time) {
	w.mu.Lock()
	w.activitySince[paneID] = t
	w.mu.Unlock()
}
