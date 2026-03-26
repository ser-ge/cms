package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
)

// NormalizeName makes a name safe for tmux.
// Dots and colons are replaced with underscores (tmux treats them specially).
func NormalizeName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

// Exists checks if a tmux session with the given name exists.
func Exists(name string) bool {
	_, err := tmux.Run("has-session", "-t", name)
	return err == nil
}

// Create creates a new detached tmux session with the given name and working directory.
func Create(name, path string) error {
	_, err := tmux.Run("new-session", "-d", "-s", name, "-c", path)
	return err
}

// InsideTmux reports whether we're running inside a tmux session.
func InsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// Attach runs tmux attach-session interactively with the terminal connected.
func Attach(args ...string) error {
	cmd, err := tmux.Command(append([]string{"attach-session"}, args...)...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Switch switches the current tmux client to the named session.
// If running outside tmux, attaches to the session instead.
func Switch(name string) error {
	if InsideTmux() {
		_, err := tmux.Run("switch-client", "-t", name)
		return err
	}
	return Attach("-t", name)
}

// SmartSwitch switches to a session, targeting the best pane based on
// the priority list. Each entry is checked in order:
//   - "waiting"  → first pane with an agent waiting for input
//   - "idle"     → first pane with an agent idle
//   - "working"  → first pane with an agent working
//   - "default"  → tmux's last-active pane (normal switch)
//
// Falls back to normal switch if no priority matches.
func SmartSwitch(name string, priority []string, sessions []tmux.Session, agents map[string]agent.AgentStatus) error {
	if len(priority) == 0 || agents == nil {
		return Switch(name)
	}

	// Find the session's panes.
	var panes []tmux.Pane
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
	return Switch(name)
}

func selectPriorityPane(panes []tmux.Pane, priority []string, agents map[string]agent.AgentStatus) string {
	for _, p := range priority {
		if p == "default" {
			return ""
		}

		var target agent.Activity
		switch p {
		case "waiting":
			target = agent.ActivityWaitingInput
		case "completed":
			target = agent.ActivityCompleted
		case "idle":
			target = agent.ActivityIdle
		case "working":
			target = agent.ActivityWorking
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
	if InsideTmux() {
		_, err := tmux.Run("switch-client", "-t", paneID)
		return err
	}
	return Attach("-t", paneID)
}

// KillPane closes a tmux pane.
func Kill(name string) error {
	_, err := tmux.Run("kill-session", "-t", name)
	return err
}

func KillPane(paneID string) error {
	_, err := tmux.Run("kill-pane", "-t", paneID)
	return err
}

// MovePane moves a pane to be adjacent to a target pane.
// Works across sessions and windows.
func MovePane(srcPaneID, dstPaneID string) error {
	_, err := tmux.Run("join-pane", "-s", srcPaneID, "-t", dstPaneID)
	return err
}

// OpenProject opens a project directory as a tmux session.
// If a session for this directory already exists, switches to it.
// Otherwise tries, in order: template bootstrap, snapshot restore, plain create.
// If the repo has linked worktrees, each becomes a tmux window.
func OpenProject(path string) error {
	projCfg := config.LoadProjectConfig(path)
	name := NormalizeName(filepath.Base(path))
	if projCfg.Session.Name != "" {
		name = NormalizeName(projCfg.Session.Name)
	}

	if Exists(name) {
		return Switch(name)
	}

	// 1. Template bootstrap (tmux source-file).
	if projCfg.Session.Bootstrap != "" {
		if err := OpenProjectFromTemplate(name, path, projCfg.Session); err != nil {
			debug.Logf("session: template bootstrap failed: %v", err)
			// Fall through to other methods.
		} else {
			return Switch(name)
		}
	}

	// 2. Restore from saved snapshot.
	restored, err := RestoreSnapshot(name, path)
	if err != nil {
		debug.Logf("session: snapshot restore failed: %v", err)
	}
	if restored {
		if shouldRestore(projCfg.Session) {
			resumeClaudePanes(name, path, projCfg.Session.Claude) // best-effort
		}
		return Switch(name)
	}

	// 3. Plain create with worktree windows.
	if err := Create(name, path); err != nil {
		return err
	}
	setupWorktreeWindows(name, path)

	if shouldRestore(projCfg.Session) {
		resumeClaudePanes(name, path, projCfg.Session.Claude) // best-effort
	}

	return Switch(name)
}

// setupWorktreeWindows creates a tmux window for each linked worktree.
// Works for both bare and normal repos. No-op if the repo has no linked worktrees.
func setupWorktreeWindows(sessionName, repoPath string) {
	wts, err := git.ListWorktrees(repoPath)
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
		tmux.Run("new-window", "-t", sessionName, "-n", windowName, "-c", wt.Path)
	}

	// Kill the initial empty window that was created with the session.
	tmux.Run("kill-window", "-t", sessionName+":^")
}

// RefreshWorktrees adds missing worktree windows to existing tmux sessions.
// If sessionFilter is non-empty, only that session is refreshed.
func RefreshWorktrees(sessionFilter string) error {
	sessions, _, err := tmux.FetchState()
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
		wts, err := git.ListWorktrees(repoPath)
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
			tmux.Run("new-window", "-t", sess.Name, "-n", name, "-c", wt.Path)
		}
	}
	return nil
}
