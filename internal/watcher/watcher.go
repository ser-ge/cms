package watcher

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/resume"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/trace"
)

// Watcher bridges tmux events to bubbletea messages.
// It manages the control mode connection, debounced output handling,
// hook listener, and slow polls for process table and git status.
type Watcher struct {
	ctrl *tmux.Client
	send func(tea.Msg)

	// State tracking.
	agentPanes      map[string]bool      // pane IDs known to have an agent running
	lastOutput      map[string]time.Time // last %output per pane
	lastLiveRecheck map[string]time.Time
	workingUntil    map[string]time.Time
	mu              sync.Mutex

	// Debouncing: coalesce rapid %output events per pane.
	outputTimers map[string]*time.Timer

	// Completed->Idle decay timers per pane.
	completedTimers map[string]*time.Timer

	// Hook integration: when hooks report for a pane, suppress observer updates.
	hookSeen     map[string]time.Time // paneID -> last hook event time
	hookCh       chan hook.Event      // receives events from hook listener
	hookListener *hook.Listener

	// Cached state for finder to read synchronously.
	sessions []tmux.Session
	agents   map[string]agent.AgentStatus
	current  tmux.CurrentTarget
	stateMu  sync.RWMutex

	// Activity transition tracking.
	activitySince map[string]time.Time // paneID -> when current activity started
	Attention     attention.Queue

	// Configurable timing.
	workingHold    time.Duration // observer: suppress false idle during output gaps
	hookStale      time.Duration // observer resumes if hooks go silent
	completedDecay time.Duration // Completed->Idle auto-decay
	hookPersist    bool          // when true, hooks never go stale

	// Transition smoothing: suppress flicker by holding the current state
	// for a configurable delay before committing a transition.
	smoothingCfg    config.StatusConfig       // carries SmoothingMs()
	smoothingTimers map[string]*time.Timer    // paneID -> pending smoothing timer
	smoothingTarget map[string]agent.Activity // paneID -> target activity when timer fires

	// Lifecycle.
	bootstrapped bool // true after initState() has run (BootstrapSync or bootstrap)
	stopCh       chan struct{}

	recorder trace.Recorder
}

const (
	settleRecheckDelay    = 300 * time.Millisecond
	liveRecheckInterval   = 250 * time.Millisecond
	liveOutputGracePeriod = 350 * time.Millisecond
)

// New creates a Watcher with default timing.
// Call ApplyConfig to override from user config.
func New() *Watcher {
	return &Watcher{
		agentPanes:      map[string]bool{},
		lastOutput:      map[string]time.Time{},
		lastLiveRecheck: map[string]time.Time{},
		workingUntil:    map[string]time.Time{},
		outputTimers:    map[string]*time.Timer{},
		completedTimers: map[string]*time.Timer{},
		hookSeen:        map[string]time.Time{},
		hookCh:          make(chan hook.Event, 64),
		activitySince:   map[string]time.Time{},
		workingHold:     4 * time.Second,
		hookStale:       30 * time.Second,
		completedDecay:  0,
		hookPersist:     true,
		smoothingTimers: map[string]*time.Timer{},
		smoothingTarget: map[string]agent.Activity{},
		stopCh:          make(chan struct{}),
		recorder:        trace.NopRecorder{},
	}
}

// ApplyConfig sets timing from user configuration.
func (w *Watcher) ApplyConfig(general config.GeneralConfig, status config.StatusConfig) {
	if general.CompletedDecayS > 0 {
		w.completedDecay = time.Duration(general.CompletedDecayS) * time.Second
	}
	if status.AlwaysHooksForStatus != nil {
		w.hookPersist = *status.AlwaysHooksForStatus
	} else {
		w.hookPersist = true
	}
	w.smoothingCfg = status
}

// Start begins the watcher goroutines. Must be called after tea.NewProgram.
func (w *Watcher) Start(send func(tea.Msg)) {
	w.send = send
	go w.bootstrap()
}

