package main

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// --- Bubbletea message types emitted by the Watcher ---

// stateMsg delivers a full state snapshot (bootstrap + structural changes).
type stateMsg struct {
	sessions []Session
	claude   map[string]ClaudeStatus
	current  CurrentTarget
}

// claudeUpdateMsg delivers incremental Claude status updates for specific panes.
type claudeUpdateMsg struct {
	updates map[string]ClaudeStatus
}

// focusChangedMsg indicates the user switched pane/window/session externally.
type focusChangedMsg struct {
	current CurrentTarget
}

// gitUpdateMsg delivers updated git info for pane working directories.
type gitUpdateMsg struct {
	gitInfo map[string]GitInfo // workingDir → GitInfo
}

// Watcher bridges tmux events to bubbletea messages.
// It manages the control mode connection, debounced output handling,
// and slow polls for process table and git status.
type Watcher struct {
	ctrl *CtrlClient
	send func(tea.Msg) // program.Send

	// State tracking.
	claudePanes map[string]bool      // pane IDs known to have Claude running
	lastOutput  map[string]time.Time // last %output per pane
	mu          sync.Mutex

	// Debouncing: coalesce rapid %output events per pane.
	outputTimers map[string]*time.Timer

	// Cached state for finder to read synchronously.
	sessions []Session
	claude   map[string]ClaudeStatus
	current  CurrentTarget
	stateMu  sync.RWMutex

	// Lifecycle.
	stopCh chan struct{}
}

// NewWatcher creates a Watcher.
func NewWatcher() *Watcher {
	return &Watcher{
		claudePanes:  map[string]bool{},
		lastOutput:   map[string]time.Time{},
		outputTimers: map[string]*time.Timer{},
		stopCh:       make(chan struct{}),
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
func (w *Watcher) CachedState() ([]Session, map[string]ClaudeStatus, CurrentTarget) {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return w.sessions, w.claude, w.current
}

func (w *Watcher) updateCache(sessions []Session, claude map[string]ClaudeStatus, current CurrentTarget) {
	w.stateMu.Lock()
	w.sessions = sessions
	w.claude = claude
	w.current = current
	w.stateMu.Unlock()
}

// bootstrap fetches the initial state and starts the event + poll goroutines.
// If tmux isn't running yet, it sends an empty stateMsg so the TUI can still
// show the finder (projects from disk). Control mode is started if available.
func (w *Watcher) bootstrap() {
	sessions, pt, err := FetchState()
	if err != nil {
		// No tmux server — send empty state so finder can still show projects.
		w.send(stateMsg{})
		return
	}
	current, _ := FetchCurrentTarget()
	claude := detectAllClaude(sessions, pt)

	// Track which panes have Claude.
	w.mu.Lock()
	for id := range claude {
		w.claudePanes[id] = true
	}
	w.mu.Unlock()

	w.updateCache(sessions, claude, current)
	w.send(stateMsg{sessions: sessions, claude: claude, current: current})

	// Start control mode for event-driven updates.
	ctrl, err := NewCtrlClient()
	if err == nil {
		w.ctrl = ctrl
		go w.runEventLoop()
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
		w.send(focusChangedMsg{current: current})

	case CtrlOutput:
		// Pane produced output — debounce then re-check Claude status.
		w.handleOutput(ev.PaneID)
	}
}

// handleOutput debounces %output events per pane.
// If the pane has Claude running, schedule a re-check after 300ms of quiescence.
func (w *Watcher) handleOutput(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastOutput[paneID] = time.Now()

	if !w.claudePanes[paneID] {
		return
	}

	// Cancel any pending timer for this pane.
	if t, ok := w.outputTimers[paneID]; ok {
		t.Stop()
	}

	// Schedule a Claude re-check after 300ms of output quiescence.
	w.outputTimers[paneID] = time.AfterFunc(300*time.Millisecond, func() {
		w.recheckPane(paneID)
	})
}

// recheckPane captures a pane and re-parses Claude status.
func (w *Watcher) recheckPane(paneID string) {
	select {
	case <-w.stopCh:
		return
	default:
	}

	content, err := capturePaneBottom(paneID)
	if err != nil {
		return
	}

	status := ClaudeStatus{Running: true}
	parsePane(content, &status)

	// Use lastOutput timestamp instead of detectStreaming.
	if status.Activity == ActivityIdle {
		w.mu.Lock()
		lastOut := w.lastOutput[paneID]
		w.mu.Unlock()
		if time.Since(lastOut) < 2*time.Second {
			status.Activity = ActivityWorking
		}
	}

	// Preserve args from previous detection.
	w.stateMu.RLock()
	if prev, ok := w.claude[paneID]; ok {
		status.Args = prev.Args
	}
	w.stateMu.RUnlock()

	updates := map[string]ClaudeStatus{paneID: status}

	// Update cache.
	w.stateMu.Lock()
	if w.claude == nil {
		w.claude = map[string]ClaudeStatus{}
	}
	w.claude[paneID] = status
	w.stateMu.Unlock()

	w.send(claudeUpdateMsg{updates: updates})
}

// refreshFullState fetches complete tmux + Claude state and emits a stateMsg.
func (w *Watcher) refreshFullState() {
	sessions, pt, err := FetchState()
	if err != nil {
		return
	}
	current, _ := FetchCurrentTarget()
	claude := detectAllClaude(sessions, pt)

	// Update Claude pane tracking.
	w.mu.Lock()
	w.claudePanes = map[string]bool{}
	for id := range claude {
		w.claudePanes[id] = true
	}
	w.mu.Unlock()

	w.updateCache(sessions, claude, current)
	w.send(stateMsg{sessions: sessions, claude: claude, current: current})
}

// runProcessPoll periodically checks for new/exited Claude processes.
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
				w.refreshFullState()
			} else {
				w.pollProcesses()
			}
		}
	}
}

// pollProcesses checks the process table for Claude appear/disappear events
// and re-captures all known Claude panes to keep status fresh.
func (w *Watcher) pollProcesses() {
	w.stateMu.RLock()
	sessions := w.sessions
	w.stateMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	pt := buildProcTable()
	updates := map[string]ClaudeStatus{}

	w.mu.Lock()
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				found, args := findClaudeInTree(pt, pane.PID)

				if found && !w.claudePanes[pane.ID] {
					// New Claude process appeared.
					w.claudePanes[pane.ID] = true
				}

				if !found && w.claudePanes[pane.ID] {
					// Claude exited.
					delete(w.claudePanes, pane.ID)
					updates[pane.ID] = ClaudeStatus{Running: false}
					continue
				}

				if found {
					// Re-capture to keep status fresh.
					content, err := capturePaneBottom(pane.ID)
					if err != nil {
						continue
					}
					status := ClaudeStatus{Running: true, Args: args}
					parsePane(content, &status)

					// Use lastOutput timestamp for streaming detection.
					if status.Activity == ActivityIdle {
						lastOut := w.lastOutput[pane.ID]
						if time.Since(lastOut) < 2*time.Second {
							status.Activity = ActivityWorking
						}
					}

					updates[pane.ID] = status
				}
			}
		}
	}
	w.mu.Unlock()

	if len(updates) > 0 {
		w.stateMu.Lock()
		if w.claude == nil {
			w.claude = map[string]ClaudeStatus{}
		}
		for id, status := range updates {
			if status.Running {
				w.claude[id] = status
			} else {
				delete(w.claude, id)
			}
		}
		w.stateMu.Unlock()

		w.send(claudeUpdateMsg{updates: updates})
	}
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
