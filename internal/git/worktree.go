package git

import (
	"os"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree entry from `git worktree list --porcelain`.
type Worktree struct {
	Path   string // absolute checkout path
	Branch string // branch name (e.g. "main"), empty for detached HEAD
	IsBare bool   // true for the bare entry itself
	IsMain bool   // true for the main worktree (first in list)
}

// ListWorktrees returns all worktrees for a repo using `git worktree list --porcelain`.
// Works for both normal and bare repos. Filters out prunable entries (missing paths).
func ListWorktrees(repoPath string) ([]Worktree, error) {
	out, err := Cmd(repoPath, "worktree", "list", "--porcelain")
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

// IsWorktreeCheckout checks if a path is a linked git worktree checkout.
// Worktree checkouts have a .git file (not dir) with gitdir pointing to
// .git/worktrees/<name>, unlike submodules which point to .git/modules/<name>.
func IsWorktreeCheckout(path string) bool {
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