func (w *Watcher) SetRecorder(rec trace.Recorder) {
	if rec == nil {
		w.recorder = trace.NopRecorder{}
		return
	}
	w.recorder = rec
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

// CachedState returns deep copies of the watcher's cached state for
// synchronous reads (e.g. finder Init).
func (w *Watcher) CachedState() ([]tmux.Session, map[string]agent.AgentStatus, tmux.CurrentTarget) {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return deepCopySessions(w.sessions), deepCopyAgents(w.agents), w.current
}

// deepCopySessions creates a deep copy of a session slice, including
// nested Windows and Panes slices.
func deepCopySessions(src []tmux.Session) []tmux.Session {
	if src == nil {
		return nil
	}
	dst := make([]tmux.Session, len(src))
	for i, sess := range src {
		dst[i] = sess
		if sess.Windows != nil {
			dst[i].Windows = make([]tmux.Window, len(sess.Windows))
			for j, win := range sess.Windows {
				dst[i].Windows[j] = win
				if win.Panes != nil {
					dst[i].Windows[j].Panes = make([]tmux.Pane, len(win.Panes))
					copy(dst[i].Windows[j].Panes, win.Panes)
				}
			}
		}
	}
	return dst
}

// deepCopyAgents creates a deep copy of an agents map.
func deepCopyAgents(src map[string]agent.AgentStatus) map[string]agent.AgentStatus {
	if src == nil {
		return nil
	}
	dst := make(map[string]agent.AgentStatus, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (w *Watcher) updateCache(sessions []tmux.Session, agents map[string]agent.AgentStatus, current tmux.CurrentTarget) {
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

// initState fetches tmux state, detects agents, restores persisted timestamps,
// seeds the agents queue, and updates the watcher cache. Returns false if
// tmux is unavailable.
func (w *Watcher) initState() ([]tmux.Session, map[string]agent.AgentStatus, tmux.CurrentTarget, bool) {
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		debug.Logf("watcher: initState no tmux err=%v", err)
		return nil, nil, tmux.CurrentTarget{}, false
	}
	current, _ := tmux.FetchCurrentTarget()
	agents := agent.DetectAll(sessions, pt)
	debug.Logf("watcher: initState sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)

	// Track which panes have a known agent and restore persisted timestamps.
	var agentPaneIDs []string
	for id := range agents {
		agentPaneIDs = append(agentPaneIDs, id)
	}
	persisted := attention.LoadPersistedExported(agentPaneIDs)

	w.mu.Lock()
	for id, status := range agents {
		w.agentPanes[id] = true
		if p, ok := persisted[id]; ok {
			if p.Activity == status.Activity.String() {
				w.activitySince[id] = p.Since
			}
			if p.Activity == agent.ActivityCompleted.String() {
				elapsed := time.Since(p.Since)
				if elapsed < w.completedDecay {
					status.Activity = agent.ActivityCompleted
					agents[id] = status
					w.activitySince[id] = p.Since
					w.completedTimers[id] = time.AfterFunc(w.completedDecay-elapsed, func() {
						w.decayCompleted(id)
					})
				}
			}
		}
		if status.Activity == agent.ActivityWaitingInput {
			w.Attention.Add(id, attention.Waiting)
		}
		if status.Activity == agent.ActivityCompleted {
			w.Attention.Add(id, attention.Finished)
		}
	}
	w.mu.Unlock()

	w.updateCache(sessions, agents, current)
	return sessions, agents, current, true
}

// bootstrap fetches the initial state and starts the event + poll goroutines.
// If tmux isn't running yet, it sends an empty StateMsg so the TUI can still
// show the finder (projects from disk). Control mode is started if available.
func (w *Watcher) bootstrap() {
	if w.bootstrapped {
		// BootstrapSync already ran — use cached state, skip redundant FetchState.
		sessions, agents, current := w.CachedState()
		snapshotID := w.recorder.RecordTmuxState("bootstrap", sessions, current)
		w.recorder.RecordIngress(trace.IngressBootstrapState, trace.BootstrapStatePayload{SnapshotID: snapshotID})
		w.send(StateMsg{Sessions: sessions, Agents: agents, Current: current})
	} else {
		sessions, agents, current, ok := w.initState()
		if !ok {
			w.send(StateMsg{})
			return
		}
		snapshotID := w.recorder.RecordTmuxState("bootstrap", sessions, current)
		w.recorder.RecordIngress(trace.IngressBootstrapState, trace.BootstrapStatePayload{SnapshotID: snapshotID})
		w.send(StateMsg{Sessions: sessions, Agents: agents, Current: current})
	}

	// Start control mode for event-driven updates.
	ctrl, err := tmux.NewClient()
	if err == nil {
		w.ctrl = ctrl
		go w.runEventLoop()
	} else {
		debug.Logf("watcher: control unavailable err=%v", err)
	}

	// Start hook listener for Claude Code hook events.
	hl, err := hook.NewListener(hook.SocketPath(), w.hookCh)
	if err != nil {
		debug.Logf("watcher: hook listener unavailable err=%v", err)
	} else {
		w.hookListener = hl
	}
	go w.runHookLoop()

	// Always run process + git polls regardless of control mode.
	go w.runProcessPoll()
	go w.runGitPoll()
}

// BootstrapSync fetches the initial state synchronously without starting
// event loops. Use for one-shot plain-text output.
func (w *Watcher) BootstrapSync() {
	w.initState()
	w.bootstrapped = true
}

// runEventLoop reads control mode events and dispatches them.
func (w *Watcher) runEventLoop() {
	ctrl := w.ctrl
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-ctrl.Events:
			if !ok {
				return
			}
			if ev.Kind == tmux.ClientDetached {
				debug.Logf("watcher: control client detached, falling back to polling")
				ctrl.Stop()
				w.ctrl = nil
				return
			}
			w.handleEvent(ev)
		}
	}
}

func (w *Watcher) handleEvent(ev tmux.Event) {
	w.recorder.RecordIngress(trace.IngressTmuxEvent, trace.TmuxEventPayload{Event: trace.NormalizeTmuxEvent(ev)})
	switch ev.Kind {
	case tmux.SessionCreated, tmux.SessionClosed,
		tmux.WindowAdd, tmux.WindowClose,
		tmux.PaneExited, tmux.LayoutChange:
		// Structural change: re-fetch full state.
		debug.Logf("watcher: structural event kind=%d triggering full refresh", ev.Kind)
		w.refreshFullState()

	case tmux.SessionChanged, tmux.WindowChanged:
		// Focus change: update current target.
		current, err := tmux.FetchCurrentTarget()
		if err != nil {
			return
		}
		w.stateMu.Lock()
		w.current = current
		w.stateMu.Unlock()
		debug.Logf("watcher: focus changed current=%s:%d.%d", current.Session, current.Window, current.Pane)
		w.send(FocusChangedMsg{Current: current})

	case tmux.Output:
		// Pane produced output -- debounce then re-check agent status.
		w.handleOutput(ev.PaneID)

		// ClientDetached is handled directly in runEventLoop.
	}
}

// refreshFullState fetches complete tmux + agent state and emits a StateMsg.
func (w *Watcher) refreshFullState() {
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		debug.Logf("watcher: refresh full state failed err=%v", err)
		return
	}
	current, _ := tmux.FetchCurrentTarget()
	agents := agent.DetectAll(sessions, pt)
	debug.Logf("watcher: full refresh sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)
	snapshotID := w.recorder.RecordTmuxState("full_refresh", sessions, current)
	w.recorder.RecordIngress(trace.IngressFullRefreshSnapshot, trace.FullRefreshSnapshotPayload{
		SnapshotID: snapshotID,
		Current:    trace.NormalizeCurrent(current),
		Agents:     trace.NormalizeAgents(agents),
	})

	// Preserve hook-sourced agent state -- don't overwrite with observer data.
	w.mu.Lock()
	w.stateMu.RLock()
	prevAgents := w.agents
	for paneID := range agents {
		if w.hookActiveFor(paneID) {
			if prev, ok := prevAgents[paneID]; ok && prev.Source == agent.SourceHook {
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
	w.send(StateMsg{Sessions: sessions, Agents: agents, Current: current})
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
func (w *Watcher) handleHookEvent(ev hook.Event) {
	w.recorder.RecordIngress(trace.IngressHookEvent, trace.HookEventPayload{Event: trace.NormalizeHookEvent(ev)})
	paneID := ev.PaneID
	if paneID == "" {
		debug.Logf("watcher: hook event %s has no pane ID, skipping", ev.Kind)
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
		status.Source = agent.SourceHook
	} else {
		status = agent.AgentStatus{
			Running:  true,
			Provider: agent.ProviderClaude,
			Source:   agent.SourceHook,
		}
	}

	// Build the raw status from the hook event.
	switch ev.Kind {
	case hook.SessionStart:
		status.Running = true
		// Preserve existing activity if the observer already detected the agent
		// as working. session-start means "I'm here", not "I stopped working".
		if !has || existing.Activity == agent.ActivityUnknown {
			status.Activity = agent.ActivityIdle
		}
		status.SessionID = ev.SessionID
		if err := resume.SaveClaudeSession(paneID, ev.SessionID); err != nil {
			debug.Logf("watcher: resume save failed pane=%s err=%v", paneID, err)
		}
		tmux.Run("set-option", "-p", "-t", paneID, "@cms_claude_session", ev.SessionID)
		debug.Logf("watcher: hook session-start pane=%s session=%s prev_activity=%s", paneID, ev.SessionID, existing.Activity)

	case hook.Stop:
		status.Activity = agent.ActivityIdle // transitionAgent will promote to Completed if prev was Working
		status.ToolName = ""
		debug.Logf("watcher: hook stop pane=%s", paneID)

	case hook.SessionEnd:
		tmux.Run("set-option", "-p", "-u", "-t", paneID, "@cms_claude_session")
		debug.Logf("watcher: hook session-end pane=%s", paneID)
		w.mu.Lock()
		delete(w.hookSeen, paneID)
		delete(w.agentPanes, paneID)
		w.mu.Unlock()
		w.applyAgentUpdate(map[string]agent.AgentStatus{paneID: {Running: false}})
		return

	case hook.Notification:
		status.Activity = agent.ActivityWaitingInput
		status.Notification = ev.Message
		debug.Logf("watcher: hook notification pane=%s msg=%q", paneID, ev.Message)

	case hook.PromptSubmit:
		status.Activity = agent.ActivityWorking
		status.Notification = ""
		debug.Logf("watcher: hook prompt-submit pane=%s", paneID)

	case hook.PreToolUse:
		status.Activity = agent.ActivityWorking
		status.ToolName = ev.ToolName
		debug.Logf("watcher: hook pre-tool-use pane=%s tool=%s", paneID, ev.ToolName)
	}

	// Run through the state machine.
	w.mu.Lock()
	status.Activity = w.transitionAgent(paneID, agent.SourceHook, existing, status)
	w.mu.Unlock()

	w.applyAgentUpdate(map[string]agent.AgentStatus{paneID: status})
}
