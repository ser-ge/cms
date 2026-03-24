package main

import (
	"regexp"
	"strconv"
	"strings"
)

// Patterns to parse the Claude status bar.
var (
	claudeStatusLineRe = regexp.MustCompile(`^\s*(.+?\([\d]+[kM] context\))\s*\|\s*(\d+)% ctx\s*\|\s*(\S+)`)
	planModeRe         = regexp.MustCompile(`plan mode on`)
	acceptEditsRe      = regexp.MustCompile(`accept edits on`)
	yoloModeRe         = regexp.MustCompile(`(bypass permissions on|dangerously accept)`)

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
func DetectClaude(pane Pane, pt procTable) AgentStatus {
	status := AgentStatus{Provider: ProviderClaude}

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

	parseClaudePane(content, &status)

	return status
}

// findClaudeInTree walks the process tree from the given PID using
// a pre-built procTable. No subprocess spawned.
func findClaudeInTree(pt procTable, panePID int) (bool, string) {
	return findProcessInTree(pt, panePID, func(p procEntry) bool {
		return strings.Contains(p.comm, "claude")
	}, extractArgsAfterBinary)
}

// capturePaneBottom captures the visible content of a tmux pane.
func capturePaneBottom(paneID string) (string, error) {
	return runTmux("capture-pane", "-t", paneID, "-p", "-J")
}

// parsePane extracts Claude activity, model info, and mode from captured pane content.
func parseClaudePane(content string, status *AgentStatus) {
	lines := strings.Split(content, "\n")

	status.Activity = detectClaudeActivity(lines)

	for i := len(lines) - 1; i >= 0 && i >= len(lines)-8; i-- {
		line := lines[i]

		if m := claudeStatusLineRe.FindStringSubmatch(line); m != nil {
			status.Model = m[1]
			status.ContextPct, _ = strconv.Atoi(m[2])
			status.ContextSet = true
			status.Branch = m[3]
		}

		if status.ModeLabel == "" {
			// Detect mode — check most permissive first.
			if yoloModeRe.MatchString(line) {
				status.Mode = ModeBypassPermissions
				status.ModeLabel = "yolo"
			} else if acceptEditsRe.MatchString(line) {
				status.Mode = ModeAcceptEdits
				status.ModeLabel = "accept edits"
			} else if planModeRe.MatchString(line) {
				status.Mode = ModePlan
				status.ModeLabel = "plan"
			}
		}
	}

	normalizeParsedAgentStatus(status)
}

// detectActivity determines what Claude is doing based on pane content.
func detectClaudeActivity(lines []string) Activity {
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
