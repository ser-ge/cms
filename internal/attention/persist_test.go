package attention

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/serge/cms/internal/tmux"
)

func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func TestPersistActivitySinceRoundTrip(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	// Get a real pane ID to test with.
	out, err := tmux.Run("display-message", "-p", "#{pane_id}")
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
	loaded := LoadPersisted([]string{paneID})
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
	loaded2 := LoadPersisted([]string{paneID})
	if _, ok := loaded2[paneID]; ok {
		t.Fatal("persisted state still present after cleanup")
	}
}
