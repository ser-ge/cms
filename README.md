# cms

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Commands

```bash
cms
cms dash
cms find
cms switch
cms open
cms next
cms config init
cms hook <event>    # send a hook event (used by Claude Code hooks)
cms hook-setup      # print hook configuration for Claude Code settings
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
```

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
