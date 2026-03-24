package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRenderHarnessDashboard(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print dashboard render output")
	}

	cfg := DefaultConfig()
	initStyles(cfg)
	m := newDashboardModel(cfg)
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

	cfg := DefaultConfig()
	initStyles(cfg)
	w := harnessWatcher()

	m := newFinderModel(cfg, w, finderSessions, 120, 18)
	t.Log("=== finder harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessQueue(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print queue render output")
	}

	cfg := DefaultConfig()
	initStyles(cfg)
	w := harnessWatcher()

	m := newQueueModel(cfg, w, 120, 18)
	t.Log("=== queue harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessLive(t *testing.T) {
	if os.Getenv("CMS_LIVE_HARNESS") == "" {
		t.Skip("set CMS_LIVE_HARNESS=1 to print live finder/dashboard render output")
	}

	cfg := LoadConfig()
	initStyles(cfg)
	sessions, pt, err := FetchState()
	if err != nil {
		t.Fatalf("FetchState: %v", err)
	}
	agents := detectAllAgents(sessions, pt)
	current, _ := FetchCurrentTarget()

	dash := newDashboardModel(cfg)
	dash.width = 140
	dash.height = 24
	updated, _ := dash.Update(stateMsg{sessions: sessions, agents: agents, current: current})
	dash = updated

	w := NewWatcher()
	w.updateCache(sessions, agents, current)
	// Seed activitySince from current state.
	now := time.Now()
	for id := range agents {
		w.activitySince[id] = now
		if agents[id].Activity == ActivityWaitingInput {
			w.Attention.Add(id, AttentionWaiting)
		}
	}
	finder := newFinderModel(cfg, w, finderSessions, 140, 24)
	queue := newQueueModel(cfg, w, 140, 24)

	t.Logf("live sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)
	t.Log("=== live dashboard ===")
	t.Log("\n" + dash.View())
	t.Log("=== live finder ===")
	t.Log("\n" + finder.View())
	t.Log("=== live queue ===")
	t.Log("\n" + queue.View())
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
						{ID: "%1", Index: 0, Command: "cms", WorkingDir: "/Users/serge/projects/cms", Active: false,
							Git: GitInfo{IsRepo: true, Branch: "feature/refactor", RepoName: "cms", Dirty: true}},
						{ID: "%2", Index: 1, Command: "codex", WorkingDir: "/Users/serge/projects/cms", Active: true,
							Git: GitInfo{IsRepo: true, Branch: "codex/functionality", RepoName: "cms"}},
						{ID: "%3", Index: 2, Command: "claude", WorkingDir: "/Users/serge/projects/cms", Active: false,
							Git: GitInfo{IsRepo: true, Branch: "codex/functionality", RepoName: "cms"}},
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
						{ID: "%4", Index: 0, Command: "claude", WorkingDir: "/Users/serge/projects/gather_git", Active: true,
							Git: GitInfo{IsRepo: true, Branch: "main", RepoName: "gather_git"}},
						{ID: "%5", Index: 1, Command: "zsh", WorkingDir: "/Users/serge/projects/gather_git", Active: false,
							Git: GitInfo{IsRepo: true, Branch: "main", RepoName: "gather_git"}},
					},
				},
			},
		},
	}
}

// harnessWatcher builds a Watcher pre-populated with harness data,
// including activitySince timestamps and attention events.
func harnessWatcher() *Watcher {
	w := NewWatcher()
	sessions := harnessSessions()
	agents := harnessAgents()
	w.updateCache(sessions, agents, CurrentTarget{Session: "cms", Window: 0, Pane: 1})

	// Seed activitySince with staggered times so the queue shows varied durations.
	now := time.Now()
	w.activitySince["%1"] = now.Add(-8 * time.Minute)  // idle 8m
	w.activitySince["%2"] = now.Add(-15 * time.Second)  // working 15s
	w.activitySince["%3"] = now.Add(-2 * time.Minute)   // waiting 2m
	w.activitySince["%4"] = now.Add(-45 * time.Second)  // working 45s

	// Seed attention: %3 is waiting, %1 just finished (was working, now idle).
	w.Attention.Add("%3", AttentionWaiting)
	w.Attention.Add("%1", AttentionFinished)

	return w
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
	cfg := DefaultConfig()
	initStyles(cfg)

	out := renderProviderSummary(ProviderCodex, providerSummary{
		total:  1,
		idle:   1,
		maxCtx: 0,
		hasCtx: true,
	}, cfg.Finder)
	if !strings.Contains(out, "0%") {
		t.Fatalf("summary %q missing 0%% context", out)
	}
}

func TestRenderHarnessFinderSummaryConfigVariants(t *testing.T) {
	cfg := DefaultConfig()
	initStyles(cfg)

	totalOnly := cfg
	totalOnly.Finder.StateOrder = []string{"total"}
	totalOnly.Finder.ShowContextPercentage = false
	out := renderProviderSummary(ProviderCodex, providerSummary{total: 3, idle: 1, working: 1, waiting: 1, maxCtx: 37, hasCtx: true}, totalOnly.Finder)
	if !strings.Contains(out, "3") || strings.Contains(out, "37%") {
		t.Fatalf("total-only summary = %q, want total without context", out)
	}

	noProviders := cfg
	noProviders.Finder.ProviderOrder = []string{}
	m := finderModel{cfg: noProviders}
	if got := m.agentSummary(harnessSessions()[0], harnessAgents()); got != "" {
		t.Fatalf("agentSummary with no providers = %q, want empty", got)
	}
}

func TestRenderHarnessDashboardConfigVariants(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Dashboard.Columns = []string{"name", "activity", "context"}
	cfg.Dashboard.WindowHeaders = "never"
	cfg.Dashboard.FooterPadding = false
	cfg.Dashboard.FooterSeparator = false
	initStyles(cfg)

	m := newDashboardModel(cfg)
	m.width = 100
	m.height = 12
	updated, _ := m.Update(stateMsg{sessions: harnessSessions(), agents: harnessAgents(), current: CurrentTarget{Session: "cms", Window: 0, Pane: 1}})
	m = updated
	view := m.View()
	if strings.Contains(view, "fish*") {
		t.Fatalf("dashboard view unexpectedly contains window header: %q", view)
	}
	if !strings.Contains(view, "idle") || !strings.Contains(view, "42%") {
		t.Fatalf("dashboard view missing configured columns: %q", view)
	}
}
