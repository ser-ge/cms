package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// testSocketPath returns a short socket path under /tmp to avoid the
// 108-character Unix socket path limit on macOS.
func testSocketPath(t *testing.T) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/cms-test-%d.sock", os.Getpid())
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func TestParseHookKindRoundTrip(t *testing.T) {
	kinds := []struct {
		str  string
		kind HookKind
	}{
		{"session-start", HookSessionStart},
		{"stop", HookStop},
		{"session-end", HookSessionEnd},
		{"notification", HookNotification},
		{"prompt-submit", HookPromptSubmit},
		{"pre-tool-use", HookPreToolUse},
	}

	for _, tc := range kinds {
		got, ok := ParseHookKind(tc.str)
		if !ok {
			t.Fatalf("ParseHookKind(%q) returned not ok", tc.str)
		}
		if got != tc.kind {
			t.Fatalf("ParseHookKind(%q) = %v, want %v", tc.str, got, tc.kind)
		}
		if got.String() != tc.str {
			t.Fatalf("HookKind(%d).String() = %q, want %q", got, got.String(), tc.str)
		}
	}

	if _, ok := ParseHookKind("bogus"); ok {
		t.Fatal("ParseHookKind(bogus) should return not ok")
	}
}

func TestHookListenerAcceptsEvent(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan HookEvent, 8)

	hl, err := NewHookListener(sock, events)
	if err != nil {
		t.Fatalf("NewHookListener: %v", err)
	}
	defer hl.Stop()

	// Connect and send a payload.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	payload := hookPayload{
		Kind:      "pre-tool-use",
		PaneID:    "%5",
		SessionID: "sess-abc",
		ToolName:  "Edit",
	}
	data, _ := json.Marshal(payload)
	data = append(data, '\n')
	conn.Write(data)

	// Read response.
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "OK\n" {
		t.Fatalf("response = %q, want OK", got)
	}

	// Check event was delivered.
	select {
	case ev := <-events:
		if ev.Kind != HookPreToolUse {
			t.Fatalf("kind = %v, want PreToolUse", ev.Kind)
		}
		if ev.PaneID != "%5" {
			t.Fatalf("paneID = %q, want %%5", ev.PaneID)
		}
		if ev.ToolName != "Edit" {
			t.Fatalf("toolName = %q, want Edit", ev.ToolName)
		}
		if ev.SessionID != "sess-abc" {
			t.Fatalf("sessionID = %q, want sess-abc", ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hook event")
	}
}

func TestHookListenerRejectsInvalidJSON(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan HookEvent, 8)

	hl, err := NewHookListener(sock, events)
	if err != nil {
		t.Fatalf("NewHookListener: %v", err)
	}
	defer hl.Stop()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	conn.Write([]byte("not json\n"))
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "ERR: invalid json\n" {
		t.Fatalf("response = %q, want ERR: invalid json", got)
	}
}

func TestHookListenerRejectsUnknownKind(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan HookEvent, 8)

	hl, err := NewHookListener(sock, events)
	if err != nil {
		t.Fatalf("NewHookListener: %v", err)
	}
	defer hl.Stop()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	payload := hookPayload{Kind: "bogus", PaneID: "%1"}
	data, _ := json.Marshal(payload)
	data = append(data, '\n')
	conn.Write(data)

	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "ERR: unknown kind\n" {
		t.Fatalf("response = %q, want ERR: unknown kind", got)
	}
}

func TestHookListenerRemovesStaleSocket(t *testing.T) {
	sock := testSocketPath(t)

	// Create a stale file at the socket path.
	os.WriteFile(sock, []byte("stale"), 0600)

	events := make(chan HookEvent, 8)
	hl, err := NewHookListener(sock, events)
	if err != nil {
		t.Fatalf("NewHookListener should succeed over stale socket: %v", err)
	}
	hl.Stop()
}

