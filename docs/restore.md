# Session Save & Restore

cms automatically saves and restores tmux session layouts so that reopening a project brings back your windows, panes, and working directories.

## How it works

### Auto-save

Every time a TUI command exits (finder, dashboard), cms snapshots **all** tmux sessions that live in a git repository. Each snapshot captures:

- Window names, indices, and layouts
- Pane working directories
- Active window and pane (focus position)

Snapshots are stored as JSON at `~/.local/state/cms/snapshots/<hash>.json`, where the hash is derived from the repo root and session name.

Headless commands (`cms next`, `cms jump`, `cms mark`, etc.) do not trigger saves — they're designed to be fast.

### Auto-restore

When `cms` opens a project that doesn't have a running tmux session, it tries to restore a saved snapshot before falling back to plain session creation. The restore recreates:

1. All windows with their original names
2. Panes within each window, set to their saved working directories
3. Window layouts (split ratios)
4. Focus (active window and pane)

This happens automatically in the `OpenProject` flow — the same path used when selecting a project from the finder.

### Manual save

You can also save the current session explicitly:

```
cms session save
```

This saves only the currently attached session.

## Configuration

### Global restore flag

```toml
# ~/.config/cms/config.toml

[general]
restore = true   # default: true
```

Set `restore = false` to disable snapshot restoration entirely. Sessions will always be created fresh. Auto-save still runs (so you can re-enable restore later without losing state).

### Per-project session mode

The global `restore` flag is a master switch. Per-project config (`.cms.toml`) can further control behavior via `session.mode`:

```toml
# .cms.toml (at repo root)

[session]
mode = "template_then_restore"   # default
```

| Mode                    | Behavior                                    |
|-------------------------|---------------------------------------------|
| `template_then_restore` | Try template bootstrap first, then snapshot  |
| `restore_only`          | Only restore from snapshot, no template      |
| `template_only`         | Only use template, ignore snapshots          |

The global `restore = false` overrides all per-project modes — no snapshot restoration will be attempted regardless of the project's `session.mode`.

## Storage

Snapshots live under `$XDG_STATE_HOME/cms/snapshots/` (defaults to `~/.local/state/cms/snapshots/`). Each file is a small JSON document named by SHA1 hash. You can safely delete individual files or the entire directory to reset saved state.
