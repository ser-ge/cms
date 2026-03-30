package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/trace"
)

func TestClaudeHookIntegration(t *testing.T) {
	if os.Getenv("CMS_CLAUDE_INTEGRATION") == "" {
		t.Skip("set CMS_CLAUDE_INTEGRATION=1 to run live Claude hook integration tests")
	}
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not available")
	}

	repoRoot := mustRepoRoot(t)
	cmsBin := filepath.Join(t.TempDir(), "cms")
	build := exec.Command("go", "build", "-o", cmsBin, repoRoot)
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cms: %v\n%s", err, string(out))
	}

	t.Run("bash_sleep_hooks", func(t *testing.T) {
		traceDir := t.TempDir()
		runClaudeScenario(t, cmsBin, traceDir, t.TempDir(), func(settingsPath string) string {
			return fmt.Sprintf(
				"claude --model haiku --settings '%s' --permission-mode bypassPermissions --allowedTools Bash \"Use Bash to run 'sleep 5' and then reply with exactly done.\"",
				settingsPath,
			)
		}, func(ingress string) bool {
			return strings.Contains(ingress, `"kind":"pre-tool-use"`) &&
				strings.Contains(ingress, `"tool_name":"Bash"`)
		})

		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		assertTraceContains(t, ingress, `"kind":"hook_event"`)
		assertTraceContains(t, ingress, `"kind":"session-start"`)
		assertTraceContains(t, ingress, `"kind":"pre-tool-use"`)
		assertTraceContains(t, ingress, `"tool_name":"Bash"`)
		t.Log("=== bash_sleep ingress.jsonl ===")
		t.Log("\n" + ingress)
		t.Logf("pane capture dir: %s", filepath.Join(traceDir, "pane_captures"))
	})

	t.Run("permission_prompt_hooks", func(t *testing.T) {
		traceDir := t.TempDir()
		h := newLiveHarness(t, traceDir)
		projectDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(projectDir, "note.txt"), []byte("hello\n"), 0o644); err != nil {
			t.Fatalf("write note.txt: %v", err)
		}
		sessionName := "claude-perm-" + sanitizeSessionName(t.Name())
		settingsPath := writeClaudeHookSettings(t, cmsBin, hook.SocketPath())
		paneID := newTmuxSession(t, sessionName, projectDir)
		defer killTmuxSession(t, sessionName)

		w, traceDirCleanup := startRecordingWatcher(t, traceDir)
		defer traceDirCleanup()
		defer w.Stop()

		cmd := fmt.Sprintf(
			"claude --model haiku --settings '%s' --permission-mode default \"Use Edit to append the word approved to note.txt.\"",
			settingsPath,
		)
		if _, err := tmux.Run("send-keys", "-t", paneID, cmd, "Enter"); err != nil {
			t.Fatalf("send-keys claude permission command: %v", err)
		}

		h.waitFor(90*time.Second, func() {
			h.capturePaneNow("notification-timeout", paneID)
		}, func() bool {
			ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
			return strings.Contains(ingress, `"kind":"hook_event"`) &&
				strings.Contains(ingress, `"kind":"notification"`)
		})
		h.capturePaneNow("notification-ready", paneID)

		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		assertTraceContains(t, ingress, `"kind":"session-start"`)
		assertTraceContains(t, ingress, `"kind":"notification"`)
		t.Log("=== permission_prompt ingress.jsonl ===")
		t.Log("\n" + ingress)
		t.Logf("pane capture dir: %s", filepath.Join(traceDir, "pane_captures"))
	})
}

