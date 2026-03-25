// Package mark manages named pane bookmarks (vim-style marks).
// Marks are stored as JSON at ~/.config/cms/marks.json.
package mark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/tmux"
)

// Mark is a named bookmark pointing to a tmux pane.
type Mark struct {
	PaneID  string `json:"pane_id"`
	Session string `json:"session"`
	Window  string `json:"window"`
}

// Path returns the marks file location.
func Path() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cms", "marks.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cms", "marks.json")
}

// Load reads all marks from disk. Returns an empty map if the file doesn't exist.
func Load() (map[string]Mark, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Mark{}, nil
		}
		return nil, err
	}
	var marks map[string]Mark
	if err := json.Unmarshal(data, &marks); err != nil {
		return nil, err
	}
	return marks, nil
}

// Save writes all marks to disk atomically.
func Save(marks map[string]Mark) error {
	data, err := json.MarshalIndent(marks, "", "  ")
	if err != nil {
		return err
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Set adds or updates a mark.
func Set(label string, m Mark) error {
	marks, err := Load()
	if err != nil {
		return err
	}
	marks[label] = m
	return Save(marks)
}

// Remove deletes a mark by label.
func Remove(label string) error {
	marks, err := Load()
	if err != nil {
		return err
	}
	delete(marks, label)
	return Save(marks)
}

// Resolve looks up a mark and checks whether its pane still exists
// in the current tmux state.
func Resolve(label string, sessions []tmux.Session) (Mark, bool, error) {
	marks, err := Load()
	if err != nil {
		return Mark{}, false, err
	}
	m, ok := marks[label]
	if !ok {
		return Mark{}, false, nil
	}
	alive := paneExists(m.PaneID, sessions)
	return m, alive, nil
}

// Alive returns a filtered map containing only marks whose panes exist.
func Alive(marks map[string]Mark, sessions []tmux.Session) map[string]Mark {
	out := make(map[string]Mark, len(marks))
	for label, m := range marks {
		if paneExists(m.PaneID, sessions) {
			out[label] = m
		}
	}
	return out
}

// IsAlive checks whether a single mark's pane exists in the session state.
func IsAlive(m Mark, sessions []tmux.Session) bool {
	return paneExists(m.PaneID, sessions)
}

func paneExists(paneID string, sessions []tmux.Session) bool {
	// Normalize: tmux pane IDs start with %, but sometimes stored without.
	id := strings.TrimPrefix(paneID, "%")
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				if strings.TrimPrefix(pane.ID, "%") == id {
					return true
				}
			}
		}
	}
	return false
}
