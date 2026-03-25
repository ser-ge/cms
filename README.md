# cms

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Commands

```bash
# Universal fuzzy switcher (default)
cms                              # finder (sessions + projects + worktrees + marks)
cms -s  / cms sessions           # sessions only
cms -p  / cms projects           # projects only
cms -q  / cms queue              # attention queue (urgency-sorted agent panes)
cms -m  / cms marks              # marks only
cms worktrees                    # worktrees only (current repo)
cms windows                      # windows only (all sessions)
cms panes                        # panes only (all sessions)

# Views
cms dash                         # dashboard (session/pane grid with agent status)

# Navigation (headless)
cms next                         # jump to next waiting/idle agent pane
cms mark <label> [pane]          # mark current pane with label
cms jump <label>                 # switch to marked pane

# Worktree operations (top-level)
cms go <branch> [path]           # switch to worktree (create if needed)
cms add [--no-open] <branch> [path]  # create worktree
cms rm <branch>                  # remove worktree
cms merge [flags] [branch]       # merge worktree
cms ls                           # worktree table (paths, branches, merge status)

# Config
cms config init                  # scaffold default config
cms hook-setup                   # print hook configuration for Claude Code

# Internal (hidden)
cms internal hook <event>        # forward Claude Code hook event
cms internal refresh [name]      # refresh worktrees for tmux session
```

### Picker keybindings

In **insert mode** (default): type to filter, `ctrl+j`/`ctrl+k` to navigate, `esc` to enter normal mode.

In **normal mode**: `j`/`k` navigate, `i` or `/` to filter, `enter` to switch, `x` to close (with y/n confirm), `esc`/`q` to go back.

## Config

Write the default config:

```bash
cms config init
```

This writes to `$XDG_CONFIG_HOME/cms/config.toml` (or `~/.config/cms/config.toml`).

```toml
[general]
default_session = ""
switch_priority = ["waiting", "idle", "default", "working"]
escape_chord = "jj"
escape_chord_ms = 250
exclusions = []
attached_last = true           # legacy — use [finder.sessions].demote_current
last_session_first = true      # legacy — use [finder.sessions].promote_recent
search_submodules = false
search_paths = [
  { path = "~/projects", max_depth = 3 }
]
completed_decay_ms = 30000

[finder]
# What bare `cms` shows and in what order.
include = ["sessions", "queue", "worktrees", "marks", "projects"]

# Global sort defaults (per-picker sections override).
demote_current = true          # push active/current item to bottom
promote_recent = false         # promote last-visited item to top
promote_open = false           # promote items with tmux session/window

[finder.sessions]
promote_recent = true          # last-visited session floats up

# Per-picker overrides — only specify what differs from global defaults.
# [finder.worktrees]
# promote_open = true          # worktrees with tmux windows float up
# [finder.marks]
# demote_current = false
```

## Marks

Vim-style named bookmarks for tmux panes. Stored as JSON at `~/.config/cms/marks.json`.

```bash
cms mark api                   # mark current pane as "api"
cms mark frontend %12          # mark specific pane
cms jump api                   # switch to marked pane
cms -m                         # browse marks in picker
```

Dead marks (pane no longer exists) are shown dimmed in the picker and can be cleaned up with `x`.

## Worktree Management

