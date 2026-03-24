package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseClaudePane(t *testing.T) {
	content := strings.Join([]string{
		"Some output",
		"✢ Thinking…",
		"────────────────────────────────────────",
		"❯",
		"Sonnet 4.5 (120k context) | 42% ctx | feature/claude",
		"accept edits on",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderClaude}
	parseClaudePane(content, &status)

	if status.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWorking)
	}
	if status.Mode != ModeAcceptEdits || status.ModeLabel != "accept edits" {
		t.Fatalf("mode = (%v, %q), want accept edits", status.Mode, status.ModeLabel)
	}
	if status.ContextPct != 42 {
		t.Fatalf("context = %d, want 42", status.ContextPct)
	}
	if !status.ContextSet {
		t.Fatal("context should be marked as parsed")
	}
	if status.Branch != "feature/claude" {
		t.Fatalf("branch = %q, want feature/claude", status.Branch)
	}
}

func TestParseCodexPane(t *testing.T) {
	content := strings.Join([]string{
		"Tool call needs your approval.",
		"Enter to approve, Esc to deny",
		"›",
		"model: gpt-5.4",
		"branch: codex/functionality",
		"61% ctx",
		"plan mode",
		"workspace-write",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityWaitingInput {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWaitingInput)
	}
	if status.Mode != ModeWorkspaceWrite || status.ModeLabel != "workspace-write" {
		t.Fatalf("mode = (%v, %q), want workspace-write", status.Mode, status.ModeLabel)
	}
	if status.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want gpt-5.4", status.Model)
	}
	if status.ContextPct != 61 {
		t.Fatalf("context = %d, want 61", status.ContextPct)
	}
	if !status.ContextSet {
		t.Fatal("context should be marked as parsed")
	}
	if status.Branch != "codex/functionality" {
		t.Fatalf("branch = %q, want codex/functionality", status.Branch)
	}
}

