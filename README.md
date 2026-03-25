# cms

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Commands

```bash
cms                  # dashboard (default)
cms dash             # dashboard
cms find             # fuzzy-find sessions + projects
cms switch           # switch sessions only
cms open <path>      # open project as tmux session
cms next             # jump to next agent needing attention
cms queue            # attention queue
cms refresh [name]   # add missing worktree windows to sessions
cms config init      # scaffold default config
```

### Worktree Management

Manage git worktrees from the CLI. Inspired by [wtp](https://github.com/satococoa/wtp) and [Worktrunk](https://github.com/max-sixty/worktrunk).

```bash
cms wt list          # list worktrees (* = current, shows merge status)
cms wt add <branch>  # create worktree + tmux window
cms wt rm <branch>   # remove worktree (agent-aware, safe deletion)
cms wt merge [target] # squash + rebase + merge + cleanup
```

#### `cms wt list`

Shows all worktrees with the current one marked `*`. Branches that have been merged into the default branch show `[merged: reason]`.

```
*  main          .
   feature-auth  ../worktrees/feature-auth
   old-fix       ../worktrees/old-fix       [merged: ancestor of main]
```

#### `cms wt add`

Creates a worktree and opens a tmux window for it.

```bash
cms wt add feature/auth       # auto-creates branch, path = ../worktrees/feature-auth
cms wt add -b my-branch       # explicit new branch
cms wt add feature/auth path  # custom path
cms wt add --no-open feature  # skip tmux window creation
cms wt add -f feature         # force (overwrite existing)
```

Branch resolution:
1. Local branch exists -- use it
2. Remote branch exists -- track it automatically
3. Neither -- create a new branch

Path generation sanitizes branch names: `feature/auth` becomes `feature-auth` in the filesystem.

**Special symbols:**

| Symbol | Meaning |
|--------|---------|
| `@` | Current branch |
| `-` | Previous branch (from reflog) |
| `^` | Default branch (main/master) |

#### `cms wt merge`

Full merge workflow: squash + rebase + merge + cleanup. Inspired by [Worktrunk](https://github.com/max-sixty/worktrunk).

```bash
cms wt merge                  # merge current branch into default (main/master), ff-only
cms wt merge develop          # merge into a specific target branch
cms wt merge --squash         # squash all commits into one before merging
cms wt merge -s -m "message"  # squash with explicit commit message
cms wt merge --no-ff          # create a merge commit even if ff is possible
cms wt merge --keep           # don't remove worktree after merge
cms wt merge -f               # skip safety checks
```

**What it does (in order):**

1. Stages uncommitted changes (if `--squash`)
2. Runs **pre-commit** hooks
3. Squashes commits to a single commit (if `--squash`)
4. Generates commit message -- explicit (`-m`), LLM (`commit_cmd` config), or default
5. Runs **post-commit** hooks
6. Rebases onto the target branch
7. Runs **pre-merge** hooks
8. Merges into target (fast-forward or `--no-ff`)
9. Runs **post-merge** hooks (in the target worktree)
10. Removes the worktree + branch + tmux window (unless `--keep`)
11. Switches to the target worktree

**LLM commit messages:**

Configure a command that reads a diff from stdin and outputs a commit message:

```toml
[worktree]
commit_cmd = "llm -m claude-haiku"
```

The diff is piped to the command with a prompt asking for a concise commit message. Falls back to a default message if the command fails.

#### `cms wt rm`

Removes a worktree with safety checks.

```bash
cms wt rm feature-auth              # remove worktree only
cms wt rm --with-branch feature     # also delete the branch (if merged)
cms wt rm -f --with-branch feature  # force delete even if not merged
```

Safety features:
- **Agent-aware**: blocks removal if Claude or Codex agents are running in any tmux pane inside the worktree. Use `--force` to override.
- **Safe branch deletion**: `--with-branch` checks if the branch is integrated into the default branch (same commit, ancestor, or no unique commits via `git cherry`). Skips deletion with a warning if not merged.
- **Self-protection**: refuses to remove the worktree you're currently inside.
- **Main protection**: refuses to remove the main worktree.

#### Hooks & Configuration

Worktree settings come from two sources, merged together (project overrides user):

1. **User config** (`~/.config/cms/config.toml` `[worktree]` section) -- personal defaults
2. **Per-repo config** (`.cms.toml` at repo root) -- project-specific, checked into git

If a project config sets hooks, they replace user hooks entirely. Scalars (`base_dir`, `commit_cmd`) use the project value if set, otherwise fall back to user config.

**Lifecycle hooks:**

| Hook | When | Blocking |
|------|------|----------|
| `hooks` (post-create) | After `cms wt add` | Yes |
| `pre_remove` | Before `cms wt rm` | Yes |
| `pre_commit` | Before squash commit in `cms wt merge` | Yes |
| `post_commit` | After squash commit in `cms wt merge` | Warning only |
| `pre_merge` | After rebase, before merge in `cms wt merge` | Yes |
| `post_merge` | After merge into target branch | Warning only |

Each hook is a shell command. `CMS_WORKTREE_PATH` and `CMS_REPO_ROOT` env vars are set automatically.

**Per-repo config** (`.cms.toml` at repo root, checked into git):

```toml
# .cms.toml

[worktree]
base_dir = "../worktrees"
commit_cmd = "llm -m claude-haiku"

[[worktree.hooks]]
command = "cp $CMS_REPO_ROOT/.env .env"

[[worktree.hooks]]
command = "ln -s $CMS_REPO_ROOT/node_modules node_modules"

[[worktree.hooks]]
command = "npm install"

[[worktree.pre_remove]]
command = "kill-dev-server.sh"

[[worktree.pre_commit]]
command = "npm run lint && npm run typecheck"

[[worktree.pre_merge]]
command = "npm test"
```

**User config** (`~/.config/cms/config.toml`) -- personal defaults:

```toml
[worktree]
base_dir = "../worktrees"
commit_cmd = "llm -m claude-haiku"

[[worktree.hooks]]
command = "cp $CMS_REPO_ROOT/.env .env"
```

Hook commands receive these environment variables:
- `CMS_WORKTREE_PATH` -- path to the target worktree
- `CMS_REPO_ROOT` -- path to the main worktree

## Config

Write the default config:

```bash
cms config init
```

This writes to:

- `$XDG_CONFIG_HOME/cms/config.toml`, or
- `~/.config/cms/config.toml`

Current user-facing config:

```toml
[general]
default_session = ""
switch_priority = ["waiting", "idle", "default", "working"]
escape_chord = "jj"
escape_chord_ms = 250
exclusions = []
attached_last = true
last_session_first = true
search_submodules = false
search_paths = [
  { path = "~/projects", max_depth = 3 }
]
```

## Development

Run tests:

```bash
go test ./...
```

Run the dev build:

```bash
make dev
```

Render harness:

```bash
CMS_RENDER_HARNESS=1 go test -run 'TestRenderHarness(Dashboard|Finder)' -v
CMS_LIVE_HARNESS=1 go test -run TestRenderHarnessLive -v
```
