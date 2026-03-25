package main

import (
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	if err != nil {
		return false
	}
	// Check if a tmux server is actually running.
	cmd := exec.Command("tmux", "list-sessions")
	return cmd.Run() == nil
}

func TestPersistActivitySinceRoundTrip(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available or no server running")
	}

	// Get a real pane ID to test with.
	out, err := runTmux("display-message", "-p", "#{pane_id}")
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := out

	// Write a known timestamp.
	ts := time.Now().Add(-3 * time.Minute).Truncate(time.Second)
	err = setTmuxPaneOption(paneID, tmuxOptActivity, "working")
	if err != nil {
		t.Fatalf("set activity: %v", err)
	}
	err = setTmuxPaneOption(paneID, tmuxOptSince, strconv.FormatInt(ts.Unix(), 10))
	if err != nil {
		t.Fatalf("set since: %v", err)
	}

	// Read it back.
	loaded := LoadPersistedActivitySince([]string{paneID})
	got, ok := loaded[paneID]
	if !ok {
		t.Fatal("persisted state not found after write")
	}
	if got.activity != "working" {
		t.Fatalf("activity = %q, want %q", got.activity, "working")
	}
	if got.since.Unix() != ts.Unix() {
		t.Fatalf("since = %v, want %v", got.since, ts)
	}

	// Clean up.
	_ = unsetTmuxPaneOption(paneID, tmuxOptActivity)
	_ = unsetTmuxPaneOption(paneID, tmuxOptSince)

	// Verify cleanup — should return empty.
	loaded2 := LoadPersistedActivitySince([]string{paneID})
	if _, ok := loaded2[paneID]; ok {
		t.Fatal("persisted state still present after cleanup")
	}
}

func TestPersistActivitySinceRestoredOnBootstrap(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available or no server running")
	}

	// Get real pane to set options on.
	out, err := runTmux("display-message", "-p", "#{pane_id}")
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := out

	// Simulate a prior cms session that persisted "idle" 5 minutes ago.
	fiveMinAgo := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	_ = setTmuxPaneOption(paneID, tmuxOptActivity, "idle")
	_ = setTmuxPaneOption(paneID, tmuxOptSince, strconv.FormatInt(fiveMinAgo.Unix(), 10))
	defer func() {
		_ = unsetTmuxPaneOption(paneID, tmuxOptActivity)
		_ = unsetTmuxPaneOption(paneID, tmuxOptSince)
	}()

	// Simulate what bootstrap does: load persisted, then apply if activity matches.
	agents := map[string]AgentStatus{
		paneID: {Running: true, Provider: ProviderClaude, Activity: ActivityIdle},
	}
	persisted := LoadPersistedActivitySince([]string{paneID})

	w := NewWatcher()
	for id, status := range agents {
		w.agentPanes[id] = true
		if p, ok := persisted[id]; ok && p.activity == status.Activity.String() {
			w.activitySince[id] = p.since
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

	out, err := runTmux("display-message", "-p", "#{pane_id}")
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := out

	// Persist "working" but current activity is "idle" — should NOT restore.
	_ = setTmuxPaneOption(paneID, tmuxOptActivity, "working")
	_ = setTmuxPaneOption(paneID, tmuxOptSince, strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10))
	defer func() {
		_ = unsetTmuxPaneOption(paneID, tmuxOptActivity)
		_ = unsetTmuxPaneOption(paneID, tmuxOptSince)
	}()

	agents := map[string]AgentStatus{
		paneID: {Running: true, Provider: ProviderClaude, Activity: ActivityIdle},
	}
	persisted := LoadPersistedActivitySince([]string{paneID})

	w := NewWatcher()
	for id, status := range agents {
		if p, ok := persisted[id]; ok && p.activity == status.Activity.String() {
			w.activitySince[id] = p.since
		}
	}

	since := w.ActivitySince()
	if _, ok := since[paneID]; ok {
		t.Fatal("activitySince should NOT be restored when persisted activity doesn't match current")
	}
}
