# Finder Sort

The finder sorts items within each section using a configurable list of **sort keys**. Keys are evaluated left-to-right; the first key that distinguishes two items wins.

## Config

```toml
[finder]
sort = ["-current"]  # global default

[finder.sessions]
sort = ["recent", "-current"]  # per-section override
```

A section with no `sort` inherits the global `[finder].sort`. An empty list means no sorting (stable order preserved).

Prefix `-` **demotes** matching items to the bottom. Without prefix, matching items are **promoted** to the top.

## Sort keys

| Key | Type | Meaning | Sections |
|-----|------|---------|----------|
| `active` | bool | Items with `Active=true` | all |
| `current` | bool | Currently focused item | sessions, worktrees, branches, windows, panes |
| `recent` | bool | Most recently visited | sessions |
| `state` | numeric | Rank from `state_order` list | queue |
| `unseen` | bool | Has unseen attention events | queue |
| `oldest` | numeric | Oldest activity timestamp first | queue |
| `newest` | numeric | Newest activity timestamp first | queue |

**Bool keys** compare true vs false. Promoted: true sorts before false. Demoted (`-`): true sorts after false.

**Numeric keys** compare values directly. Promoted: lower value first. Demoted (`-`): higher value first.

Keys that don't apply to a section (e.g. `recent` for worktrees) are no-ops — they never distinguish items, so the next key takes over.

## What "Active" means per section

| Section | Active when |
|---------|------------|
| sessions | attached |
| worktrees | has tmux pane with matching working dir |
| projects | has tmux session with matching name |
| branches | has worktree checked out |
| panes | has running agent |
| windows | has running agent in any pane |
| queue | has unseen attention events |
| marks | pane still alive |

Active is **always computed** regardless of sort config. The `active` sort key only controls ordering.

## Defaults

```toml
[finder]
sort = ["active", "-current"]
state_order = ["waiting", "completed", "idle", "working"]

[finder.sessions]
sort = ["recent", "-current"]

[finder.queue]
sort = ["state", "unseen", "oldest"]
```

All other sections inherit `["active", "-current"]`.

## Worked examples

### Sessions: `["recent", "-current"]`

Given: `api` (attached), `web`, `docs` (last-visited), `infra`

1. **"recent"**: `docs` promoted to top.
2. **"-current"**: Among the rest, `api` (attached = current) demoted to bottom.

Result (nearest to input first): `docs, infra, web, api`

### Queue: `["state", "unseen", "oldest"]`

Given 4 agent panes with `state_order = ["waiting", "completed", "idle", "working"]`:

| Pane | activity | unseen | since |
|------|----------|--------|-------|
| A | waiting | yes | 2m ago |
| B | waiting | no | 5m ago |
| C | working | no | 1m ago |
| D | completed | yes | 3m ago |

1. **"state"**: waiting (rank 0) before completed (rank 1) before working (rank 2). Groups: {A,B}, {D}, {C}.
2. **"unseen"**: Within {A,B}: A (unseen) before B.
3. **"oldest"**: Tiebreak by timestamp, oldest first.

Result: `A, B, D, C`

### Worktrees: `["active", "-current"]`

Given: `main` (current, has pane), `feature-auth` (has pane), `feature-ui` (no pane)

1. **"active"**: `main` and `feature-auth` (both have panes) before `feature-ui`.
2. **"-current"**: Among active items, `main` (current) demoted.

Result: `feature-auth, main, feature-ui`

## Interaction with fuzzy filtering

When a filter query is active, the picker applies its own fzf-score sort that overrides section ordering. Active items still sort before inactive in fuzzy results (hardcoded in picker). The config-driven sort only matters when no query is typed.

## Section order vs item sort

The `include` list and CLI flags control which sections appear and in what order. Sort keys only affect ordering **within** a section.

```toml
[finder]
include = ["sessions", "queue", "worktrees", "marks", "projects"]
```

In the picker, the first section in `include` appears closest to the input. Section order is independent of sort.
