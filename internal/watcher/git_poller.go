package watcher

import (
	"time"

	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
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

	allDirs := tmux.CollectPaneDirs(sessions)

	gitCache := git.NewCache()
	results := gitCache.DetectAll(allDirs)

	// Suppress no-op sends: compare against cached pane git info.
	w.stateMu.RLock()
	changed := false
	for _, sess := range w.sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				if info, ok := results[pane.WorkingDir]; ok && info != pane.Git {
					changed = true
				}
			}
		}
	}
	w.stateMu.RUnlock()

	if changed {
		w.send(GitUpdateMsg{GitInfo: results})
	}
}
