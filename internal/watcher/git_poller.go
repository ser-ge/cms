package watcher

import (
	"time"

	"github.com/serge/cms/internal/git"
)

// runGitPoll periodically re-checks git status for all pane working dirs.
func (w *Watcher) runGitPoll() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.pollGit()
		}
	}
}

func (w *Watcher) pollGit() {
	w.stateMu.RLock()
	sessions := w.sessions
	w.stateMu.RUnlock()

	if len(sessions) == 0 {
		return
	}

	var allDirs []string
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				allDirs = append(allDirs, pane.WorkingDir)
			}
		}
	}

	gitCache := git.NewCache()
	results := gitCache.DetectAll(allDirs)
	w.send(GitUpdateMsg{GitInfo: results})
}