Manage git worktrees from the CLI. Inspired by [wtp](https://github.com/satococoa/wtp) and [Worktrunk](https://github.com/max-sixty/worktrunk).

#### `cms go`

Switch to a worktree, creating it if needed:

```bash
cms go feature-auth            # switch to worktree, or create + switch
cms go -                       # switch to previous branch worktree
```

#### `cms ls`

Shows all worktrees with the current one marked `*`. Merged branches show `[merged: reason]`.

```
*  main          .
   feature-auth  ../worktrees/feature-auth
   old-fix       ../worktrees/old-fix       [merged: ancestor of main]
```

#### `cms add`

Creates a worktree and opens a tmux window for it.

```bash
cms add feature/auth           # auto-creates branch, path from config
cms add --no-open feature      # skip tmux window creation
```

Branch resolution: local branch > remote tracking > create new. Special symbols: `@` (current), `-` (previous), `^` (default branch).

#### `cms merge`

Full merge workflow: squash + rebase + merge + cleanup.

```bash
cms merge                      # merge current branch into default, ff-only
cms merge --squash             # squash all commits into one before merging
cms merge -s -m "message"      # squash with explicit commit message
cms merge --no-ff              # create a merge commit even if ff is possible
cms merge --keep               # don't remove worktree after merge
```

#### `cms rm`

Removes worktree + branch + tmux window. Agent-aware: blocks removal if Claude or Codex agents are running in the worktree panes.

```bash
cms rm feature-auth            # remove worktree, delete branch (if merged)
```

#### Worktree Configuration

Settings merge from user config (`~/.config/cms/config.toml`) and per-repo config (`.cms.toml`):

```toml
[worktree]
base_dir = "../worktrees"
commit_cmd = "llm -m claude-haiku"  # LLM commit message generation

[[worktree.hooks]]           # post-create hooks
command = "npm install"

[[worktree.pre_commit]]      # before squash commit
command = "npm run lint"

[[worktree.pre_merge]]       # before merge
command = "npm test"
```

Hooks receive `CMS_WORKTREE_PATH` and `CMS_REPO_ROOT` environment variables.

## Claude Code Hooks (optional)

By default, `cms` detects agent activity by observing tmux pane output. For faster, more accurate status updates, enable Claude Code hooks. When hooks are active for a pane, the observer is automatically suppressed.

### Setup

1. Generate the hook configuration:

```bash
cms hook-setup
```

2. Copy the printed JSON into `~/.claude/settings.json`, merging with existing hooks.

### Events

| Hook | Trigger | What `cms` learns |
|------|---------|-------------------|
| `SessionStart` | Claude session begins | Pane has an active agent, session ID |
| `Stop` | Claude goes idle | Work finished |
| `SessionEnd` | Claude process exits | Agent left the pane |
| `Notification` | Claude needs input | Waiting for approval + message text |
| `UserPromptSubmit` | User sends a prompt | Agent is working |
| `PreToolUse` | Tool execution starts | Which tool is running |

### Manual testing

```bash
echo '{"session_id":"test"}' | cms internal hook session-start
echo '{"tool_name":"Edit"}' | cms internal hook pre-tool-use
echo '{}' | cms internal hook stop
```

## Architecture

```
main.go / debuglog.go              CLI entry + debug wiring

internal/
  proc/         Process table + IsShellCommand
  config/       Config types, FinderConfig, PickerSortConfig, TOML loading
  git/          Git info, worktree listing
  tmux/         Session/Window/Pane types, tmux commands, control mode
  agent/        Provider-neutral detection, Claude + Codex parsing
  attention/    Attention queue + tmux pane persistence
  hook/         Claude Code hook socket listener
  mark/         Named pane bookmarks (file-backed JSON)
  session/      Session CRUD, smart switching, OpenProject
  project/      Git repo discovery from search paths
  worktree/     Worktree create/remove/merge workflow
  watcher/      State coordination: events, pane tracking, polling
  tui/          All UI: app router, dashboard, finder, picker, styles
  debug/        Package-level Logf var
```

Layers (import downward only):

- **Presentation** (`tui`) -- renders state, emits `tea.Cmd` via `actions.go`, never calls tmux/session directly
- **Business** (`watcher`, `session`, `project`, `worktree`) -- coordinates state, manages tmux sessions
- **Domain** (`agent`, `attention`, `hook`, `mark`) -- detection logic, attention queue, hook events, bookmarks
- **Infrastructure** (`tmux`, `git`, `proc`, `config`, `debug`) -- I/O boundaries, no internal deps

## Development

```bash
go test ./...                        # run all tests
go build -o /dev/null ./...          # validate build (don't write binary -- interferes with tmux)
go vet ./internal/... .              # lint
```

Render harness (visual debugging):

```bash
CMS_RENDER_HARNESS=1 go test ./internal/tui/ -run 'TestRenderHarness(Dashboard|Finder|Queue)' -v
CMS_LIVE_HARNESS=1 go test ./internal/tui/ -run TestRenderHarnessLive -v
```
