package tmux

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/proc"
)

// FetchState queries tmux for the full session/window/pane hierarchy.
// It uses a single `list-panes -a` call to get everything at once,
// which is more efficient than calling list-sessions + list-windows + list-panes separately.
func FetchState() ([]Session, proc.Table, error) {
	const format = "#{session_name}\t#{session_id}\t#{session_attached}\t" +
		"#{window_index}\t#{window_name}\t#{window_active}\t" +
		"#{pane_id}\t#{pane_index}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_active}"

	output, err := Run("list-panes", "-a", "-F", format)
	if err != nil {
		return nil, proc.Table{}, err
	}

	// Build a process table once — reused for command resolution and agent detection.
	pt := proc.BuildTable()

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
		paneCmd = proc.ResolveCommand(pt, panePID, paneCmd)

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
	gitCache := git.NewCache()
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

// FetchCurrentTarget returns the session/window/pane the user is currently in.
func FetchCurrentTarget() (CurrentTarget, error) {
	out, err := Run("display-message", "-p", "#{session_name}\t#{window_index}\t#{pane_index}")
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
	out, err := Run("display-message", "-p", "#{client_last_session}")
	if err != nil {
		return ""
	}
	return out
}
