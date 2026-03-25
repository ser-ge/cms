package main

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Bubbletea message types emitted by the Watcher ---

// stateMsg delivers a full state snapshot (bootstrap + structural changes).
type stateMsg struct {
	sessions []Session
	agents   map[string]AgentStatus
	current  CurrentTarget
}

// agentUpdateMsg delivers incremental agent status updates for specific panes.
type agentUpdateMsg struct {
	updates map[string]AgentStatus
}

// focusChangedMsg indicates the user switched pane/window/session externally.
type focusChangedMsg struct {
	current CurrentTarget
}

// gitUpdateMsg delivers updated git info for pane working directories.
type gitUpdateMsg struct {
	gitInfo map[string]GitInfo // workingDir → GitInfo
}

// attentionUpdateMsg notifies the TUI that the attention queue changed.
type attentionUpdateMsg struct{}


// Watcher bridges tmux events to bubbletea messages.
// It manages the control mode connection, debounced output handling,
// hook listener, and slow polls for process table and git status.
type Watcher struct {
	ctrl *CtrlClient
	send func(tea.Msg)

	// State tracking.
	agentPanes      map[string]bool      // pane IDs known to have an agent running
	lastOutput      map[string]time.Time // last %output per pane
	lastLiveRecheck map[string]time.Time
	workingUntil    map[string]time.Time
	mu              sync.Mutex

	// Debouncing: coalesce rapid %output events per pane.
	outputTimers map[string]*time.Timer

	// Completed→Idle decay timers per pane.
	completedTimers map[string]*time.Timer

	// Hook integration: when hooks report for a pane, suppress observer updates.
	hookSeen     map[string]time.Time // paneID → last hook event time
	hookCh       chan HookEvent       // receives events from hook listener
	hookListener *HookListener

	// Cached state for finder to read synchronously.
	sessions []Session
	agents   map[string]AgentStatus
	current  CurrentTarget
	stateMu  sync.RWMutex

	// Activity transition tracking.
	activitySince map[string]time.Time // paneID → when current activity started
	Attention     AttentionQueue

	// Configurable timing.
	workingHold    time.Duration // observer: suppress false idle during output gaps
	hookStale      time.Duration // observer resumes if hooks go silent
	completedDecay time.Duration // Completed→Idle auto-decay

	// Lifecycle.
	stopCh chan struct{}
}

const (
	settleRecheckDelay    = 300 * time.Millisecond
	liveRecheckInterval   = 250 * time.Millisecond
	liveOutputGracePeriod = 350 * time.Millisecond
)

// NewWatcher creates a Watcher with default timing.
// Call ApplyConfig to override from user config.
func NewWatcher() *Watcher {
	return &Watcher{
		agentPanes:      map[string]bool{},
		lastOutput:      map[string]time.Time{},
		lastLiveRecheck: map[string]time.Time{},
		workingUntil:    map[string]time.Time{},
		outputTimers:    map[string]*time.Timer{},
		completedTimers: map[string]*time.Timer{},
		hookSeen:        map[string]time.Time{},
		hookCh:          make(chan HookEvent, 64),
		activitySince:   map[string]time.Time{},
		workingHold:     2 * time.Second,
		hookStale:       30 * time.Second,
		completedDecay:  30 * time.Second,
		stopCh:          make(chan struct{}),
	}
}

// ApplyConfig sets timing from user configuration.
func (w *Watcher) ApplyConfig(cfg GeneralConfig) {
	if cfg.CompletedDecayMs > 0 {
		w.completedDecay = time.Duration(cfg.CompletedDecayMs) * time.Millisecond
	}
}

// Start begins the watcher goroutines. Must be called after tea.NewProgram.
func (w *Watcher) Start(send func(tea.Msg)) {
	w.send = send
	go w.bootstrap()
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	select {
	case <-w.stopCh:
		return
	default:
	}
	close(w.stopCh)

	// Cancel all pending timers.
	w.mu.Lock()
	for _, t := range w.outputTimers {
		t.Stop()
	}
	for _, t := range w.completedTimers {
		t.Stop()
	}
	w.mu.Unlock()

	if w.ctrl != nil {
		w.ctrl.Stop()
	}
	if w.hookListener != nil {
		w.hookListener.Stop()
	}
}

// CachedState returns the watcher's cached state for synchronous reads (e.g. finder Init).
func (w *Watcher) CachedState() ([]Session, map[string]AgentStatus, CurrentTarget) {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.sessions, w.agents, w.current
}

