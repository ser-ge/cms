package session

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
)

// Snapshot captures the layout of a tmux session for later restoration.
type Snapshot struct {
	Session string       `json:"session"`
	Windows []SnapWindow `json:"windows"`
	Focus   SnapFocus    `json:"focus"`
}

type SnapWindow struct {
	Index  int        `json:"index"`
	Name   string     `json:"name"`
	Layout string     `json:"layout"`
	Active bool       `json:"active"`
	Panes  []SnapPane `json:"panes"`
}

type SnapPane struct {
	Index      int    `json:"index"`
	WorkingDir string `json:"working_dir"`
}

type SnapFocus struct {
	Window int `json:"window"`
	Pane   int `json:"pane"`
}

// SaveSnapshot serializes the current layout of a tmux session to disk.
func SaveSnapshot(sessionName, repoRoot string) error {
	sess, err := findSession(sessionName)
	if err != nil {
		return err
	}
	return saveSessionSnapshot(sess, repoRoot)
}

// SaveAllSnapshots saves snapshots for every tmux session that lives in a git repo.
// Best-effort: errors for individual sessions are logged, not returned.
func SaveAllSnapshots() {
	sessions, _, err := tmux.FetchState()
	if err != nil {
		debug.Logf("session: save-all: FetchState failed: %v", err)
		return
	}
	for _, sess := range sessions {
		if len(sess.Windows) == 0 || len(sess.Windows[0].Panes) == 0 {
			continue
		}
		dir := sess.Windows[0].Panes[0].WorkingDir
		repoRoot, err := canonicalRepoRoot(dir)
		if err != nil {
			continue // not a git repo, skip silently
		}
		if err := saveSessionSnapshot(sess, repoRoot); err != nil {
			debug.Logf("session: save-all: %s: %v", sess.Name, err)
		}
	}
}

// saveSessionSnapshot saves a snapshot for a single session using pre-fetched state.
func saveSessionSnapshot(sess tmux.Session, repoRoot string) error {
	layouts, err := listWindowLayouts(sess.Name)
	if err != nil {
		return err
	}
	focus, _ := tmux.FetchCurrentTarget()

	var windows []SnapWindow
	for _, win := range sess.Windows {
		sw := SnapWindow{
			Index:  win.Index,
			Name:   win.Name,
			Layout: layouts[win.Index],
			Active: win.Active,
		}
		for _, p := range win.Panes {
			sw.Panes = append(sw.Panes, SnapPane{
				Index:      p.Index,
				WorkingDir: p.WorkingDir,
			})
		}
		windows = append(windows, sw)
	}

	snap := Snapshot{
		Session: sess.Name,
		Windows: windows,
		Focus:   SnapFocus{Window: focus.Window, Pane: focus.Pane},
	}

	path, err := snapshotPath(repoRoot, sess.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// RestoreSnapshot rebuilds a tmux session from a saved snapshot.
// Returns true if a snapshot was found and restored.
func RestoreSnapshot(sessionName, repoRoot string) (bool, error) {
	path, err := snapshotPath(repoRoot, sessionName)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return false, err
	}

	if _, err := tmux.Run("new-session", "-d", "-s", sessionName, "-c", repoRoot); err != nil {
		return false, err
	}

	for i, win := range snap.Windows {
		target := fmt.Sprintf("%s:%d", sessionName, win.Index)
		if i == 0 {
			tmux.Run("rename-window", "-t", target, win.Name)
			if len(win.Panes) > 0 {
				tmux.Run("send-keys", "-t", target, fmt.Sprintf("cd %s", win.Panes[0].WorkingDir), "C-m")
			}
		} else {
			dir := repoRoot
			if len(win.Panes) > 0 {
				dir = win.Panes[0].WorkingDir
			}
			tmux.Run("new-window", "-t", sessionName, "-n", win.Name, "-c", dir)
		}
		// Additional panes beyond the first.
		for pi := 1; pi < len(win.Panes); pi++ {
			tmux.Run("split-window", "-t", target, "-c", win.Panes[pi].WorkingDir)
		}
		if win.Layout != "" {
			tmux.Run("select-layout", "-t", target, win.Layout)
		}
	}

	tmux.Run("select-window", "-t", fmt.Sprintf("%s:%d", sessionName, snap.Focus.Window))
	tmux.Run("select-pane", "-t", fmt.Sprintf("%s:%d.%d", sessionName, snap.Focus.Window, snap.Focus.Pane))
	return true, nil
}

func findSession(name string) (tmux.Session, error) {
	sessions, _, err := tmux.FetchState()
	if err != nil {
		return tmux.Session{}, err
	}
	for _, s := range sessions {
		if s.Name == name {
			return s, nil
		}
	}
	return tmux.Session{}, fmt.Errorf("session %s not found", name)
}

func listWindowLayouts(sessionName string) (map[int]string, error) {
	out, err := tmux.Run("list-windows", "-t", sessionName, "-F", "#{window_index}\t#{window_layout}")
	if err != nil {
		return nil, err
	}
	layouts := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		var idx int
		fmt.Sscanf(parts[0], "%d", &idx)
		layouts[idx] = parts[1]
	}
	return layouts, nil
}

func snapshotPath(repoRoot, sessionName string) (string, error) {
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(repoRoot + "\x00" + sessionName))
	return filepath.Join(base, "snapshots", hex.EncodeToString(sum[:])+".json"), nil
}

// canonicalRepoRoot resolves the canonical repo root from any directory,
// consistent across worktrees. Uses --git-common-dir so that linked worktrees
// and the main worktree (or bare repo) all resolve to the same path.
func canonicalRepoRoot(dir string) (string, error) {
	commonDir, err := git.Cmd(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	return commonDir, nil
}

func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cms"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "cms"), nil
}
