package main

import (
	"os"
	"strings"
	"testing"
)

func TestRenderHarnessDashboard(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print dashboard render output")
	}

	initStyles(DefaultColors())
	m := newDashboardModel()
	m.width = 120
	m.height = 18

	sessions := harnessSessions()
	agents := harnessAgents()
	current := CurrentTarget{Session: "cms", Window: 0, Pane: 1}

	updated, _ := m.Update(stateMsg{sessions: sessions, agents: agents, current: current})
	m = updated

	t.Log("=== dashboard harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessFinder(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print finder render output")
	}

	initStyles(DefaultColors())
	w := &Watcher{}
	w.updateCache(harnessSessions(), harnessAgents(), CurrentTarget{Session: "cms", Window: 0, Pane: 1})

	m := newFinderModel(LoadConfig(), w, finderSessions, 120, 18)
	t.Log("=== finder harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessLive(t *testing.T) {
	if os.Getenv("CMS_LIVE_HARNESS") == "" {
		t.Skip("set CMS_LIVE_HARNESS=1 to print live finder/dashboard render output")
	}

	initStyles(DefaultColors())
	sessions, pt, err := FetchState()
	if err != nil {
		t.Fatalf("FetchState: %v", err)
	}
	agents := detectAllAgents(sessions, pt)
	current, _ := FetchCurrentTarget()

	dash := newDashboardModel()
	dash.width = 140
	dash.height = 24
	updated, _ := dash.Update(stateMsg{sessions: sessions, agents: agents, current: current})
	dash = updated

	w := &Watcher{}
	w.updateCache(sessions, agents, current)
	finder := newFinderModel(LoadConfig(), w, finderSessions, 140, 24)

	t.Logf("live sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)
	t.Log("=== live dashboard ===")
	t.Log("\n" + dash.View())
	t.Log("=== live finder ===")
	t.Log("\n" + finder.View())
}

func harnessSessions() []Session {
	return []Session{
		{
			Name:     "cms",
			Attached: true,
			Windows: []Window{
				{
					Index:  0,
					Name:   "fish",
					Active: true,
					Panes: []Pane{
						{ID: "%1", Index: 0, Command: "cms", WorkingDir: "/Users/serge/projects/cms", Active: false},
						{ID: "%2", Index: 1, Command: "codex", WorkingDir: "/Users/serge/projects/cms", Active: true},
						{ID: "%3", Index: 2, Command: "claude", WorkingDir: "/Users/serge/projects/cms", Active: false},
					},
				},
			},
		},
		{
			Name: "gather_git",
			Windows: []Window{
				{
					Index:  0,
					Name:   "main",
					Active: true,
					Panes: []Pane{
						{ID: "%4", Index: 0, Command: "claude", WorkingDir: "/Users/serge/projects/gather_git", Active: true},
						{ID: "%5", Index: 1, Command: "zsh", WorkingDir: "/Users/serge/projects/gather_git", Active: false},
					},
				},
			},
		},
	}
}

func harnessAgents() map[string]AgentStatus {
	return map[string]AgentStatus{
		"%1": {
			Running:    true,
			Provider:   ProviderClaude,
			Activity:   ActivityIdle,
			Model:      "sonnet",
			ContextPct: 42,
			ContextSet: true,
			Branch:     "feature/refactor",
			Mode:       ModeAcceptEdits,
			ModeLabel:  "accept edits",
		},
		"%2": {
			Running:    true,
			Provider:   ProviderCodex,
			Activity:   ActivityWorking,
			Model:      "gpt-5.4",
			ContextPct: 0,
			ContextSet: true,
			Branch:     "codex/functionality",
			Mode:       ModePlan,
			ModeLabel:  "plan mode",
		},
		"%3": {
			Running:    true,
			Provider:   ProviderCodex,
			Activity:   ActivityWaitingInput,
			Model:      "gpt-5.4",
			ContextPct: 37,
			ContextSet: true,
			Branch:     "codex/functionality",
			Mode:       ModeWorkspaceWrite,
			ModeLabel:  "workspace-write",
		},
		"%4": {
			Running:    true,
			Provider:   ProviderClaude,
			Activity:   ActivityWorking,
			Model:      "sonnet",
			ContextPct: 5,
			ContextSet: true,
			Branch:     "main",
		},
	}
}

func TestRenderHarnessProviderSummaryIncludesZeroContext(t *testing.T) {
	initStyles(DefaultColors())

	out := renderProviderSummary(ProviderCodex, providerSummary{
		total:  1,
		idle:   1,
		maxCtx: 0,
		hasCtx: true,
	})
	if !strings.Contains(out, "0%") {
		t.Fatalf("summary %q missing 0%% context", out)
	}
}
