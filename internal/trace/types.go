package trace

import "time"

const Version = 1

type IngressKind string

const (
	IngressBootstrapState      IngressKind = "bootstrap_state"
	IngressTmuxEvent           IngressKind = "tmux_event"
	IngressHookEvent           IngressKind = "hook_event"
	IngressProcessPollSnapshot IngressKind = "process_poll_snapshot"
	IngressFullRefreshSnapshot IngressKind = "full_refresh_snapshot"
	IngressTimerFired          IngressKind = "timer_fired"
	IngressCaptureSnapshot     IngressKind = "capture_snapshot"
)

type TmuxStateKind string

const (
	TmuxStateSnapshot TmuxStateKind = "tmux_state_snapshot"
)

type TimerSource string

const (
	TimerSettleRecheck  TimerSource = "settle_recheck"
	TimerCompletedDecay TimerSource = "completed_decay"
	TimerSmoothing      TimerSource = "smoothing_commit"
)

type IngressEvent struct {
	Version   int         `json:"trace_version"`
	Seq       int64       `json:"seq"`
	Timestamp time.Time   `json:"ts"`
	Kind      IngressKind `json:"kind"`
	Payload   any         `json:"payload"`
}

type TmuxStateEvent struct {
	Version   int           `json:"trace_version"`
	Seq       int64         `json:"seq"`
	Timestamp time.Time     `json:"ts"`
	Kind      TmuxStateKind `json:"kind"`
	Payload   any           `json:"payload"`
}

type BootstrapStatePayload struct {
	SnapshotID string `json:"snapshot_id,omitempty"`
}

type TmuxEventPayload struct {
	Event TmuxEvent `json:"event"`
}

type HookEventPayload struct {
	Event HookEvent `json:"event"`
}

type ProcessPollSnapshotPayload struct {
	Statuses map[string]AgentStatus `json:"statuses"`
}

type FullRefreshSnapshotPayload struct {
	SnapshotID string                 `json:"snapshot_id,omitempty"`
	Current    CurrentTarget          `json:"current"`
	Agents     map[string]AgentStatus `json:"agents"`
}

type TimerFiredPayload struct {
	Source TimerSource `json:"source"`
	PaneID string      `json:"pane_id,omitempty"`
	Target string      `json:"target,omitempty"`
}

type CaptureSnapshotPayload struct {
	PaneID  string `json:"pane_id"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

type TmuxSnapshotPayload struct {
	SnapshotID string        `json:"snapshot_id"`
	Reason     string        `json:"reason"`
	Sessions   []TmuxSession `json:"sessions"`
	Current    CurrentTarget `json:"current"`
}

type TmuxEvent struct {
	Kind       string `json:"kind"`
	SessionID  string `json:"session_id,omitempty"`
	WindowID   string `json:"window_id,omitempty"`
	PaneID     string `json:"pane_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Raw        string `json:"raw,omitempty"`
	KindNumber int    `json:"kind_number"`
}

type HookEvent struct {
	Kind       string `json:"kind"`
	PaneID     string `json:"pane_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Message    string `json:"message,omitempty"`
	KindNumber int    `json:"kind_number"`
}

type CurrentTarget struct {
	Session string `json:"session"`
	Window  int    `json:"window"`
	Pane    int    `json:"pane"`
}

type TmuxSession struct {
	Name     string       `json:"name"`
	ID       string       `json:"id"`
	Attached bool         `json:"attached"`
	Windows  []TmuxWindow `json:"windows,omitempty"`
}

type TmuxWindow struct {
	Index  int        `json:"index"`
	Name   string     `json:"name"`
	Active bool       `json:"active"`
	Panes  []TmuxPane `json:"panes,omitempty"`
}

type TmuxPane struct {
	ID         string  `json:"id"`
	Index      int     `json:"index"`
	PID        int     `json:"pid"`
	Command    string  `json:"command"`
	WorkingDir string  `json:"working_dir"`
	Active     bool    `json:"active"`
	Git        GitInfo `json:"git"`
}

type GitInfo struct {
	IsRepo       bool   `json:"is_repo"`
	Branch       string `json:"branch,omitempty"`
	RepoName     string `json:"repo_name,omitempty"`
	Dirty        bool   `json:"dirty"`
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
	LastCommit   string `json:"last_commit,omitempty"`
	LastCommitBy string `json:"last_commit_by,omitempty"`
}

type AgentStatus struct {
	Running      bool   `json:"running"`
	Provider     string `json:"provider,omitempty"`
	Activity     string `json:"activity,omitempty"`
	ProcessPID   int    `json:"process_pid"`
	Model        string `json:"model,omitempty"`
	ContextPct   int    `json:"context_pct"`
	ContextSet   bool   `json:"context_set"`
	Branch       string `json:"branch,omitempty"`
	Mode         string `json:"mode,omitempty"`
	ModeLabel    string `json:"mode_label,omitempty"`
	Args         string `json:"args,omitempty"`
	Source       string `json:"source,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ToolName     string `json:"tool_name,omitempty"`
	Notification string `json:"notification,omitempty"`
}
