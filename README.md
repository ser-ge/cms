# cms

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Commands

```bash
# Universal fuzzy switcher (default)
cms                              # finder (configurable via [finder].include)
cms sessions                     # sessions only (also: cms -s)
cms projects                     # projects only (also: cms -p)
cms queue                        # attention queue (also: cms -q)
cms marks                        # marks only (also: cms -m)
cms worktrees                    # worktrees only (current repo)
cms branches                     # local branches (current repo)
cms windows                      # windows only (all sessions)
cms panes                        # panes only (all sessions)

# Composable — comma-separated section names
cms sessions,worktrees           # sessions + worktrees
cms queue,branches               # queue + branches

# Views
cms dash                         # dashboard (session/pane grid with agent status)

# Navigation (headless)
cms next                         # jump to next waiting/idle agent pane
cms mark <label> [pane]          # mark current pane with label
cms jump <label>                 # switch to marked pane

# Worktree operations (top-level)
cms switch <branch>              # switch to existing branch's worktree
cms switch -c <branch> [start]   # create new branch + worktree
cms go <branch> [start-point] [prompt]  # switch or create; optional prompt runs go_cmd
cms rm <branch>                  # remove worktree
cms land [target]                # land current branch into target
cms ls                           # worktree table

# Config
cms config init                  # scaffold default config
cms config default               # print default config (TOML) to stdout
cms hook-setup                   # print hook configuration for Claude Code

# Internal (hidden)
cms internal hook <event>        # forward Claude Code hook event
cms internal refresh [name]      # refresh worktrees for tmux session
```

Valid section names: `sessions`, `projects`, `queue`, `worktrees`, `branches`, `panes`, `windows`, `marks`.

### Picker keybindings

In **insert mode** (default): type to filter, `ctrl+j`/`ctrl+k` to navigate, `esc` to enter normal mode.

In **normal mode**: `j`/`k` navigate, `i` or `/` to filter, `enter` to switch, `x` to close (with y/n confirm), `esc`/`q` to go back.

## Config

Write the default config:

```bash
cms config init
```

This writes to `$XDG_CONFIG_HOME/cms/config.toml` (or `~/.config/cms/config.toml`).

Print the full default config to stdout (useful for piping or reviewing):

```bash
cms config default
cms config default > ~/.config/cms/config.toml
```

```toml
[general]
default_session = ""
# Priority order for `cms next` — jump to agent panes in this state order.
switch_priority = ["waiting", "completed", "idle", "default", "working"]
# Two-key chord to exit insert mode in the TUI.
escape_chord = "jj"
escape_chord_ms = 250
# Session names to hide from the picker.
exclusions = []
# Scan git submodules when discovering projects.
search_submodules = false
# Restore tmux session snapshots when opening a project.
restore = true
# Seconds before a Completed agent decays to Idle (0 = never).
completed_decay_s = 30000
# When false, hooks go stale after initial detection; when true, hooks suppress transitions.
always_hooks_for_status = false
# Global smoothing delay (ms) for all state transitions (0 = use per-transition values).
# transition_smoothing_ms = 0

# Directories to scan for git projects.
[[general.search_paths]]
path = "~/projects"
max_depth = 3

# Per-transition smoothing delays (ms). Suppresses flicker from rapid state changes.
[general.smoothing]
working_to_idle_ms = 3000
working_to_completed_ms = 2000
idle_to_working_ms = 0
completed_to_idle_ms = 0

[finder]
# What bare `cms` shows and in what order.
include = ["sessions", "queue", "worktrees", "projects"]

# Global sort key priority list. Per-section overrides below.
# Keys evaluated left-to-right; first difference wins.
# Prefix "-" demotes (pushes matching items to bottom).
sort = ["active", "-current"]

# Queue urgency order (used by "state" sort key).
state_order = ["waiting", "completed", "idle", "working"]

# Display settings for agent summaries (session descriptions + queue).
display_provider_order = ["claude", "codex"]
display_state_order = ["idle", "working", "completed", "waiting"]
show_context_percentage = true

[finder.active_indicator]
icon = "▪"                     # or "●", "◆", "→", etc.
color = "2"                    # ANSI foreground color (green)
# background = ""              # ANSI background color
# bold = false

# Per-section sort overrides — only specify what differs from global.
[finder.sessions]
sort = ["recent", "-current"]  # last-visited first, attached last

[finder.queue]
sort = ["state", "unseen", "oldest"]  # urgency sort

# [finder.worktrees]
# sort = ["active", "-current"]
# [finder.branches]
# sort = ["active"]
```

