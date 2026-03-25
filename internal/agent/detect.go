package agent

import (
	"strings"
	"sync"

	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/tmux"
)

// providerSpec defines how to detect and parse a specific agent provider
// from tmux pane content and process trees.
type providerSpec struct {
	provider    Provider
	procMatch   func(proc.Entry) bool
	parse       func(string, *AgentStatus)
	holdWorking bool
}

var providerSpecs = []providerSpec{
	{
		provider:    ProviderClaude,
		procMatch:   func(p proc.Entry) bool { return strings.Contains(p.Comm, "claude") },
		parse:       parseClaudePane,
		holdWorking: true,
	},
	{
		provider:    ProviderCodex,
		procMatch:   func(p proc.Entry) bool { return strings.Contains(p.Comm, "codex") },
		parse:       parseCodexPane,
		holdWorking: false,
	},
}

// Detect checks known providers in a pane and returns the normalized status.
func Detect(pane tmux.Pane, pt proc.Table) AgentStatus {
	for _, spec := range providerSpecs {
		found, args := proc.FindInTree(pt, pane.PID, spec.procMatch, proc.ExtractArgsAfterBinary)
		if !found {
			continue
		}
		status := AgentStatus{Provider: spec.provider, Running: true, Args: args}
		content, err := tmux.CapturePaneBottom(pane.ID)
		if err != nil {
			return status
		}
		spec.parse(content, &status)
		return status
	}
	return AgentStatus{}
}

// DetectAll runs provider detection for all panes concurrently.
func DetectAll(sessions []tmux.Session, pt proc.Table) map[string]AgentStatus {
	var mu sync.Mutex
	results := map[string]AgentStatus{}
	var wg sync.WaitGroup

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				wg.Add(1)
				go func(p tmux.Pane) {
					defer wg.Done()
					status := Detect(p, pt)
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

// Reparse re-parses pane content for an existing agent status.
func Reparse(content string, status *AgentStatus) bool {
	spec, ok := providerSpecFor(status.Provider)
	if !ok || spec.parse == nil {
		return false
	}
	spec.parse(content, status)
	return true
}

// ShouldHoldWorking returns true if the provider's working state should be held
// (not immediately decayed to idle).
func ShouldHoldWorking(status AgentStatus) bool {
	spec, ok := providerSpecFor(status.Provider)
	return ok && spec.holdWorking
}

// KnownProviders returns all providers that have detection specs registered.
func KnownProviders() []Provider {
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