func TestApplyAgentUpdatesHookPrecedence(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityWorking,
			Source:   SourceHook,
			ToolName: "Edit",
		},
	}

	// Observer tries to overwrite with idle — should be rejected,
	// but observer's model/context fields should merge.
	updates := map[string]AgentStatus{
		"%1": {
			Running:    true,
			Provider:   ProviderClaude,
			Activity:   ActivityIdle,
			Source:     SourceObserver,
			Model:      "Opus 4.6 (1M context)",
			ContextPct: 42,
			ContextSet: true,
			Branch:     "main",
			ModeLabel:  "yolo",
			Mode:       ModeBypassPermissions,
		},
	}

	result := applyAgentUpdates(dst, updates)
	got := result["%1"]

	// Activity should stay as hook's value.
	if got.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working (hook should win)", got.Activity)
	}
	if got.Source != SourceHook {
		t.Fatalf("source = %v, want SourceHook", got.Source)
	}
	if got.ToolName != "Edit" {
		t.Fatalf("toolName = %q, want Edit (hook field preserved)", got.ToolName)
	}

	// Observer fields should merge in.
	if got.Model != "Opus 4.6 (1M context)" {
		t.Fatalf("model = %q, want merged from observer", got.Model)
	}
	if got.ContextPct != 42 || !got.ContextSet {
		t.Fatalf("context = %d/%v, want 42/true", got.ContextPct, got.ContextSet)
	}
	if got.Branch != "main" {
		t.Fatalf("branch = %q, want main", got.Branch)
	}
	if got.ModeLabel != "yolo" {
		t.Fatalf("mode = %q, want yolo", got.ModeLabel)
	}
}

func TestApplyAgentUpdatesHookOverwritesObserver(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityIdle,
			Source:   SourceObserver,
		},
	}

	// Hook update should fully overwrite observer data.
	updates := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityWorking,
			Source:   SourceHook,
			ToolName: "Bash",
		},
	}

	result := applyAgentUpdates(dst, updates)
	got := result["%1"]

	if got.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working", got.Activity)
	}
	if got.Source != SourceHook {
		t.Fatalf("source = %v, want SourceHook", got.Source)
	}
	if got.ToolName != "Bash" {
		t.Fatalf("toolName = %q, want Bash", got.ToolName)
	}
}

func TestApplyAgentUpdatesDeleteOnNotRunning(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Source: SourceHook},
	}

	updates := map[string]AgentStatus{
		"%1": {Running: false},
	}

	result := applyAgentUpdates(dst, updates)
	if _, ok := result["%1"]; ok {
		t.Fatal("pane should be deleted when Running=false")
	}
}

func TestHookActiveFor(t *testing.T) {
	w := NewWatcher()

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
	w := NewWatcher()

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

// testWatcher sets up a watcher with a message collector for testing.
func testWatcher() (*Watcher, *[]tea.Msg) {
	w := NewWatcher()
	var msgs []tea.Msg
	w.send = func(m tea.Msg) {
		msgs = append(msgs, m)
	}
	return w, &msgs
}

// findAgentUpdate returns the first agentUpdateMsg from the collected messages.
func findAgentUpdate(msgs []tea.Msg) (agentUpdateMsg, bool) {
	for _, m := range msgs {
		if u, ok := m.(agentUpdateMsg); ok {
			return u, true
		}
	}
	return agentUpdateMsg{}, false
}

func TestHandleHookEventSessionStart(t *testing.T) {
	w, msgs := testWatcher()

	w.handleHookEvent(HookEvent{
		Kind:      HookSessionStart,
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
		t.Fatal("expected agentUpdateMsg")
	}
	status := update.updates["%1"]
	if status.Activity != ActivityIdle {
		t.Fatalf("activity = %v, want Idle", status.Activity)
	}
	if status.SessionID != "sess-123" {
		t.Fatalf("sessionID = %q, want sess-123", status.SessionID)
	}
	if status.Source != SourceHook {
		t.Fatalf("source = %v, want SourceHook", status.Source)
	}
}

func TestHandleHookEventPreToolUse(t *testing.T) {
	w, msgs := testWatcher()

	// Seed existing agent state.
	w.agents = map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Activity: ActivityIdle, Model: "Opus 4.6"},
	}

	w.handleHookEvent(HookEvent{
		Kind:     HookPreToolUse,
		PaneID:   "%1",
		ToolName: "Edit",
	})

	update, ok := findAgentUpdate(*msgs)
	if !ok {
		t.Fatal("expected agentUpdateMsg")
	}
	status := update.updates["%1"]
	if status.Activity != ActivityWorking {
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
	w.agents = map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude},
	}

	w.handleHookEvent(HookEvent{
		Kind:    HookNotification,
		PaneID:  "%1",
		Message: "Allow Edit on main.go?",
	})

	update, _ := findAgentUpdate(*msgs)
	status := update.updates["%1"]
	if status.Activity != ActivityWaitingInput {
		t.Fatalf("activity = %v, want WaitingInput", status.Activity)
	}
	if status.Notification != "Allow Edit on main.go?" {
		t.Fatalf("notification = %q, want message", status.Notification)
	}
}