### Sort keys

| Key | Meaning | Sections |
|-----|---------|----------|
| `active` | Items with Active=true first | all |
| `-active` | Items with Active=true last | all |
| `current` | Current/focused item first | all (needs isCurrent predicate) |
| `-current` | Current/focused item last | all |
| `recent` | Most recently visited first | sessions |
| `state` | Sort by `state_order` list | queue |
| `unseen` | Unseen attention events first | queue |
| `oldest` | Oldest activity timestamp first | queue |
| `newest` | Newest activity timestamp first | queue |

Keys are evaluated left-to-right. The first key that distinguishes two items wins. Within equal items, `sort.SliceStable` preserves original order.

### Active indicator

Every item in the picker gets an "Active" status based on its live presence:

| Section | Active means |
|---------|-------------|
| sessions | attached |
| worktrees | has tmux pane inside |
| projects | has tmux session |
| branches | has worktree checked out |
| panes | has running agent |
| windows | has running agent |
| queue | unseen attention events |
| marks | pane still alive |

Active items always show the configured indicator icon. The `active` sort key controls whether they also sort first.

### Cross-section dedup

When composing sections that overlap, duplicates are hidden from the less-specific section:

- **branches + worktrees**: branches with worktrees are hidden from the branches section
- **projects + sessions**: projects with sessions are hidden from the projects section

When a section is shown alone (e.g. `cms branches`), all items appear with Active marking.

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

### `cms switch` — strict git switch semantics

Switch to an existing branch's worktree. Creating a new branch requires `-c`/`-C` (explicit intent). Start-point is only valid with `-c`/`-C`.

```bash
cms switch feature             # switch to existing branch's worktree
cms switch -c feature main     # create new branch from main
cms switch -c feature ^        # create from default branch
cms switch -C feature main     # force-create (reset if exists)
```

Options: `--force`/`-f` (force worktree creation), `--path <dir>` (override worktree directory), `--no-open` (skip tmux window).

If the branch has a worktree, switches to it. If the branch exists but has no worktree, creates one. If the branch doesn't exist, errors (use `-c` to create).

### `cms go` — opinionated switch-or-create

The daily driver. Same worktree/tmux behavior as switch, but auto-creates new branches from the configured `base_branch`. Optionally runs a configured command with a prompt string.

```bash
cms go feature                              # switch if exists, create from base_branch if not
cms go feature main                         # override start-point for this invocation
cms go -                                    # switch to previous branch's worktree
cms go feature "implement feature A"        # create worktree + run go_cmd with prompt
cms go feature main "implement feature A"   # start-point + prompt
```

Options: `--force`/`-f`, `--path <dir>`, `--no-open`.

Default base branch: `[worktree].base_branch` config, then `origin/HEAD`, then `main`/`master`.

The prompt (arg with spaces) is passed to the command configured in `[worktree].go_cmd`. The prompt is available as `$CMS_PROMPT` in the command's environment. If the command string doesn't reference `$CMS_PROMPT`, the prompt is appended as an argument.

### `cms land` — land current branch into target

Run from inside a feature worktree. Squashes (optional), rebases, merges, and cleans up. When the target branch is checked out in another worktree, the merge runs inside that worktree directly.

```bash
cms land                       # land into default branch, ff-only
cms land --squash              # squash all commits into one
cms land --squash -m "message" # squash with explicit commit message
cms land --no-ff               # create a merge commit
cms land --keep                # don't remove worktree after landing
cms land --abort               # abort an in-progress rebase
cms land --continue            # resume after resolving conflicts
```

