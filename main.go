package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/session"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/tui"
	"github.com/serge/cms/internal/watcher"
	"github.com/serge/cms/internal/worktree"
)

type jumpCandidate struct {
	paneID   string
	activity agent.Activity
}

func main() {
	initDebugLogger()
	cfg := config.Load()
	tui.InitStyles(cfg)

	initial := tui.ScreenFinder
	fk := tui.FinderAll
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "config":
			if len(os.Args) > 2 && os.Args[2] == "init" {
				path, err := config.WriteDefaultConfigFile()
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
			initial = tui.ScreenDashboard
		case "find", "f":
			initial = tui.ScreenFinder
		case "switch", "s":
			initial = tui.ScreenFinder
			fk = tui.FinderSessions
		case "open", "o":
			initial = tui.ScreenFinder
			fk = tui.FinderProjects
		case "queue", "q":
			initial = tui.ScreenQueue
		case "next", "n":
			if err := jumpNext(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "hook":
			if err := hook.RunCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "hook-setup":
			hook.RunSetup()
			return
		case "worktree", "wt":
			if err := worktree.RunCmd(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "refresh":
			var name string
			if len(os.Args) > 2 {
				name = os.Args[2]
			}
			if err := session.RefreshWorktrees(name); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	w := watcher.New()
	w.ApplyConfig(cfg.General)
	m := tui.NewRootModel(initial, fk, cfg, w)
	p := tea.NewProgram(m, tea.WithAltScreen())
	w.Start(p.Send)
	result, err := p.Run()
	w.Stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if rm, ok := result.(tui.RootModel); ok && rm.PostAction() != nil {
		if err := executePostAction(rm.PostAction()); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func executePostAction(a *tui.PostAction) error {
	// Direct pane switch (from dashboard).
	if a.PaneID != "" {
		return session.SwitchToPane(a.PaneID)
	}
	switch a.Kind {
	case tui.KindSession:
		// Fetch fresh state for smart switch priority resolution.
		sessions, pt, err := tmux.FetchState()
		if err != nil {
			return session.Switch(a.SessionName)
		}
		agents := agent.DetectAll(sessions, pt)
		return session.SmartSwitch(a.SessionName, a.Priority, sessions, agents)
	case tui.KindProject:
		return session.OpenProject(a.ProjectPath)
	}
	return nil
}

// jumpNext finds the next agent pane needing attention and switches to it.
// Priority: waiting > idle. Cycles through panes across all sessions,
// starting after the current pane.
func jumpNext() error {
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		return err
	}
	current, _ := tmux.FetchCurrentTarget()
	agents := agent.DetectAll(sessions, pt)

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
		return session.SwitchToPane(paneID)
	}

	return fmt.Errorf("no waiting or idle agent sessions")
}

func selectNextPane(all []jumpCandidate, currentIdx int) string {
	start := currentIdx + 1
	for _, target := range []agent.Activity{agent.ActivityWaitingInput, agent.ActivityCompleted, agent.ActivityIdle} {
		for i := 0; i < len(all); i++ {
			idx := (start + i) % len(all)
			if all[idx].activity == target {
				return all[idx].paneID
			}
		}
	}
	return ""
}
