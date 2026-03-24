# cms

Tmux session picker and dashboard with Claude/Codex agent awareness.

## Module Map

| File | Purpose |
|------|---------|
| `tui_dashboard.go` | Dashboard view & interaction |
| `watcher.go` | Event-driven state management |
| `picker.go` | Fuzzy-find picker widget |
| `tui_finder.go` | Finder view |
| `tmux.go` | tmux commands & process table |
| `config.go` | Config loading & defaults |
| `agent.go` | Provider-neutral agent detection |
| `control.go` | tmux control mode client |
| `session.go` | Session switch/create/kill/open |
| `codex.go` | Codex pane parsing |
| `project.go` | Project directory scanner |
| `main.go` | CLI entry & command routing |
| `git.go` | Git info detection |
| `tui.go` | Root TUI model & screen router |
| `claude.go` | Claude pane parsing |
| `debuglog.go` | Debug logger |
