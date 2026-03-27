# Agent Harness

## Purpose

The harness is the current test/debug layer for exercising the watcher against a real tmux environment while keeping artifacts for later inspection and replay work.

It exists to answer two questions:

1. What signals did the watcher/business layer actually receive?
2. What was visible in the pane when those signals happened?

Right now the harness is focused on:

- watcher ingress recording
- orthogonal tmux state snapshots
- persisted pane captures
- live tmux smoke tests
- gated live Claude hook integration tests

## What It Records

The harness uses the recorder in `internal/trace`.

It writes two JSONL streams:

- `ingress.jsonl`
  - raw watcher/business-model inputs
- `tmux_state.jsonl`
  - full tmux state snapshots, kept orthogonal to ingress

### `ingress.jsonl`

Current event kinds:

- `bootstrap_state`
- `tmux_event`
- `hook_event`
- `process_poll_snapshot`
- `full_refresh_snapshot`
- `timer_fired`
- `capture_snapshot`
- `activity_transition`

These are recorded from the watcher at real ingress points:

- bootstrap
- tmux control-mode events
- Claude hook events
- process polling
- full refresh
- observer pane captures
- settle/smoothing/completed-decay timers
- every activity state transition (from/parsed/resolved/final + source)

### `tmux_state.jsonl`

Current snapshot kind:

- `tmux_state_snapshot`

This contains:

- sessions
- windows
- panes
- pane pid
- working dir
- git context
- current target

This stream is intentionally separate from watcher-derived state such as:

- attention queue
- seen/unseen
- activitySince

## Pane Capture Artifacts

The live harness also persists human-readable pane captures to disk.

These live under:

- `pane_captures/`

inside the scenario temp directory.

This is separate from `capture_snapshot` events:

- `capture_snapshot` in `ingress.jsonl` is the replay-oriented observer capture data
- `pane_captures/*.txt` are debugging/review artifacts for humans

### What pane captures are used for

- automatic timeout diagnostics
- final scenario inspection
- explicit snapshots during a scenario when more context is needed

The harness can currently capture:

- after sending commands
- when trust prompts appear
- when observer conditions are met
- on timeout
- final pane state on teardown

## Live Harness Helper

File:

- `internal/watcher/live_harness_test.go`

Current helper responsibilities:

- create `pane_captures/`
- persist pane captures by label
- log capture artifact paths
- run wait loops with optional timeout capture hooks

Important helper methods:

- `newLiveHarness(...)`
- `capturePane(...)`
- `capturePaneNow(...)`
- `waitFor(...)`

## Tests That Use The Harness

### 1. Live tmux smoke test

File:

- `internal/watcher/live_trace_smoke_test.go`

Gate:

```bash
CMS_LIVE_TRACE_SMOKE=1 go test ./internal/watcher -run TestLiveTraceSmoke -v
```

What it does:

- starts a real watcher
- uses isolated tmux
- drives pane output directly
- validates observer capture recording
- forces structural refresh
- writes pane capture artifacts

What it proves today:

- watcher ingress recording works in a live tmux run
- tmux-state snapshots are orthogonal
- observer capture recording works
- timer events are recorded

### 2. Snapshot resume round-trip test

File:

- `internal/session/snapshot_resume_test.go`

Gate:

```bash
CMS_SNAPSHOT_RESUME=1 go test ./internal/session -run TestSnapshot -v
```

Subtests:

- `TestSnapshotResumeRoundTrip` — creates a tmux session with 2 panes,
  sets `@cms_claude_session` (+ marker/resume flag on one), saves snapshot,
  kills session, restores, and verifies: paneMap has both session IDs,
  restored panes have the correct user-options.
- `TestSnapshotBackwardCompat` — writes an old-format snapshot (no
  `claude_session_id` fields), restores it, verifies it works with an
  empty paneMap.

What it proves:

- `@cms_claude_session` is captured during snapshot save
- pane markers and resume flags survive save/restore
- `RestoreSnapshot` returns correct paneID → sessionID map
- old snapshots without the new fields deserialize cleanly

### 3. Claude hook integration test

File:

- `internal/watcher/claude_integration_test.go`

Gate:

```bash
CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeHookIntegration -v
```

Subtests:

- `bash_sleep_hooks`
- `permission_prompt_hooks`

What it does:

- builds a temporary `cms` binary
- writes temporary Claude hook settings that invoke:
  - `cms internal hook --socket ... session-start`
  - `cms internal hook --socket ... pre-tool-use`
  - etc.
- starts a real watcher with JSONL recording
- runs Claude inside isolated tmux
- records trace + pane captures

What it can do right now:

- detect and accept Claude’s workspace trust prompt automatically
- prove that real Claude hook events can reach the watcher
- persist pane captures around trust/startup/failure states
- surface startup blockers through traces and pane artifacts

What is not fully stable yet:

- end-to-end Bash tool execution hook coverage
- end-to-end permission prompt hook coverage

Current known live blocker:

- Claude startup environment noise, especially MCP/auth/plugin startup state, can block or delay the scenarios before they reach the hook we want

### 3. Multi-step agentic transition diagnostic

File:

- `internal/watcher/claude_multistep_test.go`

Gate:

```bash
CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeMultiStepTransitions -v -timeout 5m
```

What it does:

