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

	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/tmux"
)

// TestClaudeMultiStepTransitions runs a real Claude agentic task that requires
// multiple tool calls and records the full activity transition trace.
//
// The goal is to capture the exact hook event sequence during a multi-step task
// and detect false Working→Completed→Working cycling caused by Stop events
// firing between agentic turns.
//
// Gate:
//
//	CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeMultiStepTransitions -v -timeout 5m
func TestClaudeMultiStepTransitions(t *testing.T) {
	if os.Getenv("CMS_CLAUDE_INTEGRATION") == "" {
		t.Skip("set CMS_CLAUDE_INTEGRATION=1 to run live Claude multi-step integration test")
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

	traceDir := t.TempDir()
	h := newLiveHarness(t, traceDir)
	projectDir := t.TempDir()

	// Seed the project with files that Claude will need to read and modify.
	seedMultiStepProject(t, projectDir)

	sessionName := "claude-multistep-" + sanitizeSessionName(t.Name())
	settingsPath := writeClaudeHookSettings(t, cmsBin, hook.SocketPath())
	paneID := newTmuxSession(t, sessionName, projectDir)
	defer killTmuxSession(t, sessionName)

	w, traceDirCleanup := startRecordingWatcher(t, traceDir)
	defer traceDirCleanup()
	defer w.Stop()

	// Give Claude a multi-step task: read files, create a new file, run a test.
	// This should produce multiple tool calls with Stop events between turns.
	prompt := `Read all .go files in this directory. Then create a file called result.txt containing: the number of functions found across all files, one function name per line. Finally use Bash to run 'cat result.txt' and confirm the output looks correct. Reply with exactly DONE when finished.`

	cmd := fmt.Sprintf(
		"claude --model haiku --settings '%s' --permission-mode bypassPermissions '%s'",
		settingsPath, prompt,
	)

	if _, err := tmux.Run("send-keys", "-t", paneID, cmd, "Enter"); err != nil {
		t.Fatalf("send-keys claude command: %v", err)
	}
	h.capturePaneNow("after-send-keys", paneID)
	acceptClaudeTrustPrompt(t, h, paneID)

	// Wait for Claude to finish (look for session-end or Stop after substantial work).
	done := waitForBool(180*time.Second, func() bool {
		acceptClaudeTrustPrompt(t, h, paneID)
		ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
		// Consider done when we see at least 2 pre-tool-use events (multi-step)
		// AND a stop event after them.
		toolUseCount := strings.Count(ingress, `"kind":"pre-tool-use"`)
		hasStop := strings.Contains(ingress, `"kind":"stop"`)
		return toolUseCount >= 2 && hasStop
	})

	h.capturePaneNow("final", paneID)

	ingress := readIfExists(filepath.Join(traceDir, "ingress.jsonl"))
	if !done {
		t.Logf("WARNING: scenario did not fully complete, analyzing partial trace")
	}

	// Parse and analyze the trace.
	analysis := analyzeTransitionTrace(t, ingress)

	t.Log("=== Transition Analysis ===")
	t.Logf("Total hook events: %d", analysis.totalHookEvents)
	t.Logf("  session-start: %d", analysis.sessionStarts)
	t.Logf("  prompt-submit: %d", analysis.promptSubmits)
	t.Logf("  pre-tool-use:  %d", analysis.preToolUses)
	t.Logf("  stop:          %d", analysis.stops)
	t.Logf("  notification:  %d", analysis.notifications)
	t.Logf("  session-end:   %d", analysis.sessionEnds)
	t.Log("")
	t.Logf("Activity transitions: %d", analysis.totalTransitions)
	t.Logf("  working→completed cycles: %d", analysis.workingToCompletedCycles)
	t.Logf("  completed→working cycles: %d", analysis.completedToWorkingCycles)
	t.Logf("  Max cycle gap: %s", analysis.maxCycleGap)
	t.Log("")
	t.Log("=== Transition Timeline ===")
	for _, entry := range analysis.timeline {
		t.Log(entry)
	}
	t.Log("")
	t.Log("=== Full ingress.jsonl ===")
	t.Log("\n" + ingress)
	t.Logf("pane capture dir: %s", filepath.Join(traceDir, "pane_captures"))
	t.Logf("trace dir: %s", traceDir)

	// Report cycling as a finding, not a failure — this test is diagnostic.
	if analysis.workingToCompletedCycles > 0 {
		t.Logf("FINDING: %d working→completed→working cycles during multi-step task", analysis.workingToCompletedCycles)
		t.Logf("This confirms Stop events fire between agentic turns, causing false status resets")
	} else if analysis.stops > 1 {
		t.Logf("FINDING: %d Stop events but smoothing absorbed all cycles (smoothing effective)", analysis.stops)
	} else if analysis.preToolUses < 2 {
		t.Logf("WARNING: Only %d pre-tool-use events observed; task may not have been multi-step enough", analysis.preToolUses)
	}
}

// seedMultiStepProject creates a small Go project that gives Claude enough
// files to read and reason about, forcing multiple tool calls.
func seedMultiStepProject(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"go.mod": "module example\n\ngo 1.21\n",
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println(greet("world"))
}

func greet(name string) string {
	return "Hello, " + name
}
`,
		"util.go": `package main

func add(a, b int) int {
	return a + b
}

func multiply(a, b int) int {
	return a * b
}
`,
		"helper.go": `package main

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

type transitionAnalysis struct {
	totalHookEvents          int
	sessionStarts            int
	promptSubmits            int
	preToolUses              int
	stops                    int
	notifications            int
	sessionEnds              int
	totalTransitions         int
	workingToCompletedCycles int
	completedToWorkingCycles int
	maxCycleGap              time.Duration
	timeline                 []string
}

func analyzeTransitionTrace(t *testing.T, ingress string) transitionAnalysis {
	t.Helper()

	var a transitionAnalysis

	type eventLine struct {
		Seq     int64           `json:"seq"`
		Ts      time.Time       `json:"ts"`
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}

	type hookPayload struct {
		Event struct {
			Kind     string `json:"kind"`
			PaneID   string `json:"pane_id"`
			ToolName string `json:"tool_name,omitempty"`
		} `json:"event"`
	}

	type transitionPayload struct {
		PaneID   string `json:"pane_id"`
		Source   string `json:"source"`
		From     string `json:"from"`
		Parsed   string `json:"parsed"`
		Resolved string `json:"resolved"`
		Final    string `json:"final"`
	}

	// Track last committed activity per pane for cycle detection.
	lastActivity := map[string]string{}
	var lastCompletedTime time.Time

	for _, line := range strings.Split(ingress, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var ev eventLine
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		switch ev.Kind {
		case "hook_event":
			var p hookPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			a.totalHookEvents++
			kind := p.Event.Kind
			switch kind {
			case "session-start":
				a.sessionStarts++
			case "prompt-submit":
				a.promptSubmits++
			case "pre-tool-use":
				a.preToolUses++
			case "stop":
				a.stops++
			case "notification":
				a.notifications++
			case "session-end":
				a.sessionEnds++
			}

			toolInfo := ""
			if p.Event.ToolName != "" {
				toolInfo = fmt.Sprintf(" tool=%s", p.Event.ToolName)
			}
			a.timeline = append(a.timeline, fmt.Sprintf(
				"%s  hook %-15s pane=%-4s%s",
				ev.Ts.Format("15:04:05.000"), kind, p.Event.PaneID, toolInfo,
			))

		case "activity_transition":
			var p transitionPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			a.totalTransitions++

			a.timeline = append(a.timeline, fmt.Sprintf(
				"%s  transition %-4s src=%-8s from=%-10s parsed=%-10s resolved=%-10s final=%-10s",
				ev.Ts.Format("15:04:05.000"), p.PaneID, p.Source, p.From, p.Parsed, p.Resolved, p.Final,
			))

			// Detect cycles: working→completed followed by completed→working.
			prev := lastActivity[p.PaneID]
			if p.Final != prev && p.Final != "" {
				if prev == "working" && p.Final == "completed" {
					a.workingToCompletedCycles++
					lastCompletedTime = ev.Ts
				}
				if prev == "completed" && p.Final == "working" {
					a.completedToWorkingCycles++
					gap := ev.Ts.Sub(lastCompletedTime)
					if gap > a.maxCycleGap {
						a.maxCycleGap = gap
					}
				}
				lastActivity[p.PaneID] = p.Final
			}
		}
	}

	return a
}
