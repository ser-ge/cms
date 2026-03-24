package main

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	codexModelRe          = regexp.MustCompile(`(?i)\bmodel:\s*([^\|\n]+)`)
	codexCtxRe            = regexp.MustCompile(`(?i)\b(\d+)%\s*ctx\b`)
	codexLeftRe           = regexp.MustCompile(`(?i)\b(\d+)%\s*left\b`)
	codexBranchRe         = regexp.MustCompile(`(?i)\bbranch:\s*(\S+)`)
	codexPlanModeRe       = regexp.MustCompile(`(?i)\bplan mode\b`)
	codexAcceptEditsRe    = regexp.MustCompile(`(?i)\baccept edits\b`)
	codexReadOnlyRe       = regexp.MustCompile(`(?i)\bread-only\b`)
	codexWorkspaceWriteRe = regexp.MustCompile(`(?i)\bworkspace-write\b`)
	codexDangerRe         = regexp.MustCompile(`(?i)\bdanger-full-access\b`)
	codexFullAutoRe       = regexp.MustCompile(`(?i)\bfull auto\b`)

	codexSpinnerRe  = regexp.MustCompile(`^[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏●·] `)
	codexRunningRe  = regexp.MustCompile(`(?i)\b(running\.\.\.|executing command|applying patch|thinking\.\.\.)\b`)
	// "Working (47s • esc to interrupt)" — active task indicator shown between prompts.
	codexWorkingRe = regexp.MustCompile(`Working \(\d+s`)
	codexApprovalRe = regexp.MustCompile(`(?i)(approval|approve|deny|allow|reject)`)
	codexChoiceRe   = regexp.MustCompile(`^\s*❯?\s*\d+\.\s+\S+`)
	codexFooterRe   = regexp.MustCompile(`(?i)(enter to (approve|select)|esc to (deny|cancel)|↑/↓ to navigate)`)
)

// DetectCodex checks if Codex is running in the given pane.
func DetectCodex(pane Pane, pt procTable) AgentStatus {
	status := AgentStatus{Provider: ProviderCodex}

	found, args := findCodexInTree(pt, pane.PID)
	if !found {
		return status
	}
	status.Running = true
	status.Args = args

	content, err := capturePaneBottom(pane.ID)
	if err != nil {
		return status
	}

	parseCodexPane(content, &status)
	return status
}

func findCodexInTree(pt procTable, panePID int) (bool, string) {
	return findProcessInTree(pt, panePID, func(p procEntry) bool {
		return strings.Contains(p.comm, "codex")
	}, extractArgsAfterBinary)
}

func parseCodexPane(content string, status *AgentStatus) {
	lines := strings.Split(content, "\n")
	status.Activity = detectCodexActivity(lines)

	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		line := lines[i]

		if status.Model == "" {
			if m := codexModelRe.FindStringSubmatch(line); m != nil {
				status.Model = strings.TrimSpace(m[1])
			}
		}
		if !status.ContextSet {
			if m := codexCtxRe.FindStringSubmatch(line); m != nil {
				status.ContextPct, _ = strconv.Atoi(m[1])
				status.ContextSet = true
			} else if m := codexLeftRe.FindStringSubmatch(line); m != nil {
				left, _ := strconv.Atoi(m[1])
				if left < 0 {
					left = 0
				}
				if left > 100 {
					left = 100
				}
				status.ContextPct = 100 - left
				status.ContextSet = true
			}
		}
		if status.Branch == "" {
			if m := codexBranchRe.FindStringSubmatch(line); m != nil {
				status.Branch = m[1]
			}
		}

		if status.ModeLabel == "" {
			switch {
			case codexDangerRe.MatchString(line):
				status.Mode = ModeDangerFullAccess
				status.ModeLabel = "danger-full-access"
			case codexWorkspaceWriteRe.MatchString(line):
				status.Mode = ModeWorkspaceWrite
				status.ModeLabel = "workspace-write"
			case codexReadOnlyRe.MatchString(line):
				status.Mode = ModeReadOnly
				status.ModeLabel = "read-only"
			case codexFullAutoRe.MatchString(line):
				status.Mode = ModeBypassPermissions
				status.ModeLabel = "full auto"
			case codexAcceptEditsRe.MatchString(line):
				status.Mode = ModeAcceptEdits
				status.ModeLabel = "accept edits"
			case codexPlanModeRe.MatchString(line):
				status.Mode = ModePlan
				status.ModeLabel = "plan mode"
			}
		}
	}

	normalizeParsedAgentStatus(status)
}

func detectCodexActivity(lines []string) Activity {
	hasPrompt := false
	hasSpinner := false
	hasWorkingIndicator := false
	promptLine := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "›") || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "❯") {
			hasPrompt = true
			promptLine = i
		}
	}

	// Check for spinner/running text in a small window above the last prompt.
	if promptLine > 0 {
		start := promptLine - 3
		if start < 0 {
			start = 0
		}
		for i := start; i < promptLine; i++ {
			line := strings.TrimSpace(lines[i])
			if codexSpinnerRe.MatchString(line) || codexRunningRe.MatchString(line) {
				hasSpinner = true
				break
			}
		}
	}

	// Scan the bottom half for Codex's "Working (Xs • esc to interrupt)" indicator.
	// This appears between prompts and can be far above the last prompt line.
	scanStart := max(0, len(lines)/2)
	for i := scanStart; i < len(lines); i++ {
		if codexWorkingRe.MatchString(lines[i]) {
			hasWorkingIndicator = true
			break
		}
	}

	if hasPrompt && hasCodexApprovalUI(lines, promptLine) {
		return ActivityWaitingInput
	}
	if hasPrompt {
		if hasSpinner || hasWorkingIndicator {
			return ActivityWorking
		}
		return ActivityIdle
	}
	return ActivityUnknown
}

func hasCodexApprovalUI(lines []string, promptLine int) bool {
	start := max(0, promptLine-3)
	end := min(len(lines)-1, promptLine+2)

	hasExplicitApproval := false
	hasChoiceUI := false

	for i := start; i <= end; i++ {
		line := strings.TrimSpace(lines[i])
		lower := strings.ToLower(line)

		if strings.Contains(line, "[y/n]") || strings.Contains(line, "[Y/n]") {
			return true
		}
		if strings.Contains(lower, "tool call needs your approval") {
			hasExplicitApproval = true
		}
		if strings.Contains(lower, "enter to approve") || strings.Contains(lower, "esc to deny") {
			hasExplicitApproval = true
		}
		if codexApprovalRe.MatchString(line) && strings.Contains(lower, "approval") {
			hasExplicitApproval = true
		}
		if codexChoiceRe.MatchString(line) || codexFooterRe.MatchString(line) {
			hasChoiceUI = true
		}
	}

	return hasExplicitApproval || hasChoiceUI
}
