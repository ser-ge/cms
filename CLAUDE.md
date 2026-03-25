# cms

Tmux session picker and dashboard with Claude/Codex agent awareness.

## CLI Surface

```
# Default (finder — universal fuzzy switcher)
cms                              Finder (configurable via [finder].include)
cms -s  / cms sessions           Sessions only
cms -p  / cms projects           Projects only
cms -q  / cms queue              Attention queue (urgency-sorted agent panes)
cms -m  / cms marks              Marks only
cms worktrees                    Worktrees only (current repo)
cms windows                      Windows only (all sessions)
cms panes                        Panes only (all sessions)

# Views
cms dash                         Dashboard (session/pane grid with agent status)

# Navigation (headless)
cms next                         Jump to next waiting/idle agent pane
cms mark <label> [pane]          Mark current pane (or specified pane) with label
cms jump <label>                 Switch to marked pane

# Worktree operations (top-level)
cms go <branch> [path]           Switch to worktree (create if needed)
cms add [--no-open] <branch> [path]  Create worktree
cms rm <branch>                  Remove worktree
cms merge [flags] [branch]       Merge worktree
cms ls                           Worktree table (paths, branches, merge status)

# Config
cms config init                  Write default config file
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
    config.go                     All config types + Load, FinderConfig, PickerSortConfig

  git/
    git.go                        Info, Cache, DetectAll, Cmd
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

  hook/
    hook.go                       Kind, Event, Listener (Claude Code hook socket)
    cmd.go                        RunCmd, RunSetup (hook CLI subcommands)

  mark/
    mark.go                       Mark, Load, Save, Set, Remove, Resolve (pane bookmarks)

  session/
    session.go                    Create, Kill, Switch, SmartSwitch, SwitchToPane, OpenProject

  project/
    project.go                    Scan, Project (git repo discovery from search paths)

  worktree/
    worktree.go                   CreateWorktree, AddWorktree, RemoveWorktree, DeleteBranch, hooks
    cmd.go                        RunCmd, RunAdd, RunRemove, RunList (worktree CLI dispatch)
    merge.go                      Merge, SquashCommits, commit message generation

  watcher/
    events.go                     StateMsg, AgentUpdateMsg, FocusChangedMsg, GitUpdateMsg
    watcher.go                    Watcher: New, Start, Stop, CachedState (returns deep copies)
    pane_tracker.go               Output debounce, recheck, activity transitions, hysteresis
    proc_poller.go                Process table polling loop
    git_poller.go                 Git info polling loop

  tui/
    styles.go                     All shared lipgloss styles, InitStyles, ProviderAccent
    util.go                       ShortenHome, JoinParts
    picker.go                     PickerItem, PickerAction, pickerModel (fuzzy-find, fzf algo, normal-mode actions)
    actions.go                    tea.Cmd factories bridging views to session/tmux/mark ops
    app.go                        RootModel, Screen enum, FinderKind, PostAction
    dashboard.go                  dashboardModel (session/pane grid with agent status)
    finder.go                     finderModel (universal fuzzy picker: sessions/projects/worktrees/windows/panes/marks/queue)
    newworktree.go                newWorktreeModel (text input for quick worktree creation)
```

## Architecture Layers

```
Presentation    tui/*            Renders state, emits tea.Cmd via actions.go
                                 NEVER imports session or tmux directly
Business        watcher/*        Coordinates state; sends bubbletea messages to tui
                session          Session lifecycle ops (tmux commands)
                project          Repo scanning
                worktree         Worktree management + merge workflow
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
- **PostAction is the exit contract between TUI and main.** Screens that trigger tmux mutations (switch, open, worktree create) set a `PostAction` and quit. `main.go:executePostAction()` handles the actual infra calls after bubbletea's alt screen is torn down. New `ItemKind` values extend this: `KindSession`, `KindProject`, `KindWorktree`, `KindPane`, `KindMark`, `KindWindow`, `KindQueue`.
- **New TUI screens follow the done/action pattern.** Each sub-model exposes `done bool` and `action *PostAction`. When `done` is true, `updateActive` in app.go checks `action`: non-nil means quit-with-action, nil means cancelled. See `newWorktreeModel` as the minimal example.
- **`CreateWorktreeOpts.StartPoint` vs `Track`.** Use `StartPoint` for local base branches (e.g. `main`). Use `Track` only when checking out a remote branch that needs upstream tracking. Don't assume `origin/<branch>` exists.
- **`WorktreeConfig.BaseBranch`** is configurable in `[worktree]` (user or project config). Empty means auto-detect via `DefaultBranch()` (origin/HEAD → main → master).
- **Finder is the universal switcher.** FinderKind controls which item types appear. Queue, worktree, window, pane, and mark items are all finder modes (not separate screens). `rebuildPicker()` assembles sections driven by `[finder].include` config. Queue urgency sorting lives in `buildQueueItems()`.
- **Finder sort is config-driven.** `PickerSortConfig` (per-picker section overrides with global defaults) controls `demote_current`, `promote_recent`, and `promote_open`. Each section uses `sortedSectionItems()` with pluggable `isCurrent`/`isRecent` predicates. Queue has its own urgency sort.
- **Queue renders fixed-width columns.** Title is `session/branch`, description columns are provider (6), context% (4), activity (9, padded for ANSI), duration (4). Titles padded to longest across all items.
- **Picker supports normal-mode actions.** `PickerAction` enum (e.g. `PickerActionDelete`) with y/n confirmation prompt. Picker sets `action` + `chosen`; finder dispatches by item kind (kill session/pane, remove mark).
- **Marks are file-backed.** Stored as JSON at `~/.config/cms/marks.json`. Pane IDs are globally addressable in tmux; session/window stored for display only.
- **Use `go build -o /dev/null ./...` for validation.** The cms binary manages tmux; writing it to disk can interfere with the running session.
