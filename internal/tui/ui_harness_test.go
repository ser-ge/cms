package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/attention"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/watcher"
)

func TestRenderHarnessDashboard(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print dashboard render output")
	}

	cfg := config.DefaultConfig()
	InitStyles(cfg)
	m := newDashboardModel(cfg)
	m.width = 120
	m.height = 18

	sessions := harnessSessions()
	agents := harnessAgents()
	current := tmux.CurrentTarget{Session: "cms", Window: 0, Pane: 1}

	updated, _ := m.Update(watcher.StateMsg{Sessions: sessions, Agents: agents, Current: current})
	m = updated

	t.Log("=== dashboard harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessFinder(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print finder render output")
	}

	cfg := config.DefaultConfig()
	InitStyles(cfg)
	w := harnessWatcher()

	m := newFinderModel(cfg, w, FinderSessions, 120, 18)
	t.Log("=== finder harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessQueue(t *testing.T) {
	if os.Getenv("CMS_RENDER_HARNESS") == "" {
		t.Skip("set CMS_RENDER_HARNESS=1 to print queue render output")
	}

	cfg := config.DefaultConfig()
	InitStyles(cfg)
	w := harnessWatcher()

	m := newQueueModel(cfg, w, 120, 18)
	t.Log("=== queue harness ===")
	t.Log("\n" + m.View())
}

func TestRenderHarnessLive(t *testing.T) {
	if os.Getenv("CMS_LIVE_HARNESS") == "" {
		t.Skip("set CMS_LIVE_HARNESS=1 to print live finder/dashboard render output")
	}

	cfg := config.Load()
	InitStyles(cfg)
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		t.Fatalf("FetchState: %v", err)
	}
	agents := agent.DetectAll(sessions, pt)
	current, _ := tmux.FetchCurrentTarget()

	dash := newDashboardModel(cfg)
	dash.width = 140
	dash.height = 24
	updated, _ := dash.Update(watcher.StateMsg{Sessions: sessions, Agents: agents, Current: current})
	dash = updated

	w := watcher.New()
	w.UpdateCacheForTest(sessions, agents, current)
	// Seed activitySince from current state.
	now := time.Now()
	for id := range agents {
		w.SetActivitySinceForTest(id, now)
		if agents[id].Activity == agent.ActivityWaitingInput {
			w.Attention.Add(id, attention.Waiting)
		}
	}
	finder := newFinderModel(cfg, w, FinderSessions, 140, 24)
	queue := newQueueModel(cfg, w, 140, 24)

	t.Logf("live sessions=%d agents=%d current=%s:%d.%d", len(sessions), len(agents), current.Session, current.Window, current.Pane)
	t.Log("=== live dashboard ===")
	t.Log("\n" + dash.View())
	t.Log("=== live finder ===")
	t.Log("\n" + finder.View())
	t.Log("=== live queue ===")
	t.Log("\n" + queue.View())
}

func harnessSessions() []tmux.Session {
	return []tmux.Session{
		{
			Name:     "cms",
			Attached: true,
			Windows: []tmux.Window{
				{
					Index:  0,
					Name:   "fish",
					Active: true,
					Panes: []tmux.Pane{
						{ID: "%1", Index: 0, Command: "cms", WorkingDir: "/Users/serge/projects/cms", Active: false,
							Git: git.Info{IsRepo: true, Branch: "feature/refactor", RepoName: "cms", Dirty: true}},
						{ID: "%2", Index: 1, Command: "codex", WorkingDir: "/Users/serge/projects/cms", Active: true,
							Git: git.Info{IsRepo: true, Branch: "codex/functionality", RepoName: "cms"}},
						{ID: "%3", Index: 2, Command: "claude", WorkingDir: "/Users/serge/projects/cms", Active: false,
							Git: git.Info{IsRepo: true, Branch: "codex/functionality", RepoName: "cms"}},
					},
				},
			},
		},
		{
			Name: "gather_git",
			Windows: []tmux.Window{
				{
					Index:  0,
					Name:   "main",
					Active: true,
					Panes: []tmux.Pane{
						{ID: "%4", Index: 0, Command: "claude", WorkingDir: "/Users/serge/projects/gather_git", Active: true,
							Git: git.Info{IsRepo: true, Branch: "main", RepoName: "gather_git"}},
						{ID: "%5", Index: 1, Command: "zsh", WorkingDir: "/Users/serge/projects/gather_git", Active: false,
							Git: git.Info{IsRepo: true, Branch: "main", RepoName: "gather_git"}},
					},
				},
			},
		},
	}
}

