package main

import (
	"strings"
	"sync"
)

// providerSpec defines how to detect and parse a specific agent provider
// from tmux pane content and process trees.
type providerSpec struct {
	provider    Provider
	procMatch   func(procEntry) bool
	parse       func(string, *AgentStatus)
	holdWorking bool
}

var providerSpecs = []providerSpec{
	{
		provider:    ProviderClaude,
		procMatch:   func(p procEntry) bool { return strings.Contains(p.comm, "claude") },
		parse:       parseClaudePane,
		holdWorking: true,
	},
	{
		provider:    ProviderCodex,
		procMatch:   func(p procEntry) bool { return strings.Contains(p.comm, "codex") },
		parse:       parseCodexPane,
		holdWorking: false,
	},
}

// DetectAgent checks known providers in a pane and returns the normalized status.
func DetectAgent(pane Pane, pt procTable) AgentStatus {
	for _, spec := range providerSpecs {
		found, args := findProcessInTree(pt, pane.PID, spec.procMatch, extractArgsAfterBinary)
		if !found {
			continue
		}
		status := AgentStatus{Provider: spec.provider, Running: true, Args: args}
		content, err := capturePaneBottom(pane.ID)
		if err != nil {
			return status
		}
		spec.parse(content, &status)
		return status
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

// capturePaneBottom captures the visible content of a tmux pane.
func capturePaneBottom(paneID string) (string, error) {
	return runTmux("capture-pane", "-t", paneID, "-p", "-J")
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

// extractArgsAfterBinary strips the binary name from a full command line
// and returns just the arguments.
func extractArgsAfterBinary(fullArgs string) string {
	parts := strings.Fields(fullArgs)
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[1:], " ")
}

func isShellCommand(cmd string) bool {
	switch cmd {
	case "fish", "bash", "zsh", "sh", "dash", "tcsh", "ksh":
		return true
	default:
		return false
	}
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
