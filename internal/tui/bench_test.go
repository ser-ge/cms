package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/watcher"
)

// --- Test data generators ---

func generatePickerItems(n int) []PickerItem {
	items := make([]PickerItem, n)
	for i := range items {
		name := fmt.Sprintf("session-%d", i)
		items[i] = PickerItem{
			Title:       name,
			Description: fmt.Sprintf("%dw · 2 working", i%5+1),
			FilterValue: name + " /Users/serge/projects/" + name,
			Active:      i%3 == 0,
			Icon:        "●",
		}
	}
	return items
}

func generateSessions(n int, panesPerWin int) []tmux.Session {
	sessions := make([]tmux.Session, n)
	for i := range sessions {
		panes := make([]tmux.Pane, panesPerWin)
		for j := range panes {
			panes[j] = tmux.Pane{
				ID:         fmt.Sprintf("%%%d", i*panesPerWin+j),
				Index:      j,
				Command:    "zsh",
				WorkingDir: fmt.Sprintf("/Users/serge/projects/proj-%d", i),
			}
		}
		sessions[i] = tmux.Session{
			Name:     fmt.Sprintf("session-%d", i),
			Attached: i == 0,
			Windows: []tmux.Window{
				{
					Index: 0,
					Name:  "main",
					Panes: panes,
				},
			},
		}
	}
	return sessions
}

func generateAgents(sessions []tmux.Session) map[string]agent.AgentStatus {
	agents := make(map[string]agent.AgentStatus)
	activities := []agent.Activity{
		agent.ActivityWorking,
		agent.ActivityWaitingInput,
		agent.ActivityCompleted,
		agent.ActivityIdle,
	}
	for si, sess := range sessions {
		for _, win := range sess.Windows {
			for pi, pane := range win.Panes {
				if (si+pi)%2 == 0 { // half the panes have agents
					agents[pane.ID] = agent.AgentStatus{
						Running:    true,
						Provider:   agent.ProviderClaude,
						Activity:   activities[(si+pi)%len(activities)],
						ContextPct: (si*10 + pi*5) % 100,
						ContextSet: true,
						ModeLabel:  "workspace-write",
					}
				}
			}
		}
	}
	return agents
}

func benchCfg() config.Config {
	cfg := config.DefaultConfig()
	InitStyles(cfg)
	return cfg
}

// --- Picker benchmarks ---

func BenchmarkPickerNew(b *testing.B) {
	for _, n := range []int{10, 50, 200, 1000} {
		items := generatePickerItems(n)
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			for b.Loop() {
				newPicker("test", items, "jk", 150)
			}
		})
	}
}

func BenchmarkPickerApplyFilter(b *testing.B) {
	for _, n := range []int{10, 50, 200, 1000} {
		items := generatePickerItems(n)
		b.Run(fmt.Sprintf("items=%d/short_query", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.input.SetValue("ses")
			b.ResetTimer()
			for b.Loop() {
				p.applyFilter()
			}
		})
		b.Run(fmt.Sprintf("items=%d/multi_token", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.input.SetValue("session proj")
			b.ResetTimer()
			for b.Loop() {
				p.applyFilter()
			}
		})
		b.Run(fmt.Sprintf("items=%d/no_match", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.input.SetValue("zzzznotfound")
			b.ResetTimer()
			for b.Loop() {
				p.applyFilter()
			}
		})
	}
}

func BenchmarkPickerResetWith(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		items := generatePickerItems(n)
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.width = 120
			p.height = 40
			p.input.SetValue("ses")
			p.applyFilter()
			b.ResetTimer()
			for b.Loop() {
				p.resetWith(items, "jk", 150)
			}
		})
	}
}

func BenchmarkPickerView(b *testing.B) {
	benchCfg()
	for _, n := range []int{10, 50, 200} {
		items := generatePickerItems(n)
		b.Run(fmt.Sprintf("items=%d/no_filter", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.width = 120
			p.height = 40
			b.ResetTimer()
			for b.Loop() {
				_ = p.View()
			}
		})
		b.Run(fmt.Sprintf("items=%d/with_filter", n), func(b *testing.B) {
			p := newPicker("test", items, "jk", 150)
			p.width = 120
			p.height = 40
			p.input.SetValue("ses")
			p.applyFilter()
			b.ResetTimer()
			for b.Loop() {
				_ = p.View()
			}
		})
	}
}