func (w *Watcher) updateCache(sessions []Session, agents map[string]AgentStatus, current CurrentTarget) {
	w.stateMu.Lock()
	w.sessions = sessions
	w.agents = agents
	w.current = current
	w.stateMu.Unlock()
}

// ActivitySince returns a snapshot of activity transition timestamps.
func (w *Watcher) ActivitySince() map[string]time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]time.Time, len(w.activitySince))
	for k, v := range w.activitySince {
		out[k] = v
	}
	return out
}

// trackTransitions compares previous and new agent states,
// updates activitySince timestamps, and emits attention events.
func (w *Watcher) trackTransitions(prev, next map[string]AgentStatus) {
	changed := false
	now := time.Now()

	w.mu.Lock()
	for paneID, ns := range next {
		if !ns.Running {
			if _, had := w.activitySince[paneID]; had {
				delete(w.activitySince, paneID)
				ClearPersistedActivity(paneID)
			}
			continue
		}
		ps, existed := prev[paneID]
		if !existed || !ps.Running || ps.Activity != ns.Activity {
			w.activitySince[paneID] = now
			PersistActivitySince(paneID, ns.Activity, now)
		}
	}
	// Clean up removed panes.
	for paneID := range prev {
		if _, ok := next[paneID]; !ok {
			delete(w.activitySince, paneID)
			ClearPersistedActivity(paneID)
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
		case ActivityWaitingInput:
			if ps.Activity != ActivityWaitingInput {
				w.Attention.Add(paneID, AttentionWaiting)
				changed = true
			}
		case ActivityCompleted:
			w.Attention.Add(paneID, AttentionFinished)
			changed = true
		case ActivityWorking:
			// Started working → clear any finished/waiting attention.
			if ps.Activity != ActivityWorking {
				w.Attention.Remove(paneID, AttentionFinished)
				w.Attention.Remove(paneID, AttentionWaiting)
				changed = true
			}
		default:
			// Idle or Unknown — remove waiting if it was set.
			if ps.Activity == ActivityWaitingInput {
				w.Attention.Remove(paneID, AttentionWaiting)
				changed = true
			}
		}
	}

	if changed {
		w.send(attentionUpdateMsg{})
	}
}


// applyAgentUpdate is the single convergence point for all agent status updates,
// whether from the tmux observer or hook listener. It handles transition tracking,
// cache updates, and TUI notification.
func (w *Watcher) applyAgentUpdate(updates map[string]AgentStatus) {
	if len(updates) == 0 {
		return
	}

	w.stateMu.RLock()
	prevSnapshot := make(map[string]AgentStatus, len(updates))
	for id := range updates {
		if prev, ok := w.agents[id]; ok {
			prevSnapshot[id] = prev
		}
	}
	w.stateMu.RUnlock()

	w.stateMu.Lock()
	w.agents = applyAgentUpdates(w.agents, updates)
	w.stateMu.Unlock()

	w.trackTransitions(prevSnapshot, updates)

	// Schedule/cancel Completed→Idle decay timers.
	w.mu.Lock()
	for id, status := range updates {
		if status.Activity == ActivityCompleted {
			w.scheduleCompletedDecayLocked(id)
		} else {
			w.cancelCompletedDecayLocked(id)
		}
	}
	w.mu.Unlock()

	w.send(agentUpdateMsg{updates: updates})
}

// transitionAgent is the single state machine for all activity transitions.
// Both observer and hook paths call this to get the final activity state.
// It handles:
//   - Observer hold window (suppress false idle during streaming)
//   - Working→Idle promotion to Completed
//   - Hook events bypass hold logic entirely
//
// Caller must hold w.mu.
func (w *Watcher) transitionAgent(paneID string, source StatusSource, prev, raw AgentStatus) Activity {
	parsed := raw.Activity

	if source == SourceHook {
		// Hooks are authoritative — no hold logic.
		// Promote Working→Idle to Completed so attention queue sees it.
		if prev.Activity == ActivityWorking && parsed == ActivityIdle {
			return ActivityCompleted
		}
		// Preserve Completed until the decay timer fires.
		if prev.Activity == ActivityCompleted && parsed == ActivityIdle {
			return ActivityCompleted
		}
		return parsed
	}

	// Observer source: apply hold window to suppress false idles.
	if parsed != ActivityIdle {
		return parsed
	}

	// If previous was Working and we're within the hold window, stay Working.
	lastOut := w.lastOutput[paneID]
	workingUntil := w.workingUntil[paneID]
	now := time.Now()

	if prev.Activity == ActivityWorking {
		if now.Sub(lastOut) < liveOutputGracePeriod || now.Before(workingUntil) {
			return ActivityWorking
		}
		// Hold expired — promote to Completed.
		return ActivityCompleted
	}

	// Preserve Completed until the decay timer fires.
	if prev.Activity == ActivityCompleted {
		return ActivityCompleted
	}

	// Provider-specific optimistic hold (e.g. Claude has 2s hold).
	if shouldHoldWorking(raw) && now.Sub(lastOut) < w.workingHold {
		return ActivityWorking
	}

	return ActivityIdle
}

