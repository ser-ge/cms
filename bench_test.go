package main

import (
	"fmt"
	"testing"
	"time"
)

func TestScanTiming(t *testing.T) {
	cfg := LoadConfig()

	t0 := time.Now()
	projects := ScanProjects(cfg)
	scanTime := time.Since(t0)
	fmt.Printf("ScanProjects: %v (%d projects)\n", scanTime, len(projects))

	// Break it down: scan vs git
	t1 := time.Now()
	// Just the BFS part without git
	excluded := map[string]bool{}
	for _, e := range cfg.Exclusions {
		excluded[e] = true
	}
	count := 0
	for _, sp := range cfg.SearchPaths {
		type entry struct {
			path  string
			depth int
		}
		queue := []entry{{sp.Path, sp.MaxDepth}}
		for len(queue) > 0 {
			e := queue[0]
			queue = queue[1:]
			count++
			_ = e
		}
	}
	fmt.Printf("BFS overhead: %v (dirs visited: %d)\n", time.Since(t1), count)

	t2 := time.Now()
	sessions, pt, _ := FetchState()
	fmt.Printf("FetchState: %v (%d sessions)\n", time.Since(t2), len(sessions))

	t3 := time.Now()
	agents := detectAllAgents(sessions, pt)
	fmt.Printf("detectAllAgents: %v (%d results)\n", time.Since(t3), len(agents))
}