- seeds a small Go project (3 files, 5 functions)
- gives Claude a multi-step task requiring Read, Write, and Bash tool calls
- records full activity transition trace with `activity_transition` events
- analyzes the transition timeline for Working→Completed→Working cycles
- reports hook event counts, cycle count, and max cycle gap

What it proved:

- `session-start` hook arriving after observer already set Working caused a false Working→Completed cycle (fixed: session-start now preserves existing activity)
- hooks were silently failing due to config.Load() running before internal command dispatch (fixed: internal commands bypass config)
- observer hold window (2s) and smoothing (3s working→idle) are sufficient for sub-second gaps during active tool use
- MCP server startup failures can prevent hooks from firing in some runs

## Claude-Specific Findings From The Harness

The harness already established several facts about the installed Claude CLI/environment:

- this CLI does not support `--prompt`
- the prompt must be passed as the trailing positional argument
- Claude can stop first on a workspace trust prompt
- after trust is accepted, `session-start` hook events can be observed
- further startup issues such as MCP auth/failure can still block progress before `pre-tool-use`

This means the harness is already useful even when the test is not green:

- it converts “hung integration test” into traceable, reviewable evidence

## What The Harness Can Do Right Now

Current capabilities:

- run watcher against isolated tmux in tests
- record ingress and tmux-state JSONL streams
- persist pane captures automatically
- capture panes deliberately at meaningful checkpoints
- run gated live smoke tests
- run gated live Claude hook tests
- surface trust prompts and other startup blockers from real CLI behavior

## What The Harness Does Not Do Yet

Not implemented yet:

- deterministic replay runner from `ingress.jsonl`
- final structured harness events for actions like trust acceptance
- automatic recording of matched Claude process PID
- stable green Claude integration coverage for all target scenarios
- artifact promotion to a durable checked-in `testdata/` scenario catalog

## Recommended Next Steps

1. Stabilize Claude live scenarios with a more stripped-down Claude startup mode.
Likely try `--bare` and stricter MCP disabling.

2. Finish recording actual matched agent process PID, not just pane PID.

3. Build replay on top of `ingress.jsonl`.

4. If live scenarios become reliable, add artifact summarization so successful runs print:
- trace dir
- pane capture dir
- key hook kinds seen

## Integration Harness (scripts/harness.sh)

A standalone bash harness that runs the real `cms` binary against an
isolated tmux server with its own config and test worktree repos.

### What it sets up

- **Test repos** via `scripts/create-test-repos.sh`: 4 bare-repo projects
  (`project_a` through `project_d`) with multiple worktrees, diverging
  branches, and merged branches.
- **Isolated tmux server** (`-L cms-h-<random> -f <minimal.conf>`): each
  run gets a unique random ID so parallel runs never conflict. Server is
  separate from the user's tmux, with `base-index 0` and status bar off.
- **Isolated config** (`XDG_CONFIG_HOME` override): points `search_paths`
  at the test repos.
- **CMS_TMUX_SOCKET**: exported so `cms` talks to the harness server, not
  the user's default tmux.
- **Optional real agents** (`--agents`): launches `claude -p '...'` in
  some panes for agent detection testing.
- **Cleanup on exit**: tmux server is killed and temp dir is removed.

### Usage

```bash
./scripts/harness.sh                          # worktrees (default)
./scripts/harness.sh sessions,worktrees       # multiple sections
./scripts/harness.sh --agents worktrees       # with real claude agents
./scripts/harness.sh dash                     # dashboard view
```

Attaches to the harness tmux on exit. `Ctrl-b d` to detach (cleanup
tears down the server). Inside, re-run with `cms <section>`.

### Test repo layout

| Project     | Worktrees                          | Notes                    |
|-------------|------------------------------------|--------------------------|
| `project_a` | main, feature-auth, feature-api, bugfix-login | 3 feature branches |
| `project_b` | main, shipped-v2, feature-dashboard, refactor-db | shipped-v2 merged into main |
| `project_c` | main only                          | single worktree          |
| `project_d` | main + 8 branches                  | scrolling/filtering test |

## Quick Commands

Run all regular tests:

```bash
go test ./...
```

Render harness (visual debugging, synthetic data):

```bash
CMS_RENDER_HARNESS=1 go test ./internal/tui/ -run 'TestRenderHarness(Dashboard|Finder|Queue)' -v
CMS_LIVE_HARNESS=1 go test ./internal/tui/ -run TestRenderHarnessLive -v
```

Integration harness (real tmux + real repos):

```bash
./scripts/harness.sh worktrees
./scripts/harness.sh --agents sessions,worktrees,queue
```

Run snapshot resume test (isolated tmux):

```bash
CMS_SNAPSHOT_RESUME=1 go test ./internal/session -run TestSnapshot -v
```

Run live tmux smoke:

```bash
CMS_LIVE_TRACE_SMOKE=1 go test ./internal/watcher -run TestLiveTraceSmoke -v
```

Run Claude Bash-only subtest:

```bash
CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run 'TestClaudeHookIntegration/bash_sleep_hooks' -v
```

Run full Claude integration test:

```bash
CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeHookIntegration -v
```

Run multi-step transition diagnostic:

```bash
CMS_CLAUDE_INTEGRATION=1 go test ./internal/watcher -run TestClaudeMultiStepTransitions -v -timeout 5m
```
