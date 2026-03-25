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
cms hook <event>     # send a hook event (used by Claude Code hooks)
cms hook-setup       # print hook configuration for Claude Code settings
cms wt list          # list worktrees (* = current, shows merge status)
cms wt add <branch>  # create worktree + tmux window
cms wt rm <branch>   # remove worktree (agent-aware, safe deletion)
cms wt merge [target] # squash + rebase + merge + cleanup
```

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
completed_decay_ms = 30000  # how long "completed" shows before becoming "idle"
```

## Worktree Management

Manage git worktrees from the CLI. Inspired by [wtp](https://github.com/satococoa/wtp) and [Worktrunk](https://github.com/max-sixty/worktrunk).

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
cms wt add --no-open feature  # skip tmux window creation
cms wt add -f feature         # force (overwrite existing)
```

Branch resolution: local branch > remote tracking > create new. Special symbols: `@` (current), `-` (previous), `^` (default branch).

#### `cms wt merge`

Full merge workflow: squash + rebase + merge + cleanup.

```bash
cms wt merge                  # merge current branch into default, ff-only
cms wt merge --squash         # squash all commits into one before merging
cms wt merge -s -m "message"  # squash with explicit commit message
cms wt merge --no-ff          # create a merge commit even if ff is possible
cms wt merge --keep           # don't remove worktree after merge
```

#### `cms wt rm`

Removes worktree + branch + tmux window. Agent-aware: blocks removal if Claude or Codex agents are running in the worktree panes.

```bash
cms wt rm feature-auth              # remove worktree, delete branch (if merged)
cms wt rm --keep-branch feature     # remove worktree but keep the branch
cms wt rm -f feature                # force: skip checks, delete unmerged branch
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

By default, `cms` detects agent activity by observing tmux pane output. For faster, more accurate status updates, you can enable Claude Code hooks. When hooks are active for a pane, the observer is automatically suppressed; if hooks stop, the observer resumes.

### Setup

1. Generate the hook configuration:

```bash
cms hook-setup
```

2. Copy the printed JSON into your Claude Code settings file (`~/.claude/settings.json`), merging it with any existing hooks.

3. That's it — the next time Claude Code starts in a tmux pane, it will send lifecycle events to `cms` over a Unix socket.

### How it works

`cms` starts a Unix socket listener on launch. The hooks call `cms hook <event>`, which reads Claude Code's JSON payload from stdin, resolves the tmux pane via `$TMUX_PANE`, and forwards a structured event to the running `cms` instance.

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
# Simulate a hook event (run inside a tmux pane):
echo '{"session_id":"test"}' | cms hook session-start
echo '{"tool_name":"Edit"}' | cms hook pre-tool-use
echo '{}' | cms hook stop
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
