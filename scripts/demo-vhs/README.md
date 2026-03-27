# VHS Demo (scripted)

Automated, repeatable GIF recording. VHS runs its own headless terminal —
it does **not** capture your real tmux/shell config.

Best for: reproducible demos, README GIFs, CI-generated assets.

## Prerequisites

```
brew install charmbracelet/tap/vhs
```

## Quick start

```bash
# Record with defaults (sessions,worktrees, Catppuccin Mocha theme)
./scripts/vhs-record.sh

# Customize
./scripts/vhs-record.sh \
  --output cms-finder.gif \
  --theme Nord \
  --sections projects,worktrees \
  --font-size 20
```

Output lands in the current directory.

## How it works

1. Builds cms from source
2. Creates fake git repos with worktrees (via `create-test-repos.sh`)
3. Starts an **isolated** tmux server (won't touch your real sessions)
4. Writes a temporary cms config pointing at the test repos
5. Runs VHS with `demo.tape` (substituting paths into the template)
6. Cleans up everything on exit

## Editing the demo

Edit `scripts/demo.tape` — it's a template with `__PLACEHOLDER__` tokens.
VHS commands: `Type`, `Enter`, `Sleep`, `Down`, `Up`, `Ctrl+U`, `Escape`, etc.

```tape
# Example: add a pause before selecting
Down
Sleep 400ms
Enter
Sleep 1s
```

See `vhs manual` for the full command reference.

## Options

| Flag | Default | Notes |
|------|---------|-------|
| `--output <file>` | `demo.gif` | .gif, .mp4, .webm supported |
| `--theme <name>` | `Catppuccin Mocha` | `vhs themes` to list all |
| `--width <px>` | `1200` | Terminal width |
| `--height <px>` | `600` | Terminal height |
| `--font-size <n>` | `16` | |
| `--sections <list>` | `sessions,worktrees` | Comma-separated cms sections |
| `--tape <file>` | `scripts/demo.tape` | Custom tape template |
