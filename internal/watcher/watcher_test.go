package watcher

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/tmux"
)

// testWatcher sets up a watcher with a message collector for testing.
func testWatcher() (*Watcher, *[]tea.Msg) {
	w := New()
	var msgs []tea.Msg
	w.send = func(m tea.Msg) {
		msgs = append(msgs, m)
	}
	return w, &msgs
}

// findAgentUpdate returns the first AgentUpdateMsg from the collected messages.
func findAgentUpdate(msgs []tea.Msg) (AgentUpdateMsg, bool) {
	for _, m := range msgs {
		if u, ok := m.(AgentUpdateMsg); ok {
			return u, true
		}
	}
	return AgentUpdateMsg{}, false
}

// --- Hook-related watcher tests ---

func TestHookActiveFor(t *testing.T) {
	w := New()

	// No hook seen — should return false.
	if w.hookActiveFor("%1") {
		t.Fatal("hookActiveFor should be false for unseen pane")
	}

	// Recent hook — should return true.
	w.hookSeen["%1"] = time.Now()
	if !w.hookActiveFor("%1") {
		t.Fatal("hookActiveFor should be true for recently seen pane")
	}

	// Stale hook — should return false.
	w.hookSeen["%2"] = time.Now().Add(-w.hookStale - time.Second)
	if w.hookActiveFor("%2") {
		t.Fatal("hookActiveFor should be false for stale pane")
	}
}

func TestHookStats(t *testing.T) {
	w := New()

	count, listening := w.HookStats()
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if listening {
		t.Fatal("listening should be false when no hook listener")
	}

	w.hookSeen["%1"] = time.Now()
	w.hookSeen["%2"] = time.Now().Add(-w.hookStale - time.Second) // stale

	count, _ = w.HookStats()
	if count != 1 {
		t.Fatalf("count = %d, want 1 (only non-stale)", count)
	}
}

func TestHandleHookEventSessionStart(t *testing.T) {
	w, msgs := testWatcher()

	w.handleHookEvent(hook.Event{
		Kind:      hook.SessionStart,
		PaneID:    "%1",
		SessionID: "sess-123",
	})

	if !w.hookActiveFor("%1") {
		t.Fatal("hook should be active after session-start")
	}
	if !w.agentPanes["%1"] {
		t.Fatal("pane should be tracked after session-start")
	}
	update, ok := findAgentUpdate(*msgs)
	if !ok {
		t.Fatal("expected AgentUpdateMsg")
	}
	status := update.Updates["%1"]
	if status.Activity != agent.ActivityIdle {
		t.Fatalf("activity = %v, want Idle", status.Activity)
	}
	if status.SessionID != "sess-123" {
		t.Fatalf("sessionID = %q, want sess-123", status.SessionID)
	}
	if status.Source != agent.SourceHook {
		t.Fatalf("source = %v, want SourceHook", status.Source)
	}
}

func TestHandleHookEventPreToolUse(t *testing.T) {
	w, msgs := testWatcher()

	// Seed existing agent state.
	w.agents = map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle, Model: "Opus 4.6"},
	}

	w.handleHookEvent(hook.Event{
		Kind:     hook.PreToolUse,
		PaneID:   "%1",
		ToolName: "Edit",
	})

	update, ok := findAgentUpdate(*msgs)
	if !ok {
		t.Fatal("expected AgentUpdateMsg")
	}
	status := update.Updates["%1"]
	if status.Activity != agent.ActivityWorking {
		t.Fatalf("activity = %v, want Working", status.Activity)
	}
	if status.ToolName != "Edit" {
		t.Fatalf("toolName = %q, want Edit", status.ToolName)
	}
	// Should preserve existing model.
	if status.Model != "Opus 4.6" {
		t.Fatalf("model = %q, want preserved Opus 4.6", status.Model)
	}
}

func TestHandleHookEventNotification(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude},
	}

	w.handleHookEvent(hook.Event{
		Kind:    hook.Notification,
		PaneID:  "%1",
		Message: "Allow Edit on main.go?",
	})

	update, _ := findAgentUpdate(*msgs)
	status := update.Updates["%1"]
	if status.Activity != agent.ActivityWaitingInput {
		t.Fatalf("activity = %v, want WaitingInput", status.Activity)
	}
	if status.Notification != "Allow Edit on main.go?" {
		t.Fatalf("notification = %q, want message", status.Notification)
	}
}

