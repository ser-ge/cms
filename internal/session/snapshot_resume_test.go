package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serge/cms/internal/tmux"
)

// TestSnapshotResumeRoundTrip verifies that Claude session IDs and pane
// markers survive a snapshot save → kill → restore cycle.
//
// Run: CMS_SNAPSHOT_RESUME=1 go test ./internal/session -run TestSnapshotResumeRoundTrip -v
func TestSnapshotResumeRoundTrip(t *testing.T) {
	if os.Getenv("CMS_SNAPSHOT_RESUME") == "" {
		t.Skip("set CMS_SNAPSHOT_RESUME=1 to run")
	}

	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	sessionName := "cms-snap-resume-test"
	repoDir := initTestRepo(t)

	// Create a session with two panes.
	pane0, err := tmux.Run("new-session", "-d", "-s", sessionName, "-c", repoDir, "-P", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("new-session: %v", err)
	}
	pane0 = strings.TrimSpace(pane0)
	defer func() { tmux.Run("kill-session", "-t", sessionName) }()

	pane1, err := tmux.Run("split-window", "-t", sessionName, "-c", repoDir, "-P", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("split-window: %v", err)
	}
	pane1 = strings.TrimSpace(pane1)

	// Simulate what the watcher hook would do: set @cms_claude_session on pane0.
	claudeSession0 := "test-session-abc-123"
	tmux.Run("set-option", "-p", "-t", pane0, "@cms_claude_session", claudeSession0)
	tmux.Run("set-option", "-p", "-t", pane0, "@cms_pane_id", "editor")
	tmux.Run("set-option", "-p", "-t", pane0, "@cms_claude_resume", "1")

	// Set only a session ID on pane1 (no marker — tests the auto-resume path).
	claudeSession1 := "test-session-xyz-789"
	tmux.Run("set-option", "-p", "-t", pane1, "@cms_claude_session", claudeSession1)

	// --- Save snapshot ---
	if err := SaveSnapshot(sessionName, repoDir); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	// Verify snapshot file contains our session IDs.
	snapPath, err := snapshotPath(repoDir, sessionName)
	if err != nil {
		t.Fatalf("snapshotPath: %v", err)
	}
	snapData, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("ReadFile snapshot: %v", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(snapData, &snap); err != nil {
		t.Fatalf("Unmarshal snapshot: %v", err)
	}
	if len(snap.Windows) == 0 || len(snap.Windows[0].Panes) < 2 {
		t.Fatalf("expected ≥2 panes, got %d windows with %v panes",
			len(snap.Windows), func() []int {
				var n []int
				for _, w := range snap.Windows {
					n = append(n, len(w.Panes))
				}
				return n
			}())
	}

	sp0 := snap.Windows[0].Panes[0]
	if sp0.ClaudeSessionID != claudeSession0 {
		t.Errorf("snap pane0 ClaudeSessionID = %q, want %q", sp0.ClaudeSessionID, claudeSession0)
	}
	if sp0.Marker != "editor" {
		t.Errorf("snap pane0 Marker = %q, want %q", sp0.Marker, "editor")
	}
	if !sp0.ResumeFlag {
		t.Error("snap pane0 ResumeFlag = false, want true")
	}

	sp1 := snap.Windows[0].Panes[1]
	if sp1.ClaudeSessionID != claudeSession1 {
		t.Errorf("snap pane1 ClaudeSessionID = %q, want %q", sp1.ClaudeSessionID, claudeSession1)
	}
	if sp1.Marker != "" {
		t.Errorf("snap pane1 Marker = %q, want empty", sp1.Marker)
	}

	// --- Kill and restore ---
	tmux.Run("kill-session", "-t", sessionName)

	restored, paneMap, err := RestoreSnapshot(sessionName, repoDir)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if !restored {
		t.Fatal("RestoreSnapshot returned false, expected true")
	}

	// paneMap should have both session IDs.
	if len(paneMap) != 2 {
		t.Fatalf("paneMap has %d entries, want 2: %v", len(paneMap), paneMap)
	}

	foundIDs := map[string]bool{}
	for _, sid := range paneMap {
		foundIDs[sid] = true
	}
	if !foundIDs[claudeSession0] {
		t.Errorf("paneMap missing %q", claudeSession0)
	}
	if !foundIDs[claudeSession1] {
		t.Errorf("paneMap missing %q", claudeSession1)
	}

	// Verify restored panes have @cms_claude_session set.
	for paneID, sid := range paneMap {
		got := paneOpt(t, paneID, "@cms_claude_session")
		if got != sid {
			t.Errorf("pane %s @cms_claude_session = %q, want %q", paneID, got, sid)
		}
	}

	// Verify marker + resume flag restored on the pane that had them.
	for paneID, sid := range paneMap {
		if sid != claudeSession0 {
			continue
		}
		if m := paneOpt(t, paneID, "@cms_pane_id"); m != "editor" {
			t.Errorf("pane %s @cms_pane_id = %q, want %q", paneID, m, "editor")
		}
		if r := paneOpt(t, paneID, "@cms_claude_resume"); r != "1" {
			t.Errorf("pane %s @cms_claude_resume = %q, want %q", paneID, r, "1")
		}
	}
}

// TestSnapshotBackwardCompat verifies that a snapshot without the new fields
// restores cleanly with an empty paneMap.
func TestSnapshotBackwardCompat(t *testing.T) {
	if os.Getenv("CMS_SNAPSHOT_RESUME") == "" {
		t.Skip("set CMS_SNAPSHOT_RESUME=1 to run")
	}

	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	sessionName := "cms-snap-compat-test"
	repoDir := initTestRepo(t)

	// Write an old-format snapshot (no claude_session_id fields).
	oldSnap := Snapshot{
		Session: sessionName,
		Windows: []SnapWindow{{
			Index: 0,
			Name:  "main",
			Panes: []SnapPane{
				{Index: 0, WorkingDir: repoDir},
			},
		}},
		Focus: SnapFocus{Window: 0, Pane: 0},
	}
	snapPath, err := snapshotPath(repoDir, sessionName)
	if err != nil {
		t.Fatalf("snapshotPath: %v", err)
	}
	os.MkdirAll(filepath.Dir(snapPath), 0o755)
	data, _ := json.MarshalIndent(oldSnap, "", "  ")
	os.WriteFile(snapPath, data, 0o644)

	restored, paneMap, err := RestoreSnapshot(sessionName, repoDir)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	defer func() { tmux.Run("kill-session", "-t", sessionName) }()

	if !restored {
		t.Fatal("RestoreSnapshot returned false")
	}
	if len(paneMap) != 0 {
		t.Errorf("paneMap should be empty for old snapshot, got %v", paneMap)
	}
}

// --- helpers ---

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s (%v)", args, out, err)
		}
	}
	return dir
}

func paneOpt(t *testing.T, paneID, option string) string {
	t.Helper()
	out, err := tmux.Run("display-message", "-p", "-t", paneID, "#{"+option+"}")
	if err != nil {
		t.Fatalf("display-message %s %s: %v", paneID, option, err)
	}
	return strings.TrimSpace(out)
}
