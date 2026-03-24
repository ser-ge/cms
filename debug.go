//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	dbgSpinnerRe     = regexp.MustCompile(`^[✢✶·⏳⏺●] \S+…`)
	dbgToolRunningRe = regexp.MustCompile(`Running…`)
	dbgChoiceNavRe   = regexp.MustCompile(`Enter to select.*↑/↓ to navigate`)
)

func main() {
	paneID := "%2" // default
	if len(os.Args) > 1 {
		paneID = os.Args[1]
	}

	for {
		content, err := capturePane(paneID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture error: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		lines := strings.Split(content, "\n")

		hasInputField := false
		hasSpinner := false
		hasPermissionPrompt := false
		promptLine := -1

		// First pass: find prompt line
		for i, line := range lines {
			if strings.Contains(line, "❯") && i > 0 && strings.Contains(lines[i-1], "─") {
				hasInputField = true
				promptLine = i
			}
		}

		// Check bottom 8 lines only for permission/choice UI
		for i := max(0, len(lines)-8); i < len(lines); i++ {
			line := lines[i]
			if strings.Contains(line, "[y/n]") || strings.Contains(line, "[Y/n]") {
				hasPermissionPrompt = true
			}
			if dbgChoiceNavRe.MatchString(line) {
				hasPermissionPrompt = true
			}
		}

		// Second pass: look for spinner above prompt
		if promptLine > 0 {
			start := promptLine - 10
			if start < 0 {
				start = 0
			}
			for i := start; i < promptLine; i++ {
				trimmed := strings.TrimSpace(lines[i])
				if dbgSpinnerRe.MatchString(trimmed) || dbgToolRunningRe.MatchString(trimmed) {
					hasSpinner = true
					break
				}
			}
		}

		fmt.Printf("\033[2J\033[H") // clear screen
		fmt.Printf("=== Debug %s pane=%s ===\n", time.Now().Format("15:04:05"), paneID)
		fmt.Printf("Lines: %d  promptLine: %d\n\n", len(lines), promptLine)

		// Show last 20 lines with annotations
		start := len(lines) - 20
		if start < 0 {
			start = 0
		}
		for i := start; i < len(lines); i++ {
			line := lines[i]
			markers := []string{}

			if strings.Contains(line, "─") {
				markers = append(markers, "DASH")
			}
			if strings.Contains(line, "❯") {
				markers = append(markers, "PROMPT")
			}
			trimmed := strings.TrimSpace(line)
			if dbgSpinnerRe.MatchString(trimmed) {
				markers = append(markers, "SPINNER")
			}
			if dbgToolRunningRe.MatchString(trimmed) {
				markers = append(markers, "TOOL_RUNNING")
			}
			if dbgChoiceNavRe.MatchString(line) {
				markers = append(markers, "CHOICE_NAV")
			}
			if i == promptLine {
				markers = append(markers, "← PROMPT_LINE")
			}

			annotation := ""
			if len(markers) > 0 {
				annotation = "  « " + strings.Join(markers, ", ")
			}

			display := line
			if len(display) > 80 {
				display = display[:80] + "…"
			}
			fmt.Printf("  %3d │ %s%s\n", i, display, annotation)
		}

		fmt.Println()
		fmt.Printf("inputField=%v  spinner=%v  permission=%v\n", hasInputField, hasSpinner, hasPermissionPrompt)

		activity := "unknown"
		if hasPermissionPrompt {
			activity = "waiting"
		} else if hasInputField {
			if hasSpinner {
				activity = "working"
			} else {
				activity = "idle"
			}
		}
		fmt.Printf(">>> ACTIVITY: %s\n", activity)

		time.Sleep(2 * time.Second)
	}
}

func capturePane(paneID string) (string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-t", paneID, "-p", "-J")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
