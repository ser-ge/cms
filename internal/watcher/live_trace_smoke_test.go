package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/trace"
)

func TestLiveTraceSmoke(t *testing.T) {
	if os.Getenv("CMS_LIVE_TRACE_SMOKE") == "" {
		t.Skip("set CMS_LIVE_TRACE_SMOKE=1 to run live watcher/tmux trace smoke test")
	}
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	traceDir := t.TempDir()
	h := newLiveHarness(t, traceDir)
	rec, err := trace.NewJSONLRecorder(traceDir)
	if err != nil {
		t.Fatalf("NewJSONLRecorder: %v", err)
	}
	defer rec.Close()

	sessionName := "trace-smoke"
	_, _ = tmux.Run("kill-session", "-t", sessionName)

	paneID, err := tmux.Run("new-session", "-d", "-s", sessionName, "-P", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("new-session: %v", err)
	}
	defer func() {
		h.capturePaneNow("final", paneID)
		_, _ = tmux.Run("kill-session", "-t", sessionName)
	}()

	w := New()
	w.SetRecorder(rec)
	msgs := make(chan tea.Msg, 64)
	w.Start(func(m tea.Msg) { msgs <- m })
	defer w.Stop()

	h.waitFor(5*time.Second, func() {
		h.capturePaneNow("bootstrap-timeout", paneID)
	}, func() bool {
		ingress := readIfExists(traceDir + "/ingress.jsonl")
		return strings.Contains(ingress, `"kind":"bootstrap_state"`)
	})

	w.mu.Lock()
	w.agentPanes[paneID] = true
	w.mu.Unlock()

	if _, err := tmux.Run("send-keys", "-t", paneID, "printf 'trace smoke line\\n'", "Enter"); err != nil {
		t.Fatalf("send-keys print: %v", err)
	}

	h.waitFor(5*time.Second, func() {
		h.capturePaneNow("observer-timeout", paneID)
	}, func() bool {
		ingress := readIfExists(traceDir + "/ingress.jsonl")
		return strings.Contains(ingress, `"kind":"timer_fired"`) &&
			strings.Contains(ingress, `"kind":"capture_snapshot"`) &&
			strings.Contains(ingress, `trace smoke line`)
	})
	h.capturePaneNow("after-observer", paneID)

	extraPane, err := tmux.Run("split-window", "-d", "-t", paneID, "-P", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("split-window: %v", err)
	}
	if _, err := tmux.Run("kill-pane", "-t", extraPane); err != nil {
		t.Fatalf("kill-pane: %v", err)
	}

	h.waitFor(5*time.Second, func() {
		h.capturePaneNow("structural-timeout", paneID)
	}, func() bool {
		ingress := readIfExists(traceDir + "/ingress.jsonl")
		tmuxState := readIfExists(traceDir + "/tmux_state.jsonl")
		return strings.Contains(ingress, `"kind":"tmux_event"`) &&
			strings.Contains(ingress, `"kind":"full_refresh_snapshot"`) &&
			strings.Contains(tmuxState, `"reason":"bootstrap"`) &&
			strings.Contains(tmuxState, `"reason":"full_refresh"`)
	})

	t.Log("=== ingress.jsonl ===")
	t.Log("\n" + readIfExists(traceDir+"/ingress.jsonl"))
	t.Log("=== tmux_state.jsonl ===")
	t.Log("\n" + readIfExists(traceDir+"/tmux_state.jsonl"))
	t.Logf("pane capture dir: %s", filepath.Join(traceDir, "pane_captures"))

	_ = msgs
}

func readIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
