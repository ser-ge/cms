package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type jumpCandidate struct {
	paneID   string
	activity Activity
}

func main() {
	initDebugLogger()
	cfg := LoadConfig()
	initStyles(cfg)

	initial := screenFinder
	fk := finderAll
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "config":
			if len(os.Args) > 2 && os.Args[2] == "init" {
				path, err := WriteDefaultConfigFile()
				if err != nil {
					if err == os.ErrExist {
						fmt.Fprintf(os.Stderr, "error: config already exists at %s\n", path)
					} else {
						fmt.Fprintf(os.Stderr, "error: %v\n", err)
					}
					os.Exit(1)
				}
				fmt.Println(path)
				return
			}
			fmt.Fprintln(os.Stderr, "error: usage: cms config init")
			os.Exit(1)
		case "dash", "d", "dashboard":
			initial = screenDashboard
		case "find", "f":
			initial = screenFinder
		case "switch", "s":
			initial = screenFinder
			fk = finderSessions
		case "open", "o":
			initial = screenFinder
			fk = finderProjects
		case "queue", "q":
			initial = screenQueue
		case "next", "n":
			if err := jumpNext(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "hook":
			if err := runHookCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "hook-setup":
			runHookSetup()
			return
		case "refresh":
			var name string
			if len(os.Args) > 2 {
				name = os.Args[2]
			}
			if err := RefreshWorktrees(name); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	watcher := NewWatcher()
	watcher.ApplyConfig(cfg.General)
	m := newRootModel(initial, fk, cfg, watcher)
	p := tea.NewProgram(m, tea.WithAltScreen())
	watcher.Start(p.Send)
	result, err := p.Run()
	watcher.Stop()
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
		return SmartSwitchSession(a.sessionName, a.priority, a.sessions, a.agents)
	case kindProject:
		return OpenProject(a.projectPath)
	}
	return nil
}

// jumpNext finds the next agent pane needing attention and switches to it.
// Priority: waiting > idle. Cycles through panes across all sessions,
// starting after the current pane.
func jumpNext() error {
	sessions, pt, err := FetchState()
	if err != nil {
		return err
	}
	current, _ := FetchCurrentTarget()
	agents := detectAllAgents(sessions, pt)

	// Flatten all panes with agent status.
	var all []jumpCandidate
	currentIdx := -1

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				cs, ok := agents[pane.ID]
				if !ok || !cs.Running {
					continue
				}
				if sess.Name == current.Session && win.Index == current.Window && pane.Index == current.Pane {
					currentIdx = len(all)
				}
				all = append(all, jumpCandidate{paneID: pane.ID, activity: cs.Activity})
			}
		}
	}

	if len(all) == 0 {
		return fmt.Errorf("no agent sessions found")
	}

	if paneID := selectNextPane(all, currentIdx); paneID != "" {
		return SwitchToPane(paneID)
	}

	return fmt.Errorf("no waiting or idle agent sessions")
}

func selectNextPane(all []jumpCandidate, currentIdx int) string {
	start := currentIdx + 1
	for _, target := range []Activity{ActivityWaitingInput, ActivityCompleted, ActivityIdle} {
		for i := 0; i < len(all); i++ {
			idx := (start + i) % len(all)
			if all[idx].activity == target {
				return all[idx].paneID
			}
		}
	}
	return ""
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
