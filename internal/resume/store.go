package resume

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/debug"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
)

const (
	tmuxPaneIDOption       = "@cms_pane_id"
	tmuxClaudeResumeOption = "@cms_claude_resume"
	defaultClaudeResumeCmd = "claude --resume {session_id}"
)

// State holds persisted Claude session IDs keyed by pane marker.
type State struct {
	RepoRoot string            `json:"repo_root"`
	Claude   map[string]string `json:"claude"` // marker -> session ID
}

// PaneMeta holds metadata read from tmux pane user-options.
type PaneMeta struct {
	ID           string
	Marker       string
	WorkingDir   string
	Command      string
	ResumeClaude bool
}

// SaveClaudeSession persists a Claude session ID for later resume.
// The pane must have @cms_pane_id and @cms_claude_resume set.
func SaveClaudeSession(paneID, sessionID string) error {
	if paneID == "" || sessionID == "" {
		return nil
	}

	meta, ok, err := FetchPaneMeta(paneID)
	if err != nil || !ok || !meta.ResumeClaude || meta.Marker == "" {
		return err
	}

	repoRoot, err := repoRootForDir(meta.WorkingDir)
	if err != nil || repoRoot == "" {
		return err
	}

	state, err := Load(repoRoot)
	if err != nil {
		return err
	}
	state.RepoRoot = repoRoot
	if state.Claude == nil {
		state.Claude = map[string]string{}
	}
	state.Claude[meta.Marker] = sessionID
	debug.Logf("resume: saved claude session repo=%s marker=%s session=%s", repoRoot, meta.Marker, sessionID)
	return Save(state)
}

// Load reads persisted resume state for a repo.
func Load(repoRoot string) (State, error) {
	path, err := statePath(repoRoot)
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{RepoRoot: repoRoot, Claude: map[string]string{}}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Claude == nil {
		state.Claude = map[string]string{}
	}
	if state.RepoRoot == "" {
		state.RepoRoot = repoRoot
	}
	return state, nil
}

// Save writes resume state to disk.
func Save(state State) error {
	path, err := statePath(state.RepoRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// FetchPaneMeta reads cms-specific tmux pane user-options.
func FetchPaneMeta(paneID string) (PaneMeta, bool, error) {
	format := strings.Join([]string{
		"#{pane_id}",
		"#{pane_current_path}",
		"#{pane_current_command}",
		"#{" + tmuxPaneIDOption + "}",
		"#{" + tmuxClaudeResumeOption + "}",
	}, "\t")
	out, err := tmux.Run("display-message", "-p", "-t", paneID, format)
	if err != nil {
		return PaneMeta{}, false, err
	}
	fields := strings.Split(out, "\t")
	if len(fields) != 5 {
		return PaneMeta{}, false, nil
	}
	meta := PaneMeta{
		ID:           fields[0],
		WorkingDir:   fields[1],
		Command:      fields[2],
		Marker:       fields[3],
		ResumeClaude: isTrue(fields[4]),
	}
	return meta, true, nil
}

// ListSessionPanes returns pane metadata for all panes in a session.
func ListSessionPanes(sessionName string) ([]PaneMeta, error) {
	format := strings.Join([]string{
		"#{session_name}",
		"#{pane_id}",
		"#{pane_current_path}",
		"#{pane_current_command}",
		"#{" + tmuxPaneIDOption + "}",
		"#{" + tmuxClaudeResumeOption + "}",
	}, "\t")
	out, err := tmux.Run("list-panes", "-a", "-F", format)
	if err != nil {
		return nil, err
	}
	var panes []PaneMeta
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 || fields[0] != sessionName {
			continue
		}
		panes = append(panes, PaneMeta{
			ID:           fields[1],
			WorkingDir:   fields[2],
			Command:      fields[3],
			Marker:       fields[4],
			ResumeClaude: isTrue(fields[5]),
		})
	}
	return panes, nil
}

// DefaultClaudeCommand returns the command template, using a default if empty.
func DefaultClaudeCommand(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return defaultClaudeResumeCmd
	}
	return cmd
}

func statePath(repoRoot string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(repoRoot))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}

func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cms", "resume"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "cms", "resume"), nil
}

func repoRootForDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	commonDir, err := git.Cmd(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	return commonDir, nil
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "on", "yes", "true":
		return true
	default:
		return false
	}
}
