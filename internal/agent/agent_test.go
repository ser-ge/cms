package agent

import (
	"strings"
	"testing"
)

func TestParseClaudePane(t *testing.T) {
	content := strings.Join([]string{
		"Some output",
		"\u2722 Thinking\u2026",
		"\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500",
		"\u276f",
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
		"\u203a",
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
		"\u203a Implement feature",
		"gpt-5.4 default \u00b7 63% left \u00b7 ~/projects/cms",
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
		"\u203a Ready",
		"gpt-5.4 default \u00b7 100% left \u00b7 ~/projects/cms",
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
		"\u203a",
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
		"\u280b running task",
		"executing command",
		"\u203a",
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
		"\u203a",
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
		"\u276f 1. Approve",
		"  2. Deny",
		"Enter to select \u00b7 Esc to cancel",
		"\u203a",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityWaitingInput {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWaitingInput)
	}
}

func TestParseCodexPaneWorkingIndicatorBetweenPrompts(t *testing.T) {
	// Real Codex output: "Working (47s • esc to interrupt)" appears between
	// an old prompt and a new prompt, far from the last › line.
	content := strings.Join([]string{
		"\u203a run sleep for 120",
		"",
		"\u2022 Running sleep 120 in the shell now.",
		"",
		"\u2022 Working (47s \u2022 esc to interrupt) \u00b7 1 background terminal running",
		"",
		"\u203a Summarize recent commits",
		"",
		"  gpt-5.4 default \u00b7 100% left \u00b7 ~/projects/cms",
	}, "\n")

	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane(content, &status)

	if status.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityWorking)
	}
}

func TestParseClaudePaneUnknownDefaultsToIdle(t *testing.T) {
	status := AgentStatus{Running: true, Provider: ProviderClaude}
	parseClaudePane("older output only\nno prompt visible", &status)

	if status.Activity != ActivityIdle {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityIdle)
	}
}

func TestParseCodexPaneUnknownDefaultsToIdle(t *testing.T) {
	status := AgentStatus{Running: true, Provider: ProviderCodex}
	parseCodexPane("older output only\nno prompt visible", &status)

	if status.Activity != ActivityIdle {
		t.Fatalf("activity = %v, want %v", status.Activity, ActivityIdle)
	}
}

func TestParseClaudePaneSpinnerFarAbovePrompt(t *testing.T) {
	// When Claude outputs many lines of text, the spinner can be 20+ lines
	// above the prompt. The detection window must be wide enough to find it.
	lines := []string{"Some earlier output", "✢ Writing…"}
	// Add 25 lines of generated text between spinner and prompt.
	for i := 0; i < 25; i++ {
		lines = append(lines, "Lorem ipsum dolor sit amet, consectetur adipiscing elit.")
	}
	lines = append(lines,
		"────────────────────────────────────────",
		"❯",
		"Sonnet 4.5 (120k context) | 42% ctx | main",
	)
	content := strings.Join(lines, "\n")

	status := AgentStatus{Running: true, Provider: ProviderClaude}
	parseClaudePane(content, &status)

	if status.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working (spinner 25 lines above prompt)", status.Activity)
	}
}

func TestShouldHoldWorking(t *testing.T) {
	if !ShouldHoldWorking(AgentStatus{Provider: ProviderClaude}) {
		t.Fatal("claude should keep working hold behavior")
	}
	if ShouldHoldWorking(AgentStatus{Provider: ProviderCodex}) {
		t.Fatal("codex should not keep working hold behavior")
	}
}

func TestKnownProviders(t *testing.T) {
	providers := KnownProviders()
	if len(providers) < 2 {
		t.Fatalf("KnownProviders() = %v, want at least claude and codex", providers)
	}
	if providers[0] != ProviderClaude || providers[1] != ProviderCodex {
		t.Fatalf("KnownProviders() = %v, want [ProviderClaude ProviderCodex ...]", providers)
	}
}

