package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// HookKind identifies the type of Claude Code hook event.
type HookKind int

const (
	HookSessionStart HookKind = iota // Claude session started
	HookStop                         // Claude went idle (finished working)
	HookSessionEnd                   // Claude process exiting
	HookNotification                 // Claude needs user input
	HookPromptSubmit                 // User submitted a prompt
	HookPreToolUse                   // Tool execution starting
)

func (k HookKind) String() string {
	switch k {
	case HookSessionStart:
		return "session-start"
	case HookStop:
		return "stop"
	case HookSessionEnd:
		return "session-end"
	case HookNotification:
		return "notification"
	case HookPromptSubmit:
		return "prompt-submit"
	case HookPreToolUse:
		return "pre-tool-use"
	default:
		return "unknown"
	}
}

// ParseHookKind converts a string to HookKind.
func ParseHookKind(s string) (HookKind, bool) {
	switch s {
	case "session-start":
		return HookSessionStart, true
	case "stop":
		return HookStop, true
	case "session-end":
		return HookSessionEnd, true
	case "notification":
		return HookNotification, true
	case "prompt-submit":
		return HookPromptSubmit, true
	case "pre-tool-use":
		return HookPreToolUse, true
	default:
		return 0, false
	}
}

// HookEvent is the internal representation of a hook event
// after parsing the Claude Code JSON payload.
type HookEvent struct {
	Kind      HookKind
	PaneID    string // tmux pane ID (from CMS_PANE_ID env var in hook)
	SessionID string // Claude Code session ID
	CWD       string // working directory
	ToolName  string // tool name (PreToolUse only)
	Message   string // notification message (Notification only)
}

// hookPayload is the JSON structure received from the `cms hook` CLI command
// over the Unix socket.
type hookPayload struct {
	Kind      string `json:"kind"`
	PaneID    string `json:"pane_id"`
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Message   string `json:"message,omitempty"`
}

// HookListener listens on a Unix socket for hook events from `cms hook` commands.
type HookListener struct {
	socketPath string
	listener   net.Listener
	events     chan<- HookEvent
	stopCh     chan struct{}
}

// HookSocketPath returns the path to the CMS hook socket.
func HookSocketPath() string {
	dir := os.TempDir()
	return filepath.Join(dir, fmt.Sprintf("cms-%d.sock", os.Getuid()))
}

// NewHookListener creates a listener on the given Unix socket path.
// Events are sent to the provided channel.
func NewHookListener(socketPath string, events chan<- HookEvent) (*HookListener, error) {
	// Remove stale socket file if it exists.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", socketPath, err)
	}

	// Restrict socket permissions to owner only.
	os.Chmod(socketPath, 0700)

	hl := &HookListener{
		socketPath: socketPath,
		listener:   ln,
		events:     events,
		stopCh:     make(chan struct{}),
	}
	go hl.acceptLoop()
	debugf("hook: listener started on %s", socketPath)
	return hl, nil
}

// Stop shuts down the listener and removes the socket file.
func (hl *HookListener) Stop() {
	select {
	case <-hl.stopCh:
		return
	default:
	}
	close(hl.stopCh)
	hl.listener.Close()
	os.Remove(hl.socketPath)
	debugf("hook: listener stopped")
}

func (hl *HookListener) acceptLoop() {
	for {
		conn, err := hl.listener.Accept()
		if err != nil {
			select {
			case <-hl.stopCh:
				return
			default:
			}
			debugf("hook: accept error: %v", err)
			continue
		}
		go hl.handleConn(conn)
	}
}

func (hl *HookListener) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		debugf("hook: empty connection")
		conn.Write([]byte("ERR: empty\n"))
		return
	}

	line := scanner.Text()
	var payload hookPayload
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		debugf("hook: invalid JSON: %v", err)
		conn.Write([]byte("ERR: invalid json\n"))
		return
	}

	kind, ok := ParseHookKind(payload.Kind)
	if !ok {
		debugf("hook: unknown kind: %q", payload.Kind)
		conn.Write([]byte("ERR: unknown kind\n"))
		return
	}

	ev := HookEvent{
		Kind:      kind,
		PaneID:    payload.PaneID,
		SessionID: payload.SessionID,
		CWD:       payload.CWD,
		ToolName:  payload.ToolName,
		Message:   payload.Message,
	}

	select {
	case hl.events <- ev:
		conn.Write([]byte("OK\n"))
	case <-hl.stopCh:
		conn.Write([]byte("ERR: shutting down\n"))
	}
}

// HooksConfig returns the JSON hooks configuration that should be added to
// Claude Code's settings to enable CMS hook integration.
func HooksConfig(socketPath string) string {
	// The hook commands will set CMS_PANE_ID from the tmux pane they run in.
	cmsBin := "cms"
	if exe, err := os.Executable(); err == nil {
		cmsBin = exe
	}
	base := fmt.Sprintf(`%s hook --socket %s`, cmsBin, socketPath)

	hooks := map[string][]map[string]interface{}{
		"SessionStart": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " session-start", "timeout": 10,
			}},
		}},
		"Stop": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " stop", "timeout": 5,
			}},
		}},
		"SessionEnd": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " session-end", "timeout": 1,
			}},
		}},
		"Notification": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " notification", "timeout": 10,
			}},
		}},
		"UserPromptSubmit": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " prompt-submit", "timeout": 10,
			}},
		}},
		"PreToolUse": {{
			"matcher": "", "hooks": []map[string]interface{}{{
				"type": "command", "command": base + " pre-tool-use", "timeout": 5, "async": true,
			}},
		}},
	}

	b, _ := json.MarshalIndent(map[string]interface{}{"hooks": hooks}, "", "  ")
	return string(b)
}

// claudeHookPayload is the JSON structure that Claude Code sends to hook
// commands via stdin. We parse this to extract the fields we need.
type claudeHookPayload struct {
	SessionID string `json:"session_id"`
	// SessionStart
	CWD string `json:"cwd"`
	// Notification
	Notification *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"notification"`
	// PreToolUse
	ToolName string `json:"tool_name"`
}

// ParseClaudeHookStdin reads and parses the JSON that Claude Code pipes to
// hook commands via stdin. Returns empty struct on parse failure (hooks
// should not block Claude on errors).
func ParseClaudeHookStdin() claudeHookPayload {
	var payload claudeHookPayload
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > 0 {
		json.Unmarshal([]byte(strings.Join(lines, "\n")), &payload)
	}
	return payload
}
