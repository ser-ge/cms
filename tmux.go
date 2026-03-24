package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Session represents a tmux session.
type Session struct {
	Name     string
	ID       string
	Attached bool
	Windows  []Window
}

// Window represents a tmux window within a session.
type Window struct {
	Index  int
	Name   string
	Active bool
	Panes  []Pane
}

// Pane represents a single tmux pane.
type Pane struct {
	ID         string // unique pane ID like %0, %1
	Index      int
	PID        int
	Command    string // current foreground command
	WorkingDir string
	Active     bool
	Git        GitInfo // git context for the pane's working directory
}

// CurrentTarget identifies exactly where the user is right now.
type CurrentTarget struct {
	Session string
	Window  int
	Pane    int
}

// FetchCurrentTarget returns the session/window/pane the user is currently in.
func FetchCurrentTarget() (CurrentTarget, error) {
	out, err := runTmux("display-message", "-p", "#{session_name}\t#{window_index}\t#{pane_index}")
	if err != nil {
		return CurrentTarget{}, err
	}
	fields := strings.Split(out, "\t")
	if len(fields) != 3 {
		return CurrentTarget{}, fmt.Errorf("unexpected display-message output: %q", out)
	}
	winIdx, _ := strconv.Atoi(fields[1])
	paneIdx, _ := strconv.Atoi(fields[2])
	return CurrentTarget{
		Session: fields[0],
		Window:  winIdx,
		Pane:    paneIdx,
	}, nil
}

// FetchLastSession returns the name of the session the user was in before the current one.
// Returns "" if there is no previous session.
func FetchLastSession() string {
	out, err := runTmux("display-message", "-p", "#{client_last_session}")
	if err != nil {
		return ""
	}
	return out
}

// runTmux executes a tmux command and returns its trimmed stdout.
// This is a helper so we don't repeat exec.Command boilerplate everywhere.
func runTmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// FetchState queries tmux for the full session/window/pane hierarchy.
// It uses a single `list-panes -a` call to get everything at once,
// which is more efficient than calling list-sessions + list-windows + list-panes separately.
func FetchState() ([]Session, procTable, error) {
	const format = "#{session_name}\t#{session_id}\t#{session_attached}\t" +
		"#{window_index}\t#{window_name}\t#{window_active}\t" +
		"#{pane_id}\t#{pane_index}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_active}"

	output, err := runTmux("list-panes", "-a", "-F", format)
	if err != nil {
		return nil, procTable{}, err
	}

	// Build a process table once — reused for command resolution and Claude detection.
	pt := buildProcTable()

	type sessionKey = string
	type windowKey = string

	sessions := map[sessionKey]*Session{}
	windows := map[windowKey]*Window{}
	sessionOrder := []string{}
	windowOrder := map[sessionKey][]string{}

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 12 {
			continue
		}

		sessName := fields[0]
		sessID := fields[1]
		sessAttached := fields[2] != "0"
		winIdx, _ := strconv.Atoi(fields[3])
		winName := fields[4]
		winActive := fields[5] == "1"
		paneID := fields[6]
		paneIdx, _ := strconv.Atoi(fields[7])
		panePID, _ := strconv.Atoi(fields[8])
		paneCmd := fields[9]
		paneDir := fields[10]
		paneActive := fields[11] == "1"

		// Resolve the real command name. tmux's pane_current_command can be
		// wrong (e.g. neovim reports its version string "2.1.81" as argv[0]).
		// We find the foreground child of the shell and use its real comm.
		paneCmd = resolveCommand(pt, panePID, paneCmd)

		if _, ok := sessions[sessName]; !ok {
			sessions[sessName] = &Session{
				Name:     sessName,
				ID:       sessID,
				Attached: sessAttached,
			}
			sessionOrder = append(sessionOrder, sessName)
		}

		wKey := fmt.Sprintf("%s:%d", sessName, winIdx)
		if _, ok := windows[wKey]; !ok {
			windows[wKey] = &Window{
				Index:  winIdx,
				Name:   winName,
				Active: winActive,
			}
			windowOrder[sessName] = append(windowOrder[sessName], wKey)
		}

		windows[wKey].Panes = append(windows[wKey].Panes, Pane{
			ID:         paneID,
			Index:      paneIdx,
			PID:        panePID,
			Command:    paneCmd,
			WorkingDir: paneDir,
			Active:     paneActive,
		})
	}

	// Collect all unique working dirs and resolve git info concurrently.
	var allDirs []string
	for _, wKeys := range windowOrder {
		for _, wKey := range wKeys {
			for _, p := range windows[wKey].Panes {
				allDirs = append(allDirs, p.WorkingDir)
			}
		}
	}
	gitCache := NewGitCache()
	gitResults := gitCache.DetectAll(allDirs)

	// Assign git info back to panes.
	for _, wKeys := range windowOrder {
		for _, wKey := range wKeys {
			for i := range windows[wKey].Panes {
				if info, ok := gitResults[windows[wKey].Panes[i].WorkingDir]; ok {
					windows[wKey].Panes[i].Git = info
				}
			}
		}
	}

	// Assemble the hierarchy.
	result := make([]Session, 0, len(sessionOrder))
	for _, sName := range sessionOrder {
		sess := sessions[sName]
		for _, wKey := range windowOrder[sName] {
			sess.Windows = append(sess.Windows, *windows[wKey])
		}
		result = append(result, *sess)
	}

	return result, pt, nil
}

