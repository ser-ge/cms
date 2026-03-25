package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NormalizeSessionName makes a name safe for tmux.
// Dots and colons are replaced with underscores (tmux treats them specially).
func NormalizeSessionName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

// SessionExists checks if a tmux session with the given name exists.
func SessionExists(name string) bool {
	_, err := runTmux("has-session", "-t", name)
	return err == nil
}

// CreateSession creates a new detached tmux session with the given name and working directory.
func CreateSession(name, path string) error {
	_, err := runTmux("new-session", "-d", "-s", name, "-c", path)
	return err
}

// insideTmux reports whether we're running inside a tmux session.
func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// attachTmux runs tmux attach-session interactively with the terminal connected.
func attachTmux(args ...string) error {
	cmd, err := tmuxCommand(append([]string{"attach-session"}, args...)...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SwitchSession switches the current tmux client to the named session.
// If running outside tmux, attaches to the session instead.
func SwitchSession(name string) error {
	if insideTmux() {
		_, err := runTmux("switch-client", "-t", name)
		return err
	}
	return attachTmux("-t", name)
}

// SmartSwitchSession switches to a session, targeting the best pane based on
// the priority list. Each entry is checked in order:
//   - "waiting"  → first pane with an agent waiting for input
//   - "idle"     → first pane with an agent idle
//   - "working"  → first pane with an agent working
//   - "default"  → tmux's last-active pane (normal switch)
//
// Falls back to normal switch if no priority matches.
func SmartSwitchSession(name string, priority []string, sessions []Session, agents map[string]AgentStatus) error {
	if len(priority) == 0 || agents == nil {
		return SwitchSession(name)
	}

	// Find the session's panes.
	var panes []Pane
	for _, sess := range sessions {
		if sess.Name == name {
			for _, win := range sess.Windows {
				panes = append(panes, win.Panes...)
			}
			break
		}
	}

	if paneID := selectPriorityPane(panes, priority, agents); paneID != "" {
		return SwitchToPane(paneID)
	}

	// No priority matched — normal switch.
	return SwitchSession(name)
}

func selectPriorityPane(panes []Pane, priority []string, agents map[string]AgentStatus) string {
	for _, p := range priority {
		if p == "default" {
			return ""
		}

		var target Activity
		switch p {
		case "waiting":
			target = ActivityWaitingInput
		case "completed":
			target = ActivityCompleted
		case "idle":
			target = ActivityIdle
		case "working":
			target = ActivityWorking
		default:
			continue
		}

		for _, pane := range panes {
			cs, ok := agents[pane.ID]
			if ok && cs.Running && cs.Activity == target {
				return pane.ID
			}
		}
	}
	return ""
}

// SwitchToPane switches the tmux client to focus a specific pane.
// Uses the pane ID (e.g. %7) which tmux resolves to the right session/window.
// If running outside tmux, attaches to the pane's session instead.
func SwitchToPane(paneID string) error {
	if insideTmux() {
		_, err := runTmux("switch-client", "-t", paneID)
		return err
	}
	return attachTmux("-t", paneID)
}

// KillPane closes a tmux pane.
func KillPane(paneID string) error {
	_, err := runTmux("kill-pane", "-t", paneID)
	return err
}

// MovePane moves a pane to be adjacent to a target pane.
// Works across sessions and windows.
func MovePane(srcPaneID, dstPaneID string) error {
	_, err := runTmux("join-pane", "-s", srcPaneID, "-t", dstPaneID)
	return err
}

// OpenProject opens a project directory as a tmux session.
// If a session for this directory already exists, switches to it.
// Otherwise creates a new session and switches.
// If the repo has linked worktrees, each becomes a tmux window.
func OpenProject(path string) error {
	name := NormalizeSessionName(filepath.Base(path))

	if SessionExists(name) {
		return SwitchSession(name)
	}

	if err := CreateSession(name, path); err != nil {
		return err
	}

	setupWorktreeWindows(name, path)

	return SwitchSession(name)
}

// setupWorktreeWindows creates a tmux window for each linked worktree.
// Works for both bare and normal repos. No-op if the repo has no linked worktrees.
func setupWorktreeWindows(sessionName, repoPath string) {
	wts, err := listWorktrees(repoPath)
	if err != nil || len(wts) <= 1 {
		return
	}

	// Sort: main/master first, then alphabetical.
	sort.SliceStable(wts, func(i, j int) bool {
		iMain := wts[i].Branch == "main" || wts[i].Branch == "master"
		jMain := wts[j].Branch == "main" || wts[j].Branch == "master"
		if iMain != jMain {
			return iMain
		}
		return wts[i].Branch < wts[j].Branch
	})

	for _, wt := range wts {
		if wt.IsBare {
			continue
		}
		windowName := wt.Branch
		if windowName == "" {
			windowName = filepath.Base(wt.Path)
		}
		runTmux("new-window", "-t", sessionName, "-n", windowName, "-c", wt.Path)
	}

	// Kill the initial empty window that was created with the session.
	runTmux("kill-window", "-t", sessionName+":^")
}

// RefreshWorktrees adds missing worktree windows to existing tmux sessions.
// If sessionFilter is non-empty, only that session is refreshed.
func RefreshWorktrees(sessionFilter string) error {
	sessions, _, err := FetchState()
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sessionFilter != "" && sess.Name != sessionFilter {
			continue
		}
		if len(sess.Windows) == 0 || len(sess.Windows[0].Panes) == 0 {
			continue
		}
		repoPath := sess.Windows[0].Panes[0].WorkingDir
		wts, err := listWorktrees(repoPath)
		if err != nil || len(wts) <= 1 {
			continue
		}
		existing := map[string]bool{}
		for _, win := range sess.Windows {
			existing[win.Name] = true
		}
		for _, wt := range wts {
			if wt.IsBare {
				continue
			}
			name := wt.Branch
			if name == "" {
				name = filepath.Base(wt.Path)
			}
			if existing[name] {
				continue
			}
			runTmux("new-window", "-t", sess.Name, "-n", name, "-c", wt.Path)
		}
	}
	return nil
}