func TestHandleHookEventStop(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityWorking, ToolName: "Bash"},
	}

	w.handleHookEvent(hook.Event{Kind: hook.Stop, PaneID: "%1"})

	update, _ := findAgentUpdate(*msgs)
	status := update.Updates["%1"]
	if status.Activity != agent.ActivityCompleted {
		t.Fatalf("activity = %v, want Completed (was Working before Stop)", status.Activity)
	}
	if status.ToolName != "" {
		t.Fatalf("toolName = %q, want cleared", status.ToolName)
	}
}

func TestHandleHookEventSessionEnd(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude, Source: agent.SourceHook},
	}
	w.hookSeen["%1"] = time.Now()
	w.agentPanes["%1"] = true

	w.handleHookEvent(hook.Event{Kind: hook.SessionEnd, PaneID: "%1"})

	if w.hookActiveFor("%1") {
		t.Fatal("hook should be cleared after session-end")
	}
	if w.agentPanes["%1"] {
		t.Fatal("agentPanes should be cleared after session-end")
	}

	update, _ := findAgentUpdate(*msgs)
	status := update.Updates["%1"]
	if status.Running {
		t.Fatal("Running should be false after session-end")
	}
}

func TestHandleHookEventPromptSubmit(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityWaitingInput, Notification: "Allow?"},
	}

	w.handleHookEvent(hook.Event{Kind: hook.PromptSubmit, PaneID: "%1"})

	update, _ := findAgentUpdate(*msgs)
	status := update.Updates["%1"]
	if status.Activity != agent.ActivityWorking {
		t.Fatalf("activity = %v, want Working", status.Activity)
	}
	if status.Notification != "" {
		t.Fatalf("notification = %q, want cleared", status.Notification)
	}
}

func TestHandleHookEventEmptyPaneIDSkipped(t *testing.T) {
	w, msgs := testWatcher()

	w.handleHookEvent(hook.Event{Kind: hook.Stop, PaneID: ""})

	if len(*msgs) != 0 {
		t.Fatal("should not send message for empty pane ID")
	}
}

// --- Transition tests ---

func TestTransitionAgentObserverKeepsWorkingDuringHold(t *testing.T) {
	w := New()
	paneID := "%1"
	w.lastOutput[paneID] = time.Now().Add(-100 * time.Millisecond)
	w.workingUntil[paneID] = time.Now().Add(1500 * time.Millisecond)

	prev := agent.AgentStatus{Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityWorking}
	raw := agent.AgentStatus{Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityIdle}

	got := w.transitionAgent(paneID, agent.SourceObserver, prev, raw)
	if got != agent.ActivityWorking {
		t.Fatalf("transitionAgent = %v, want Working (within hold window)", got)
	}
}

func TestTransitionAgentObserverCompletedAfterHoldExpires(t *testing.T) {
	w := New()
	paneID := "%1"
	w.lastOutput[paneID] = time.Now().Add(-3 * time.Second)
	w.workingUntil[paneID] = time.Now().Add(-1 * time.Second)

	prev := agent.AgentStatus{Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityWorking}
	raw := agent.AgentStatus{Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityIdle}

	got := w.transitionAgent(paneID, agent.SourceObserver, prev, raw)
	if got != agent.ActivityCompleted {
		t.Fatalf("transitionAgent = %v, want Completed (hold expired, was Working)", got)
	}
}

func TestTransitionAgentHookCompletedOnStop(t *testing.T) {
	w := New()
	prev := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityWorking}
	raw := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle}

	got := w.transitionAgent("%1", agent.SourceHook, prev, raw)
	if got != agent.ActivityCompleted {
		t.Fatalf("transitionAgent = %v, want Completed (hook Working->Idle)", got)
	}
}

