# cms

Tmux session picker and dashboard with Claude/Codex agent awareness.

## CLI Surface

```
# Default (finder — universal fuzzy switcher)
cms                              Finder (configurable via [finder].include)
cms sessions                     Sessions only (also: cms -s)
cms projects                     Projects only (also: cms -p)
cms queue                        Attention queue (also: cms -q)
cms marks                        Marks only (also: cms -m)
cms worktrees                    Worktrees only (current repo)
cms branches                     Local branches (current repo)
cms windows                      Windows only (all sessions)
cms panes                        Panes only (all sessions)
cms sessions,worktrees           Composable: comma-separated section names

# Views
cms dash                         Dashboard (session/pane grid with agent status)

# Navigation (headless)
cms next                         Jump to next waiting/idle agent pane
cms mark <label> [pane]          Mark current pane (or specified pane) with label
cms jump <label>                 Switch to marked pane

# Worktree operations (top-level)
cms switch <branch>              Switch to existing branch's worktree (strict git switch semantics)
cms switch -c <branch> [start]   Create new branch + worktree (error if exists)
cms switch -C <branch> [start]   Force-create/reset branch + worktree
cms go <branch> [start-point] [prompt]  Switch or create from base_branch; optional prompt runs go_cmd
cms rm <branch>                  Remove worktree (+ merged branch)
cms land [target]                Land current branch into target (rebase + merge + cleanup)
cms ls                           Worktree table (paths, branches, merge status)

# Config
cms config init                  Write default config file
cms config default               Print default config (TOML) to stdout
cms hook-setup                   Print Claude Code hook config

# Internal (hidden)
cms internal hook <event>        Forward Claude Code hook event
cms internal refresh [name]      Refresh worktrees for tmux session
```

## File Tree

```
main.go                           CLI entry: flag parsing, command dispatch, headless cmds
debuglog.go                       Debug logger init, wires debug.Logf

internal/
  debug/
    debug.go                      Package-level Logf var (no-op default, set by main)

  proc/
    proc.go                       Process table (Entry, Table, BuildTable, IsShellCommand)

  config/
    config.go                     All config types + Load, FinderConfig, SortKeys, PickerSortConfig

  git/
    git.go                        Info, Cache, DetectAll, Cmd
    branch.go                     ListLocalBranches
    worktree.go                   Worktree, ListWorktrees, IsWorktreeCheckout

  tmux/
    types.go                      Session, Window, Pane, CurrentTarget
    tmux.go                       Run, Command (low-level tmux execution)
    state.go                      FetchState, FetchCurrentTarget, FetchLastSession
    capture.go                    CapturePaneBottom (screen scraping)
    control.go                    Client, Event (tmux control mode connection)

  agent/
    agent.go                      Provider, Activity, AgentModeKind, AgentStatus, ApplyUpdates
    detect.go                     Detect, DetectAll, Reparse, ShouldHoldWorking
    claude.go                     Claude-specific regex patterns + parseClaudePane
    codex.go                      Codex-specific regex patterns + parseCodexPane

  attention/
    queue.go                      Queue, Event, Reason (panes needing user attention)
    persist.go                    PersistActivitySince, LoadPersisted (tmux pane options)
    persisted.go                  LoadPersistedExported (exported wrapper for bootstrap)

  hook/
    hook.go                       Kind, Event, Listener (Claude Code hook socket)
    cmd.go                        RunCmd, RunSetup (hook CLI subcommands)

  mark/
    mark.go                       Mark, Load, Save, Set, Remove, Resolve (pane bookmarks)

  session/
    session.go                    Create, Kill, Switch, SmartSwitch, SwitchToPane, OpenProject
    snapshot.go                   SaveSnapshot, RestoreSnapshot (tmux session persistence)
    template.go                   Session templates for project types

  resume/
    store.go                      SaveClaudeSession, LoadClaudeSession (agent resume state)

  project/
    project.go                    Scan, Project (git repo discovery from search paths)

  worktree/
    worktree.go                   CreateWorktree, SwitchWorktree, GoWorktree, RemoveWorktree, DeleteBranch, hooks
    cmd.go                        RunCmd, RunSwitch, RunGo, RunRemove, RunList (worktree CLI dispatch)
    land.go                       Land, squashCommits, resolveBranchesAndWorktrees, commit msg gen

  trace/
    types.go                      Event types for JSONL trace recording
    record.go                     Recorder interface, JSONLRecorder, NopRecorder
    jsonl_test.go                 JSONL serialization tests

  trace/
    types.go                      IngressKind, ActivityTransitionPayload, all trace event types
    record.go                     Recorder interface, JSONLRecorder, normalize helpers

  watcher/
    events.go                     StateMsg, AgentUpdateMsg, FocusChangedMsg, GitUpdateMsg
    watcher.go                    Watcher: New, Start, Stop, CachedState (returns deep copies)
    pane_tracker.go               Output debounce, recheck, activity transitions, hysteresis
    proc_poller.go                Process table polling loop
    git_poller.go                 Git info polling loop
    live_harness_test.go          Live harness helper (pane captures, wait loops)
    live_trace_smoke_test.go      Live tmux smoke test (CMS_LIVE_TRACE_SMOKE)
    claude_integration_test.go    Claude hook integration tests (CMS_CLAUDE_INTEGRATION)
    claude_multistep_test.go      Multi-step agentic transition diagnostic

  tui/
    styles.go                     All shared lipgloss styles, InitStyles, ProviderAccent
    util.go                       ShortenHome, JoinParts
    picker.go                     PickerItem, PickerAction, pickerModel (fuzzy-find, fzf algo, normal-mode actions)
    actions.go                    tea.Cmd factories bridging views to session/tmux/mark ops
    app.go                        RootModel, Screen enum, ValidSections, PostAction
    dashboard.go                  dashboardModel (session/pane grid with agent status)
    finder.go                     finderModel (universal fuzzy picker: sessions/projects/worktrees/windows/panes/marks/queue)
    newworktree.go                newWorktreeModel (text input for quick worktree creation)

scripts/
  create-test-repos.sh            Generate bare-repo worktree layouts for testing
  harness.sh                      Integration harness: randomised isolation, per-run tmux server

docs/
  finder-sort.md                  Worked examples for finder sort key config
  finder-design-system.md         Visual design tokens and style guide
  restore.md                      Session restore design
  agents/
    harness.md                    Agent harness architecture, trace recording, test catalog
```

