package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/debug"
)

// Kind identifies the type of Claude Code hook event.
type Kind int

const (
	SessionStart Kind = iota // Claude session started
	Stop                     // Claude went idle (finished working)
	SessionEnd               // Claude process exiting
	Notification             // Claude needs user input
	PromptSubmit             // User submitted a prompt
	PreToolUse               // Tool execution starting
)

func (k Kind) String() string {
	switch k {
	case SessionStart:
		return "session-start"
	case Stop:
		return "stop"
	case SessionEnd:
		return "session-end"
	case Notification:
		return "notification"
	case PromptSubmit:
		return "prompt-submit"
	case PreToolUse:
		return "pre-tool-use"
	default:
		return "unknown"
	}
}

// ParseKind converts a string to Kind.
func ParseKind(s string) (Kind, bool) {
	switch s {
	case "session-start":
		return SessionStart, true
	case "stop":
		return Stop, true
	case "session-end":
		return SessionEnd, true
	case "notification":
		return Notification, true
	case "prompt-submit":
		return PromptSubmit, true
	case "pre-tool-use":
		return PreToolUse, true
	default:
		return 0, false
	}
}

// Event is the internal representation of a hook event
// after parsing the Claude Code JSON payload.
type Event struct {
	Kind      Kind
	PaneID    string // tmux pane ID (from CMS_PANE_ID env var in hook)
	SessionID string // Claude Code session ID
	CWD       string // working directory
	ToolName  string // tool name (PreToolUse only)
	Message   string // notification message (Notification only)
}

// payload is the JSON structure received from the `cms hook` CLI command
// over the Unix socket.
type payload struct {
	Kind      string `json:"kind"`
	PaneID    string `json:"pane_id"`
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Listener listens on a Unix socket for hook events from `cms hook` commands.
type Listener struct {
	socketPath string
	listener   net.Listener
	events     chan<- Event
	stopCh     chan struct{}
}

// SocketPath returns the path to the CMS hook socket.
func SocketPath() string {
	dir := os.TempDir()
	return filepath.Join(dir, fmt.Sprintf("cms-%d.sock", os.Getuid()))
}

// NewListener creates a listener on the given Unix socket path.
// Events are sent to the provided channel.
func NewListener(socketPath string, events chan<- Event) (*Listener, error) {
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

	hl := &Listener{
		socketPath: socketPath,
		listener:   ln,
		events:     events,
		stopCh:     make(chan struct{}),
	}
	go hl.acceptLoop()
	debug.Logf("hook: listener started on %s", socketPath)
	return hl, nil
}

// Stop shuts down the listener and removes the socket file.
func (hl *Listener) Stop() {
	select {
	case <-hl.stopCh:
		return
	default:
	}
	close(hl.stopCh)
	hl.listener.Close()
	os.Remove(hl.socketPath)
	debug.Logf("hook: listener stopped")
}

func (hl *Listener) acceptLoop() {
	for {
		conn, err := hl.listener.Accept()
		if err != nil {
			select {
			case <-hl.stopCh:
				return
			default:
			}
			log.Printf("hook: accept error: %v", err)
			continue
		}
		go hl.handleConn(conn)
	}
}

func (hl *Listener) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		log.Printf("hook: empty connection")
		conn.Write([]byte("ERR: empty\n"))
		return
	}

	line := scanner.Text()
	var p payload
	if err := json.Unmarshal([]byte(line), &p); err != nil {
		log.Printf("hook: invalid JSON: %v", err)
		conn.Write([]byte("ERR: invalid json\n"))
		return
	}

	kind, ok := ParseKind(p.Kind)
	if !ok {
		log.Printf("hook: unknown kind: %q", p.Kind)
		conn.Write([]byte("ERR: unknown kind\n"))
		return
	}

	ev := Event{
		Kind:      kind,
		PaneID:    p.PaneID,
		SessionID: p.SessionID,
		CWD:       p.CWD,
		ToolName:  p.ToolName,
		Message:   p.Message,
	}

	select {
	case hl.events <- ev:
		conn.Write([]byte("OK\n"))
	case <-hl.stopCh:
		conn.Write([]byte("ERR: shutting down\n"))
	}
}

// Config returns the JSON hooks configuration that should be added to
// Claude Code's settings to enable CMS hook integration.
func Config(socketPath string) string {
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

// claudePayload is the JSON structure that Claude Code sends to hook
// commands via stdin. We parse this to extract the fields we need.
type claudePayload struct {
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

// ParseClaudeStdin reads and parses the JSON that Claude Code pipes to
// hook commands via stdin. Returns empty struct on parse failure (hooks
// should not block Claude on errors).
func ParseClaudeStdin() claudePayload {
	var p claudePayload
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > 0 {
		json.Unmarshal([]byte(strings.Join(lines, "\n")), &p)
	}
	return p
}