// scheduleCompletedDecayLocked starts a timer to transition Completed→Idle.
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

	if !ok || status.Activity != ActivityCompleted {
		return
	}

	debugf("watcher: completed decay pane=%s → idle", paneID)
	status.Activity = ActivityIdle
	w.applyAgentUpdate(map[string]AgentStatus{paneID: status})
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

// HookStats returns debug info about hook state.
func (w *Watcher) HookStats() (activeCount int, listening bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for _, t := range w.hookSeen {
		if now.Sub(t) < w.hookStale {
			activeCount++
		}
	}
	listening = w.hookListener != nil
	return
}

// bootstrap fetches the initial state and starts the event + poll goroutines.
// If tmux isn't running yet, it sends an empty stateMsg so the TUI can still
// show the finder (projects from disk). Control mode is started if available.
func (w *Watcher) bootstrap() {
	sessions, pt, err := FetchState()
	if err != nil {
		// No tmux server — send empty state so finder can still show projects.
		debugf("watcher: bootstrap no tmux err=%v", err)
		w.send(stateMsg{})
		return
	}
	current, _ := FetchCurrentTarget()
	agents := detectAllAgents(sessions, pt)
	debugf("watcher: bootstrap sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)

	// Track which panes have a known agent and restore persisted timestamps.
	var agentPaneIDs []string
	for id := range agents {
		agentPaneIDs = append(agentPaneIDs, id)
	}
	persisted := LoadPersistedActivitySince(agentPaneIDs)

	w.mu.Lock()
	for id, status := range agents {
		w.agentPanes[id] = true
		if p, ok := persisted[id]; ok {
			if p.activity == status.Activity.String() {
				w.activitySince[id] = p.since
			}
			// Restore Completed state from previous run.
			// If the decay window hasn't expired, set Completed and schedule decay
			// for the remaining time. Otherwise leave as Idle.
			if p.activity == ActivityCompleted.String() {
				elapsed := time.Since(p.since)
				if elapsed < w.completedDecay {
					status.Activity = ActivityCompleted
					agents[id] = status
					w.activitySince[id] = p.since
					w.completedTimers[id] = time.AfterFunc(w.completedDecay-elapsed, func() {
						w.decayCompleted(id)
					})
				}
			}
		}
		// Seed initial attention for panes already waiting or just completed.
		if status.Activity == ActivityWaitingInput {
			w.Attention.Add(id, AttentionWaiting)
		}
		if status.Activity == ActivityCompleted {
			w.Attention.Add(id, AttentionFinished)
		}
	}
	w.mu.Unlock()

	w.updateCache(sessions, agents, current)
	w.send(stateMsg{sessions: sessions, agents: agents, current: current})

	// Start control mode for event-driven updates.
	ctrl, err := NewCtrlClient()
	if err == nil {
		w.ctrl = ctrl
		go w.runEventLoop()
	} else {
		debugf("watcher: control unavailable err=%v", err)
	}

	// Start hook listener for Claude Code hook events.
	hl, err := NewHookListener(HookSocketPath(), w.hookCh)
	if err != nil {
		debugf("watcher: hook listener unavailable err=%v", err)
	} else {
		w.hookListener = hl
	}
	go w.runHookLoop()

	// Always run process + git polls regardless of control mode.
	go w.runProcessPoll()
	go w.runGitPoll()
}

// runEventLoop reads control mode events and dispatches them.
func (w *Watcher) runEventLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.ctrl.events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		}
	}
}

func (w *Watcher) handleEvent(ev CtrlEvent) {
	switch ev.Kind {
	case CtrlSessionCreated, CtrlSessionClosed,
		CtrlWindowAdd, CtrlWindowClose,
		CtrlPaneExited, CtrlLayoutChange:
		// Structural change: re-fetch full state.
		debugf("watcher: structural event kind=%d triggering full refresh", ev.Kind)
		w.refreshFullState()

	case CtrlSessionChanged, CtrlWindowChanged:
		// Focus change: update current target.
		current, err := FetchCurrentTarget()
		if err != nil {
			return
		}
		w.stateMu.Lock()
		w.current = current
		w.stateMu.Unlock()
		debugf("watcher: focus changed current=%s:%d.%d", current.Session, current.Window, current.Pane)
		w.send(focusChangedMsg{current: current})

	case CtrlOutput:
		// Pane produced output — debounce then re-check agent status.
		w.handleOutput(ev.PaneID)

	case CtrlClientDetached:
		// Control client was kicked — nil out so poll fallback takes over.
		debugf("watcher: control client detached, falling back to polling")
		w.ctrl = nil
	}
}

