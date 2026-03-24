# cms

tmux session switcher and dashboard with Claude and Codex pane detection.

## Development

Run the normal test suite:

```bash
go test ./...
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
