# Handover

## What Changed

- Added separate Claude and Codex parsers with a shared normalized agent model.
- Refactored watcher, finder, dashboard, switch, and next to use provider-neutral agent state.
- Added Codex-specific parsing for activity, modes, and context percentage.
- Tightened Codex waiting/working detection and reduced false positives from stale scrollback.
- Added live tmux `%output` handling so working state updates during streaming output.
- Fixed the working-state flicker by preventing live rechecks from fighting active output.
- Added debug logging for control-mode, watcher updates, and finder updates.
- Added a render harness for dashboard and finder output.
- Simplified the user-facing config to a single `[general]` section.
- Added `cms config init` to generate the default config file.
- Added optional `search_submodules` support to project scanning.
- Polished dashboard and finder layout, indicators, footer, and summary formatting.

## Current User Config

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

## Useful Commands

```bash
go test ./...
make dev
cms config init
CMS_RENDER_HARNESS=1 go test -run 'TestRenderHarness(Dashboard|Finder)' -v
CMS_LIVE_HARNESS=1 go test -run TestRenderHarnessLive -v
```

## What's Next

- Decide whether to add an attention queue for `waiting` / newly-finished sessions.
- Decide whether to add a `jump to newest attention` command.
- Keep the internal UI/theme config structure in code, but only expose more of it once the user-facing shape is stable.
- If needed, add backward-compat handling for older config shapes.
