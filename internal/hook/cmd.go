package hook

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
)

// RunCmd handles `cms hook [--socket PATH] <event-kind>`.
// It reads Claude Code's JSON payload from stdin, resolves the tmux pane ID,
// and sends a payload to the running CMS instance over the Unix socket.
func RunCmd(args []string) error {
	socketPath := SocketPath()
	var kindStr string

	// Parse args: [--socket PATH] <kind>
	for i := 0; i < len(args); i++ {
		if args[i] == "--socket" && i+1 < len(args) {
			socketPath = args[i+1]
			i++
		} else if kindStr == "" {
			kindStr = args[i]
		}
	}

	if kindStr == "" {
		return fmt.Errorf("usage: cms hook [--socket PATH] <event-kind>\n" +
			"events: session-start, stop, session-end, notification, prompt-submit, pre-tool-use")
	}

	if _, ok := ParseKind(kindStr); !ok {
		return fmt.Errorf("unknown hook event: %q", kindStr)
	}

	// Read Claude Code's JSON payload from stdin.
	ccPayload := ParseClaudeStdin()

	// Resolve the tmux pane ID. Claude Code hooks run inside the pane's shell,
	// so we can get it from the TMUX_PANE environment variable.
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		// Fallback: some setups may set CMS_PANE_ID explicitly.
		paneID = os.Getenv("CMS_PANE_ID")
	}
	if paneID == "" {
		log.Printf("hook-cmd: %s no TMUX_PANE or CMS_PANE_ID set, skipping", kindStr)
		return nil
	}

	// Build our internal payload.
	p := payload{
		Kind:      kindStr,
		PaneID:    paneID,
		SessionID: ccPayload.SessionID,
		CWD:       ccPayload.CWD,
	}

	if ccPayload.ToolName != "" {
		p.ToolName = ccPayload.ToolName
	}
	if ccPayload.Notification != nil {
		p.Message = ccPayload.Notification.Message
	}

	log.Printf("hook-cmd: %s pane=%s session=%s socket=%s", kindStr, paneID, ccPayload.SessionID, socketPath)

	// Send to the CMS daemon socket.
	return SendPayload(socketPath, p)
}

// SendPayload sends a hook payload to the CMS daemon over the Unix socket.
func SendPayload(socketPath string, p payload) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Printf("hook-cmd: socket connect failed: %v", err)
		return nil
	}
	defer conn.Close()

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal hook payload: %w", err)
	}

	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		log.Printf("hook-cmd: socket write failed: %v", err)
		return nil
	}

	// Read acknowledgment.
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	log.Printf("hook-cmd: response=%s", string(buf[:n]))

	return nil
}

// RunSetup prints the hook configuration that users should add to their
// Claude Code settings (~/.claude/settings.json).
func RunSetup() {
	socketPath := SocketPath()
	fmt.Println("Add the following to your Claude Code settings (~/.claude/settings.json):")
	fmt.Println()
	fmt.Println(Config(socketPath))
	fmt.Println()
	fmt.Printf("Socket path: %s\n", socketPath)
}