On `--continue`, branch resolution is deferred until after the rebase finishes (during a conflicted rebase, HEAD is detached). This ensures the merge step targets the correct branch.

### `cms rm` — remove worktree

Removes worktree + branch + tmux window. Agent-aware: blocks removal if Claude or Codex agents are running in the worktree.

```bash
cms rm feature                 # remove worktree, delete branch (if merged)
cms rm feature --keep-branch   # remove worktree, keep the branch
cms rm -f -D feature           # force remove + force delete unmerged branch
cms rm --dry-run feature       # preview what would be removed
```

`--force`/`-f` forces worktree removal and skips agent checks. `-D` force-deletes the branch even if not merged (mirrors `git branch -D`).

### `cms ls` — list worktrees

Shows all worktrees with the current one marked `*`. Merged branches show `[merged: reason]`.

```
*  main          .
   feature-auth  ../worktrees/feature-auth
   old-fix       ../worktrees/old-fix       [merged: ancestor of main]
```

### Symbols

Special branch symbols work in `switch`, `go`, `land`, and `rm`:

| Symbol | Meaning |
|--------|---------|
| `@` | Current branch |
| `-` | Previous branch (from reflog) |
| `^` | Default branch (main/master) |

### Worktree Configuration

Settings merge from user config (`~/.config/cms/config.toml`) and per-repo config (`.cms.toml`):

```toml
[worktree]
base_dir = "../worktrees"
base_branch = "main"               # default start-point for cms go
commit_cmd = "llm -m claude-haiku"  # LLM commit message generation
go_cmd = "claude -p \"$CMS_PROMPT\""  # command to run when prompt is given to cms go

[[worktree.hooks]]           # post-create hooks
command = "npm install"

[[worktree.pre_commit]]      # before squash commit
command = "npm run lint"

[[worktree.pre_merge]]       # before landing
command = "npm test"
```

Hooks and `go_cmd` receive `CMS_WORKTREE_PATH` and `CMS_REPO_ROOT` environment variables. `go_cmd` also receives `CMS_PROMPT`.

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

## Shell Completions

```bash
# Fish
cms completion fish > ~/.config/fish/completions/cms.fish

# Bash
eval "$(cms completion bash)"
# Or persist: cms completion bash >> ~/.bashrc

# Zsh
cms completion zsh > ~/.zfunc/_cms
# Then ensure ~/.zfunc is in fpath and run compinit:
#   fpath+=(~/.zfunc); autoload -Uz compinit; compinit
```

Completions include subcommands, short flags, dynamic branch/worktree names for worktree commands, and mark labels for `jump`.

## Architecture

```
main.go / debuglog.go              CLI entry + debug wiring

internal/
  proc/         Process table + IsShellCommand
  config/       Config types, FinderConfig, PickerSortConfig, TOML loading
  git/          Git info, branch listing, worktree listing
  tmux/         Session/Window/Pane types, tmux commands, control mode
  agent/        Provider-neutral detection, Claude + Codex parsing
  attention/    Attention queue + tmux pane persistence
  hook/         Claude Code hook socket listener
  mark/         Named pane bookmarks (file-backed JSON)
  session/      Session CRUD, smart switching, OpenProject
  project/      Git repo discovery from search paths
  worktree/     Worktree switch/go/rm/land workflow
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

Integration harness (isolated tmux with test repos):

```bash
./scripts/harness.sh                          # worktrees section (default)
./scripts/harness.sh sessions,worktrees       # multiple sections
./scripts/harness.sh --agents worktrees       # with real claude agents
./scripts/harness.sh dash                     # dashboard view
```

Creates bare-repo worktree layouts under `/tmp/cms-harness/repos`, starts an
isolated tmux server with its own config, and drops you into the TUI. Use
`--agents` to launch real `claude -p` processes in some panes for agent
detection testing. See `scripts/create-test-repos.sh` for the repo layout.