// procEntry is a single row from `ps`.
type procEntry struct {
	pid  int
	ppid int
	comm string // real binary name from the kernel, not argv[0]
	args string // full command line (argv)
}

// procTable maps PID → procEntry and tracks parent→children.
type procTable struct {
	procs    map[int]procEntry
	children map[int][]int
}

// buildProcTable runs `ps` once and builds a lookup table.
func buildProcTable() procTable {
	pt := procTable{
		procs:    map[int]procEntry{},
		children: map[int][]int{},
	}

	// Use a custom delimiter to reliably split comm from args.
	// Format: "pid ppid comm args..."
	// comm is a single word (kernel name), args is everything after.
	cmd := exec.Command("ps", "-eo", "pid,ppid,comm,args")
	out, err := cmd.Output()
	if err != nil {
		return pt
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		comm := fields[2]
		args := strings.Join(fields[3:], " ")
		// Strip leading dash from login shells (e.g. "-fish" → "fish")
		comm = strings.TrimLeft(comm, "-")
		// Take basename in case it's a full path
		if idx := strings.LastIndex(comm, "/"); idx >= 0 {
			comm = comm[idx+1:]
		}

		pt.procs[pid] = procEntry{pid: pid, ppid: ppid, comm: comm, args: args}
		pt.children[ppid] = append(pt.children[ppid], pid)
	}

	return pt
}

// resolveCommand figures out the real command running in a pane.
// The pane PID is typically a shell. We find its first child (the foreground job)
// and return that child's real binary name from ps.
// Falls back to tmux's pane_current_command if we can't resolve.
func resolveCommand(pt procTable, panePID int, tmuxCmd string) string {
	// If the pane process itself isn't a shell, just use its real comm.
	entry, ok := pt.procs[panePID]
	if !ok {
		return tmuxCmd
	}

	// Is the pane PID a shell?
	if !isShell(entry.comm) {
		return entry.comm
	}

	// Find the direct child — that's the foreground job.
	kids := pt.children[panePID]
	if len(kids) == 0 {
		return entry.comm // just a shell, no foreground job
	}

	// Use the first child's real binary name.
	if child, ok := pt.procs[kids[0]]; ok {
		return child.comm
	}

	return tmuxCmd
}

func isShell(comm string) bool {
	switch comm {
	case "fish", "bash", "zsh", "sh", "dash", "tcsh", "ksh":
		return true
	}
	return false
}
