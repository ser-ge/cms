package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/project"
	"github.com/serge/cms/internal/tmux"
)

func TestFinderFlow(t *testing.T) {
	cfg, _, err := config.Load()
	if err != nil {
		t.Skipf("config error: %v", err)
	}
	fmt.Printf("Config search paths: %+v\n", cfg.General.SearchPaths)

	t0 := time.Now()
	sessions, pt, err := tmux.FetchState()
	fmt.Printf("FetchState: %v, %d sessions, err=%v\n", time.Since(t0), len(sessions), err)

	t1 := time.Now()
	projects := project.Scan(cfg)
	fmt.Printf("ScanProjects: %v, %d projects\n", time.Since(t1), len(projects))

	t2 := time.Now()
	agents := agent.DetectAll(sessions, pt)
	fmt.Printf("detectAllAgents: %v, %d results\n", time.Since(t2), len(agents))

	// Show some projects
	for i, p := range projects {
		if i > 5 {
			fmt.Printf("  ... and %d more\n", len(projects)-5)
			break
		}
		fmt.Printf("  [%d] %s (%s)\n", i, p.Name, p.Path)
	}
}
