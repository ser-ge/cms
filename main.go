package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	// Persistent activity state for hysteresis across ticks.
	// Once working is detected, require 10 consecutive idle readings (~5s) to switch back.
	prevActivity := map[string]Activity{}
	idleCount := map[string]int{}
	const idleThreshold = 10

	for {
		sessions, pt, err := FetchState()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		current, err := FetchCurrentTarget()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not get current target: %v\n", err)
		}

		claudeResults := detectAllClaude(sessions, pt)

		// Apply hysteresis: smooth working→idle transitions.
		for id, cs := range claudeResults {
			if cs.Activity == ActivityWorking {
				idleCount[id] = 0
			} else if prevActivity[id] == ActivityWorking && cs.Activity == ActivityIdle {
				idleCount[id]++
				if idleCount[id] < idleThreshold {
					cs.Activity = ActivityWorking // hold working
					claudeResults[id] = cs
				}
			}
			prevActivity[id] = claudeResults[id].Activity
		}

		// Clear screen and redraw.
		fmt.Print("\033[2J\033[H")

		for _, sess := range sessions {
			isCurrent := sess.Name == current.Session
			label := ""
			if isCurrent {
				label = " ← you are here"
			} else if sess.Attached {
				label = " (attached)"
			}
			fmt.Printf("Session: %s%s\n", sess.Name, label)

			for _, win := range sess.Windows {
				active := ""
				if win.Active {
					active = "*"
				}
				fmt.Printf("  Window %d: %s%s\n", win.Index, win.Name, active)

				for _, pane := range win.Panes {
					marker := " "
					if isCurrent && win.Index == current.Window && pane.Index == current.Pane {
						marker = "▶"
					} else if pane.Active {
						marker = "▸"
					}

					parts := []string{}

					if dir := shortenHome(pane.WorkingDir); dir != "" {
						parts = append(parts, dir)
					}

					if pane.Git.IsRepo {
						g := pane.Git.Branch
						if pane.Git.Dirty {
							g += "*"
						}
						if pane.Git.Ahead > 0 {
							g += fmt.Sprintf("↑%d", pane.Git.Ahead)
						}
						if pane.Git.Behind > 0 {
							g += fmt.Sprintf("↓%d", pane.Git.Behind)
						}
						parts = append(parts, g)
					}

					cmd := pane.Command
					if cmd != "fish" && cmd != "bash" && cmd != "zsh" {
						parts = append(parts, cmd)
					}

					if claude, ok := claudeResults[pane.ID]; ok && claude.Running {
						c := claude.Activity.Icon()
						if claude.ContextPct > 0 {
							c += fmt.Sprintf(" %d%%", claude.ContextPct)
						}
						if claude.Mode != "" {
							c += " " + claude.Mode
						}
						c += " " + claude.Activity.String()
						parts = append(parts, c)
					}

					fmt.Printf("   %s %d │ %s\n", marker, pane.Index, strings.Join(parts, " │ "))
				}
			}
			fmt.Println()
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// detectAllClaude runs Claude detection for all panes concurrently.
func detectAllClaude(sessions []Session, pt procTable) map[string]ClaudeStatus {
	var mu sync.Mutex
	results := map[string]ClaudeStatus{}
	var wg sync.WaitGroup

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				wg.Add(1)
				go func(p Pane) {
					defer wg.Done()
					status := DetectClaude(p, pt)
					if status.Running {
						mu.Lock()
						results[p.ID] = status
						mu.Unlock()
					}
				}(pane)
			}
		}
	}
	wg.Wait()
	return results
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return filepath.Join("~", path[len(home):])
	}
	return path
}
