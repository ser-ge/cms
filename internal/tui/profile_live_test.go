package tui_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/project"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/tui"
	"github.com/serge/cms/internal/watcher"
)

// requireLiveTmux skips unless CMS_TMUX_SOCKET is set (pointing at the real server).
// Run with:
//
//	CMS_TMUX_SOCKET=/private/tmp/tmux-$(id -u)/default go test -v -run TestProfileLive ./internal/tui/
func requireLiveTmux(t testing.TB) {
	t.Helper()
	if os.Getenv("CMS_TMUX_SOCKET") == "" {
		t.Skip("set CMS_TMUX_SOCKET to profile against real tmux (see test comment)")
	}
}

// TestProfileLiveStartup measures each phase of the real CLI startup path.
func TestProfileLiveStartup(t *testing.T) {
	requireLiveTmux(t)
	total := time.Now()

	// Phase 1: Config load
	t0 := time.Now()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	configDur := time.Since(t0)

	// Phase 2: Style init
	t1 := time.Now()
	tui.InitStyles(cfg)
	stylesDur := time.Since(t1)

	// Phase 3: Fetch tmux state (the tmux IPC call)
	t2 := time.Now()
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		t.Fatalf("FetchState: %v", err)
	}
	fetchDur := time.Since(t2)

	// Phase 4: Detect agents (ps + screen scrape)
	t3 := time.Now()
	agents := agent.DetectAll(sessions, pt)
	detectDur := time.Since(t3)

	// Phase 5: Scan projects (filesystem walk)
	t4 := time.Now()
	projects := project.Scan(cfg)
	projectDur := time.Since(t4)

	// Phase 6: Watcher init + BootstrapSync (the real --plain path)
	t5 := time.Now()
	w := watcher.New()
	w.ApplyConfig(cfg.General)
	w.BootstrapSync()
	bootstrapDur := time.Since(t5)

	// Phase 7: RunHeadless (builds finder items + picker, runs sync scans)
	t6 := time.Now()
	h := tui.RunHeadless(cfg, w, cfg.Finder.Include)
	headlessDur := time.Since(t6)

	// Phase 8: PlainSnapshot (builds output rows)
	t7 := time.Now()
	rows := h.PlainSnapshot()
	snapDur := time.Since(t7)

	// Phase 9: NewRootModel + View (the TUI path for comparison)
	t8 := time.Now()
	m := tui.NewRootModel(tui.ScreenFinder, cfg.Finder.Include, cfg, w)
	modelDur := time.Since(t8)

	t9 := time.Now()
	_ = m.View()
	viewDur := time.Since(t9)

	totalDur := time.Since(total)

	paneCount := 0
	for _, s := range sessions {
		for _, win := range s.Windows {
			paneCount += len(win.Panes)
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== cms CLI Startup Profile (live tmux) ===\n")
	fmt.Fprintf(os.Stderr, "Sessions: %d, Panes: %d, Agents: %d, Projects: %d, Rows: %d\n\n",
		len(sessions), paneCount, len(agents), len(projects), len(rows))
	fmt.Fprintf(os.Stderr, "  config.Load()       %8s\n", configDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  InitStyles()        %8s\n", stylesDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  FetchState()        %8s  ← tmux IPC\n", fetchDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  DetectAll()         %8s  ← ps + screen scrape\n", detectDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  project.Scan()      %8s  ← filesystem walk\n", projectDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  BootstrapSync()     %8s  ← watcher init + FetchState\n", bootstrapDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  RunHeadless()       %8s  ← build items + picker + scans\n", headlessDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  PlainSnapshot()     %8s\n", snapDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  NewRootModel()      %8s  ← TUI path (comparison)\n", modelDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  View()              %8s  ← first render\n", viewDur.Round(time.Microsecond))
	fmt.Fprintf(os.Stderr, "  ────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "  TOTAL               %8s\n\n", totalDur.Round(time.Microsecond))
}

// --- Live benchmarks ---
//
// Run with:
//   CMS_TMUX_SOCKET=/private/tmp/tmux-$(id -u)/default \
//     go test -run='^$' -bench='BenchmarkLive' -benchmem ./internal/tui/

func BenchmarkLiveFetchState(b *testing.B) {
	requireLiveTmux(b)
	for b.Loop() {
		tmux.FetchState()
	}
}

func BenchmarkLiveDetectAll(b *testing.B) {
	requireLiveTmux(b)
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		agent.DetectAll(sessions, pt)
	}
}

func BenchmarkLiveProjectScan(b *testing.B) {
	requireLiveTmux(b)
	cfg, _ := config.Load()
	b.ResetTimer()
	for b.Loop() {
		project.Scan(cfg)
	}
}

func BenchmarkLiveBootstrapSync(b *testing.B) {
	requireLiveTmux(b)
	cfg, _ := config.Load()
	b.ResetTimer()
	for b.Loop() {
		w := watcher.New()
		w.ApplyConfig(cfg.General)
		w.BootstrapSync()
	}
}

func BenchmarkLiveRunHeadless(b *testing.B) {
	requireLiveTmux(b)
	cfg, _ := config.Load()
	tui.InitStyles(cfg)
	w := watcher.New()
	w.ApplyConfig(cfg.General)
	w.BootstrapSync()
	b.ResetTimer()
	for b.Loop() {
		tui.RunHeadless(cfg, w, cfg.Finder.Include)
	}
}

func BenchmarkLiveNewRootModel(b *testing.B) {
	requireLiveTmux(b)
	cfg, _ := config.Load()
	tui.InitStyles(cfg)
	w := watcher.New()
	w.ApplyConfig(cfg.General)
	w.BootstrapSync()
	b.ResetTimer()
	for b.Loop() {
		tui.NewRootModel(tui.ScreenFinder, cfg.Finder.Include, cfg, w)
	}
}
