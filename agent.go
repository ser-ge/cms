package main

import (
	"strings"
	"sync"
)

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
)

func (a Activity) String() string {
	switch a {
	case ActivityIdle:
		return "idle"
	case ActivityWorking:
		return "working"
	case ActivityWaitingInput:
		return "waiting"
	default:
		return "unknown"
	}
}

func (a Activity) Icon() string {
	switch a {
	case ActivityIdle:
		return "💤"
	case ActivityWorking:
		return "⚡"
	case ActivityWaitingInput:
		return "❓"
	default:
		return "·"
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

// AgentStatus is the provider-neutral runtime state for a pane.
type AgentStatus struct {
	Running    bool
	Provider   Provider
	Activity   Activity
	Model      string
	ContextPct int
	Branch     string
	Mode       AgentModeKind
	ModeLabel  string
	Args       string
}

type providerSpec struct {
	provider    Provider
	detect      func(Pane, procTable) AgentStatus
	parse       func(string, *AgentStatus)
	holdWorking bool
}

var providerSpecs = []providerSpec{
	{
		provider:    ProviderClaude,
		detect:      DetectClaude,
		parse:       parseClaudePane,
		holdWorking: true,
	},
	{
		provider:    ProviderCodex,
		detect:      DetectCodex,
		parse:       parseCodexPane,
		holdWorking: false,
	},
}

// DetectAgent checks known providers in a pane and returns the normalized status.
func DetectAgent(pane Pane, pt procTable) AgentStatus {
	for _, spec := range providerSpecs {
		if status := spec.detect(pane, pt); status.Running {
			return status
		}
	}
	return AgentStatus{}
}

// detectAllAgents runs provider detection for all panes concurrently.
func detectAllAgents(sessions []Session, pt procTable) map[string]AgentStatus {
	var mu sync.Mutex
	results := map[string]AgentStatus{}
	var wg sync.WaitGroup

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				wg.Add(1)
				go func(p Pane) {
					defer wg.Done()
					status := DetectAgent(p, pt)
					if status.Running {
						mu.Lock()
						results[p.ID] = status
						mu.Unlock()
					}
				}(pane)
			}
		}
	}
	wg.Wait()
	return results
}

// findProcessInTree walks a pane's process tree and returns the first matching descendant.
func findProcessInTree(pt procTable, panePID int, match func(procEntry) bool, extractArgs func(string) string) (bool, string) {
	queue := []int{panePID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childPID := range pt.children[current] {
			child := pt.procs[childPID]
			if match(child) {
				if extractArgs == nil {
					return true, ""
				}
				return true, extractArgs(child.args)
			}
			queue = append(queue, childPID)
		}
	}
	return false, ""
}

func isShellCommand(cmd string) bool {
	switch cmd {
	case "fish", "bash", "zsh":
		return true
	default:
		return false
	}
}

func joinParts(parts []string) string {
	return strings.Join(parts, " · ")
}

func knownProviders() []Provider {
	providers := make([]Provider, 0, len(providerSpecs))
	for _, spec := range providerSpecs {
		providers = append(providers, spec.provider)
	}
	return providers
}

func providerSpecFor(provider Provider) (providerSpec, bool) {
	for _, spec := range providerSpecs {
		if spec.provider == provider {
			return spec, true
		}
	}
	return providerSpec{}, false
}

func reparseAgentStatus(content string, status *AgentStatus) bool {
	spec, ok := providerSpecFor(status.Provider)
	if !ok || spec.parse == nil {
		return false
	}
	spec.parse(content, status)
	return true
}

func shouldHoldWorking(status AgentStatus) bool {
	spec, ok := providerSpecFor(status.Provider)
	return ok && spec.holdWorking
}
