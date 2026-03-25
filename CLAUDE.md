# cms

Tmux session picker and dashboard with Claude/Codex agent awareness.

## File Tree

```
main.go                           CLI entry: flag parsing, command dispatch
debuglog.go                       Debug logger init, wires debug.Logf

internal/
  debug/
    debug.go                      Package-level Logf var (no-op default, set by main)

  proc/
    proc.go                       Process table (Entry, Table, BuildTable, IsShellCommand)

  config/
    config.go                     All config types + Load, WorktreeConfig, ProjectConfig

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

  session/
    session.go                    Create, Kill, Switch, SmartSwitch, SwitchToPane, OpenProject

  project/
    project.go                    Scan, Project (git repo discovery from search paths)

  worktree/
    worktree.go                   CreateWorktree, RemoveWorktree, DeleteBranch, hooks
    cmd.go                        RunCmd (worktree CLI dispatch: add/rm/list/merge)
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
    picker.go                     PickerItem, pickerModel (fuzzy-find widget, fzf algo)
    actions.go                    tea.Cmd factories bridging views to session/tmux ops
    app.go                        RootModel, Screen enum, FinderKind, PostAction
    dashboard.go                  dashboardModel (session/pane grid with agent status)
    finder.go                     finderModel (fuzzy session/project picker)
    queue.go                      queueModel (attention queue sorted by urgency)
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
Infrastructure  tmux/*           tmux I/O (types, commands, control mode)
                git/*            Git I/O (branch info, worktrees)
                proc/*           Process table (ps parsing)
                config/*         Config loading (TOML)
                debug/*          Debug log wiring
```

## Design Rules

- **Layers only import downward.** tui imports watcher/agent/tmux types but never session directly. watcher imports tmux/agent/hook/attention but never tui. Infrastructure has no internal deps (except proc/git used by tmux).
- **`proc` breaks the tmux-agent cycle.** `IsShellCommand` and process table types live in proc, imported by both tmux and agent without circular dependency.
- **Views never call infrastructure.** Dashboard/finder/queue emit intents via `actions.go` tea.Cmd factories. Actions call session/tmux and return result messages. This prevents the implicit feedback loop where views trigger tmux mutations and hope watcher notices.
- **`CachedState()` returns deep copies.** Watcher holds canonical state under mutex; views get snapshots they can freely read without data races.
- **Watcher is a thin coordinator.** Decomposed into 5 files: events (message types), watcher (lifecycle), pane_tracker (debounce/transitions), proc_poller, git_poller. Each file has a single responsibility.
- **Hysteresis lives in watcher, not views.** Activity transition logic (Working hold windows, Completed decay timers) is domain logic in `pane_tracker.go`, not presentation logic.
- **Provider parsers are data, not logic.** `claude.go` and `codex.go` supply regex patterns and provider-specific parsing; `detect.go` owns the detection pipeline shared across providers.
- **Shared styles in one place.** All lipgloss styles live in `tui/styles.go`, initialized once by `InitStyles(cfg)`. No view imports another view for styles.
- **Config types are centralized.** `WorktreeConfig`, `ProjectConfig`, `WorktreeHook` live in the config package alongside all other config types, even though worktree operations use them.
- **`debug.Logf` is a package-level var.** Set by main after init. Internal packages call `debug.Logf(...)` without importing the logger implementation.
- **Use `go build -o /dev/null ./...` for validation.** The cms binary manages tmux; writing it to disk can interfere with the running session.
