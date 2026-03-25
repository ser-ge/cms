package tmux

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	tmuxPathOnce sync.Once
	tmuxPath     string
	tmuxPathErr  error
)

// Run executes a tmux command and returns its trimmed stdout.
func Run(args ...string) (string, error) {
	cmd, err := Command(args...)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		msg := fmt.Sprintf("tmux %s: %s", strings.Join(args, " "), err)
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg += " (" + strings.TrimSpace(string(ee.Stderr)) + ")"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// Command creates an *exec.Cmd for the given tmux arguments.
func Command(args ...string) (*exec.Cmd, error) {
	path, err := tmuxExecutable()
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...), nil
}

func tmuxExecutable() (string, error) {
	tmuxPathOnce.Do(func() {
		tmuxPath, tmuxPathErr = exec.LookPath("tmux")
	})
	if tmuxPathErr != nil {
		return "", fmt.Errorf("find tmux: %w", tmuxPathErr)
	}
	return tmuxPath, nil
}
