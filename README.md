# cms

tmux session switcher and dashboard with Claude and Codex pane detection.

## Development

Run the normal test suite:

```bash
go test ./...
```

Write the default config file to `$XDG_CONFIG_HOME/cms/config.toml` or `~/.config/cms/config.toml`:

```bash
cms config init
```

Print canned dashboard and finder render output from the UI harness:

```bash
CMS_RENDER_HARNESS=1 go test -run 'TestRenderHarness(Dashboard|Finder)' -v
```

Print live dashboard and finder render output from the current tmux state:

```bash
CMS_LIVE_HARNESS=1 go test -run TestRenderHarnessLive -v
```

These harness tests live in `ui_harness_test.go` and are intended for UI debugging and regression checks.

## Config

`cms` reads config from `~/.config/cms/config.toml`.

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

Notes:

- `search_submodules = true` includes Git submodule checkouts where `.git` is a file
- `attached_last` controls whether the currently attached tmux session is pushed to the end of finder session results
- `last_session_first` promotes tmux's last session to the front of finder session results when available
- Additional UI/theme settings exist in code defaults but are not exposed in the user config yet