func BenchmarkHighlightMatches(b *testing.B) {
	benchCfg()
	s := "session-42 /Users/serge/projects/session-42"
	idxs := []int{0, 1, 2, 7, 8, 30, 31, 32}
	b.Run("8_matches", func(b *testing.B) {
		for b.Loop() {
			_ = highlightMatches(s, idxs, nil)
		}
	})
	b.Run("no_matches", func(b *testing.B) {
		for b.Loop() {
			_ = highlightMatches(s, nil, nil)
		}
	})
}

// --- Finder benchmarks ---

// stubFinderModel builds a finderModel without a watcher (using pre-populated data).
func stubFinderModel(cfg config.Config, sessions []tmux.Session, agents map[string]agent.AgentStatus, sections []string) finderModel {
	w := watcher.New()
	m := finderModel{
		sections:  sections,
		cfg:       cfg,
		width:     120,
		height:    40,
		sessData:  sessions,
		agentData: agents,
		watcher:   w,
	}

	want := sectionSet(sections)

	if want["sessions"] {
		m.buildSessionItems(agents)
		m.hasSess = true
	} else {
		m.hasSess = true
	}
	if want["agents"] {
		m.buildAgentsQueueItems()
		m.hasAgentsQueue = true
	} else {
		m.hasAgentsQueue = true
	}
	if want["panes"] {
		m.buildPaneItems()
		m.hasPane = true
	} else {
		m.hasPane = true
	}
	if want["windows"] {
		m.buildWindowItems()
		m.hasWindow = true
	} else {
		m.hasWindow = true
	}

	// Mark unwanted sections as done.
	if !want["projects"] {
		m.hasProj = true
	}
	if !want["worktrees"] {
		m.hasWorktree = true
	}
	if !want["branches"] {
		m.hasBranch = true
	}
	if !want["marks"] {
		m.hasMark = true
	}

	m.rebuildPicker()
	return m
}

func BenchmarkFinderBuildSessionItems(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			m := finderModel{cfg: cfg, sessData: sessions}
			b.ResetTimer()
			for b.Loop() {
				m.buildSessionItems(agents)
			}
		})
	}
}

func BenchmarkFinderBuildAgentsQueueItems(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			w := watcher.New()
			m := finderModel{cfg: cfg, sessData: sessions, agentData: agents, watcher: w}
			b.ResetTimer()
			for b.Loop() {
				m.buildAgentsQueueItems()
			}
		})
	}
}

func BenchmarkFinderBuildPaneItems(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			m := finderModel{cfg: cfg, sessData: sessions, agentData: agents}
			b.ResetTimer()
			for b.Loop() {
				m.buildPaneItems()
			}
		})
	}
}

func BenchmarkFinderBuildWindowItems(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			m := finderModel{cfg: cfg, sessData: sessions, agentData: agents}
			b.ResetTimer()
			for b.Loop() {
				m.buildWindowItems()
			}
		})
	}
}

func BenchmarkFinderRebuildPicker(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d/sessions_only", n), func(b *testing.B) {
			m := stubFinderModel(cfg, sessions, agents, []string{"sessions"})
			b.ResetTimer()
			for b.Loop() {
				m.rebuildPicker()
			}
		})
		b.Run(fmt.Sprintf("sessions=%d/all_sections", n), func(b *testing.B) {
			m := stubFinderModel(cfg, sessions, agents, []string{"sessions", "agents", "panes", "windows"})
			b.ResetTimer()
			for b.Loop() {
				m.rebuildPicker()
			}
		})
	}
}

func BenchmarkFinderView(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			m := stubFinderModel(cfg, sessions, agents, []string{"sessions", "agents", "panes", "windows"})
			b.ResetTimer()
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}