## Architecture Layers

```
Presentation    tui/*            Renders state, emits tea.Cmd via actions.go
                                 NEVER imports session or tmux directly
Business        watcher/*        Coordinates state; sends bubbletea messages to tui
                session          Session lifecycle ops (tmux commands)
                project          Repo scanning
                worktree         Worktree management + land workflow
Domain          agent/*          Agent detection, status types, parsing
                attention/*      Attention queue logic + persistence
                hook/*           Hook socket listener + event types
                mark/*           Named pane bookmarks (file-backed)
Infrastructure  tmux/*           tmux I/O (types, commands, control mode)
                git/*            Git I/O (branch info, worktrees)
                proc/*           Process table (ps parsing)
                config/*         Config loading (TOML)
                debug/*          Debug log wiring
```

## Design Rules

- **Layers only import downward.** tui imports watcher/agent/tmux types but never session directly. watcher imports tmux/agent/hook/attention but never tui. Infrastructure has no internal deps (except proc/git used by tmux). Exception: finder's async `tea.Cmd` factories (like `scanWorktreesCmd`) import business-layer packages (worktree, mark) — consistent with how `actions.go` imports session.
- **`proc` breaks the tmux-agent cycle.** `IsShellCommand` and process table types live in proc, imported by both tmux and agent without circular dependency.
- **Views never call infrastructure.** Dashboard/finder emit intents via `actions.go` tea.Cmd factories. Actions call session/tmux and return result messages. This prevents the implicit feedback loop where views trigger tmux mutations and hope watcher notices.
- **`CachedState()` returns deep copies.** Watcher holds canonical state under mutex; views get snapshots they can freely read without data races.
- **Watcher is a thin coordinator.** Decomposed into 5 files: events (message types), watcher (lifecycle), pane_tracker (debounce/transitions), proc_poller, git_poller. Each file has a single responsibility.
- **Hysteresis lives in watcher, not views.** Activity transition logic (Working hold windows, Completed decay timers) is domain logic in `pane_tracker.go`, not presentation logic.
- **Provider parsers are data, not logic.** `claude.go` and `codex.go` supply regex patterns and provider-specific parsing; `detect.go` owns the detection pipeline shared across providers.
- **Shared styles in one place.** All lipgloss styles live in `tui/styles.go`, initialized once by `InitStyles(cfg)`. No view imports another view for styles.
- **Config types are centralized.** `WorktreeConfig`, `ProjectConfig`, `WorktreeHook` live in the config package alongside all other config types, even though worktree operations use them.
- **`debug.Logf` is a package-level var.** Set by main after init. Internal packages call `debug.Logf(...)` without importing the logger implementation.
- **PostAction is the exit contract between TUI and main.** Screens that trigger tmux mutations (switch, open, worktree create) set a `PostAction` and quit. `main.go:executePostAction()` handles the actual infra calls after bubbletea's alt screen is torn down. New `ItemKind` values extend this: `KindSession`, `KindProject`, `KindWorktree`, `KindBranch`, `KindPane`, `KindMark`, `KindWindow`, `KindQueue`.
- **New TUI screens follow the done/action pattern.** Each sub-model exposes `done bool` and `action *PostAction`. When `done` is true, `updateActive` in app.go checks `action`: non-nil means quit-with-action, nil means cancelled. See `newWorktreeModel` as the minimal example.
- **`CreateWorktreeOpts.StartPoint` vs `Track`.** Use `StartPoint` for local base branches (e.g. `main`). Use `Track` only when checking out a remote branch that needs upstream tracking. Don't assume `origin/<branch>` exists.
- **`WorktreeConfig.BaseBranch`** is configurable in `[worktree]` (user or project config). Empty means auto-detect via `DefaultBranch()` (origin/HEAD → main → master).
- **Finder is the universal switcher.** A `[]string` of section names (passed from CLI or config) controls which item types appear. Sections are composable via comma-separated CLI args (e.g. `cms sessions,worktrees`). `rebuildPicker()` assembles sections in order. Valid sections: `sessions`, `projects`, `queue`, `worktrees`, `branches`, `panes`, `windows`, `marks`.
- **Finder sort is config-driven.** `sort = [...]` lists define sort key priority (left-to-right, first difference wins). Global default in `[finder].sort`, per-section override in `[finder.<section>].sort`. All sections including queue go through `sortedSectionItems()` with pluggable predicates. Keys: `active`, `current`, `recent` (bool), `state`, `unseen`, `oldest`, `newest` (queue-specific). Prefix `-` demotes. See `docs/finder-sort.md` for worked examples.
- **Active is always computed.** Every item gets `Active` set based on its "live presence" (sessions: attached, worktrees: has tmux pane, projects: has session, panes: has agent, windows: has agent, branches: has worktree, queue: unseen events, marks: pane alive). The `active` sort key controls only sort order, not whether Active is computed. The Active indicator visual is configurable via `[finder.active_indicator]`.
- **Cross-section dedup.** When overlapping sections are composed, the "more specific" section wins: branches with worktrees are hidden from branches when worktrees section is visible; projects with sessions are hidden from projects when sessions section is visible.
- **Agent display config is separated.** `display_provider_order`, `display_state_order`, and `show_context_percentage` on `[finder]` control agent summary rendering in session descriptions and queue.
- **Queue renders fixed-width columns.** Title is `session/branch`, description columns are provider (6), context% (4), activity (9, padded for ANSI), duration (4). Titles padded to longest across all items.
- **Picker supports normal-mode actions.** `PickerAction` enum (e.g. `PickerActionDelete`) with y/n confirmation prompt. Picker sets `action` + `chosen`; finder dispatches by item kind (kill session/pane, remove mark).
- **Marks are file-backed.** Stored as JSON at `~/.config/cms/marks.json`. Pane IDs are globally addressable in tmux; session/window stored for display only.
- **Internal commands bypass config loading.** `cms internal hook` and `cms internal refresh` are dispatched in `main()` before `config.Load()`. Hook commands are called by Claude Code and must never fail due to config validation errors. Any new internal subcommand must also run before config.
- **`session-start` preserves existing activity.** If the observer already detected an agent as Working before the `session-start` hook arrives, the hook preserves the current activity instead of resetting to Idle. `session-start` means "I'm here", not "I stopped working".
- **`activity_transition` trace events record the full state machine.** Every call to `transitionAgent` emits a trace with `from`, `parsed` (raw), `resolved` (after hold/promotion), and `final` (after smoothing) activity plus source (hook/observer). Use `ingress.jsonl` to diagnose status cycling.
- **Use `go build -o /dev/null ./...` for validation.** The cms binary manages tmux; writing it to disk can interfere with the running session.
