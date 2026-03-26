package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	tmuxPathOnce sync.Once
	tmuxPath     string
	tmuxPathErr  error
	testSocket   string
	testSocketMu sync.Mutex
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
	cmdArgs := make([]string, 0, len(args)+2)
	socket, err := tmuxSocketPath(path)
	if err != nil {
		return nil, err
	}
	if socket != "" {
		cmdArgs = append(cmdArgs, "-S", socket)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(path, cmdArgs...)
	if socket != "" {
		cmd.Env = filteredTmuxEnv()
	}
	return cmd, nil
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

func filteredTmuxEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func tmuxSocketPath(tmuxPath string) (string, error) {
	if socket := strings.TrimSpace(os.Getenv("CMS_TMUX_SOCKET")); socket != "" {
		return socket, nil
	}
	if !runningUnderGoTest() {
		return "", nil
	}
	return ensureTestSocket(tmuxPath)
}

func runningUnderGoTest() bool {
	return strings.HasSuffix(os.Args[0], ".test")
}

func ensureTestSocket(tmuxPath string) (string, error) {
	testSocketMu.Lock()
	defer testSocketMu.Unlock()

	if testSocket != "" {
		return testSocket, nil
	}

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("cms-go-test-%d.sock", os.Getpid()))
	cmd := exec.Command(tmuxPath, "-S", socket, "new-session", "-d", "-s", "cms-test")
	cmd.Env = filteredTmuxEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("init isolated tmux server: %s (%w)", msg, err)
		}
		return "", fmt.Errorf("init isolated tmux server: %w", err)
	}

	testSocket = socket
	return testSocket, nil
}