func TestHandleHookEventStop(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Activity: ActivityWorking, ToolName: "Bash"},
	}

	w.handleHookEvent(HookEvent{Kind: HookStop, PaneID: "%1"})

	update, _ := findAgentUpdate(*msgs)
	status := update.updates["%1"]
	if status.Activity != ActivityCompleted {
		t.Fatalf("activity = %v, want Completed (was Working before Stop)", status.Activity)
	}
	if status.ToolName != "" {
		t.Fatalf("toolName = %q, want cleared", status.ToolName)
	}
}

func TestHandleHookEventSessionEnd(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Source: SourceHook},
	}
	w.hookSeen["%1"] = time.Now()
	w.agentPanes["%1"] = true

	w.handleHookEvent(HookEvent{Kind: HookSessionEnd, PaneID: "%1"})

	if w.hookActiveFor("%1") {
		t.Fatal("hook should be cleared after session-end")
	}
	if w.agentPanes["%1"] {
		t.Fatal("agentPanes should be cleared after session-end")
	}

	update, _ := findAgentUpdate(*msgs)
	status := update.updates["%1"]
	if status.Running {
		t.Fatal("Running should be false after session-end")
	}
}

func TestHandleHookEventPromptSubmit(t *testing.T) {
	w, msgs := testWatcher()
	w.agents = map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Activity: ActivityWaitingInput, Notification: "Allow?"},
	}

	w.handleHookEvent(HookEvent{Kind: HookPromptSubmit, PaneID: "%1"})

	update, _ := findAgentUpdate(*msgs)
	status := update.updates["%1"]
	if status.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working", status.Activity)
	}
	if status.Notification != "" {
		t.Fatalf("notification = %q, want cleared", status.Notification)
	}
}

func TestHandleHookEventEmptyPaneIDSkipped(t *testing.T) {
	w, msgs := testWatcher()

	w.handleHookEvent(HookEvent{Kind: HookStop, PaneID: ""})

	if len(*msgs) != 0 {
		t.Fatal("should not send message for empty pane ID")
	}
}

func TestHooksConfigContainsAllEvents(t *testing.T) {
	cfg := HooksConfig("/tmp/test.sock")

	for _, event := range []string{
		"SessionStart", "Stop", "SessionEnd",
		"Notification", "UserPromptSubmit", "PreToolUse",
	} {
		if !contains(cfg, event) {
			t.Fatalf("HooksConfig missing event %q", event)
		}
	}

	// PreToolUse should be async.
	if !contains(cfg, `"async": true`) {
		t.Fatal("PreToolUse should have async: true")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && // avoid trivial match
		stringContains(s, substr)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
