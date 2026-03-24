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
// and slow polls for process table and git status.
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

	// Cached state for finder to read synchronously.
	sessions []Session
	agents   map[string]AgentStatus
	current  CurrentTarget
	stateMu  sync.RWMutex

	// Activity transition tracking.
	activitySince map[string]time.Time // paneID → when current activity started
	Attention     AttentionQueue

	// Lifecycle.
	stopCh chan struct{}
}

const (
	settleRecheckDelay    = 300 * time.Millisecond
	liveRecheckInterval   = 250 * time.Millisecond
	liveOutputGracePeriod = 350 * time.Millisecond
	optimisticWorkingHold = 2 * time.Second
)

// NewWatcher creates a Watcher.
func NewWatcher() *Watcher {
	return &Watcher{
		agentPanes:      map[string]bool{},
		lastOutput:      map[string]time.Time{},
		lastLiveRecheck: map[string]time.Time{},
		workingUntil:    map[string]time.Time{},
		outputTimers:    map[string]*time.Timer{},
		activitySince:   map[string]time.Time{},
		stopCh:          make(chan struct{}),
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

	// Cancel all pending debounce timers.
	w.mu.Lock()
	for _, t := range w.outputTimers {
		t.Stop()
	}
	w.mu.Unlock()

	if w.ctrl != nil {
		w.ctrl.Stop()
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

	// Emit attention events based on transitions.
	for paneID, ns := range next {
		if !ns.Running {
			w.Attention.RemovePane(paneID)
			changed = true
			continue
		}
		ps := prev[paneID]

		// Became waiting → attention.
		if ns.Activity == ActivityWaitingInput && ps.Activity != ActivityWaitingInput {
			w.Attention.Add(paneID, AttentionWaiting)
			changed = true
		}
		// Stopped waiting → remove waiting attention.
		if ns.Activity != ActivityWaitingInput && ps.Activity == ActivityWaitingInput {
			w.Attention.Remove(paneID, AttentionWaiting)
			changed = true
		}

		// Just finished work (Working → Idle).
		if ns.Activity == ActivityIdle && ps.Activity == ActivityWorking {
			w.Attention.Add(paneID, AttentionFinished)
			changed = true
		}
		// Started working again → remove finished attention.
		if ns.Activity == ActivityWorking && ps.Activity != ActivityWorking {
			w.Attention.Remove(paneID, AttentionFinished)
			changed = true
		}
	}

	if changed {
		w.send(attentionUpdateMsg{})
	}
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
		// Restore persisted activity timestamp if the activity hasn't changed.
		if p, ok := persisted[id]; ok && p.activity == status.Activity.String() {
			w.activitySince[id] = p.since
		}
		// Seed initial attention for panes already waiting.
		if status.Activity == ActivityWaitingInput {
			w.Attention.Add(id, AttentionWaiting)
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
	optimisticUpdate := w.promotePaneWorkingLocked(paneID)

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
		if optimisticUpdate != nil {
			w.send(agentUpdateMsg{updates: optimisticUpdate})
		}
		debugf("watcher: live recheck pane=%s scheduled", paneID)
		go w.liveRecheckPane(paneID)
		return
	}

	w.mu.Unlock()
	if optimisticUpdate != nil {
		w.send(agentUpdateMsg{updates: optimisticUpdate})
	}
	debugf("watcher: live recheck pane=%s throttled age=%s", paneID, now.Sub(lastLive))
}

func (w *Watcher) promotePaneWorkingLocked(paneID string) map[string]AgentStatus {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()

	status, ok := w.agents[paneID]
	if !ok || !status.Running {
		return nil
	}
	if status.Activity == ActivityWaitingInput || status.Activity == ActivityWorking {
		return nil
	}

	status.Activity = ActivityWorking
	w.agents[paneID] = status
	w.workingUntil[paneID] = time.Now().Add(optimisticWorkingHold)
	debugf("watcher: optimistic working pane=%s provider=%s", paneID, status.Provider.String())
	return map[string]AgentStatus{paneID: status}
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
	now := time.Now()
	lastOut := w.lastOutput[paneID]
	workingUntil := w.workingUntil[paneID]
	status.Activity = heldActivity(source, prev, status, lastOut, workingUntil, now)
	if source == "settle" || status.Activity != ActivityWorking {
		delete(w.workingUntil, paneID)
	}
	w.mu.Unlock()
	// Preserve args and provider from previous detection (reparse only updates
	// activity, model, context, mode — not process-tree fields).
	status.Args = prev.Args
	status.Provider = prev.Provider
	debugf("watcher: %s recheck pane=%s provider=%s activity=%s mode=%q ctx=%d capture_lines=%d", source, paneID, status.Provider.String(), status.Activity.String(), status.ModeLabel, status.ContextPct, len(strings.Split(content, "\n")))

	updates := map[string]AgentStatus{paneID: status}

	// Track transitions before updating cache.
	w.stateMu.RLock()
	prevSnapshot := map[string]AgentStatus{paneID: prev}
	w.stateMu.RUnlock()

	// Update cache.
	w.stateMu.Lock()
	w.agents = applyAgentUpdates(w.agents, updates)
	w.stateMu.Unlock()

	w.trackTransitions(prevSnapshot, updates)
	w.send(agentUpdateMsg{updates: updates})
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

	// Track transitions against previous state.
	w.stateMu.RLock()
	prevAgents := w.agents
	w.stateMu.RUnlock()

	// Update agent pane tracking.
	w.mu.Lock()
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
					if t, ok := w.outputTimers[pane.ID]; ok {
						t.Stop()
						delete(w.outputTimers, pane.ID)
					}
					debugf("watcher: process poll exited pane=%s", pane.ID)
					updates[pane.ID] = AgentStatus{Running: false}
					continue
				}

				if status.Running {
					prev := cachedAgents[pane.ID]
					lastOut := w.lastOutput[pane.ID]
					workingUntil := w.workingUntil[pane.ID]
					status.Activity = heldActivity("poll", prev, status, lastOut, workingUntil, time.Now())
					debugf("watcher: process poll pane=%s provider=%s activity=%s mode=%q", pane.ID, status.Provider.String(), status.Activity.String(), status.ModeLabel)
					updates[pane.ID] = status
				}
			}
		}
	}
	w.mu.Unlock()

	if len(updates) > 0 {
		w.trackTransitions(cachedAgents, updates)

		w.stateMu.Lock()
		w.agents = applyAgentUpdates(w.agents, updates)
		w.stateMu.Unlock()

		w.send(agentUpdateMsg{updates: updates})
	}
}

func heldActivity(source string, prev AgentStatus, status AgentStatus, lastOut, workingUntil, now time.Time) Activity {
	activity := status.Activity
	if activity != ActivityIdle {
		return activity
	}

	if source != "settle" && prev.Activity == ActivityWorking {
		if now.Sub(lastOut) < liveOutputGracePeriod || now.Before(workingUntil) {
			return ActivityWorking
		}
	}

	if shouldHoldWorking(status) && now.Sub(lastOut) < optimisticWorkingHold {
		return ActivityWorking
	}

	return activity
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
