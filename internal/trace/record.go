package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/tmux"
)

type Recorder interface {
	RecordIngress(kind IngressKind, payload any)
	RecordTmuxState(reason string, sessions []tmux.Session, current tmux.CurrentTarget) string
}

type NopRecorder struct{}

func (NopRecorder) RecordIngress(kind IngressKind, payload any) {}
func (NopRecorder) RecordTmuxState(reason string, sessions []tmux.Session, current tmux.CurrentTarget) string {
	return ""
}

type JSONLRecorder struct {
	mu         sync.Mutex
	ingress    *os.File
	tmuxState  *os.File
	ingressSeq int64
	tmuxSeq    int64
	snapshotID int64
}

func NewJSONLRecorder(dir string) (*JSONLRecorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ingress, err := os.OpenFile(filepath.Join(dir, "ingress.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	tmuxState, err := os.OpenFile(filepath.Join(dir, "tmux_state.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = ingress.Close()
		return nil, err
	}
	return &JSONLRecorder{ingress: ingress, tmuxState: tmuxState}, nil
}

func (r *JSONLRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	if r.ingress != nil {
		if err := r.ingress.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.tmuxState != nil {
		if err := r.tmuxState.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *JSONLRecorder) RecordIngress(kind IngressKind, payload any) {
	if r == nil || r.ingress == nil {
		return
	}
	ev := IngressEvent{
		Version:   Version,
		Seq:       atomic.AddInt64(&r.ingressSeq, 1),
		Timestamp: time.Now(),
		Kind:      kind,
		Payload:   normalizePayload(payload),
	}
	r.writeJSONL(r.ingress, ev)
}

func (r *JSONLRecorder) RecordTmuxState(reason string, sessions []tmux.Session, current tmux.CurrentTarget) string {
	if r == nil || r.tmuxState == nil {
		return ""
	}
	snapshotID := atomic.AddInt64(&r.snapshotID, 1)
	payload := TmuxSnapshotPayload{
		SnapshotID: formatSnapshotID(snapshotID),
		Reason:     reason,
		Sessions:   normalizeSessions(sessions),
		Current:    normalizeCurrent(current),
	}
	ev := TmuxStateEvent{
		Version:   Version,
		Seq:       atomic.AddInt64(&r.tmuxSeq, 1),
		Timestamp: time.Now(),
		Kind:      TmuxStateSnapshot,
		Payload:   payload,
	}
	r.writeJSONL(r.tmuxState, ev)
	return payload.SnapshotID
}

func (r *JSONLRecorder) writeJSONL(f *os.File, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = f.Write(append(data, '\n'))
}

func formatSnapshotID(n int64) string {
	return fmt.Sprintf("snapshot-%d", n)
}

func normalizePayload(payload any) any {
	switch p := payload.(type) {
	case BootstrapStatePayload:
		return p
	case TmuxEventPayload:
		return p
	case HookEventPayload:
		return p
	case ProcessPollSnapshotPayload:
		return p
	case FullRefreshSnapshotPayload:
		return p
	case TimerFiredPayload:
		return p
	case CaptureSnapshotPayload:
		return p
	default:
		return payload
	}
}

func normalizeSessions(src []tmux.Session) []TmuxSession {
	if src == nil {
		return nil
	}
	dst := make([]TmuxSession, len(src))
	for i, sess := range src {
		dst[i] = TmuxSession{
			Name:     sess.Name,
			ID:       sess.ID,
			Attached: sess.Attached,
		}
		if sess.Windows != nil {
			dst[i].Windows = make([]TmuxWindow, len(sess.Windows))
			for j, win := range sess.Windows {
				dst[i].Windows[j] = TmuxWindow{
					Index:  win.Index,
					Name:   win.Name,
					Active: win.Active,
				}
				if win.Panes != nil {
					dst[i].Windows[j].Panes = make([]TmuxPane, len(win.Panes))
					for k, pane := range win.Panes {
						dst[i].Windows[j].Panes[k] = TmuxPane{
							ID:         pane.ID,
							Index:      pane.Index,
							PID:        pane.PID,
							Command:    pane.Command,
							WorkingDir: pane.WorkingDir,
							Active:     pane.Active,
							Git:        normalizeGitInfo(pane.Git),
						}
					}
				}
			}
		}
	}
	return dst
}

func normalizeAgents(src map[string]agent.AgentStatus) map[string]AgentStatus {
	if src == nil {
		return nil
	}
	dst := make(map[string]AgentStatus, len(src))
	for k, v := range src {
		dst[k] = AgentStatus{
			Running:      v.Running,
			Provider:     v.Provider.String(),
			Activity:     v.Activity.String(),
			Model:        v.Model,
			ContextPct:   v.ContextPct,
			ContextSet:   v.ContextSet,
			Branch:       v.Branch,
			Mode:         normalizeMode(v.Mode),
			ModeLabel:    v.ModeLabel,
			Args:         v.Args,
			Source:       normalizeSource(v.Source),
			SessionID:    v.SessionID,
			ToolName:     v.ToolName,
			Notification: v.Notification,
		}
	}
	return dst
}

func normalizeCurrent(current tmux.CurrentTarget) CurrentTarget {
	return CurrentTarget{
		Session: current.Session,
		Window:  current.Window,
		Pane:    current.Pane,
	}
}

func normalizeTmuxEvent(ev tmux.Event) TmuxEvent {
	return TmuxEvent{
		Kind:       normalizeTmuxEventKind(ev.Kind),
		SessionID:  ev.SessionID,
		WindowID:   ev.WindowID,
		PaneID:     ev.PaneID,
		Name:       ev.Name,
		Raw:        ev.Raw,
		KindNumber: int(ev.Kind),
	}
}

func normalizeHookEvent(ev hook.Event) HookEvent {
	return HookEvent{
		Kind:       ev.Kind.String(),
		PaneID:     ev.PaneID,
		SessionID:  ev.SessionID,
		CWD:        ev.CWD,
		ToolName:   ev.ToolName,
		Message:    ev.Message,
		KindNumber: int(ev.Kind),
	}
}

func NormalizeTmuxEvent(ev tmux.Event) TmuxEvent {
	return normalizeTmuxEvent(ev)
}

func NormalizeHookEvent(ev hook.Event) HookEvent {
	return normalizeHookEvent(ev)
}

func NormalizeCurrent(current tmux.CurrentTarget) CurrentTarget {
	return normalizeCurrent(current)
}

func NormalizeAgents(src map[string]agent.AgentStatus) map[string]AgentStatus {
	return normalizeAgents(src)
}

func NormalizeSessions(src []tmux.Session) []TmuxSession {
	return normalizeSessions(src)
}

func normalizeTmuxEventKind(kind tmux.EventKind) string {
	switch kind {
	case tmux.Output:
		return "output"
	case tmux.SessionCreated:
		return "session_created"
	case tmux.SessionClosed:
		return "session_closed"
	case tmux.SessionChanged:
		return "session_changed"
	case tmux.WindowAdd:
		return "window_add"
	case tmux.WindowClose:
		return "window_close"
	case tmux.WindowChanged:
		return "window_changed"
	case tmux.PaneExited:
		return "pane_exited"
	case tmux.LayoutChange:
		return "layout_change"
	case tmux.ClientDetached:
		return "client_detached"
	default:
		return "unhandled"
	}
}

func normalizeGitInfo(info git.Info) GitInfo {
	return GitInfo{
		IsRepo:       info.IsRepo,
		Branch:       info.Branch,
		RepoName:     info.RepoName,
		Dirty:        info.Dirty,
		Ahead:        info.Ahead,
		Behind:       info.Behind,
		LastCommit:   info.LastCommit,
		LastCommitBy: info.LastCommitBy,
	}
}

func normalizeMode(mode agent.AgentModeKind) string {
	switch mode {
	case agent.ModePlan:
		return "plan"
	case agent.ModeAcceptEdits:
		return "accept_edits"
	case agent.ModeBypassPermissions:
		return "bypass_permissions"
	case agent.ModeReadOnly:
		return "read_only"
	case agent.ModeWorkspaceWrite:
		return "workspace_write"
	case agent.ModeDangerFullAccess:
		return "danger_full_access"
	default:
		return ""
	}
}

func normalizeSource(source agent.StatusSource) string {
	switch source {
	case agent.SourceObserver:
		return "observer"
	case agent.SourceHook:
		return "hook"
	default:
		return ""
	}
}
