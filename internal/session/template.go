package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/resume"
	"github.com/serge/cms/internal/tmux"
)

// OpenProjectFromTemplate bootstraps a new tmux session using a repo-local
// tmux config file, then optionally resumes Claude Code in marked panes.
func OpenProjectFromTemplate(sessionName, repoRoot string, cfg config.SessionConfig) error {
	bootstrap := cfg.Bootstrap
	if !filepath.IsAbs(bootstrap) {
		bootstrap = filepath.Join(repoRoot, bootstrap)
	}
	if _, err := os.Stat(bootstrap); err != nil {
		return fmt.Errorf("session bootstrap %q: %w", bootstrap, err)
	}

	args := []string{
		"new-session", "-d", "-s", sessionName, "-c", repoRoot,
		";",
		"source-file", bootstrap,
		";",
		"select-window", "-t", sessionName + ":^",
	}
	if _, err := tmux.Run(args...); err != nil {
		return err
	}

	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" || mode == "template_then_restore" {
		if err := resumeClaudePanes(sessionName, repoRoot, cfg.Claude); err != nil {
			return err
		}
	}
	return nil
}

// shouldRestore reports whether the session mode allows restoring Claude sessions.
func shouldRestore(cfg config.SessionConfig) bool {
	switch strings.TrimSpace(cfg.Mode) {
	case "", "template_then_restore", "restore_only":
		return true
	default:
		return false
	}
}

// resumeClaudePanes sends claude --resume commands to panes that have
// persisted Claude session IDs.
func resumeClaudePanes(sessionName, repoRoot string, cfg config.SessionClaudeConfig) error {
	if !cfg.Resume {
		return nil
	}

	state, err := resume.Load(repoRoot)
	if err != nil {
		return err
	}
	if len(state.Claude) == 0 {
		return nil
	}

	panes, err := resume.ListSessionPanes(sessionName)
	if err != nil {
		return err
	}
	cmdTemplate := resume.DefaultClaudeCommand(cfg.Command)

	for _, pane := range panes {
		if cfg.OnlyInMarkedPanes && !pane.ResumeClaude {
			continue
		}
		if pane.Marker == "" {
			continue
		}
		sessionID := state.Claude[pane.Marker]
		if sessionID == "" {
			continue
		}
		if cfg.OnlyIfPaneEmpty && !proc.IsShellCommand(pane.Command) {
			continue
		}
		cmd := strings.ReplaceAll(cmdTemplate, "{session_id}", sessionID)
		if _, err := tmux.Run("send-keys", "-t", pane.ID, cmd, "C-m"); err != nil {
			return err
		}
	}
	return nil
}