// handleOutput debounces %output events per pane.
// If the pane has an agent running, schedule a re-check after 300ms of quiescence.
func (w *Watcher) handleOutput(paneID string) {
	w.mu.Lock()
	now := time.Now()
	w.lastOutput[paneID] = now

	if !w.agentPanes[paneID] {
		w.mu.Unlock()
		debugf("watcher: ignore output pane=%s not tracked", paneID)
		return
	}
	debugf("watcher: output pane=%s tracked=true", paneID)

	// Set optimistic working hold — transitionAgent will use this.
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
		debugf("watcher: live recheck pane=%s scheduled", paneID)
		go w.liveRecheckPane(paneID)
		return
	}

	w.mu.Unlock()
	debugf("watcher: live recheck pane=%s throttled age=%s", paneID, now.Sub(lastLive))
}


func (w *Watcher) liveRecheckPane(paneID string) {
	w.recheckPane(paneID, "live")
}

func (w *Watcher) settleRecheckPane(paneID string) {
	debugf("watcher: settle recheck pane=%s fired", paneID)
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
		debugf("watcher: %s recheck pane=%s skipped (hook active)", source, paneID)
		return
	}

	content, err := capturePaneBottom(paneID)
	if err != nil {
		debugf("watcher: %s recheck capture failed pane=%s err=%v", source, paneID, err)
		return
	}

	var status AgentStatus
	var prev AgentStatus
	w.stateMu.RLock()
	if cached, ok := w.agents[paneID]; ok {
		prev = cached
		status = cached
	}
	w.stateMu.RUnlock()
	if !status.Running {
		debugf("watcher: %s recheck pane=%s skipped not running", source, paneID)
		return
	}
	if !reparseAgentStatus(content, &status) {
		debugf("watcher: %s recheck pane=%s skipped unknown provider=%d", source, paneID, status.Provider)
		return
	}

	w.mu.Lock()
	status.Activity = w.transitionAgent(paneID, SourceObserver, prev, status)
	if source == "settle" || status.Activity != ActivityWorking {
		delete(w.workingUntil, paneID)
	}
	w.mu.Unlock()
	// Preserve args and provider from previous detection (reparse only updates
	// activity, model, context, mode — not process-tree fields).
	status.Args = prev.Args
	status.Provider = prev.Provider
	status.Source = SourceObserver
	debugf("watcher: %s recheck pane=%s provider=%s activity=%s mode=%q ctx=%d capture_lines=%d", source, paneID, status.Provider.String(), status.Activity.String(), status.ModeLabel, status.ContextPct, len(strings.Split(content, "\n")))

	w.applyAgentUpdate(map[string]AgentStatus{paneID: status})
}

// refreshFullState fetches complete tmux + agent state and emits a stateMsg.
func (w *Watcher) refreshFullState() {
	sessions, pt, err := FetchState()
	if err != nil {
		debugf("watcher: refresh full state failed err=%v", err)
		return
	}
	current, _ := FetchCurrentTarget()
	agents := detectAllAgents(sessions, pt)
	debugf("watcher: full refresh sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)

	// Preserve hook-sourced agent state — don't overwrite with observer data.
	w.mu.Lock()
	w.stateMu.RLock()
	prevAgents := w.agents
	for paneID := range agents {
		if w.hookActiveFor(paneID) {
			if prev, ok := prevAgents[paneID]; ok && prev.Source == SourceHook {
				agents[paneID] = prev
			}
		}
	}
	w.stateMu.RUnlock()

	// Update agent pane tracking.
	w.agentPanes = map[string]bool{}
	for id := range agents {
		w.agentPanes[id] = true
	}
	w.mu.Unlock()

	w.trackTransitions(prevAgents, agents)
	w.updateCache(sessions, agents, current)
	w.send(stateMsg{sessions: sessions, agents: agents, current: current})
}

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
				debugf("watcher: process poll fallback tick=%d triggering full refresh", tick)
				w.refreshFullState()
			} else {
				debugf("watcher: process poll tick=%d ctrl_connected=%v", tick, w.ctrl != nil)
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
	cachedAgents := make(map[string]AgentStatus, len(w.agents))
	for id, status := range w.agents {
		cachedAgents[id] = status
	}
	w.stateMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	pt := buildProcTable()
	updates := map[string]AgentStatus{}

	w.mu.Lock()
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				status := DetectAgent(pane, pt)

				if status.Running && !w.agentPanes[pane.ID] {
					// New agent process appeared.
					w.agentPanes[pane.ID] = true
					debugf("watcher: process poll discovered pane=%s provider=%s", pane.ID, status.Provider.String())
				}

				if !status.Running && w.agentPanes[pane.ID] {
					// Agent exited — clean up tracking state.
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
					debugf("watcher: process poll exited pane=%s", pane.ID)
					updates[pane.ID] = AgentStatus{Running: false}
					continue
				}

				if status.Running {
					// Skip observer status update if hooks are active for this pane.
					if w.hookActiveFor(pane.ID) {
						debugf("watcher: process poll pane=%s skipped (hook active)", pane.ID)
						continue
					}
					prev := cachedAgents[pane.ID]
					status.Activity = w.transitionAgent(pane.ID, SourceObserver, prev, status)
					status.Source = SourceObserver
					debugf("watcher: process poll pane=%s provider=%s activity=%s mode=%q", pane.ID, status.Provider.String(), status.Activity.String(), status.ModeLabel)
					updates[pane.ID] = status
				}
			}
		}
	}
	w.mu.Unlock()

	if len(updates) > 0 {
		w.applyAgentUpdate(updates)
	}
}


