# Picker & Finder Benchmark Results

Baseline captured 2026-03-27 on Apple M1 Pro.

## Live CLI Profile

Against real tmux: 3 sessions, 14 panes, 5 agents, 121 projects, 142 finder rows.

| Phase | Time | Notes |
|---|---:|---|
| config.Load() | 200µs | |
| InitStyles() | 28µs | |
| FetchState() | 111ms | tmux IPC (`list-panes -a`) |
| DetectAll() | 7ms | `ps aux` + screen scrape per agent pane |
| project.Scan() | 5ms | filesystem walk, 121 projects |
| BootstrapSync() | 148ms | watcher init → FetchState + DetectAll again |
| RunHeadless() | 315ms | build items + picker + sync scans (project/worktree/branch/marks) |
| PlainSnapshot() | 55µs | |
| NewRootModel() | 5ms | TUI path (async scans, shows "Loading..." immediately) |
| View() | 1µs | first render |

**TUI time-to-first-paint**: ~264ms (FetchState + BootstrapSync + NewRootModel).
Async scans (projects, worktrees, branches, marks) arrive later and trigger `rebuildPicker`.

**Bottleneck**: tmux IPC called twice (~111ms each). Picker/finder Go code is 5ms.

## Synthetic Benchmarks (unit, no tmux)

### Picker core

| Benchmark | ns/op | B/op | allocs | Notes |
|---|---:|---:|---:|---|
| PickerNew items=10 | 22,544 | 213,445 | 10 | 208KB is fzf slab |
| PickerNew items=1000 | 23,245 | 213,388 | 10 | slab dominates, item count irrelevant |
| ApplyFilter 10/short | 6,524 | 2,656 | 50 | |
| ApplyFilter 50/short | 32,020 | 11,936 | 212 | |
| ApplyFilter 200/short | 119,417 | 48,369 | 814 | |
| ApplyFilter 200/multi_token | 291,908 | 101,194 | 1,414 | 2x single token |
| ApplyFilter 1000/short | 584,422 | 209,909 | 4,016 | |
| ApplyFilter 1000/multi_token | 1,462,387 | 473,939 | 7,016 | |
| ResetWith items=10 | 30,003 | 216,146 | 66 | re-creates slab every time |
| ResetWith items=50 | 60,909 | 225,427 | 228 | |
| ResetWith items=200 | 155,251 | 261,862 | 830 | |
| View 10/no_filter | 27,750 | 3,923 | 92 | |
| View 50/with_filter | 222,388 | 26,298 | 1,108 | |
| View 200/with_filter | 225,025 | 43,182 | 1,135 | |
| HighlightMatches 8 | 10,013 | 888 | 52 | per-rune lipgloss Render |

### Finder (combined picker with sections)

| Benchmark | ns/op | B/op | allocs | Notes |
|---|---:|---:|---:|---|
| BuildSessionItems 5 | 36,540 | 6,436 | 198 | |
| BuildSessionItems 20 | 142,605 | 26,754 | 753 | |
| BuildSessionItems 50 | 354,508 | 60,366 | 1,865 | |
| BuildQueueItems 50 | 340,106 | 72,391 | 1,778 | |
| BuildPaneItems 50 | 506,069 | 207,885 | 3,286 | most expensive builder |
| BuildWindowItems 50 | 192,591 | 49,380 | 1,239 | |
| RebuildPicker 50/sessions | 53,850 | 236,883 | 128 | includes 208KB slab |
| RebuildPicker 50/all_sections | 150,559 | 517,202 | 741 | |
| FullRefresh 5 | 296,745 | 302,171 | 1,246 | |
| FullRefresh 20 | 803,299 | 528,339 | 3,945 | |
| FullRefresh 50 | 1,830,024 | 957,163 | 9,189 | ~1MB per refresh |
| SortedSectionItems 200 | 12,912 | 45,497 | 5 | sorting is fast |

### Live benchmarks (real tmux IPC)

| Benchmark | ns/op | B/op | allocs |
|---|---:|---:|---:|
| FetchState | 111,160,120 | 2,331,444 | 4,487 |
| DetectAll | 7,157,594 | 454,155 | 406 |
| ProjectScan | 3,780,385 | 741,391 | 7,149 |
| BootstrapSync | 147,923,743 | 3,136,103 | 5,286 |
| RunHeadless | 315,345,533 | 4,181,540 | 15,184 |
| NewRootModel | 5,075,942 | 277,841 | 339 |

## Key findings

1. **FetchState is 93% of startup** — tmux IPC dominates. Called twice (explicit + inside BootstrapSync).
2. **Slab re-allocation** — `resetWith` creates a new 208KB fzf slab on every `rebuildPicker` call. This happens on every watcher state update.
3. **highlightMatches** — 52 allocs for 8 matched chars. Per-rune `lipgloss.Render()`.
4. **BuildPaneItems** — slowest builder (columnar `fmt.Sprintf` + styled rendering).
5. **FullRefresh at 50 sessions** — 1.8ms, ~1MB. Acceptable for real workloads (typical: 5-20 sessions).
6. **View is near-free** — only renders visible items (viewport windowing works).

## Optimization targets (by impact)

| Target | Savings | Difficulty |
|---|---|---|
| Reuse fzf slab across resetWith | 208KB/rebuild | trivial |
| Batch highlightMatches (collect runs, single Render per run) | ~50 allocs/item | easy |
| Pre-allocate slices in build methods | reduce grow-copies | easy |
| Debounce rebuildPicker on rapid watcher updates | fewer full rebuilds | medium |

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