func TestTransitionAgentHookPassesThroughNonIdle(t *testing.T) {
	w := New()
	prev := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle}
	raw := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityWorking}

	got := w.transitionAgent("%1", agent.SourceHook, prev, raw)
	if got != agent.ActivityWorking {
		t.Fatalf("transitionAgent = %v, want Working (hook passthrough)", got)
	}
}

func TestTransitionAgentObserverIdleFromIdle(t *testing.T) {
	w := New()
	paneID := "%1"
	w.lastOutput[paneID] = time.Now().Add(-5 * time.Second)

	prev := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle}
	raw := agent.AgentStatus{Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle}

	got := w.transitionAgent(paneID, agent.SourceObserver, prev, raw)
	if got != agent.ActivityIdle {
		t.Fatalf("transitionAgent = %v, want Idle (no transition)", got)
	}
}

// --- Persist tests that depend on Watcher ---

func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return false
	}
	// Check if a tmux server is actually running.
	cmd := exec.Command("tmux", "list-sessions")
	return cmd.Run() == nil
}

func TestPersistActivitySinceRestoredOnBootstrap(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available or no server running")
	}

	// Get real pane to set options on.
	out, err := tmux.Run("display-message", "-p", "#{pane_id}")
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := out

	// Simulate a prior cms session that persisted "idle" 5 minutes ago.
	fiveMinAgo := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	_ = setTmuxPaneOption(paneID, "@cms_activity", "idle")
	_ = setTmuxPaneOption(paneID, "@cms_since", strconv.FormatInt(fiveMinAgo.Unix(), 10))
	defer func() {
		_ = unsetTmuxPaneOption(paneID, "@cms_activity")
		_ = unsetTmuxPaneOption(paneID, "@cms_since")
	}()

	// Simulate what bootstrap does: load persisted, then apply if activity matches.
	agents := map[string]agent.AgentStatus{
		paneID: {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle},
	}
	persisted := attention.LoadPersistedExported([]string{paneID})

	w := New()
	for id, status := range agents {
		w.agentPanes[id] = true
		if p, ok := persisted[id]; ok && p.Activity == status.Activity.String() {
			w.activitySince[id] = p.Since
		}
	}

	// Verify the timestamp was restored.
	since := w.ActivitySince()
	got, ok := since[paneID]
	if !ok {
		t.Fatal("activitySince not restored from persisted state")
	}
	if got.Unix() != fiveMinAgo.Unix() {
		t.Fatalf("activitySince = %v, want %v (diff = %v)", got, fiveMinAgo, got.Sub(fiveMinAgo))
	}

	elapsed := time.Since(got)
	if elapsed < 4*time.Minute || elapsed > 6*time.Minute {
		t.Fatalf("elapsed = %v, want ~5m", elapsed)
	}
}

func TestPersistActivitySinceMismatchIgnored(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available or no server running")
	}

	out, err := tmux.Run("display-message", "-p", "#{pane_id}")
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := out

	// Persist "working" but current activity is "idle" — should NOT restore.
	_ = setTmuxPaneOption(paneID, "@cms_activity", "working")
	_ = setTmuxPaneOption(paneID, "@cms_since", strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10))
	defer func() {
		_ = unsetTmuxPaneOption(paneID, "@cms_activity")
		_ = unsetTmuxPaneOption(paneID, "@cms_since")
	}()

	agents := map[string]agent.AgentStatus{
		paneID: {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityIdle},
	}
	persisted := attention.LoadPersistedExported([]string{paneID})

	w := New()
	for id, status := range agents {
		if p, ok := persisted[id]; ok && p.Activity == status.Activity.String() {
			w.activitySince[id] = p.Since
		}
	}

	since := w.ActivitySince()
	if _, ok := since[paneID]; ok {
		t.Fatal("activitySince should NOT be restored when persisted activity doesn't match current")
	}
}

// setTmuxPaneOption sets a pane user option.
func setTmuxPaneOption(paneID, key, value string) error {
	_, err := tmux.Run("set-option", "-p", "-t", paneID, key, value)
	return err
}

// unsetTmuxPaneOption removes a pane user option.
func unsetTmuxPaneOption(paneID, key string) error {
	_, err := tmux.Run("set-option", "-p", "-t", paneID, "-u", key)
	return err
}
