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
