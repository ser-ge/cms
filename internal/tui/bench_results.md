# Picker & Finder Benchmark Results

Baseline captured 2026-03-27 on Apple M1 Pro.
Optimizations applied on same date.

## Summary of changes

Four optimizations were applied across `picker.go`, `finder.go`, `main.go`, and `watcher.go`:

1. **Reuse fzf slab across resetWith** (`picker.go`) — The 208KB fzf scratch buffer was re-allocated on every `rebuildPicker()` call (triggered by each watcher state/agent/attention update). Now preserved across resets.

2. **Batch highlightMatches** (`picker.go`) — Per-rune `lipgloss.Render()` calls (52 allocs for 8 matches) replaced with consecutive run batching. Groups matched and unmatched rune runs, renders each group once. Typically 3 Render calls instead of 10+.

3. **Pre-allocate slices in build methods** (`finder.go`) — `buildSessionItems`, `buildQueueItems`, `buildPaneItems`, `buildWindowItems` now pre-allocate output slices using known capacity from session/pane counts instead of growing via append from nil.

4. **Pre-bootstrap watcher + sync scans** (`main.go`, `watcher.go`, `finder.go`) — `BootstrapSync()` called before `NewRootModel()` so the watcher cache has session/agent data when the finder is created. Projects (~5ms) and marks (~1ms) are scanned synchronously in `newFinderModel`. Worktrees/branches remain async (expensive git merge-status checks, ~250ms). `Init()` skips scans already completed. Redundant `initState()` in `bootstrap()` is skipped when `BootstrapSync` already ran.

### Before vs After: perceived startup

```
Before:  alt screen → "Loading..." → projects (5ms) → sessions+agents (120ms) → worktrees (250ms)
After:   BootstrapSync (120ms) → alt screen → all sections visible except worktrees/branches
         → worktrees/branches pop in ~250ms later (git merge-status checks)
```

## Live CLI Profile (baseline, before optimizations)

Against real tmux: 3 sessions, 14 panes, 5 agents, 121 projects, 142 finder rows.

| Phase | Time | Notes |
|---|---:|---|
| config.Load() | 200µs | |
| InitStyles() | 28µs | |
| FetchState() | 111ms | tmux IPC (`list-panes -a`) |
| DetectAll() | 7ms | `ps aux` + screen scrape per agent pane |
| project.Scan() | 5ms | filesystem walk, 121 projects |
| BootstrapSync() | 148ms | watcher init → FetchState + DetectAll again |
| RunHeadless() | 315ms | build items + picker + sync scans |
| PlainSnapshot() | 55µs | |
| NewRootModel() | 5ms | TUI path (async scans, shows "Loading..." immediately) |
| View() | 1µs | first render |

**Baseline TUI startup**: alt screen appears in ~5ms but shows "Loading..." then staggered pop-in over 120-370ms as async scans complete.

**Bottleneck**: tmux IPC (`FetchState`) dominates at ~111ms per call. Picker/finder Go code is ~5ms.

## Live CLI Profile (after optimizations)

Same tmux: 3 sessions, 14 panes, 4 agents, 121 projects, 153 finder rows.

| Phase | Time | Notes |
|---|---:|---|
| config.Load() | 240µs | |
| InitStyles() | 76µs | |
| BootstrapSync() | 136ms | watcher init → FetchState + DetectAll (once) |
| NewRootModel() | 10ms | builds sessions/agents/projects/queue/panes/windows/marks sync |
| View() | 1µs | first render — all sections except worktrees/branches |

**After TUI startup**: BootstrapSync runs before alt screen (~136ms), then first render has all sections populated. Worktrees/branches arrive async (~250ms git ops). No "Loading..." screen.

## Synthetic Benchmark Comparison

### Allocation reductions (median of 3 runs, count=3)

| Benchmark | Before B/op | After B/op | Delta | Before allocs | After allocs |
|---|---:|---:|---:|---:|---:|
| HighlightMatches/8 | 888 | 496 | **-44%** | 52 | 28 (-46%) |
| View 10/with_filter | 7,558 | 5,717 | **-24%** | 322 | 212 (-34%) |
| View 50/with_filter | 26,302 | 19,305 | **-27%** | 1,108 | 690 (-38%) |
| View 200/with_filter | 43,191 | 36,196 | **-16%** | 1,135 | 717 (-37%) |
| BuildSessionItems/5 | 6,437 | 4,323 | **-33%** | 198 | 192 |
| BuildSessionItems/50 | 60,372 | 41,539 | **-31%** | 1,865 | 1,853 |
| BuildWindowItems/5 | 5,284 | 3,170 | **-40%** | 135 | 129 |
| BuildWindowItems/50 | 49,388 | 30,552 | **-38%** | 1,239 | 1,227 |
| BuildPaneItems/50 | 207,941 | 178,450 | **-14%** | 3,287 | 3,272 |
| BuildQueueItems/50 | 72,402 | 63,430 | **-12%** | 1,778 | 1,765 |
| FullRefresh/5 | 302,102 | 294,274 | **-3%** | 1,245 | 1,219 |
| FullRefresh/20 | 528,249 | 490,271 | **-7%** | 3,945 | 3,902 |
| FullRefresh/50 | 957,222 | 881,101 | **-8%** | 9,190 | 9,138 |