// harnessWatcher builds a Watcher pre-populated with harness data,
// including activitySince timestamps and attention events.
func harnessWatcher() *watcher.Watcher {
	w := watcher.New()
	sessions := harnessSessions()
	agents := harnessAgents()
	w.UpdateCacheForTest(sessions, agents, tmux.CurrentTarget{Session: "cms", Window: 0, Pane: 1})

	// Seed activitySince with staggered times so the queue shows varied durations.
	now := time.Now()
	w.SetActivitySinceForTest("%1", now.Add(-8*time.Minute))  // idle 8m
	w.SetActivitySinceForTest("%2", now.Add(-15*time.Second))  // working 15s
	w.SetActivitySinceForTest("%3", now.Add(-2*time.Minute))   // waiting 2m
	w.SetActivitySinceForTest("%4", now.Add(-45*time.Second))  // working 45s

	// Seed attention: %3 is waiting, %1 just finished (was working, now idle).
	w.Attention.Add("%3", attention.Waiting)
	w.Attention.Add("%1", attention.Finished)

	return w
}

func harnessAgents() map[string]agent.AgentStatus {
	return map[string]agent.AgentStatus{
		"%1": {
			Running:    true,
			Provider:   agent.ProviderClaude,
			Activity:   agent.ActivityIdle,
			Model:      "sonnet",
			ContextPct: 42,
			ContextSet: true,
			Branch:     "feature/refactor",
			Mode:       agent.ModeAcceptEdits,
			ModeLabel:  "accept edits",
		},
		"%2": {
			Running:    true,
			Provider:   agent.ProviderCodex,
			Activity:   agent.ActivityWorking,
			Model:      "gpt-5.4",
			ContextPct: 0,
			ContextSet: true,
			Branch:     "codex/functionality",
			Mode:       agent.ModePlan,
			ModeLabel:  "plan mode",
		},
		"%3": {
			Running:    true,
			Provider:   agent.ProviderCodex,
			Activity:   agent.ActivityWaitingInput,
			Model:      "gpt-5.4",
			ContextPct: 37,
			ContextSet: true,
			Branch:     "codex/functionality",
			Mode:       agent.ModeWorkspaceWrite,
			ModeLabel:  "workspace-write",
		},
		"%4": {
			Running:    true,
			Provider:   agent.ProviderClaude,
			Activity:   agent.ActivityWorking,
			Model:      "sonnet",
			ContextPct: 5,
			ContextSet: true,
			Branch:     "main",
		},
	}
}

func TestRenderHarnessProviderSummaryIncludesZeroContext(t *testing.T) {
	cfg := config.DefaultConfig()
	InitStyles(cfg)

	out := renderProviderSummary(agent.ProviderCodex, providerSummary{
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
	cfg := config.DefaultConfig()
	InitStyles(cfg)

	totalOnly := cfg
	totalOnly.Finder.StateOrder = []string{"total"}
	totalOnly.Finder.ShowContextPercentage = false
	out := renderProviderSummary(agent.ProviderCodex, providerSummary{total: 3, idle: 1, working: 1, waiting: 1, maxCtx: 37, hasCtx: true}, totalOnly.Finder)
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
	cfg := config.DefaultConfig()
	cfg.Dashboard.Columns = []string{"name", "activity", "context"}
	cfg.Dashboard.WindowHeaders = "never"
	cfg.Dashboard.FooterPadding = false
	cfg.Dashboard.FooterSeparator = false
	InitStyles(cfg)

	m := newDashboardModel(cfg)
	m.width = 100
	m.height = 12
	updated, _ := m.Update(watcher.StateMsg{Sessions: harnessSessions(), Agents: harnessAgents(), Current: tmux.CurrentTarget{Session: "cms", Window: 0, Pane: 1}})
	m = updated
	view := m.View()
	if strings.Contains(view, "fish*") {
		t.Fatalf("dashboard view unexpectedly contains window header: %q", view)
	}
	if !strings.Contains(view, "idle") || !strings.Contains(view, "42%") {
		t.Fatalf("dashboard view missing configured columns: %q", view)
	}
}
