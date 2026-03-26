package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/serge/cms/internal/tmux"
)

type liveHarness struct {
	t          *testing.T
	dir        string
	captureDir string
}

func newLiveHarness(t *testing.T, dir string) *liveHarness {
	t.Helper()
	h := &liveHarness{
		t:          t,
		dir:        dir,
		captureDir: filepath.Join(dir, "pane_captures"),
	}
	if err := os.MkdirAll(h.captureDir, 0o755); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	return h
}

func (h *liveHarness) capturePane(label, paneID string) string {
	h.t.Helper()
	content, err := tmux.CapturePaneBottom(paneID)
	if err != nil {
		content = "capture error: " + err.Error()
	}
	name := sanitizeFileComponent(label) + "-" + sanitizeFileComponent(strings.TrimPrefix(paneID, "%")) + ".txt"
	path := filepath.Join(h.captureDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatalf("write pane capture %s: %v", path, err)
	}
	return path
}

func (h *liveHarness) capturePaneNow(label, paneID string) {
	h.t.Helper()
	path := h.capturePane(label, paneID)
	h.t.Logf("pane capture %s: %s", label, path)
}

func (h *liveHarness) waitFor(timeout time.Duration, onTimeout func(), cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if onTimeout != nil {
		onTimeout()
	}
	h.t.Fatal("condition not met before timeout")
}

func sanitizeFileComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "capture"
	}
	replacer := strings.NewReplacer("/", "-", " ", "-", "%", "", "_", "-")
	return replacer.Replace(s)
}
