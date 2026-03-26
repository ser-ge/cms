package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/tmux"
)

func TestJSONLRecorderWritesBothStreams(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewJSONLRecorder(dir)
	if err != nil {
		t.Fatalf("NewJSONLRecorder: %v", err)
	}
	defer rec.Close()

	rec.RecordIngress(IngressTimerFired, TimerFiredPayload{Source: TimerSettleRecheck, PaneID: "%1"})
	snapshotID := rec.RecordTmuxState("bootstrap", []tmux.Session{{Name: "cms"}}, tmux.CurrentTarget{Session: "cms"})
	if snapshotID == "" {
		t.Fatal("expected snapshot id")
	}

	ingress, err := os.ReadFile(filepath.Join(dir, "ingress.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile ingress: %v", err)
	}
	if !strings.Contains(string(ingress), `"kind":"timer_fired"`) {
		t.Fatalf("ingress missing timer_fired event: %s", string(ingress))
	}

	tmuxState, err := os.ReadFile(filepath.Join(dir, "tmux_state.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile tmux_state: %v", err)
	}
	if !strings.Contains(string(tmuxState), snapshotID) {
		t.Fatalf("tmux state missing snapshot id %q: %s", snapshotID, string(tmuxState))
	}
}

func TestJSONLRecorderNormalizesEventPayloads(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewJSONLRecorder(dir)
	if err != nil {
		t.Fatalf("NewJSONLRecorder: %v", err)
	}
	defer rec.Close()

	rec.RecordIngress(IngressTmuxEvent, TmuxEventPayload{
		Event: NormalizeTmuxEvent(tmux.Event{Kind: tmux.Output, PaneID: "%1", Raw: "%output %1 hi"}),
	})
	rec.RecordIngress(IngressHookEvent, HookEventPayload{
		Event: NormalizeHookEvent(hook.Event{Kind: hook.PreToolUse, PaneID: "%1", ToolName: "Edit"}),
	})

	ingress, err := os.ReadFile(filepath.Join(dir, "ingress.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile ingress: %v", err)
	}
	text := string(ingress)
	if !strings.Contains(text, `"event":{"kind":"output","pane_id":"%1"`) {
		t.Fatalf("tmux event not normalized: %s", text)
	}
	if !strings.Contains(text, `"event":{"kind":"pre-tool-use","pane_id":"%1","tool_name":"Edit"`) {
		t.Fatalf("hook event not normalized: %s", text)
	}
}
