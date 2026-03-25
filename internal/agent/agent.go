package agent


// Provider identifies the agent runtime in a tmux pane.
type Provider int

const (
	ProviderUnknown Provider = iota
	ProviderClaude
	ProviderCodex
)

func (p Provider) String() string {
	switch p {
	case ProviderClaude:
		return "claude"
	case ProviderCodex:
		return "codex"
	default:
		return ""
	}
}

// Activity represents what an agent is doing right now.
type Activity int

const (
	ActivityUnknown Activity = iota
	ActivityIdle
	ActivityWorking
	ActivityWaitingInput
	ActivityCompleted // just finished work (Working->Idle), decays to Idle
)

func (a Activity) String() string {
	switch a {
	case ActivityIdle:
		return "idle"
	case ActivityWorking:
		return "working"
	case ActivityWaitingInput:
		return "waiting"
	case ActivityCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// AgentModeKind is a normalized mode/category surfaced in the UI.
type AgentModeKind int

const (
	ModeNone AgentModeKind = iota
	ModePlan
	ModeAcceptEdits
	ModeBypassPermissions
	ModeReadOnly
	ModeWorkspaceWrite
	ModeDangerFullAccess
)

// StatusSource identifies where an AgentStatus update originated.
type StatusSource int

const (
	SourceObserver StatusSource = iota // tmux capture-pane heuristics
	SourceHook                         // Claude Code hook (authoritative)
)

// AgentStatus is the provider-neutral runtime state for a pane.
type AgentStatus struct {
	Running    bool
	Provider   Provider
	Activity   Activity
	Model      string
	ContextPct int
	ContextSet bool
	Branch     string
	Mode       AgentModeKind
	ModeLabel  string
	Args       string
	Source     StatusSource

	// Hook-enriched fields (only populated via SourceHook).
	SessionID    string // Claude Code session ID
	ToolName     string // current tool being executed
	Notification string // last notification message
}

// ApplyUpdates merges incremental agent status updates into an existing map.
// Hook-sourced updates take precedence over observer-sourced updates.
func ApplyUpdates(dst map[string]AgentStatus, updates map[string]AgentStatus) map[string]AgentStatus {
	if dst == nil {
		dst = make(map[string]AgentStatus, len(updates))
	}
	for id, status := range updates {
		if !status.Running {
			delete(dst, id)
			continue
		}
		existing, has := dst[id]
		if has && existing.Source == SourceHook && status.Source == SourceObserver {
			// Don't overwrite hook data with observer data.
			// Merge only fields that observer knows better (model, context, mode from status line).
			if status.Model != "" {
				existing.Model = status.Model
			}
			if status.ContextSet {
				existing.ContextPct = status.ContextPct
				existing.ContextSet = true
			}
			if status.Branch != "" {
				existing.Branch = status.Branch
			}
			if status.ModeLabel != "" {
				existing.Mode = status.Mode
				existing.ModeLabel = status.ModeLabel
			}
			dst[id] = existing
		} else {
			dst[id] = status
		}
	}
	return dst
}

// NormalizeParsed sets default activity for a running agent status.
func NormalizeParsed(status *AgentStatus) {
	if status == nil || !status.Running {
		return
	}
	if status.Activity == ActivityUnknown {
		status.Activity = ActivityIdle
	}
}