// TestClaudeObserverLongOutput runs Claude without hooks (observer-only) and
// asks it to generate a long text. The test verifies that the observer
// maintains Working status throughout the generation without false
// Completed/Idle transitions.
//
// Gate:
//
//	CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeObserverLongOutput -v -timeout 5m
func TestClaudeObserverLongOutput(t *testing.T) {
	if os.Getenv("CMS_CLAUDE_INTEGRATION") == "" {
		t.Skip("set CMS_CLAUDE_INTEGRATION=1 to run live Claude observer integration tests")
	}
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not available")
	}

	traceDir := t.TempDir()
	h := newLiveHarness(t, traceDir)
	projectDir := t.TempDir()

	sessionName := "claude-observer-" + sanitizeSessionName(t.Name())
	paneID := newTmuxSession(t, sessionName, projectDir)
	defer killTmuxSession(t, sessionName)

	rec, err := trace.NewJSONLRecorder(traceDir)
	if err != nil {
		t.Fatalf("NewJSONLRecorder: %v", err)
	}
	defer func() { _ = rec.Close() }()

	w := New()
	w.SetRecorder(rec)
	// Do NOT set up hooks — this tests the observer-only path.
	msgs := make(chan tea.Msg, 256)
	w.Start(func(m tea.Msg) { msgs <- m })
	defer w.Stop()

	h.waitFor(10*time.Second, nil, func() bool {
		return strings.Contains(readIfExists(filepath.Join(traceDir, "ingress.jsonl")), `"kind":"bootstrap_state"`)
	})

	// Ask Claude to write a long story — no hooks configured, so observer must detect Working.
	cmd := fmt.Sprintf(
		"claude --model haiku --permission-mode bypassPermissions \"Write a detailed short story about a dragon who learns to code. Make it at least 40 lines long. Do not use any tools, just write the story directly.\"",
	)
	if _, err := tmux.Run("send-keys", "-t", paneID, cmd, "Enter"); err != nil {
		t.Fatalf("send-keys claude command: %v", err)
	}
	h.capturePaneNow("after-send-keys", paneID)
	acceptClaudeTrustPrompt(t, h, paneID)

	// Wait for Claude to start producing output (look for activity transitions).
	started := waitForBool(60*time.Second, func() bool {
		acceptClaudeTrustPrompt(t, h, paneID)
		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		return strings.Contains(ingress, `"final":"working"`)
	})
	if !started {
		h.capturePaneNow("never-started", paneID)
		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		t.Fatalf("observer never detected Working state\n=== ingress.jsonl ===\n%s", ingress)
	}
	h.capturePaneNow("working-detected", paneID)

	// Let Claude generate for a while, capturing periodically.
	time.Sleep(10 * time.Second)
	h.capturePaneNow("mid-generation", paneID)

	// Wait for Claude to finish (prompt returns to idle).
	waitForBool(120*time.Second, func() bool {
		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		return strings.Contains(ingress, `"final":"completed"`) ||
			strings.Contains(ingress, `"final":"idle"`)
	})
	h.capturePaneNow("final", paneID)

	// Analyze the trace for false Working→Completed→Working cycles.
	ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
	analysis := analyzeTransitionTrace(t, ingress)

	t.Log("=== Observer Long Output Transition Analysis ===")
	t.Logf("Activity transitions: %d", analysis.totalTransitions)
	t.Logf("  working→completed cycles: %d", analysis.workingToCompletedCycles)
	t.Logf("  completed→working cycles: %d", analysis.completedToWorkingCycles)
	t.Log("")
	t.Log("=== Transition Timeline ===")
	for _, entry := range analysis.timeline {
		t.Log(entry)
	}
	t.Log("")
	t.Logf("trace dir: %s", traceDir)
	t.Logf("pane capture dir: %s", filepath.Join(traceDir, "pane_captures"))

	// The key assertion: during a single continuous generation, there should
	// be no false Working→Completed→Working cycling from the observer.
	if analysis.workingToCompletedCycles > 1 {
		t.Errorf("REGRESSION: %d working→completed cycles during continuous generation (want ≤1, the final real completion)",
			analysis.workingToCompletedCycles)
	}
}

func startRecordingWatcher(t *testing.T, traceDir string) (*Watcher, func()) {
	t.Helper()
	rec, err := trace.NewJSONLRecorder(traceDir)
	if err != nil {
		t.Fatalf("NewJSONLRecorder: %v", err)
	}
	w := New()
	w.SetRecorder(rec)
	msgs := make(chan tea.Msg, 64)
	w.Start(func(m tea.Msg) { msgs <- m })
	h := newLiveHarness(t, traceDir)
	h.waitFor(10*time.Second, nil, func() bool {
		return strings.Contains(readIfExists(filepath.Join(traceDir, "ingress.jsonl")), `"kind":"bootstrap_state"`)
	})
	return w, func() { _ = rec.Close() }
}

