# cms

[![Release](https://img.shields.io/github/v/release/ser-ge/cms)](https://github.com/ser-ge/cms/releases)
[![Go](https://img.shields.io/github/go-mod-go-version/ser-ge/cms)](https://go.dev/)
[![License](https://img.shields.io/github/license/ser-ge/cms)](LICENSE)


"There are many like it, but this one is mine."

`cms` is an agent-aware tmux session manager based on [tms](https://github.com/jrmoulton/tmux-sessionizer) which in turn is based on ThePrimeagen's [tmux-sessionizer](https://github.com/ThePrimeagen/.dotfiles/blob/master/bin/.local/scripts/tmux-sessionizer).

Fuzzy switcher for projects (repos), worktrees, and running agents.

This project started because I've been (unsuccessfully) hitting "jj" to esc in the tms fzf
picker for years, vim brain kept trying and failing to be normal. The project
spiralled.


## Quickstart

### The finder

`cms` with no arguments opens a fuzzy picker over your tmux sessions, projects (repos),
worktrees, branches, and agents.

Agent status is detected and agent queue is updated live, pushing agents waiting for input to the top.

```bash
cms                # everything (configurable via [finder].include)
cms -p             # projects (what tms does out of the box)
cms -a             # agents only (live sorted by status)
cms -awsp           # agents, worktrees, sessions, projects
```

Flags: `-s` sessions, `-p` projects, `-a` agents, `-m` marks, `-w` worktrees,
`-b` branches, `-W` windows, `-P` panes.  Composable: the order in which the sections
stack in the finder is set by the order the flags are passed.

![cms demo](demo.gif)

### "Open and hit enter" always useful

`cms` tries to make the first item in the list always useful:

- **Agents** — sorted by state (`waiting` for input first, then `completed`, `idle`, `working`).
- **Projects / Worktrees** — sorted by `recent`. First item takes you back to last visited.

The headless version: [`cms next`](#navigation) will jump to the first item in a given list.

E.g. `cms next -a` will cycle through the agent queue based on priority.

<!-- gif: cms next cycling through waiting agents -->

### Useful tmux bindings

As with the original `tms`, binding the commands to tmux popup windows is most useful.

`cms` does not add any bindings.

Add these to your `~/.tmux.conf`:

```bash
# Popup finder (prefix + f)
bind f display-popup -E -w 80% -h 70% "cms"

# Popup agent queue (prefix + a)
bind a display-popup -E -w 80% -h 70% "cms -a"

# Jump to next waiting agent (prefix + n)
bind n run-shell "cms next -a"

```

### The worktree loop

`cms go` is the one command for branch work. Worktree exists? Switch to it. Doesn't exist? Create it from your configured base branch.

```bash
cms go feature-x                        # switch or create
cms go feature-x "implement auth flow"  # create + spawn agent with prompt
```

The optional prompt string runs your configured `go_cmd` (default: `claude -p "$CMS_PROMPT"`).

When you're done, `cms land` closes the loop:

```bash
cms land              # squash, rebase, fast-forward merge, remove worktree
cms land -m "msg"     # explicit squash message
cms land --no-squash  # preserve individual commits
```

One command: squash → rebase onto target → merge → cleanup worktree and branch. Conflicts? Fix, `git rebase --continue`, `cms land --continue`. Backup refs are saved automatically.

The lifecycle: **go → work → land → gone.**

---


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


## Config


Settings merge from user config (`~/.config/cms/config.toml`) and per-repo config (`.cms.toml`):

### user config


```toml

[general]
default_session = ""
# Priority order for pane selection when switching to a session or window.
switch_priority = ["waiting", "completed", "idle", "default", "working"]
# Two-key chord to exit insert mode in the TUI.
escape_chord = "jj"
escape_chord_ms = 250
# Scan git submodules when discovering projects.
search_submodules = false
# Restore tmux session snapshots when opening a project.
restore = true
# Seconds before a Completed agent decays to Idle (0 = never).
completed_decay_s = 30000
# Directories to scan for git projects.
[[general.search_paths]]
path = "~"
max_depth = 3
exclusions = []

[finder]
# What bare `cms` shows and in what order.
include = ["agents", "worktrees", "sessions", "projects"]

# Global sort key priority list. Per-section overrides below.
# Keys evaluated left-to-right; first difference wins.
# Prefix "-" demotes (pushes matching items to bottom).
sort = ["active", "-current"]

# Agents queue urgency order (used by "state" sort key).
state_order = ["waiting", "completed", "idle", "working"]

# Show max context percentage in aggregate session/worktree summaries.
show_context_percentage = true

[finder.section_icons]
sessions = "S"
agents_queue = "*"
worktrees = "⎇"
branches = "B"
panes = ">"
windows = "W"
marks = "M"
projects = "P"

# Per-section sort overrides — only specify what differs from global.
[finder.sessions]
sort = ["recent", "-current"]  # last-visited first, attached last

[finder.agents_queue]
sort = ["state", "unseen", "oldest"]  # urgency sort

```


### project config


Note: `.cms.toml` must be placed in project root. For bare repos `cms`
expects one config file at repo root in the bare repo folder itself.

```toml
[worktree]
base_dir = "../worktrees" # target path for new worktrees
base_branch = "main"        # default start-point for cms go + target for cms land
commit_cmd = "claude -p --no-session-persistence --model=haiku --tools='' --disable-slash-commands --setting-sources='' --system-prompt=''"
go_cmd = "claude -p \"$CMS_PROMPT\""  # command to run when prompt is given to cms go

[[worktree.hooks]]           # post-create hooks
command = "npm install"

[[worktree.pre_commit]]      # before squash commit
command = "npm run lint"

[[worktree.pre_merge]]       # before landing
command = "npm test"
```

Hooks and `go_cmd` receive `CMS_WORKTREE_PATH` and `CMS_REPO_ROOT` environment
variables. `go_cmd` also receives `CMS_PROMPT`.


See [examples/config.toml](examples/config.toml) for the user config and [examples/config-full.toml](examples/config-full.toml) for every option including status tracking tuning, colors, icons, and dashboard layout.

## Claude Code Hooks (Recommended)

By default, `cms` detects agent activity by observing tmux pane output. For
faster, more accurate status updates, enable Claude Code hooks.

You will be prompted on first run (when no config file detected) to auto
install hooks.


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

# Navigation (headless)
cms next                         # jump to next in list (same flags as cms)

# Worktree operations, tmux nav built in
cms go <branch> [start-point] [prompt]  # switch or create; optional prompt runs go_cmd
cms land [target]                # land current branch into target


cms switch <branch>              # switch to existing branch's worktree
cms switch -c <branch> [start]   # create new branch + worktree
cms rm <branch>                  # remove worktree
```



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

Keys are evaluated left-to-right. The first key that distinguishes two items wins. Within equal items, original order is preserved.

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

## Worktree Management

Manage git worktrees from the CLI. Inspired by [wtp](https://github.com/satococoa/wtp) and [Worktrunk](https://github.com/max-sixty/worktrunk).
Tmux navigation baked in.


### `cms go` — opinionated switch-or-create

Same worktree/tmux behavior as switch, but auto-creates new
branches from the configured `base_branch` if available. Optionally runs a configured
command with a prompt string.

```bash
cms go feature                              # switch if exists, create from base_branch if not
cms go feature main                         # override start-point for this invocation
cms go -                                    # switch to previous branch's worktree
cms go feature "implement feature A"        # create worktree + run go_cmd with prompt
cms go feature main "implement feature A"   # start-point + prompt
```

Options: `--force`/`-f`, `--path <dir>`, `--no-open`.

**Base branch resolution** (for new branches): explicit start-point arg →
`[worktree].base_branch` from project `.cms.toml` → `[worktree].base_branch`
from user `config.toml` → `origin/HEAD` → local `main` → local `master` →
current HEAD. The chosen base is recorded in `git config
branch.<name>.cms-base` so `cms land` can use it as the default target.

The prompt (arg with spaces) is passed to the command configured in `[worktree].go_cmd`. The prompt is available as `$CMS_PROMPT` in the command's environment. If the command string doesn't reference `$CMS_PROMPT`, the prompt is appended as an argument.

### `cms land` — land current branch into target

Run from inside a feature worktree. Squashes commits, rebases onto the target
branch, fast-forward merges, and cleans up the worktree, branch, and tmux
window. The full pipeline:

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

**Target resolution:** `[worktree].base_branch` from project `.cms.toml` → `[worktree].base_branch` from user `config.toml` → `origin/HEAD` → `main` → `master`.

If the target worktree has uncommitted changes, `cms land` will prompt to stash
them before merging and pop them back after. Use `--autostash` to skip the
prompt. If the stash pop conflicts, your changes stay in the stash — run `git
stash list` in the target worktree to find them.

**Backup refs:** With `--squash`, land saves the pre-squash HEAD to `refs/cms-wt-backup/<branch>` so original commit history can be recovered via `git log refs/cms-wt-backup/<branch>`.

**Conflict recovery:** On rebase conflicts, land exits with instructions. Fix
conflicts, `git rebase --continue`, then `cms land --continue` to finish the
merge and cleanup. Or `cms land --abort` to cancel. On `--continue`, branch
resolution is deferred until after the rebase finishes (during a conflicted
rebase, HEAD is detached), ensuring the merge step targets the correct branch.

**LLM commit messages:** With `[worktree].commit_cmd` configured, the diff is
piped to the command for auto-generated commit messages. Falls back to a
default message on failure. Use `--no-squash` to skip squashing and preserve
individual commits.

### The Vibes Report

Vibe score: 9 / 10. This project was entirely written by claude code in a programming
language which I am not at all familiar with.

The following repos were used as direct reference by claude:

- [tms](https://github.com/jrmoulton/tmux-sessionizer?tab=readme-ov-file)
- [worktrunk](https://github.com/max-sixty/worktrunk)
- [tmux-up](https://github.com/jamesottaway/tmux-up)
- [tmux-continuum](https://github.com/tmux-plugins/tmux-continuum/tree/master)