func TestReparseAgentStatusUsesProviderParser(t *testing.T) {
	claude := AgentStatus{Running: true, Provider: ProviderClaude}
	if !Reparse(strings.Join([]string{
		"\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500",
		"\u276f",
		"Sonnet 4.5 (120k context) | 42% ctx | feature/claude",
	}, "\n"), &claude) {
		t.Fatal("Reparse should support claude")
	}
	if claude.ContextPct != 42 {
		t.Fatalf("claude context = %d, want 42", claude.ContextPct)
	}

	codex := AgentStatus{Running: true, Provider: ProviderCodex}
	if !Reparse(strings.Join([]string{
		"\u203a",
		"model: gpt-5.4",
		"61% ctx",
	}, "\n"), &codex) {
		t.Fatal("Reparse should support codex")
	}
	if codex.ContextPct != 61 {
		t.Fatalf("codex context = %d, want 61", codex.ContextPct)
	}
}

func TestApplyAgentUpdatesHookPrecedence(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityWorking,
			Source:   SourceHook,
			ToolName: "Edit",
		},
	}

	// Observer tries to overwrite with idle — should be rejected,
	// but observer's model/context fields should merge.
	updates := map[string]AgentStatus{
		"%1": {
			Running:    true,
			Provider:   ProviderClaude,
			Activity:   ActivityIdle,
			Source:     SourceObserver,
			Model:      "Opus 4.6 (1M context)",
			ContextPct: 42,
			ContextSet: true,
			Branch:     "main",
			ModeLabel:  "yolo",
			Mode:       ModeBypassPermissions,
		},
	}

	result := ApplyUpdates(dst, updates)
	got := result["%1"]

	// Activity should stay as hook's value.
	if got.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working (hook should win)", got.Activity)
	}
	if got.Source != SourceHook {
		t.Fatalf("source = %v, want SourceHook", got.Source)
	}
	if got.ToolName != "Edit" {
		t.Fatalf("toolName = %q, want Edit (hook field preserved)", got.ToolName)
	}

	// Observer fields should merge in.
	if got.Model != "Opus 4.6 (1M context)" {
		t.Fatalf("model = %q, want merged from observer", got.Model)
	}
	if got.ContextPct != 42 || !got.ContextSet {
		t.Fatalf("context = %d/%v, want 42/true", got.ContextPct, got.ContextSet)
	}
	if got.Branch != "main" {
		t.Fatalf("branch = %q, want main", got.Branch)
	}
	if got.ModeLabel != "yolo" {
		t.Fatalf("mode = %q, want yolo", got.ModeLabel)
	}
}

func TestApplyAgentUpdatesHookOverwritesObserver(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityIdle,
			Source:   SourceObserver,
		},
	}

	// Hook update should fully overwrite observer data.
	updates := map[string]AgentStatus{
		"%1": {
			Running:  true,
			Provider: ProviderClaude,
			Activity: ActivityWorking,
			Source:   SourceHook,
			ToolName: "Bash",
		},
	}

	result := ApplyUpdates(dst, updates)
	got := result["%1"]

	if got.Activity != ActivityWorking {
		t.Fatalf("activity = %v, want Working", got.Activity)
	}
	if got.Source != SourceHook {
		t.Fatalf("source = %v, want SourceHook", got.Source)
	}
	if got.ToolName != "Bash" {
		t.Fatalf("toolName = %q, want Bash", got.ToolName)
	}
}

func TestApplyAgentUpdatesDeleteOnNotRunning(t *testing.T) {
	dst := map[string]AgentStatus{
		"%1": {Running: true, Provider: ProviderClaude, Source: SourceHook},
	}

	updates := map[string]AgentStatus{
		"%1": {Running: false},
	}

	result := ApplyUpdates(dst, updates)
	if _, ok := result["%1"]; ok {
		t.Fatal("pane should be deleted when Running=false")
	}
}
