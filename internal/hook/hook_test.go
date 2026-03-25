package hook

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// testSocketPath returns a short socket path under /tmp to avoid the
// 108-character Unix socket path limit on macOS.
func testSocketPath(t *testing.T) string {
	t.Helper()
	path := fmt.Sprintf("/tmp/cms-test-%d.sock", os.Getpid())
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func TestParseHookKindRoundTrip(t *testing.T) {
	kinds := []struct {
		str  string
		kind Kind
	}{
		{"session-start", SessionStart},
		{"stop", Stop},
		{"session-end", SessionEnd},
		{"notification", Notification},
		{"prompt-submit", PromptSubmit},
		{"pre-tool-use", PreToolUse},
	}

	for _, tc := range kinds {
		got, ok := ParseKind(tc.str)
		if !ok {
			t.Fatalf("ParseKind(%q) returned not ok", tc.str)
		}
		if got != tc.kind {
			t.Fatalf("ParseKind(%q) = %v, want %v", tc.str, got, tc.kind)
		}
		if got.String() != tc.str {
			t.Fatalf("Kind(%d).String() = %q, want %q", got, got.String(), tc.str)
		}
	}

	if _, ok := ParseKind("bogus"); ok {
		t.Fatal("ParseKind(bogus) should return not ok")
	}
}

func TestHookListenerAcceptsEvent(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan Event, 8)

	hl, err := NewListener(sock, events)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer hl.Stop()

	// Connect and send a payload.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	p := payload{
		Kind:      "pre-tool-use",
		PaneID:    "%5",
		SessionID: "sess-abc",
		ToolName:  "Edit",
	}
	data, _ := json.Marshal(p)
	data = append(data, '\n')
	conn.Write(data)

	// Read response.
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "OK\n" {
		t.Fatalf("response = %q, want OK", got)
	}

	// Check event was delivered.
	select {
	case ev := <-events:
		if ev.Kind != PreToolUse {
			t.Fatalf("kind = %v, want PreToolUse", ev.Kind)
		}
		if ev.PaneID != "%5" {
			t.Fatalf("paneID = %q, want %%5", ev.PaneID)
		}
		if ev.ToolName != "Edit" {
			t.Fatalf("toolName = %q, want Edit", ev.ToolName)
		}
		if ev.SessionID != "sess-abc" {
			t.Fatalf("sessionID = %q, want sess-abc", ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hook event")
	}
}

func TestHookListenerRejectsInvalidJSON(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan Event, 8)

	hl, err := NewListener(sock, events)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer hl.Stop()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	conn.Write([]byte("not json\n"))
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "ERR: invalid json\n" {
		t.Fatalf("response = %q, want ERR: invalid json", got)
	}
}

func TestHookListenerRejectsUnknownKind(t *testing.T) {
	sock := testSocketPath(t)
	events := make(chan Event, 8)

	hl, err := NewListener(sock, events)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer hl.Stop()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	p := payload{Kind: "bogus", PaneID: "%1"}
	data, _ := json.Marshal(p)
	data = append(data, '\n')
	conn.Write(data)

	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	conn.Close()

	if got := string(buf[:n]); got != "ERR: unknown kind\n" {
		t.Fatalf("response = %q, want ERR: unknown kind", got)
	}
}

func TestHookListenerRemovesStaleSocket(t *testing.T) {
	sock := testSocketPath(t)

	// Create a stale file at the socket path.
	os.WriteFile(sock, []byte("stale"), 0600)

	events := make(chan Event, 8)
	hl, err := NewListener(sock, events)
	if err != nil {
		t.Fatalf("NewListener should succeed over stale socket: %v", err)
	}
	hl.Stop()
}

func TestHooksConfigContainsAllEvents(t *testing.T) {
	cfg := Config("/tmp/test.sock")

	for _, event := range []string{
		"SessionStart", "Stop", "SessionEnd",
		"Notification", "UserPromptSubmit", "PreToolUse",
	} {
		if !contains(cfg, event) {
			t.Fatalf("Config missing event %q", event)
		}
	}

	// PreToolUse should be async.
	if !contains(cfg, `"async": true`) {
		t.Fatal("PreToolUse should have async: true")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && // avoid trivial match
		stringContains(s, substr)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
