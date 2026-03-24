package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	cmd := exec.Command("tmux", append([]string{"attach-session"}, args...)...)
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
//   - "waiting"  → first pane with Claude waiting for input
//   - "idle"     → first pane with Claude idle
//   - "working"  → first pane with Claude working
//   - "default"  → tmux's last-active pane (normal switch)
//
// Falls back to normal switch if no priority matches.
func SmartSwitchSession(name string, priority []string, sessions []Session, claude map[string]ClaudeStatus) error {
	if len(priority) == 0 || claude == nil {
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

	// Walk the priority list and find the first matching pane.
	for _, p := range priority {
		if p == "default" {
			return SwitchSession(name)
		}

		var target Activity
		switch p {
		case "waiting":
			target = ActivityWaitingInput
		case "idle":
			target = ActivityIdle
		case "working":
			target = ActivityWorking
		default:
			continue
		}

		for _, pane := range panes {
			cs, ok := claude[pane.ID]
			if ok && cs.Running && cs.Activity == target {
				return SwitchToPane(pane.ID)
			}
		}
	}

	// No priority matched — normal switch.
	return SwitchSession(name)
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
// For bare repos, each worktree becomes a window in the session.
func OpenProject(path string) error {
	name := NormalizeSessionName(filepath.Base(path))

	if SessionExists(name) {
		return SwitchSession(name)
	}

	if err := CreateSession(name, path); err != nil {
		return err
	}

	// For bare repos, create a window per worktree and kill the initial empty window.
	if isBareRepo(path) {
		setupBareRepoWindows(name, path)
	}

	return SwitchSession(name)
}

// isBareRepo checks if a path is a bare git repository.
func isBareRepo(path string) bool {
	// Bare repos have HEAD at the top level and no .git directory.
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return false
	}
	_, err := os.Stat(filepath.Join(path, "HEAD"))
	return err == nil
}

// setupBareRepoWindows creates a tmux window for each worktree in a bare repo.
// Mirrors tms behavior: each worktree branch gets its own window.
func setupBareRepoWindows(sessionName, bareRepoPath string) {
	worktreesDir := filepath.Join(bareRepoPath, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return
	}

	createdWindows := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wtName := entry.Name()
		// Read the worktree's gitdir file to find its checkout path.
		gitdirFile := filepath.Join(worktreesDir, wtName, "gitdir")
		data, err := os.ReadFile(gitdirFile)
		if err != nil {
			continue
		}
		// gitdir points to the .git file inside the worktree checkout.
		// The checkout path is its parent.
		gitFilePath := strings.TrimSpace(string(data))
		checkoutPath := filepath.Dir(gitFilePath)
		if _, err := os.Stat(checkoutPath); err != nil {
			continue
		}

		runTmux("new-window", "-t", sessionName, "-n", wtName, "-c", checkoutPath)
		createdWindows = true
	}

	if createdWindows {
		// Kill the initial empty window that was created with the session.
		runTmux("kill-window", "-t", sessionName+":^")
	}
}
