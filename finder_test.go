package main

import (
	"fmt"
	"testing"
	"time"
)

func TestFinderFlow(t *testing.T) {
	cfg := LoadConfig()
	fmt.Printf("Config search paths: %+v\n", cfg.General.SearchPaths)

	t0 := time.Now()
	sessions, pt, err := FetchState()
	fmt.Printf("FetchState: %v, %d sessions, err=%v\n", time.Since(t0), len(sessions), err)

	t1 := time.Now()
	projects := ScanProjects(cfg)
	fmt.Printf("ScanProjects: %v, %d projects\n", time.Since(t1), len(projects))

	t2 := time.Now()
	agents := detectAllAgents(sessions, pt)
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
