package watcher

import (
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/trace"
)

// runProcessPoll periodically checks for new/exited agent processes.
// When control mode is not connected, every 5th tick also does a full state
// refresh to catch structural changes (new/removed sessions and windows).
func (w *Watcher) runProcessPoll() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	tick := 0
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			tick++
			if w.ctrl == nil && tick%5 == 0 {
				debug.Logf("watcher: process poll fallback tick=%d triggering full refresh", tick)
				w.refreshFullState()
			} else {
				debug.Logf("watcher: process poll tick=%d ctrl_connected=%v", tick, w.ctrl != nil)
				w.pollProcesses()
			}
		}
	}
}

// pollProcesses checks the process table for agent appear/disappear events
// and re-captures all known agent panes to keep status fresh.
func (w *Watcher) pollProcesses() {
	w.stateMu.RLock()
	sessions := w.sessions
	cachedAgents := make(map[string]agent.AgentStatus, len(w.agents))
	for id, status := range w.agents {
		cachedAgents[id] = status
	}
	w.stateMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	pt := proc.BuildTable()
	updates := map[string]agent.AgentStatus{}

	w.mu.Lock()
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				status := agent.Detect(pane, pt)

				if status.Running && !w.agentPanes[pane.ID] {
					// New agent process appeared.
					w.agentPanes[pane.ID] = true
					debug.Logf("watcher: process poll discovered pane=%s provider=%s", pane.ID, status.Provider.String())
				}

				if !status.Running && w.agentPanes[pane.ID] {
					// Agent exited -- clean up tracking state.
					delete(w.agentPanes, pane.ID)
					delete(w.lastOutput, pane.ID)
					delete(w.lastLiveRecheck, pane.ID)
					delete(w.workingUntil, pane.ID)
					delete(w.hookSeen, pane.ID)
					if t, ok := w.outputTimers[pane.ID]; ok {
						t.Stop()
						delete(w.outputTimers, pane.ID)
					}
					w.cancelCompletedDecayLocked(pane.ID)
					debug.Logf("watcher: process poll exited pane=%s", pane.ID)
					updates[pane.ID] = agent.AgentStatus{Running: false}
					continue
				}

				if status.Running {
					// Skip observer status update if hooks are active for this pane.
					if w.hookActiveFor(pane.ID) {
						debug.Logf("watcher: process poll pane=%s skipped (hook active)", pane.ID)
						continue
					}
					prev := cachedAgents[pane.ID]
					status.Activity = w.transitionAgent(pane.ID, agent.SourceObserver, prev, status)
					status.Source = agent.SourceObserver
					// Only emit update if something actually changed.
					if prev != status {
						debug.Logf("watcher: process poll pane=%s provider=%s activity=%s mode=%q", pane.ID, status.Provider.String(), status.Activity.String(), status.ModeLabel)
						updates[pane.ID] = status
					}
				}
			}
		}
	}
	w.mu.Unlock()

	w.recorder.RecordIngress(trace.IngressProcessPollSnapshot, trace.ProcessPollSnapshotPayload{
		Statuses: trace.NormalizeAgents(updates),
	})

	if len(updates) > 0 {
		w.applyAgentUpdate(updates)
	}
}