### Baseline synthetic benchmarks (before optimizations)

#### Picker core

| Benchmark | ns/op | B/op | allocs | Notes |
|---|---:|---:|---:|---|
| PickerNew items=10 | 22,544 | 213,445 | 10 | 208KB is fzf slab |
| PickerNew items=1000 | 23,245 | 213,388 | 10 | slab dominates, item count irrelevant |
| ApplyFilter 200/short | 119,417 | 48,369 | 814 | |
| ApplyFilter 200/multi_token | 291,908 | 101,194 | 1,414 | 2x single token |
| ApplyFilter 1000/multi_token | 1,462,387 | 473,939 | 7,016 | |
| ResetWith items=200 | 155,251 | 261,862 | 830 | re-creates slab every time |
| View 50/with_filter | 222,388 | 26,298 | 1,108 | |
| HighlightMatches 8 | 10,013 | 888 | 52 | per-rune lipgloss Render |

#### Finder

| Benchmark | ns/op | B/op | allocs | Notes |
|---|---:|---:|---:|---|
| BuildSessionItems 50 | 354,508 | 60,366 | 1,865 | |
| BuildPaneItems 50 | 506,069 | 207,885 | 3,286 | most expensive builder |
| BuildWindowItems 50 | 192,591 | 49,380 | 1,239 | |
| RebuildPicker 50/all | 150,559 | 517,202 | 741 | includes 208KB slab |
| FullRefresh 50 | 1,830,024 | 957,163 | 9,189 | ~1MB per refresh |
| SortedSectionItems 200 | 12,912 | 45,497 | 5 | sorting is fast |

#### Live benchmarks (real tmux IPC)

| Benchmark | ns/op | B/op | allocs |
|---|---:|---:|---:|
| FetchState | 111,160,120 | 2,331,444 | 4,487 |
| DetectAll | 7,157,594 | 454,155 | 406 |
| ProjectScan | 3,780,385 | 741,391 | 7,149 |
| BootstrapSync | 147,923,743 | 3,136,103 | 5,286 |
| RunHeadless | 315,345,533 | 4,181,540 | 15,184 |
| NewRootModel | 5,075,942 | 277,841 | 339 |

## Key findings

1. **FetchState is 93% of startup** — tmux IPC dominates at ~111ms. Now called once (BootstrapSync), not twice.
2. **Slab re-allocation fixed** — 208KB fzf slab preserved across `resetWith`/`rebuildPicker` calls.
3. **highlightMatches fixed** — batched runs: 52 → 28 allocs, 888 → 496 B/op per call.
4. **Slice pre-allocation** — build methods save 12-40% B/op by pre-sizing output slices.
5. **Sync bootstrap eliminates pop-in** — sessions/agents/projects/queue/panes/windows/marks all visible on first render.
6. **Worktrees remain async** — git merge-status checks cost ~250ms (16 worktrees); blocking would add too much latency.
7. **View is near-free** — only renders visible items (viewport windowing works).

## Remaining targets

| Target | Savings | Difficulty |
|---|---|---|
| Debounce rebuildPicker on rapid watcher updates | fewer full rebuilds | medium |
| Speed up worktree IsBranchIntegrated checks | reduce async pop-in | hard (git IPC) |

## How to run

```sh
# Synthetic benchmarks (no tmux needed)
go test -run='^$' -bench='^Benchmark[^L]' -benchmem ./internal/tui/

# Live profile (requires tmux)
CMS_TMUX_SOCKET=/private/tmp/tmux-$(id -u)/default \
  go test -v -run TestProfileLiveStartup ./internal/tui/

# Live benchmarks
CMS_TMUX_SOCKET=/private/tmp/tmux-$(id -u)/default \
  go test -run='^$' -bench='BenchmarkLive' -benchmem ./internal/tui/
```
