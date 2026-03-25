package watcher

import (
	"strings"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/tmux"
)

// handleOutput debounces %output events per pane.
// If the pane has an agent running, schedule a re-check after 300ms of quiescence.
func (w *Watcher) handleOutput(paneID string) {
	w.mu.Lock()
	now := time.Now()
	w.lastOutput[paneID] = now

	if !w.agentPanes[paneID] {
		w.mu.Unlock()
		debug.Logf("watcher: ignore output pane=%s not tracked", paneID)
		return
	}
	debug.Logf("watcher: output pane=%s tracked=true", paneID)

	// Set optimistic working hold -- transitionAgent will use this.
	w.workingUntil[paneID] = now.Add(w.workingHold)

	// Cancel any pending timer for this pane.
	if t, ok := w.outputTimers[paneID]; ok {
		t.Stop()
	}

	// Schedule a settle re-check after output quiesces.
	w.outputTimers[paneID] = time.AfterFunc(settleRecheckDelay, func() {
		w.settleRecheckPane(paneID)
	})

	lastLive := w.lastLiveRecheck[paneID]
	if now.Sub(lastLive) >= liveRecheckInterval {
		w.lastLiveRecheck[paneID] = now
		w.mu.Unlock()
		debug.Logf("watcher: live recheck pane=%s scheduled", paneID)
		go w.liveRecheckPane(paneID)
		return
	}

	w.mu.Unlock()
	debug.Logf("watcher: live recheck pane=%s throttled age=%s", paneID, now.Sub(lastLive))
}

// promotePaneWorkingLocked sets a pane to working state.
// Caller must hold w.mu.
func (w *Watcher) promotePaneWorkingLocked(paneID string) {
	w.workingUntil[paneID] = time.Now().Add(w.workingHold)
}

func (w *Watcher) liveRecheckPane(paneID string) {
	w.recheckPane(paneID, "live")
}

func (w *Watcher) settleRecheckPane(paneID string) {
	debug.Logf("watcher: settle recheck pane=%s fired", paneID)
	w.recheckPane(paneID, "settle")
}

// recheckPane captures a pane and re-parses agent status.
func (w *Watcher) recheckPane(paneID, source string) {
	select {
	case <-w.stopCh:
		return
	default:
	}

	// Skip observer recheck if hooks are active for this pane.
	w.mu.Lock()
	hookActive := w.hookActiveFor(paneID)
	w.mu.Unlock()
	if hookActive {
		debug.Logf("watcher: %s recheck pane=%s skipped (hook active)", source, paneID)
		return
	}

	content, err := tmux.CapturePaneBottom(paneID)
	if err != nil {
		debug.Logf("watcher: %s recheck capture failed pane=%s err=%v", source, paneID, err)
		return
	}

	var status agent.AgentStatus
	var prev agent.AgentStatus
	w.stateMu.RLock()
	if cached, ok := w.agents[paneID]; ok {
		prev = cached
		status = cached
	}
	w.stateMu.RUnlock()
	if !status.Running {
		debug.Logf("watcher: %s recheck pane=%s skipped not running", source, paneID)
		return
	}
	if !agent.Reparse(content, &status) {
		debug.Logf("watcher: %s recheck pane=%s skipped unknown provider=%d", source, paneID, status.Provider)
		return
	}

	w.mu.Lock()
	status.Activity = w.transitionAgent(paneID, agent.SourceObserver, prev, status)
	if source == "settle" || status.Activity != agent.ActivityWorking {
		delete(w.workingUntil, paneID)
	}
	w.mu.Unlock()
	// Preserve args and provider from previous detection (reparse only updates
	// activity, model, context, mode -- not process-tree fields).
	status.Args = prev.Args
	status.Provider = prev.Provider
	status.Source = agent.SourceObserver
	debug.Logf("watcher: %s recheck pane=%s provider=%s activity=%s mode=%q ctx=%d capture_lines=%d", source, paneID, status.Provider.String(), status.Activity.String(), status.ModeLabel, status.ContextPct, len(strings.Split(content, "\n")))

	w.applyAgentUpdate(map[string]agent.AgentStatus{paneID: status})
}

// transitionAgent is the single state machine for all activity transitions.
// Both observer and hook paths call this to get the final activity state.
// It handles:
//   - Observer hold window (suppress false idle during streaming)
//   - Working->Idle promotion to Completed
//   - Hook events bypass hold logic entirely
//
// Caller must hold w.mu.
func (w *Watcher) transitionAgent(paneID string, source agent.StatusSource, prev, raw agent.AgentStatus) agent.Activity {
	parsed := raw.Activity

	if source == agent.SourceHook {
		// Hooks are authoritative -- no hold logic.
		// Promote Working->Idle to Completed so attention queue sees it.
		if prev.Activity == agent.ActivityWorking && parsed == agent.ActivityIdle {
			return agent.ActivityCompleted
		}
		// Preserve Completed until the decay timer fires.
		if prev.Activity == agent.ActivityCompleted && parsed == agent.ActivityIdle {
			return agent.ActivityCompleted
		}
		return parsed
	}

	// Observer source: apply hold window to suppress false idles.
	if parsed != agent.ActivityIdle {
		return parsed
	}

	// If previous was Working and we're within the hold window, stay Working.
	lastOut := w.lastOutput[paneID]
	workingUntil := w.workingUntil[paneID]
	now := time.Now()

	if prev.Activity == agent.ActivityWorking {
		if now.Sub(lastOut) < liveOutputGracePeriod || now.Before(workingUntil) {
			return agent.ActivityWorking
		}
		// Hold expired -- promote to Completed.
		return agent.ActivityCompleted
	}

	// Preserve Completed until the decay timer fires.
	if prev.Activity == agent.ActivityCompleted {
		return agent.ActivityCompleted
	}

	// Provider-specific optimistic hold (e.g. Claude has 2s hold).
	if agent.ShouldHoldWorking(raw) && now.Sub(lastOut) < w.workingHold {
		return agent.ActivityWorking
	}

	return agent.ActivityIdle
}

