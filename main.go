package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg := LoadConfig()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "find", "f", "switch", "s", "open", "o":
			m := newRootModel(screenFinder, cfg)
			p := tea.NewProgram(m, tea.WithAltScreen())
			result, err := p.Run()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			// Execute post-exit action (e.g. tmux attach when outside tmux).
			if rm, ok := result.(rootModel); ok && rm.postAction != nil {
				if err := executePostAction(rm.postAction); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					os.Exit(1)
				}
			}
			return
		case "next", "n":
			if err := jumpNext(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	m := newRootModel(screenDashboard, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if rm, ok := result.(rootModel); ok && rm.postAction != nil {
		if err := executePostAction(rm.postAction); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func executePostAction(a *postAction) error {
	// Direct pane switch (from dashboard).
	if a.paneID != "" {
		return SwitchToPane(a.paneID)
	}
	switch a.kind {
	case kindSession:
		return SmartSwitchSession(a.sessionName, a.priority, a.sessions, a.claude)
	case kindProject:
		return OpenProject(a.projectPath)
	}
	return nil
}

// jumpNext finds the next Claude pane needing attention and switches to it.
// Priority: waiting > idle. Cycles through panes across all sessions,
// starting after the current pane.
func jumpNext() error {
	sessions, pt, err := FetchState()
	if err != nil {
		return err
	}
	current, _ := FetchCurrentTarget()
	claude := detectAllClaude(sessions, pt)

	// Flatten all panes with Claude status.
	type candidate struct {
		paneID   string
		activity Activity
	}
	var all []candidate
	currentIdx := -1

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				cs, ok := claude[pane.ID]
				if !ok || !cs.Running {
					continue
				}
				if sess.Name == current.Session && win.Index == current.Window && pane.Index == current.Pane {
					currentIdx = len(all)
				}
				all = append(all, candidate{paneID: pane.ID, activity: cs.Activity})
			}
		}
	}

	if len(all) == 0 {
		return fmt.Errorf("no claude sessions found")
	}

	// Find next waiting pane (cycling from current position).
	start := currentIdx + 1
	for _, target := range []Activity{ActivityWaitingInput, ActivityIdle} {
		for i := 0; i < len(all); i++ {
			idx := (start + i) % len(all)
			if all[idx].activity == target {
				return SwitchToPane(all[idx].paneID)
			}
		}
	}

	return fmt.Errorf("no waiting or idle claude sessions")
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
