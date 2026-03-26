# Finder Picker Design System

The finder is the universal fuzzy picker in cms. It composes 8 section types
into a single searchable list. This document describes the visual design
system used to render each section consistently.

## Design Rationale

The finder's hierarchy mirrors the tmux/git structure:
- **Session** = project/repo (one session per repo)
- **Worktree** = a window within a session (opened via `cms go`)
- **Window/Pane** = explicit tmux-level views

Sessions, windows, and worktrees show **aggregate** state counts (not
per-agent detail) because they are containers. Panes and queue show
**per-agent detail** because they are leaf items where you need provider,
context%, mode, and duration to decide what to act on.

Icons default to ASCII (`S W > B M P *`) for universal terminal font
compatibility. The exception is `⎇` for worktrees (the standard git
branch symbol). Users can override with Unicode glyphs via config.

This replaces the previous `[finder.active_indicator]` config which used
a single `▪` icon for all sections with no type distinction.

## Section Icons

Each section has a distinct icon glyph that identifies the item type. The
icon's **color** encodes the item's state. Icons are configurable via
`[finder.section_icons]` in `config.toml`.

| Section   | Default | Meaning              |
|-----------|---------|----------------------|
| Sessions  | `S`     | tmux session         |
| Queue     | `*`     | agent attention item |
| Worktrees | `⎇`     | git worktree         |
| Branches  | `B`     | local git branch     |
| Windows   | `W`     | tmux window          |
| Panes     | `>`     | tmux pane            |
| Marks     | `M`     | named pane bookmark  |
| Projects  | `P`     | scanned project dir  |

## Icon Color = State

Icon color comes from the existing activity style palette. There are two
categories of sections:

### Agent-bearing sections (sessions, queue, worktrees, windows, panes)

The icon is colored by the agent's activity state using the same styles
that color activity text in the dashboard:

| Activity  | Style          | Typical Color |
|-----------|----------------|---------------|
| working   | `workingStyle` | green         |
| waiting   | `waitingStyle` | orange        |
| completed | `waitingStyle` | orange        |
| idle      | `idleStyle`    | blue/dim      |
| no agent  | `dimStyle`     | gray          |

For **aggregate sections** (sessions, windows) that may contain multiple
agents, the icon takes the color of the **most urgent** activity. Urgency
is determined by `state_order` config (default: waiting > completed >
working > idle). This reuses the same `stateRank()` function used for
queue sorting.

For **queue** items specifically:
- **Unseen** attention: activity color + **bold**
- **Seen**: activity color + **faint**

### Worktrees (aggregate, like sessions)

Worktrees use the same icon-color-by-urgency logic as sessions and
windows. When agents are running inside a worktree, the icon takes the
color of the most urgent activity. When panes exist but no agents are
running, icon uses `workingStyle`. Otherwise `dimStyle`.

"Active" means: has tmux panes inside the worktree path, or has running
agents.

### Non-agent sections (branches, marks, projects)

| State    | Style          |
|----------|----------------|
| Active   | `workingStyle` |
| Inactive | `dimStyle`     |

"Active" means:
- Branches: has an associated worktree
- Marks: the marked pane is still alive
- Projects: has an open tmux session

## Two Presentation Tiers

### Aggregate (sessions, windows, worktrees)

Shows counts, not per-agent detail. Format:

```
icon title  {N}w|{N}p  {state counts}  [attached]
```

State counts render non-zero activity counts as styled text:
`1 waiting · 2 working · 1 idle`. Each count is styled with the
matching activity color. Zero-count states are omitted.

Examples:
```
 S cms          1w · 1 waiting · 1 working · 1 idle · attached
 S gather_git   1w · 1 working
 W cms:fish     3p · 1 waiting · 1 working · 1 idle
```

### Detail (queue, panes)

Fixed-width columnar descriptions for per-agent data:

**Queue columns**: `provider  context%  activity  mode  duration`
```
 * cms/feature/refactor*    claude   42%  idle       accept edits       8m
 * cms/codex/functionality  codex     0%  working    plan mode         15s
```

**Pane columns**: `path  provider  context%  activity  mode`
```
 > cms:fish.0        ~/projects/cms   claude   42%  idle     accept edits
 > cms:fish.1        ~/projects/cms   codex     0%  working  plan mode
   gather_git:main.1 ~/projects/foo   zsh
```

Non-agent panes show `path  command` without the columnar layout.

Column widths:
- `providerW = 6`  (longest: "claude")
- `contextW  = 4`  (longest: "100%")
- `activityW = 9`  (longest: "completed")
- `modeW     = 15` (longest: "workspace-write")
- `durationW = 4`  (longest: "999m")

Path width is computed per-rebuild as the max across all pane paths.

## Path Shortening

All displayed paths use `CompactPath(ShortenHome(path), maxLen)`:

1. `ShortenHome` replaces the home directory with `~`
2. `CompactPath` abbreviates each intermediate directory to its first
   character, keeping the last component in full

Examples:
- `~/projects/cms/worktrees/feature` → `~/p/c/w/feature`
- `~/projects/gather_git` → `~/p/gather_git`
- `~/notes` → `~/notes` (only 2 components, no abbreviation)

Abbreviation only triggers when the path exceeds `maxLen` (currently 25
characters). Short paths like `~/projects/cms` stay full; longer paths
like `~/projects/cms/worktrees/feature` (31 chars) get abbreviated.

### Worktrees (aggregate counts)

Title is `project/branch`. Description shows agent state counts (from
live data, recomputed on every picker rebuild) plus static merged status
(computed once at scan time):

```
 ⎇ cms/main
 ⎇ cms/ui          1 waiting · 1 completed
 ⎇ cms/work        1 working · [merged: ancestor of main]
 ⎇ cms/restore     [merged: same commit as main]
```

### Simple (branches, marks, projects)

Free-form descriptions with middle-dot separators via `JoinParts()`:

```
 B fix/typo  local branch
 M build     cms:fish
 P notes     ~/notes · main*
```

## Title Alignment

Titles are padded per-section in `rebuildPicker()`. After collecting items
from all sections, each section's titles are right-padded with spaces to
the maximum title width within that section. This ensures description
columns align vertically within each section.

## Rendering Pipeline

In `picker.go View()`, each line is assembled as:

```
" " + icon + " " + title + " " + description
```

Where:
- `icon` = pre-rendered styled string from `item.Icon` (or 1-space padding)
- `title` = plain text, padded per-section
- `description` = styled via `pickerDescStyle`

The `PickerItem.Icon` field is set by each section builder with the
appropriate style baked in. The picker renderer does not need to know
about section types or state — it just displays the pre-rendered icon.

## Cross-Section Dedup

When overlapping sections are composed:
- Projects with open sessions are hidden when sessions section is visible
- Branches with worktrees are hidden when worktrees section is visible

## Config Reference

```toml
[finder.section_icons]
sessions  = "S"
queue     = "*"
worktrees = "⎇"
branches  = "B"
panes     = ">"
windows   = "W"
marks     = "M"
projects  = "P"
```

## Harness Tests

Visual output can be inspected with:

```bash
# Synthetic data (all section types):
CMS_RENDER_HARNESS=1 go test ./internal/tui/ -run TestRenderHarnessAllSections -v

# Individual sections:
CMS_RENDER_HARNESS=1 go test ./internal/tui/ -run 'TestRenderHarness(Finder|Queue|Dashboard)' -v

# Live tmux state:
CMS_LIVE_HARNESS=1 go test ./internal/tui/ -run TestRenderHarnessLive -v
```
