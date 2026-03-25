package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Info holds git repository context for a working directory.
type Info struct {
	IsRepo       bool
	Branch       string
	RepoName     string // basename of the repo root
	Dirty        bool   // uncommitted changes
	Ahead        int    // commits ahead of upstream
	Behind       int    // commits behind upstream
	LastCommit   string // short subject of HEAD commit
	LastCommitBy string // author of HEAD commit
}

// Cache deduplicates git lookups so we only query each repo once.
// Multiple panes can share the same repo root.
type Cache struct {
	mu    sync.Mutex
	roots map[string]Info // repo root -> info
}

func NewCache() *Cache {
	return &Cache{roots: map[string]Info{}}
}

// DetectAll resolves git info for all directories concurrently,
// deduplicating by repo root.
func (gc *Cache) DetectAll(dirs []string) map[string]Info {
	// First pass: resolve each dir to its repo root concurrently.
	// Deduplicate input dirs first to avoid redundant calls.
	uniqueDirs := map[string]bool{}
	for _, dir := range dirs {
		if dir != "" {
			uniqueDirs[dir] = true
		}
	}

	type rootResult struct {
		dir  string
		root string
	}

	var wg sync.WaitGroup
	rootCh := make(chan rootResult, len(uniqueDirs))
	for dir := range uniqueDirs {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			if root, err := Cmd(d, "rev-parse", "--show-toplevel"); err == nil {
				rootCh <- rootResult{dir: d, root: root}
			}
		}(dir)
	}
	wg.Wait()
	close(rootCh)

	dirToRoot := map[string]string{}
	uniqueRoots := map[string]string{}
	for rr := range rootCh {
		dirToRoot[rr.dir] = rr.root
		uniqueRoots[rr.root] = rr.dir
	}

	// Query each unique root concurrently.
	var wg2 sync.WaitGroup
	for root, dir := range uniqueRoots {
		wg2.Add(1)
		go func(root, dir string) {
			defer wg2.Done()
			info := detectGitForRoot(root, dir)
			gc.mu.Lock()
			gc.roots[root] = info
			gc.mu.Unlock()
		}(root, dir)
	}
	wg2.Wait()

	// Build result: dir -> Info via the root lookup.
	result := map[string]Info{}
	for _, dir := range dirs {
		if root, ok := dirToRoot[dir]; ok {
			result[dir] = gc.roots[root]
		}
	}
	return result
}

// detectGitForRoot fetches git info for a known repo root using a single
// `git status -b --porcelain=v2` call, which gives branch, ahead/behind,
// and dirty status all at once.
func detectGitForRoot(root, dir string) Info {
	info := Info{}
	info.IsRepo = true
	info.RepoName = filepath.Base(root)

	// Single call for branch + dirty + ahead/behind.
	// Porcelain v2 output:
	//   # branch.head main
	//   # branch.ab +3 -1
	//   1 M. file.go          (modified tracked file)
	//   ? untracked.txt       (untracked file)
	out, err := Cmd(dir, "status", "-b", "--porcelain=v2")
	if err != nil {
		return info
	}

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			// Format: "# branch.ab +3 -1" -> ahead 3, behind 1
			ab := strings.TrimPrefix(line, "# branch.ab ")
			parts := strings.Fields(ab)
			if len(parts) == 2 {
				fmt.Sscanf(parts[0], "+%d", &info.Ahead)
				fmt.Sscanf(parts[1], "-%d", &info.Behind)
			}
		case len(line) > 0 && line[0] != '#':
			// Any non-header line means there are changes.
			info.Dirty = true
		}
	}

	return info
}

// Cmd runs a git command in the given directory and returns trimmed stdout.
func Cmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