// BenchmarkFinderFullRefresh simulates what happens on a watcher StateMsg:
// rebuild all item lists + rebuild picker + render.
func BenchmarkFinderFullRefresh(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{5, 20, 50} {
		sessions := generateSessions(n, 3)
		agents := generateAgents(sessions)
		b.Run(fmt.Sprintf("sessions=%d", n), func(b *testing.B) {
			m := stubFinderModel(cfg, sessions, agents, []string{"sessions", "agents", "panes", "windows"})
			b.ResetTimer()
			for b.Loop() {
				m.buildSessionItems(agents)
				m.buildAgentsQueueItems()
				m.buildPaneItems()
				m.buildWindowItems()
				m.rebuildPicker()
				_ = m.View()
			}
		})
	}
}

// --- Sorting benchmarks ---

func BenchmarkSortedSectionItems(b *testing.B) {
	cfg := benchCfg()
	for _, n := range []int{10, 50, 200} {
		items := generatePickerItems(n)
		idx := make([]finderEntry, n)
		for i := range idx {
			idx[i] = finderEntry{kind: KindSession, sessionName: items[i].Title}
		}
		m := finderModel{cfg: cfg}
		isCurrent := func(i int) bool { return i == 0 }
		isRecent := func(i int) bool { return i == 1 }
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			for b.Loop() {
				m.sortedSectionItems(items, idx, "sessions", isCurrent, isRecent)
			}
		})
	}
}

// --- String building benchmarks ---

func BenchmarkJoinParts(b *testing.B) {
	parts := []string{"3w", "2 working", "attached"}
	for b.Loop() {
		_ = JoinParts(parts)
	}
}

func BenchmarkBuildAgentsQueueDescription(b *testing.B) {
	benchCfg()
	cs := agent.AgentStatus{
		Running:    true,
		Provider:   agent.ProviderClaude,
		Activity:   agent.ActivityWorking,
		ContextPct: 65,
		ContextSet: true,
		ModeLabel:  "workspace-write",
	}
	for b.Loop() {
		_ = buildAgentsQueueDescription(cs, 5*60*1e9, true)
	}
}

func BenchmarkBuildPaneDescription(b *testing.B) {
	benchCfg()
	cs := agent.AgentStatus{
		Running:    true,
		Provider:   agent.ProviderClaude,
		Activity:   agent.ActivityWaitingInput,
		ContextPct: 42,
		ContextSet: true,
		ModeLabel:  "plan",
	}
	for b.Loop() {
		_ = buildPaneDescription("~/p/c/feature", cs, 25)
	}
}

func BenchmarkRenderStateCounts(b *testing.B) {
	benchCfg()
	counts := map[agent.Activity]int{
		agent.ActivityWorking:      3,
		agent.ActivityWaitingInput: 2,
		agent.ActivityCompleted:    1,
		agent.ActivityIdle:         1,
	}
	for b.Loop() {
		_ = renderStateCounts(counts, []string{"waiting", "completed", "idle", "working"})
	}
}

// --- Utility benchmarks ---

func BenchmarkCompactPath(b *testing.B) {
	path := "~/projects/cms/worktrees/feature-branch"
	b.Run("needs_compact", func(b *testing.B) {
		for b.Loop() {
			_ = CompactPath(path, 25)
		}
	})
	b.Run("already_short", func(b *testing.B) {
		for b.Loop() {
			_ = CompactPath("~/notes", 25)
		}
	})
}

func BenchmarkNormalizeName(b *testing.B) {
	for b.Loop() {
		_ = normalizeName("my.project:name")
	}
}

func BenchmarkSectionSet(b *testing.B) {
	sections := []string{"sessions", "projects", "agents", "worktrees", "branches", "panes", "windows", "marks"}
	for b.Loop() {
		_ = sectionSet(sections)
	}
}

// --- Title padding benchmark (rebuildPicker inner loop) ---

func BenchmarkTitlePadding(b *testing.B) {
	items := generatePickerItems(200)
	b.ResetTimer()
	for b.Loop() {
		maxW := 0
		for i := range items {
			if w := len(items[i].Title); w > maxW {
				maxW = w
			}
		}
		for i := range items {
			if w := len(items[i].Title); w < maxW {
				items[i].Title += strings.Repeat(" ", maxW-w)
			}
		}
	}
}
