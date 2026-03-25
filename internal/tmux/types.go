package tmux

import (
	"github.com/serge/cms/internal/git"
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
	Git        git.Info // git context for the pane's working directory
}

// CurrentTarget identifies exactly where the user is right now.
type CurrentTarget struct {
	Session string
	Window  int
	Pane    int
}

// CollectPaneDirs returns all unique pane working directories from a session list.
func CollectPaneDirs(sessions []Session) []string {
	var dirs []string
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				dirs = append(dirs, pane.WorkingDir)
			}
		}
	}
	return dirs
}
