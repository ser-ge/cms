package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// CtrlEventKind identifies the type of control mode notification.
type CtrlEventKind int

const (
	CtrlOutput         CtrlEventKind = iota // %output — pane produced output
	CtrlSessionCreated                      // %session-created
	CtrlSessionClosed                       // %session-closed
	CtrlSessionChanged                      // %session-changed — focus moved to another session
	CtrlWindowAdd                           // %window-add
	CtrlWindowClose                         // %window-close
	CtrlWindowChanged                       // %session-window-changed
	CtrlPaneExited                          // %pane-exited (tmux 3.3a+: %unlinked-window-close can also signal this)
	CtrlLayoutChange                        // %layout-change
	CtrlClientDetached                      // %client-detached — our control client was kicked
	CtrlUnhandled                           // unknown notification
)

// CtrlEvent is a parsed notification from tmux control mode.
type CtrlEvent struct {
	Kind      CtrlEventKind
	SessionID string // e.g. "$1"
	WindowID  string // e.g. "@3"
	PaneID    string // e.g. "%7"
	Name      string // session/window name when available
	Raw       string // full raw line for debugging
}

// CtrlClient manages a tmux control mode connection.
// It attaches to the most recent session in control mode for global event visibility.
type CtrlClient struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	events chan CtrlEvent
	done   chan struct{}
	mu     sync.Mutex
}

// NewCtrlClient starts a tmux control mode connection.
func NewCtrlClient() (*CtrlClient, error) {
	cmd := exec.Command("tmux", "-C", "attach-session")
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
	debugf("control: connected pid=%d", cmd.Process.Pid)

	c := &CtrlClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdinPipe),
		events: make(chan CtrlEvent, 256),
		done:   make(chan struct{}),
	}

	// Start the reader goroutine.
	go c.readLoop(bufio.NewScanner(stdoutPipe))

	return c, nil
}

// Stop closes the control mode connection.
func (c *CtrlClient) Stop() {
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
	debugf("control: stopped")
}

// readLoop reads lines from tmux control mode stdout and parses notifications.
func (c *CtrlClient) readLoop(scanner *bufio.Scanner) {
	defer close(c.events)

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

		ev := parseLine(line)
		if ev.Kind == CtrlUnhandled {
			continue
		}
		debugf("control: event kind=%d pane=%s session=%s window=%s raw=%q", ev.Kind, ev.PaneID, ev.SessionID, ev.WindowID, ev.Raw)

		select {
		case c.events <- ev:
		case <-c.done:
			return
		}
	}
}

// parseLine parses a single tmux control mode notification line.
func parseLine(line string) CtrlEvent {
	ev := CtrlEvent{Raw: line}

	// Split on space: %notification-type arg1 arg2 ...
	parts := strings.SplitN(line, " ", 3)
	if len(parts) == 0 {
		ev.Kind = CtrlUnhandled
		return ev
	}

	switch parts[0] {
	case "%output":
		// %output %<paneID> <data>
		ev.Kind = CtrlOutput
		if len(parts) >= 2 {
			ev.PaneID = parts[1]
		}

	case "%session-created":
		// %session-created $<id>
		ev.Kind = CtrlSessionCreated
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}

	case "%session-closed":
		ev.Kind = CtrlSessionClosed
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}

	case "%session-changed":
		// %session-changed $<id> <name>
		ev.Kind = CtrlSessionChanged
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}
		if len(parts) >= 3 {
			ev.Name = parts[2]
		}

	case "%window-add":
		// %window-add @<id>
		ev.Kind = CtrlWindowAdd
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%window-close":
		ev.Kind = CtrlWindowClose
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%session-window-changed":
		// %session-window-changed $<sessID> @<winID>
		ev.Kind = CtrlWindowChanged
		if len(parts) >= 2 {
			ev.SessionID = parts[1]
		}
		if len(parts) >= 3 {
			ev.WindowID = parts[2]
		}

	case "%pane-exited":
		ev.Kind = CtrlPaneExited
		if len(parts) >= 2 {
			ev.PaneID = parts[1]
		}

	case "%unlinked-window-close":
		// Treat as pane exited / structural change.
		ev.Kind = CtrlPaneExited
		if len(parts) >= 2 {
			ev.WindowID = parts[1]
		}

	case "%layout-change":
		ev.Kind = CtrlLayoutChange

	case "%client-detached":
		ev.Kind = CtrlClientDetached

	case "%begin", "%end", "%error":
		// Command response markers — skip for now (Phase 3).
		ev.Kind = CtrlUnhandled

	default:
		ev.Kind = CtrlUnhandled
	}

	return ev
}
