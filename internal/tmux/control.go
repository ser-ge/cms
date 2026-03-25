package tmux

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/serge/cms/internal/debug"
)

// EventKind identifies the type of control mode notification.
type EventKind int

const (
	Output         EventKind = iota // %output — pane produced output
	SessionCreated                  // %session-created
	SessionClosed                   // %session-closed
	SessionChanged                  // %session-changed — focus moved to another session
	WindowAdd                       // %window-add
	WindowClose                     // %window-close
	WindowChanged                   // %session-window-changed
	PaneExited                      // %pane-exited (tmux 3.3a+: %unlinked-window-close can also signal this)
	LayoutChange                    // %layout-change
	ClientDetached                  // %client-detached — our control client was kicked
	Unhandled                       // unknown notification
)

// Event is a parsed notification from tmux control mode.
type Event struct {
	Kind      EventKind
	SessionID string // e.g. "$1"
	WindowID  string // e.g. "@3"
	PaneID    string // e.g. "%7"
	Name      string // session/window name when available
	Raw       string // full raw line for debugging
}

// Client manages a tmux control mode connection.
// It attaches to the most recent session in control mode for global event visibility.
type Client struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	Events chan Event
	done   chan struct{}
	mu     sync.Mutex
}

// NewClient starts a tmux control mode connection.
func NewClient() (*Client, error) {
	cmd, err := Command("-C", "attach-session")
	if err != nil {
		return nil, fmt.Errorf("control command: %w", err)
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("control stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("control stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdinPipe.Close()
		return nil, fmt.Errorf("control start: %w", err)
	}
	debug.Logf("control: connected pid=%d", cmd.Process.Pid)

	c := &Client{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdinPipe),
		Events: make(chan Event, 256),
		done:   make(chan struct{}),
	}

	// Start the reader goroutine.
	go c.readLoop(bufio.NewScanner(stdoutPipe))

	return c, nil
}

// Stop closes the control mode connection.
func (c *Client) Stop() {
	select {
	case <-c.done:
		return // already stopped
	default:
	}
	close(c.done)

	// Detach the control client gracefully.
	c.mu.Lock()
	c.stdin.WriteString("detach-client\n")
	c.stdin.Flush()
	c.mu.Unlock()

	c.cmd.Process.Kill()
	c.cmd.Wait()
	debug.Logf("control: stopped")
}

// readLoop reads lines from tmux control mode stdout and parses notifications.
func (c *Client) readLoop(scanner *bufio.Scanner) {
	defer close(c.Events)

	for scanner.Scan() {
		select {
		case <-c.done:
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "%") {
			continue
		}

		ev := ParseLine(line)
		if ev.Kind == Unhandled {
			continue
		}
		debug.Logf("control: event kind=%d pane=%s session=%s window=%s raw=%q", ev.Kind, ev.PaneID, ev.SessionID, ev.WindowID, ev.Raw)

		select {
		case c.Events <- ev:
		case <-c.done:
			return
		}
	}
}

// ParseLine parses a single tmux control mode notification line.
func ParseLine(line string) Event {
	ev := Event{Raw: line}

	// Split on space: %notification-type arg1 arg2 ...
	parts := strings.SplitN(line, " ", 3)
	if len(parts) == 0 {
		ev.Kind = Unhandled
		return ev
	}

	switch parts[0] {
	case "%output":
		// %output %<paneID> <data>
		ev.Kind = Output
		if len(parts) >= 2 {
			ev.PaneID = parts[1]
		}

	case "%session-created":
		// %session-created $<id>
		ev.Kind = SessionCreated
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}

	case "%session-closed":
		ev.Kind = SessionClosed
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}

	case "%session-changed":
		// %session-changed $<id> <name>
		ev.Kind = SessionChanged
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}
		if len(parts) >= 3 {
			ev.Name = parts[2]
		}

	case "%window-add":
		// %window-add @<id>
		ev.Kind = WindowAdd
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%window-close":
		ev.Kind = WindowClose
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%session-window-changed":
		// %session-window-changed $<sessID> @<winID>
		ev.Kind = WindowChanged
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}
		if len(parts) >= 3 {
			ev.WindowID = parts[2]
		}

	case "%pane-exited":
		ev.Kind = PaneExited
		if len(parts) >= 2 {
			ev.PaneID = parts[1]
		}

	case "%unlinked-window-close":
		// Treat as pane exited / structural change.
		ev.Kind = PaneExited
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%layout-change":
		ev.Kind = LayoutChange

	case "%client-detached":
		ev.Kind = ClientDetached

	case "%begin", "%end", "%error":
		// Command response markers — skip for now.
		ev.Kind = Unhandled

	default:
		ev.Kind = Unhandled
	}

	return ev
}
