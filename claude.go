package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Activity represents what Claude is doing right now.
type Activity int

const (
	ActivityUnknown      Activity = iota
	ActivityIdle                          // at the ❯ prompt, waiting for input
	ActivityWorking                       // actively generating (ctrl+c to interrupt)
	ActivityWaitingInput                  // needs user confirmation ([y/n], permission prompts)
)

func (a Activity) String() string {
	switch a {
	case ActivityIdle:
		return "idle"
	case ActivityWorking:
		return "working"
	case ActivityWaitingInput:
		return "waiting"
	default:
		return "unknown"
	}
}

func (a Activity) Icon() string {
	switch a {
	case ActivityIdle:
		return "💤"
	case ActivityWorking:
		return "⚡"
	case ActivityWaitingInput:
		return "❓"
	default:
		return "·"
	}
}

// ClaudeMode represents the operating mode of Claude Code.
type ClaudeMode int

const (
	ModeNormal ClaudeMode = iota
	ModePlan
	ModeAcceptEdits
	ModeYolo // dangerously accept all
)

func (m ClaudeMode) String() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeAcceptEdits:
		return "auto-edit"
	case ModeYolo:
		return "yolo"
	default:
		return ""
	}
}

// ClaudeStatus represents the state of a Claude Code instance in a pane.
type ClaudeStatus struct {
	Running    bool
	Activity   Activity
	Model      string
	ContextPct int
	Branch     string
	Mode       ClaudeMode
	Args       string
}

// Patterns to parse the Claude status bar.
var (
	statusLineRe = regexp.MustCompile(`^\s*(.+?\([\d]+[kM] context\))\s*\|\s*(\d+)% ctx\s*\|\s*(\S+)`)
	planModeRe       = regexp.MustCompile(`plan mode on`)
	acceptEditsRe    = regexp.MustCompile(`accept edits on`)
	yoloModeRe       = regexp.MustCompile(`dangerously accept`)

	// Spinner line: e.g. "✢ Booping…", "· Cultivating… (32s · ↓ 277 tokens)"
	spinnerRe = regexp.MustCompile(`^[✢✶·⏳⏺●] \S+…`)
	// Tool running: "⎿  Running…" or similar tool output indicator
	toolRunningRe = regexp.MustCompile(`Running…`)
	// Approval prompt: "Do you want to proceed/allow?" followed by Yes/No choices
	approvalRe = regexp.MustCompile(`Do you want to (proceed|allow)`)
	// Numbered selection list with ❯ selector: "❯ 1. Yes" or "  2. No"
	numberedSelectRe = regexp.MustCompile(`^\s*❯?\s*\d+\.\s+(Yes|No)`)
	// Multiple choice navigation hint at the bottom of selection UIs
	choiceNavRe = regexp.MustCompile(`(Enter to select.*↑/↓ to navigate|Esc to cancel.*Tab to amend)`)
)

// DetectClaude checks if Claude Code is running in the given pane.
// It reuses an existing procTable to avoid calling `ps` multiple times.
func DetectClaude(pane Pane, pt procTable) ClaudeStatus {
	status := ClaudeStatus{}

	// Step 1: Check the process tree for a "claude" descendant.
	found, args := findClaudeInTree(pt, pane.PID)
	if !found {
		return status
	}
	status.Running = true
	status.Args = args

	// Step 2: Capture the visible pane content and parse everything.
	content, err := capturePaneBottom(pane.ID)
	if err != nil {
		return status
	}

	parsePane(content, &status)

	// If detected as idle (no spinner), sample the pane to catch streaming output.
	// When Claude streams text there's no spinner — content just changes.
	// Once we see a change (working), require 3 consecutive unchanged samples
	// before switching back to idle. This prevents flickering during pauses.
	if status.Activity == ActivityIdle {
		status.Activity = detectStreaming(pane.ID, content)
	}

	return status
}

// findClaudeInTree walks the process tree from the given PID using
// a pre-built procTable. No subprocess spawned.
func findClaudeInTree(pt procTable, panePID int) (bool, string) {
	queue := []int{panePID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childPID := range pt.children[current] {
			child := pt.procs[childPID]
			if strings.Contains(child.comm, "claude") {
				return true, extractClaudeArgs(child.args)
			}
			queue = append(queue, childPID)
		}
	}
	return false, ""
}

// extractClaudeArgs strips the binary name and returns just the flags.
func extractClaudeArgs(fullArgs string) string {
	parts := strings.Fields(fullArgs)
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[1:], " ")
}

// capturePaneBottom captures the visible content of a tmux pane.
func capturePaneBottom(paneID string) (string, error) {
	return runTmux("capture-pane", "-t", paneID, "-p", "-J")
}

// parsePane extracts Claude activity, model info, and mode from captured pane content.
func parsePane(content string, status *ClaudeStatus) {
	lines := strings.Split(content, "\n")

	status.Activity = detectActivity(lines)

	for i := len(lines) - 1; i >= 0 && i >= len(lines)-8; i-- {
		line := lines[i]

		if m := statusLineRe.FindStringSubmatch(line); m != nil {
			status.Model = m[1]
			status.ContextPct, _ = strconv.Atoi(m[2])
			status.Branch = m[3]
		}

		// Detect mode — check most permissive first.
		if yoloModeRe.MatchString(line) {
			status.Mode = ModeYolo
		} else if acceptEditsRe.MatchString(line) {
			status.Mode = ModeAcceptEdits
		} else if planModeRe.MatchString(line) {
			status.Mode = ModePlan
		}
	}
}

// detectActivity determines what Claude is doing based on pane content.
func detectActivity(lines []string) Activity {
	hasInputField := false
	hasSpinner := false
	hasPermissionPrompt := false
	promptLine := -1

	for i, line := range lines {
		if strings.Contains(line, "❯") && i > 0 && strings.Contains(lines[i-1], "─") {
			hasInputField = true
			promptLine = i
		}
	}

	// Only check for permission/choice UI in the bottom 15 lines to avoid
	// false positives from conversation text containing "Allow"/"Deny"/etc.
	for i := max(0, len(lines)-15); i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "[y/n]") || strings.Contains(line, "[Y/n]") {
			hasPermissionPrompt = true
		}
		if approvalRe.MatchString(line) || choiceNavRe.MatchString(line) || numberedSelectRe.MatchString(line) {
			hasPermissionPrompt = true
		}
	}

	// Look for spinner/working indicators above the prompt line.
	// These appear between content and the ❯ prompt when Claude is generating.
	if promptLine > 0 {
		// Scan the ~10 lines above the prompt for spinner activity
		start := promptLine - 10
		if start < 0 {
			start = 0
		}
		for i := start; i < promptLine; i++ {
			line := strings.TrimSpace(lines[i])
			if spinnerRe.MatchString(line) || toolRunningRe.MatchString(line) {
				hasSpinner = true
				break
			}
		}
	}

	if hasPermissionPrompt {
		return ActivityWaitingInput
	}
	if hasInputField {
		if hasSpinner {
			return ActivityWorking
		}
		return ActivityIdle
	}
	return ActivityUnknown
}

// detectStreaming samples a pane to detect content changes (streaming output).
// Takes 3 samples over ~600ms. Returns ActivityWorking as soon as any change
// is detected, or ActivityIdle if all samples are unchanged.
func detectStreaming(paneID, initial string) Activity {
	prev := initial
	for range 3 {
		time.Sleep(200 * time.Millisecond)
		cur, err := capturePaneBottom(paneID)
		if err != nil {
			continue
		}
		if cur != prev {
			return ActivityWorking
		}
		prev = cur
	}
	return ActivityIdle
}