func runClaudeScenario(t *testing.T, cmsBin, traceDir, projectDir string, command func(settingsPath string) string, ready func(string) bool) {
	t.Helper()
	h := newLiveHarness(t, traceDir)
	sessionName := "claude-hook-" + sanitizeSessionName(t.Name())
	settingsPath := writeClaudeHookSettings(t, cmsBin, hook.SocketPath())
	paneID := newTmuxSession(t, sessionName, projectDir)
	cmd := command(settingsPath)

	w, cleanup := startRecordingWatcher(t, traceDir)
	defer cleanup()
	defer w.Stop()
	defer func() {
		h.capturePaneNow("final", paneID)
		killTmuxSession(t, sessionName)
	}()

	if _, err := tmux.Run("send-keys", "-t", paneID, cmd, "Enter"); err != nil {
		t.Fatalf("send-keys claude command: %v", err)
	}
	h.capturePaneNow("after-send-keys", paneID)
	acceptClaudeTrustPrompt(t, h, paneID)

	if waitForBool(120*time.Second, func() bool {
		acceptClaudeTrustPrompt(t, h, paneID)
		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		return ready(ingress)
	}) {
		h.capturePaneNow("ready", paneID)
		return
	}

	ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
	timeoutCapture := h.capturePane("timeout", paneID)
	t.Fatalf("scenario did not reach ready state\ncommand: %s\n=== ingress.jsonl ===\n%s\n=== pane capture file ===\n%s\n=== pane capture ===\n%s", cmd, ingress, timeoutCapture, readIfExists(timeoutCapture))
}

func newTmuxSession(t *testing.T, sessionName, dir string) string {
	t.Helper()
	_, _ = tmux.Run("kill-session", "-t", sessionName)
	paneID, err := tmux.Run("new-session", "-d", "-s", sessionName, "-c", dir, "-P", "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("new-session: %v", err)
	}
	return paneID
}

func killTmuxSession(t *testing.T, sessionName string) {
	t.Helper()
	_, _ = tmux.Run("kill-session", "-t", sessionName)
}

func writeClaudeHookSettings(t *testing.T, cmsBin, socketPath string) string {
	t.Helper()
	base := fmt.Sprintf(`%s internal hook --socket %s`, cmsBin, socketPath)
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " session-start", "timeout": 10}},
			}},
			"Stop": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " stop", "timeout": 5}},
			}},
			"SessionEnd": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " session-end", "timeout": 2}},
			}},
			"Notification": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " notification", "timeout": 10}},
			}},
			"UserPromptSubmit": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " prompt-submit", "timeout": 10}},
			}},
			"PreToolUse": []map[string]any{{
				"matcher": "",
				"hooks":   []map[string]any{{"type": "command", "command": base + " pre-tool-use", "timeout": 5, "async": true}},
			}},
		},
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), "claude-settings.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

func assertTraceContains(t *testing.T, traceText, needle string) {
	t.Helper()
	if !strings.Contains(traceText, needle) {
		t.Fatalf("trace missing %q\n%s", needle, traceText)
	}
}

func waitForBool(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func acceptClaudeTrustPrompt(t *testing.T, h *liveHarness, paneID string) {
	t.Helper()
	content, err := tmux.CapturePaneBottom(paneID)
	if err != nil {
		return
	}
	if !strings.Contains(content, "Yes, I trust this folder") {
		return
	}
	if !strings.Contains(content, "Quick safety check") {
		return
	}

	h.capturePaneNow("trust-prompt", paneID)
	if _, err := tmux.Run("send-keys", "-t", paneID, "Enter"); err != nil {
		t.Fatalf("send-keys trust accept: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	h.capturePaneNow("trust-accepted", paneID)
}

func sanitizeSessionName(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer("/", "-", " ", "-", "_", "-").Replace(name)
	return name
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root from working directory")
		}
		dir = parent
	}
}
