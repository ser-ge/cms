# Asciinema Demo (hand-recorded)

Record your real terminal — your shell prompt, colors, everything.
You drive the demo live.

Best for: authentic recordings that show the real look and feel.

## Prerequisites

```
brew install asciinema agg
```

## Quick start

```bash
# 1. Set up demo environment (builds cms, creates repos + tmux sessions)
#    This also loads a minimal tmux config and sets CMS_CONFIG_DIR
#    in the tmux environment. No existing tmux server needed — one
#    will be started automatically.
./scripts/demo-setup.sh

# 2. Source the env in your shell (for direct CLI use / asciinema)
source /tmp/cms-demo/env.fish      # fish
source /tmp/cms-demo/env.sh        # bash/zsh

# 3. Start recording
asciinema rec demo.cast

# 4. Do the demo (this is recorded)
cms sessions,worktrees
#   ↕  scroll with j/k or arrows
#   type to filter
#   Enter to select
#   q to quit

# 5. Stop recording
#   Ctrl-D or type 'exit'

# 6. Convert to GIF
agg demo.cast demo.gif
```

## What the setup does

- Builds `cms` to `/tmp/cms-demo/cms`
- Creates test git repos with worktrees in `/tmp/cms-demo/repos/`
- Writes a cms config to `/tmp/cms-demo/config/config.toml` (uses
  `CMS_CONFIG_DIR`, not `XDG_CONFIG_HOME` — your shell config is untouched)
- Loads `scripts/demo-tmux.conf` (minimal: navigation, splits, cms
  bindings — no theme or plugins)
- Sets `CMS_CONFIG_DIR` and `PATH` in the tmux global environment so
  popup commands (`C-s C-s`, etc.) find the demo binary and config

## Recording tips

- **Resize your terminal** before recording — `agg` captures at the
  recorded terminal size. 120x35 is a good size for demos.
- **Pause between actions** so viewers can follow. No need to rush.
- **Re-record freely** — `asciinema rec` overwrites if you pass the same file,
  or just record to a new file and pick the best take.
- **Preview first** — `asciinema play demo.cast` replays in terminal before
  you commit to GIF conversion.

## agg options

```bash
# Slower playback (default 1.0)
agg --speed 0.8 demo.cast demo.gif

# Custom font size
agg --font-size 16 demo.cast demo.gif

# Custom theme (agg uses asciinema themes)
agg --theme monokai demo.cast demo.gif

# Limit idle time (cap pauses to 2s max)
agg --idle-time-limit 2 demo.cast demo.gif

# All together
agg --speed 0.8 --idle-time-limit 2 --font-size 14 demo.cast demo.gif
```

## Cleanup

```bash
./scripts/demo-setup.sh --cleanup
```

This kills demo sessions, unsets tmux env vars, and removes `/tmp/cms-demo`.

Or re-run setup to refresh:

```bash
./scripts/demo-setup.sh              # recreates everything
./scripts/demo-setup.sh --keep-repos # rebuild cms only, keep repos
```

## demo-setup.sh options

| Flag | Default | Notes |
|------|---------|-------|
| `--sections <list>` | `sessions,worktrees` | Controls finder.include in config |
| `--keep-repos` | off | Skip repo creation if already present |
| `--agents` | off | Launch real Claude agents in some panes |
| `--cleanup` | — | Tear down demo sessions, tmux env, and temp files |