// runHookLoop reads hook events from the hook channel and applies them as
// authoritative agent status updates.
func (w *Watcher) runHookLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.hookCh:
			if !ok {
				return
			}
			w.handleHookEvent(ev)
		}
	}
}

// handleHookEvent translates a hook event into an agent status update.
func (w *Watcher) handleHookEvent(ev HookEvent) {
	paneID := ev.PaneID
	if paneID == "" {
		debugf("watcher: hook event %s has no pane ID, skipping", ev.Kind)
		return
	}

	w.mu.Lock()
	w.hookSeen[paneID] = time.Now()
	w.agentPanes[paneID] = true
	w.mu.Unlock()

	// Read current state for this pane to merge with.
	w.stateMu.RLock()
	existing, has := w.agents[paneID]
	w.stateMu.RUnlock()

	status := existing
	if has {
		status.Source = SourceHook
	} else {
		status = AgentStatus{
			Running:  true,
			Provider: ProviderClaude,
			Source:   SourceHook,
		}
	}

	// Build the raw status from the hook event.
	switch ev.Kind {
	case HookSessionStart:
		status.Running = true
		status.Activity = ActivityIdle
		status.SessionID = ev.SessionID
		debugf("watcher: hook session-start pane=%s session=%s", paneID, ev.SessionID)

	case HookStop:
		status.Activity = ActivityIdle // transitionAgent will promote to Completed if prev was Working
		status.ToolName = ""
		debugf("watcher: hook stop pane=%s", paneID)

	case HookSessionEnd:
		debugf("watcher: hook session-end pane=%s", paneID)
		w.mu.Lock()
		delete(w.hookSeen, paneID)
		delete(w.agentPanes, paneID)
		w.mu.Unlock()
		w.applyAgentUpdate(map[string]AgentStatus{paneID: {Running: false}})
		return

	case HookNotification:
		status.Activity = ActivityWaitingInput
		status.Notification = ev.Message
		debugf("watcher: hook notification pane=%s msg=%q", paneID, ev.Message)

	case HookPromptSubmit:
		status.Activity = ActivityWorking
		status.Notification = ""
		debugf("watcher: hook prompt-submit pane=%s", paneID)

	case HookPreToolUse:
		status.Activity = ActivityWorking
		status.ToolName = ev.ToolName
		debugf("watcher: hook pre-tool-use pane=%s tool=%s", paneID, ev.ToolName)
	}

	// Run through the state machine.
	w.mu.Lock()
	status.Activity = w.transitionAgent(paneID, SourceHook, existing, status)
	w.mu.Unlock()

	w.applyAgentUpdate(map[string]AgentStatus{paneID: status})
}

// runGitPoll periodically re-checks git status for all pane working dirs.
func (w *Watcher) runGitPoll() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.pollGit()
		}
	}
}

func (w *Watcher) pollGit() {
	w.stateMu.RLock()
	sessions := w.sessions
	w.stateMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	var allDirs []string
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				allDirs = append(allDirs, pane.WorkingDir)
			}
		}
	}

	gitCache := NewGitCache()
	results := gitCache.DetectAll(allDirs)
	w.send(gitUpdateMsg{gitInfo: results})
}