// scheduleCompletedDecayLocked starts a timer to transition Completed->Idle.
// Caller must hold w.mu.
func (w *Watcher) scheduleCompletedDecayLocked(paneID string) {
	// Cancel existing timer if any.
	if t, ok := w.completedTimers[paneID]; ok {
		t.Stop()
	}
	w.completedTimers[paneID] = time.AfterFunc(w.completedDecay, func() {
		w.decayCompleted(paneID)
	})
}

// cancelCompletedDecayLocked cancels a pending decay timer.
// Caller must hold w.mu.
func (w *Watcher) cancelCompletedDecayLocked(paneID string) {
	if t, ok := w.completedTimers[paneID]; ok {
		t.Stop()
		delete(w.completedTimers, paneID)
	}
}

// decayCompleted transitions a pane from Completed to Idle.
func (w *Watcher) decayCompleted(paneID string) {
	select {
	case <-w.stopCh:
		return
	default:
	}

	w.mu.Lock()
	delete(w.completedTimers, paneID)
	w.mu.Unlock()

	w.stateMu.RLock()
	status, ok := w.agents[paneID]
	w.stateMu.RUnlock()

	if !ok || status.Activity != agent.ActivityCompleted {
		return
	}

	debug.Logf("watcher: completed decay pane=%s -> idle", paneID)
	status.Activity = agent.ActivityIdle
	w.applyAgentUpdate(map[string]agent.AgentStatus{paneID: status})
}

// hookActiveFor returns true if hooks have reported for this pane recently enough
// that the observer should defer to hook data.
func (w *Watcher) hookActiveFor(paneID string) bool {
	lastSeen, ok := w.hookSeen[paneID]
	if !ok {
		return false
	}
	return time.Since(lastSeen) < w.hookStale
}

// applyAgentUpdate is the single convergence point for all agent status updates,
// whether from the tmux observer or hook listener. It handles transition tracking,
// cache updates, and TUI notification.
func (w *Watcher) applyAgentUpdate(updates map[string]agent.AgentStatus) {
	if len(updates) == 0 {
		return
	}

	w.stateMu.Lock()
	prevSnapshot := make(map[string]agent.AgentStatus, len(updates))
	for id := range updates {
		if prev, ok := w.agents[id]; ok {
			prevSnapshot[id] = prev
		}
	}
	w.agents = agent.ApplyUpdates(w.agents, updates)
	w.stateMu.Unlock()

	w.trackTransitions(prevSnapshot, updates)

	// Schedule/cancel Completed->Idle decay timers.
	w.mu.Lock()
	for id, status := range updates {
		if status.Activity == agent.ActivityCompleted {
			w.scheduleCompletedDecayLocked(id)
		} else {
			w.cancelCompletedDecayLocked(id)
		}
	}
	w.mu.Unlock()

	w.send(AgentUpdateMsg{Updates: updates})
}

// trackTransitions compares previous and new agent states,
// updates activitySince timestamps, and emits attention events.
func (w *Watcher) trackTransitions(prev, next map[string]agent.AgentStatus) {
	changed := false
	now := time.Now()

	w.mu.Lock()
	for paneID, ns := range next {
		if !ns.Running {
			if _, had := w.activitySince[paneID]; had {
				delete(w.activitySince, paneID)
				attention.ClearPersisted(paneID)
			}
			continue
		}
		ps, existed := prev[paneID]
		if !existed || !ps.Running || ps.Activity != ns.Activity {
			w.activitySince[paneID] = now
			attention.PersistActivitySince(paneID, ns.Activity, now)
		}
	}
	// Clean up removed panes.
	for paneID := range prev {
		if _, ok := next[paneID]; !ok {
			delete(w.activitySince, paneID)
			attention.ClearPersisted(paneID)
		}
	}
	w.mu.Unlock()

	// Emit attention events based on current state (not diffs).
	for paneID, ns := range next {
		if !ns.Running {
			w.Attention.RemovePane(paneID)
			changed = true
			continue
		}
		ps := prev[paneID]

		switch ns.Activity {
		case agent.ActivityWaitingInput:
			if ps.Activity != agent.ActivityWaitingInput {
				w.Attention.Add(paneID, attention.Waiting)
				changed = true
			}
		case agent.ActivityCompleted:
			w.Attention.Add(paneID, attention.Finished)
			changed = true
		case agent.ActivityWorking:
			// Started working -> clear any finished/waiting attention.
			if ps.Activity != agent.ActivityWorking {
				w.Attention.Remove(paneID, attention.Finished)
				w.Attention.Remove(paneID, attention.Waiting)
				changed = true
			}
		default:
			// Idle or Unknown -- remove waiting if it was set.
			if ps.Activity == agent.ActivityWaitingInput {
				w.Attention.Remove(paneID, attention.Waiting)
				changed = true
			}
		}
	}

	if changed {
		w.send(AttentionUpdateMsg{})
	}
}
