package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// GitInfo holds git repository context for a working directory.
type GitInfo struct {
	IsRepo       bool
	Branch       string
	RepoName     string // basename of the repo root
	Dirty        bool   // uncommitted changes
	Ahead        int    // commits ahead of upstream
	Behind       int    // commits behind upstream
	LastCommit   string // short subject of HEAD commit
	LastCommitBy string // author of HEAD commit
}

// GitCache deduplicates git lookups so we only query each repo once.
// Multiple panes can share the same repo root.
type GitCache struct {
	mu    sync.Mutex
	roots map[string]GitInfo // repo root → info
}

func NewGitCache() *GitCache {
	return &GitCache{roots: map[string]GitInfo{}}
}

// DetectAll resolves git info for all directories concurrently,
// deduplicating by repo root.
func (gc *GitCache) DetectAll(dirs []string) map[string]GitInfo {
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
			if root, err := gitCmd(d, "rev-parse", "--show-toplevel"); err == nil {
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

	// Build result: dir → GitInfo via the root lookup.
	result := map[string]GitInfo{}
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
func detectGitForRoot(root, dir string) GitInfo {
	info := GitInfo{}
	info.IsRepo = true
	info.RepoName = filepath.Base(root)

	// Single call for branch + dirty + ahead/behind.
	// Porcelain v2 output:
	//   # branch.head main
	//   # branch.ab +3 -1
	//   1 M. file.go          (modified tracked file)
	//   ? untracked.txt       (untracked file)
	out, err := gitCmd(dir, "status", "-b", "--porcelain=v2")
	if err != nil {
		return info
	}

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			// Format: "# branch.ab +3 -1" → ahead 3, behind 1
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

// Worktree represents a git worktree entry from `git worktree list --porcelain`.
type Worktree struct {
	Path   string // absolute checkout path
	Branch string // branch name (e.g. "main"), empty for detached HEAD
	IsBare bool   // true for the bare entry itself
	IsMain bool   // true for the main worktree (first in list)
}

// listWorktrees returns all worktrees for a repo using `git worktree list --porcelain`.
// Works for both normal and bare repos. Filters out prunable entries (missing paths).
func listWorktrees(repoPath string) ([]Worktree, error) {
	out, err := gitCmd(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []Worktree
	first := true
	for _, block := range strings.Split(out, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var wt Worktree
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				wt.Path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "branch refs/heads/"):
				wt.Branch = strings.TrimPrefix(line, "branch refs/heads/")
			case line == "bare":
				wt.IsBare = true
			}
		}
		if wt.Path == "" {
			continue
		}
		if first {
			wt.IsMain = true
			first = false
		}
		// Skip prunable worktrees (path no longer exists).
		if !wt.IsBare {
			if _, err := os.Stat(wt.Path); err != nil {
				continue
			}
		}
		worktrees = append(worktrees, wt)
	}
	return worktrees, nil
}

// isWorktreeCheckout checks if a path is a linked git worktree checkout.
// Worktree checkouts have a .git file (not dir) with gitdir pointing to
// .git/worktrees/<name>, unlike submodules which point to .git/modules/<name>.
func isWorktreeCheckout(path string) bool {
	gitFile := filepath.Join(path, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return false
	}
	target := strings.TrimPrefix(line, "gitdir: ")
	return strings.Contains(target, filepath.Join(".git", "worktrees"))
}

// gitCmd runs a git command in the given directory and returns trimmed stdout.
func gitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