func TestParseCodexPaneLeftFooterConvertsToUsedContext(t *testing.T) {
	content := strings.Join([]string{
		"› Implement feature",
		"gpt-5.4 default · 63% left · ~/projects/cms",
		"Plan mode (shift+tab to cycle)",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.ContextPct != 37 {
		t.Fatalf("context = %d, want 37", status.ContextPct)
	}
	if !status.ContextSet {
		t.Fatal("context should be marked as parsed")
	}
}

func TestParseCodexPaneHundredPercentLeftShowsZeroUsedContext(t *testing.T) {
	content := strings.Join([]string{
		"› Ready",
		"gpt-5.4 default · 100% left · ~/projects/cms",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.ContextPct != 0 {
		t.Fatalf("context = %d, want 0", status.ContextPct)
	}
	if !status.ContextSet {
		t.Fatal("zero used context should still be marked as parsed")
	}
}

func TestParseCodexPaneStaleApprovalFallsBackToIdle(t *testing.T) {
	content := strings.Join([]string{
		"Previous step: Tool call needs your approval.",
		"Enter to approve, Esc to deny",
		"older output",
		"older output",
		"older output",
		"›",
		"model: gpt-5.4",
		"branch: codex/functionality",
		"plan mode",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityIdle {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityIdle)
	}
}

func TestParseCodexPaneStaleApprovalFallsBackToWorking(t *testing.T) {
	content := strings.Join([]string{
		"Previous step: Tool call needs your approval.",
		"Enter to approve, Esc to deny",
		"older output",
		"⠋ running task",
		"executing command",
		"›",
		"model: gpt-5.4",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWorking)
	}
}

func TestParseCodexPaneStaleRunningTextFallsBackToIdle(t *testing.T) {
	content := strings.Join([]string{
		"older output",
		"running something earlier",
		"more older output",
		"›",
		"model: gpt-5.4",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityIdle {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityIdle)
	}
}

func TestParseCodexPaneChoiceUINearPromptIsWaiting(t *testing.T) {
	content := strings.Join([]string{
		"Choose an action",
		"❯ 1. Approve",
		"  2. Deny",
		"Enter to select · Esc to cancel",
		"›",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityWaitingInput {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWaitingInput)
	}
}

func TestAgentSummaryMixedProviders(t *testing.T) {
	initStyles(DefaultColors())

	sess := Session{
		Name: "cms",
		Windows: []Window{
			{
				Panes: []Pane{
					{ID: "%1"},
					{ID: "%2"},
					{ID: "%3"},
				},
			},
		},
	}
	agents := map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Activity: ActivityWorking, ContextPct: 40},
		"%2": {Running: true, Provider: ProviderCodex, Activity: ActivityWaitingInput, ContextPct: 65},
		"%3": {Running: true, Provider: ProviderCodex, Activity: ActivityIdle, ContextPct: 12},
	}

	summary := agentSummary(sess, agents)
	if !strings.Contains(summary, "claude") {
		t.Fatalf("summary %q missing claude label", summary)
	}
	if !strings.Contains(summary, "codex") {
		t.Fatalf("summary %q missing codex label", summary)
	}
}

func TestSelectPriorityPaneMixedProviders(t *testing.T) {
	panes := []Pane{{ID: "%1"}, {ID: "%2"}, {ID: "%3"}}
	agents := map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Activity: ActivityIdle},
		"%2": {Running: true, Provider: ProviderCodex, Activity: ActivityWaitingInput},
		"%3": {Running: true, Provider: ProviderCodex, Activity: ActivityWorking},
	}

	if got := selectPriorityPane(panes, []string{"waiting", "idle", "default"}, agents); got != "%2" {
		t.Fatalf("selectPriorityPane = %q, want %%2", got)
	}
}

func TestSelectNextPaneMixedProviders(t *testing.T) {
	all := []jumpCandidate{
		{paneID: "%1", activity: ActivityWorking},
		{paneID: "%2", activity: ActivityIdle},
		{paneID: "%3", activity: ActivityWaitingInput},
	}

	if got := selectNextPane(all, 0); got != "%3" {
		t.Fatalf("selectNextPane = %q, want %%3", got)
	}
}

func TestShouldHoldWorking(t *testing.T) {
	if !shouldHoldWorking(AgentStatus{Provider: ProviderClaude}) {
		t.Fatal("claude should keep working hold behavior")
	}
	if shouldHoldWorking(AgentStatus{Provider: ProviderCodex}) {
		t.Fatal("codex should not keep working hold behavior")
	}
}

func TestKnownProviders(t *testing.T) {
	providers := knownProviders()
	if len(providers) < 2 {
		t.Fatalf("knownProviders() = %v, want at least claude and codex", providers)
	}
	if providers[0] != ProviderClaude || providers[1] != ProviderCodex {
		t.Fatalf("knownProviders() = %v, want [ProviderClaude ProviderCodex ...]", providers)
	}
}

func TestReparseAgentStatusUsesProviderParser(t *testing.T) {
	claude := AgentStatus{Running: true, Provider: ProviderClaude}
	if !reparseAgentStatus(strings.Join([]string{
		"────────────────────────────────────────",
		"❯",
		"Sonnet 4.5 (120k context) | 42% ctx | feature/claude",
	}, "\n"), &claude) {
		t.Fatal("reparseAgentStatus should support claude")
	}
	if claude.ContextPct != 42 {
		t.Fatalf("claude context = %d, want 42", claude.ContextPct)
	}

	codex := AgentStatus{Running: true, Provider: ProviderCodex}
	if !reparseAgentStatus(strings.Join([]string{
		"›",
		"model: gpt-5.4",
		"61% ctx",
	}, "\n"), &codex) {
		t.Fatal("reparseAgentStatus should support codex")
	}
	if codex.ContextPct != 61 {
		t.Fatalf("codex context = %d, want 61", codex.ContextPct)
	}
}

func TestHeldActivityKeepsWorkingDuringLiveOutput(t *testing.T) {
	now := time.Now()
	prev := AgentStatus{Running: true, Provider: ProviderCodex, Activity: ActivityWorking}
	parsed := AgentStatus{Running: true, Provider: ProviderCodex, Activity: ActivityIdle}

	activity := heldActivity("live", prev, parsed, now.Add(-100*time.Millisecond), now.Add(1500*time.Millisecond), now)
	if activity != ActivityWorking {
		t.Fatalf("heldActivity(live) = %v, want %v", activity, ActivityWorking)
	}
}

func TestHeldActivityAllowsSettleToReturnIdle(t *testing.T) {
	now := time.Now()
	prev := AgentStatus{Running: true, Provider: ProviderCodex, Activity: ActivityWorking}
	parsed := AgentStatus{Running: true, Provider: ProviderCodex, Activity: ActivityIdle}

	activity := heldActivity("settle", prev, parsed, now.Add(-400*time.Millisecond), now.Add(1500*time.Millisecond), now)
	if activity != ActivityIdle {
		t.Fatalf("heldActivity(settle) = %v, want %v", activity, ActivityIdle)
	}
}
