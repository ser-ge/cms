package tui

import (
	"strings"
	"testing"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/tmux"
)

func TestAgentSummaryMixedProviders(t *testing.T) {
	InitStyles(config.DefaultConfig())

	sess := tmux.Session{
		Name: "cms",
		Windows: []tmux.Window{
			{
				Panes: []tmux.Pane{
					{ID: "%1"},
					{ID: "%2"},
					{ID: "%3"},
				},
			},
		},
	}
	agents := map[string]agent.AgentStatus{
		"%1": {Running: true, Provider: agent.ProviderClaude, Activity: agent.ActivityWorking, ContextPct: 40},
		"%2": {Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityWaitingInput, ContextPct: 65},
		"%3": {Running: true, Provider: agent.ProviderCodex, Activity: agent.ActivityIdle, ContextPct: 12},
	}

	m := finderModel{cfg: config.DefaultConfig()}
	summary := m.agentSummary(sess, agents)
	if !strings.Contains(summary, "claude") {
		t.Fatalf("summary %q missing claude label", summary)
	}
	if !strings.Contains(summary, "codex") {
		t.Fatalf("summary %q missing codex label", summary)
	}
}
