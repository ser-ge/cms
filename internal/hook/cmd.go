package hook

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// settingsPath returns the path to Claude Code's settings.json.
func settingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// cmsHookEntries returns the hook entries CMS wants to install, keyed by event name.
// Each entry is a single hook-group (matcher + hooks array) per event.
func cmsHookEntries(socketPath string) map[string]map[string]interface{} {
	cmsBin := "cms"
	if exe, err := os.Executable(); err == nil {
		cmsBin = exe
	}
	base := fmt.Sprintf(`%s internal hook --socket %s`, cmsBin, socketPath)

	return map[string]map[string]interface{}{
		"SessionStart": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " session-start", "timeout": float64(10),
			}},
		},
		"Stop": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " stop", "timeout": float64(5),
			}},
		},
		"SessionEnd": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " session-end", "timeout": float64(1),
			}},
		},
		"Notification": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " notification", "timeout": float64(10),
			}},
		},
		"UserPromptSubmit": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " prompt-submit", "timeout": float64(10),
			}},
		},
		"PreToolUse": {
			"matcher": "", "hooks": []interface{}{map[string]interface{}{
				"type": "command", "command": base + " pre-tool-use", "timeout": float64(5), "async": true,
			}},
		},
	}
}

// isCMSHookGroup returns true if a hook group (a single entry in an event's
// array) contains a command that looks like a CMS hook command.
func isCMSHookGroup(group map[string]interface{}) bool {
	hooks, ok := group["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, "cms internal hook") || strings.Contains(cmd, "cms hook --socket") {
			return true
		}
	}
	return false
}

// RunInstall merges CMS hook entries into Claude Code's settings.json.
// It appends CMS hook groups to existing event arrays without disturbing
// other hooks. If CMS hooks are already present, it skips the event.
func RunInstall() error {
	path := settingsPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	// Read existing settings (or start fresh).
	settings := map[string]interface{}{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	// Ensure hooks map exists.
	hooksRaw, _ := settings["hooks"].(map[string]interface{})
	if hooksRaw == nil {
		hooksRaw = map[string]interface{}{}
		settings["hooks"] = hooksRaw
	}

	socketPath := SocketPath()
	entries := cmsHookEntries(socketPath)

	installed := 0
	skipped := 0
	for event, entry := range entries {
		// Get existing array for this event.
		var existing []interface{}
		if arr, ok := hooksRaw[event].([]interface{}); ok {
			existing = arr
		}

		// Check if CMS hooks are already present.
		alreadyInstalled := false
		for _, item := range existing {
			group, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if isCMSHookGroup(group) {
				alreadyInstalled = true
				break
			}
		}

		if alreadyInstalled {
			skipped++
			continue
		}

		// Append CMS hook group.
		existing = append(existing, entry)
		hooksRaw[event] = existing
		installed++
	}

	if installed == 0 && skipped > 0 {
		fmt.Println("cms hooks already installed")
		return nil
	}

	// Ensure the directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Write back.
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Printf("installed cms hooks into %s (%d events)\n", path, installed)
	if skipped > 0 {
		fmt.Printf("skipped %d events (already installed)\n", skipped)
	}
	fmt.Printf("socket: %s\n", socketPath)
	return nil
}

// RunUninstall removes CMS hook entries from Claude Code's settings.json.
// It removes only hook groups whose commands match CMS patterns, leaving
// all other hooks untouched. Empty event arrays are removed.
func RunUninstall() error {
	path := settingsPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no settings file found, nothing to uninstall")
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	settings := map[string]interface{}{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	hooksRaw, _ := settings["hooks"].(map[string]interface{})
	if hooksRaw == nil {
		fmt.Println("no hooks configured, nothing to uninstall")
		return nil
	}

	removed := 0
	for event, val := range hooksRaw {
		arr, ok := val.([]interface{})
		if !ok {
			continue
		}

		var kept []interface{}
		for _, item := range arr {
			group, ok := item.(map[string]interface{})
			if !ok {
				kept = append(kept, item)
				continue
			}
			if isCMSHookGroup(group) {
				removed++
				continue
			}
			kept = append(kept, item)
		}

		if len(kept) == 0 {
			delete(hooksRaw, event)
		} else {
			hooksRaw[event] = kept
		}
	}

	if removed == 0 {
		fmt.Println("no cms hooks found, nothing to uninstall")
		return nil
	}

	// Clean up empty hooks map.
	if len(hooksRaw) == 0 {
		delete(settings, "hooks")
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	fmt.Printf("removed %d cms hook entries from %s\n", removed, path)
	return nil
}
