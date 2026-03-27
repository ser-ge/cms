# cms

[![Release](https://img.shields.io/github/v/release/ser-ge/cms)](https://github.com/ser-ge/cms/releases)
[![Go](https://img.shields.io/github/go-mod-go-version/ser-ge/cms)](https://go.dev/)
[![License](https://img.shields.io/github/license/ser-ge/cms)](LICENSE)

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Install

### Homebrew

```bash
brew install ser-ge/tap/cms
```

### Go

```bash
go install github.com/ser-ge/cms@latest
```

### Binary

Download from [GitHub Releases](https://github.com/ser-ge/cms/releases).

## Quickstart

### The finder

`cms` with no arguments opens a fuzzy picker over your tmux sessions, worktrees, branches, and agents — all in one view. Short flags select sections, and they compose:

```bash
cms                # everything (configurable via [finder].include)
cms -a             # agents only
cms -s             # sessions only
cms -sw            # sessions + worktrees
cms -swa           # sessions + worktrees + agents
```

Flags: `-s` sessions, `-p` projects, `-a` agents, `-m` marks, `-w` worktrees, `-b` branches, `-W` windows, `-P` panes. Spell out the full name or comma-separate if you prefer: `cms sessions,worktrees`.

<!-- gif: cms finder with composable sections and agent status badges -->

Every item shows agent status inline. Sessions and worktrees display compact activity counts (`?1 · ⚡2 · ●1`) so you can see at a glance where work is happening.

### Sort makes "open and enter" always useful

Each view has sort defaults tuned to its purpose:

- **Agents** — sorted by state (`waiting` first), then unseen, then oldest. Enter jumps to the agent that needs you most.
- **Sessions** — sorted by `recent`. Enter takes you back to where you just were.
- **Worktrees** — `active` first, `-current` deprioritized. You see what else is going on, not where you already are.

The sort is why `cms` + enter is always the right move — not just a fuzzy filter, but an opinionated "take me to the most useful thing." All sort keys are configurable in `[finder]` and per-section overrides.

For the headless version of the same idea: [`cms next`](#navigation) cycles through agent panes by priority (waiting → completed → idle), no picker needed.

<!-- gif: cms next cycling through waiting agents -->

### The worktree loop

`cms go` is the one command for branch work. Worktree exists? Switch to it. Doesn't exist? Create it from your configured base branch.

```bash
cms go feature-x                        # switch or create
cms go feature-x "implement auth flow"  # create + spawn agent with prompt
```

The optional prompt string runs your configured `go_cmd` (default: `claude -p "$CMS_PROMPT"`), so one command gets you a fresh worktree with an agent already working.

When you're done, `cms land` closes the loop:

```bash
cms land              # squash, rebase, fast-forward merge, remove worktree
cms land -m "msg"     # explicit squash message
cms land --no-squash  # preserve individual commits
```

One command: squash → rebase onto target → merge → cleanup worktree and branch. Conflicts? Fix, `git rebase --continue`, `cms land --continue`. Backup refs are saved automatically.

The lifecycle: **go → work → land → gone.**

---

## Commands

```bash
# Universal fuzzy switcher (default)
cms                              # finder (configurable via [finder].include)
cms -s                           # sessions only (or: cms sessions)
cms -p                           # projects only (or: cms projects)
cms -a                           # agents only (or: cms agents)
cms -m                           # marks only (or: cms marks)
cms -w                           # worktrees only (or: cms worktrees)
cms -b                           # branches only (or: cms branches)
cms -W                           # windows only (or: cms windows)
cms -P                           # panes only (or: cms panes)

# Composable — short flags compose, or comma-separate full names
cms -sw                          # sessions + worktrees
cms -swa                         # sessions + worktrees + agents
cms agents,branches              # agents + branches

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

Valid section names: `sessions`, `projects`, `agents`, `worktrees`, `branches`, `panes`, `windows`, `marks`.

### Picker keybindings

In **insert mode** (default): type to filter, `ctrl+j`/`ctrl+k` to navigate, `esc` to enter normal mode.

In **normal mode**: `j`/`k` navigate, `i` or `/` to filter, `enter` to switch, `x` to close (with y/n confirm), `esc`/`q` to go back.

## Config

Write the default config:

```bash
cms config init
```

This writes to `$XDG_CONFIG_HOME/cms/config.toml` (or `~/.config/cms/config.toml`).

Print the default config to stdout (useful for piping or reviewing):

```bash
cms config default                              # user-facing config
cms config full                                 # all options including internal tuning
cms config default > ~/.config/cms/config.toml  # overwrite with defaults
```

See [examples/config.toml](examples/config.toml) for the user config and [examples/config-full.toml](examples/config-full.toml) for every option including status tracking tuning, colors, icons, and dashboard layout.

### Sort keys

| Key | Meaning | Sections |
|-----|---------|----------|
| `active` | Items with Active=true first | all |
| `-active` | Items with Active=true last | all |
| `current` | Current/focused item first | all (needs isCurrent predicate) |
| `-current` | Current/focused item last | all |
| `recent` | Most recently visited first | sessions |
| `state` | Sort by `state_order` list | agents |
| `unseen` | Unseen attention events first | agents |
| `oldest` | Oldest activity timestamp first | agents |
| `newest` | Newest activity timestamp first | agents |

Keys are evaluated left-to-right. The first key that distinguishes two items wins. Within equal items, `sort.SliceStable` preserves original order.

### Icon colors

Each section icon's color encodes item state. All colors are configurable
in `[colors.shared]` (ANSI 256 palette).

**Agent-bearing sections** (sessions, windows, panes, agents) use activity
colors — icon takes the most urgent agent state per `state_order`:

| Activity  | Default | Color          |
|-----------|---------|----------------|
| waiting   | `1`     | red            |
| completed | `208`   | orange         |
| working   | `3`     | yellow         |
| idle      | `12`    | blue           |
| no agent  | `240`   | gray           |

**Worktrees** use git repo state (not agent state):

| State          | Default | Color | Meaning                          |
|----------------|---------|-------|----------------------------------|
| dirty / ahead  | `1`     | red   | uncommitted changes or unpushed  |
| clean diverged | `2`     | green | in flight, not yet merged        |
| merged         | `240`   | gray  | done, can be cleaned up          |

**Non-agent sections** (branches, marks, projects) use presence colors:

| State    | Default | Color |
|----------|---------|-------|
| active   | `2`     | green |
| inactive | `240`   | gray  |

"Active" means:

| Section | Active means |
|---------|-------------|
| sessions | attached |
| worktrees | has tmux pane and not merged, or dirty/ahead |
| projects | has tmux session |
| branches | has worktree checked out |
| panes | has running agent |
| windows | has running agent |
| agents | unseen attention events |
| marks | pane still alive |

The `active` sort key controls whether active items sort first.

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

**Base branch resolution** (for new branches): explicit start-point arg → `[worktree].base_branch` from project `.cms.toml` → `[worktree].base_branch` from user `config.toml` → `origin/HEAD` → local `main` → local `master` → current HEAD. The chosen base is recorded in `git config branch.<name>.cms-base` so `cms land` can use it as the default target.

The prompt (arg with spaces) is passed to the command configured in `[worktree].go_cmd`. The prompt is available as `$CMS_PROMPT` in the command's environment. If the command string doesn't reference `$CMS_PROMPT`, the prompt is appended as an argument.

### `cms land` — land current branch into target

Run from inside a feature worktree. Squashes commits, rebases onto the target branch, fast-forward merges, and cleans up the worktree, branch, and tmux window. The full pipeline:

1. Stage uncommitted changes
2. Run `pre_commit` hooks
3. Save backup ref to `refs/cms-wt-backup/<branch>`
4. Squash commits into one
5. Run `post_commit` hooks
6. Rebase onto target
7. Run `pre_merge` hooks (pre-land)
8. Fast-forward merge into target (falls back to merge commit if ff fails)
9. Run `post_merge` hooks (post-land)
10. Remove worktree + branch + tmux window (unless `--keep`)

Steps 1-5 are skipped with `--no-squash`. When the target branch is checked out in another worktree, the merge runs inside that worktree directly (no checkout needed). After landing, pauses for confirmation before cleanup so you can review the result.

**Squash commit message** (in priority order):

1. `-m "message"` — explicit message from the command line
2. `[worktree].commit_cmd` — diff is piped via stdin to the configured command (e.g. `claude -p --model=haiku ...`) for LLM-generated messages; diff summary + detailed diff (truncated at 8KB) sent via stdin
3. Interactive editor — if no `-m` and no `commit_cmd`, opens `$EDITOR` (unless `--no-edit`)
4. Default — `"Merge branch '<name>'"` + `git diff --stat`

```bash
cms land                       # squash + rebase + ff-merge into default branch
cms land main                  # land into explicit target
cms land -m "message"          # squash with explicit commit message
cms land --no-edit             # squash, skip editor for commit message
cms land --no-squash           # preserve individual commits (rebase + ff-merge only)
cms land --no-ff               # create a merge commit
cms land --keep                # don't remove worktree after landing
cms land --abort               # abort an in-progress rebase
cms land --continue            # resume after resolving conflicts
cms land --autostash           # stash dirty target worktree without prompting
```

**Target resolution:** `[worktree].base_branch` from project `.cms.toml` → `[worktree].base_branch` from user `config.toml` → `git config branch.<name>.cms-base` (recorded at worktree creation) → `origin/HEAD` → local `main` → local `master`. Supports symbols: `^` (default branch), `-` (previous branch), `@` (current).

If the target worktree has uncommitted changes, `cms land` will prompt to stash them before merging and pop them back after. Use `--autostash` to skip the prompt. If the stash pop conflicts, your changes stay in the stash — run `git stash list` in the target worktree to find them.

**Backup refs:** With `--squash`, land saves the pre-squash HEAD to `refs/cms-wt-backup/<branch>` so original commit history can be recovered via `git log refs/cms-wt-backup/<branch>`.

**Conflict recovery:** On rebase conflicts, land exits with instructions. Fix conflicts, `git rebase --continue`, then `cms land --continue` to finish the merge and cleanup. Or `cms land --abort` to cancel. On `--continue`, branch resolution is deferred until after the rebase finishes (during a conflicted rebase, HEAD is detached), ensuring the merge step targets the correct branch.

**LLM commit messages:** With `[worktree].commit_cmd` configured, the diff is piped to the command for auto-generated commit messages. Falls back to a default message on failure. Use `--no-squash` to skip squashing and preserve individual commits.

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
base_branch = "main"               # default start-point for cms go + target for cms land
commit_cmd = "claude -p --no-session-persistence --model=haiku --tools='' --disable-slash-commands --setting-sources='' --system-prompt=''"
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
  attention/    Attention tracking + tmux pane persistence
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
- **Domain** (`agent`, `attention`, `hook`, `mark`) -- detection logic, attention tracking, hook events, bookmarks
- **Infrastructure** (`tmux`, `git`, `proc`, `config`, `debug`) -- I/O boundaries, no internal deps

## Development

```bash
go test ./...                        # run all tests
go build -o /dev/null ./...          # validate build (don't write binary -- interferes with tmux)
go vet ./internal/... .              # lint
```

Render harness (visual debugging):

```bash
CMS_RENDER_HARNESS=1 go test ./internal/tui/ -run 'TestRenderHarness(Dashboard|Finder|Agents)' -v
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
