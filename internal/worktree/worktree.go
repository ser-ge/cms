package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
)

// SanitizeBranch replaces characters that break filesystem paths.
// "feature/auth" -> "feature-auth", "bug\fix" -> "bug-fix"
func SanitizeBranch(branch string) string {
	s := strings.NewReplacer("/", "-", "\\", "-").Replace(branch)
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// ResolveWorktreeSymbol expands special symbols:
//
//	"@" -> current branch (from cwd)
//	"-" -> previous branch (from git reflog)
//	"^" -> default branch (main/master)
func ResolveWorktreeSymbol(repoRoot, symbol string) (string, error) {
	switch symbol {
	case "@":
		branch, err := git.Cmd(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return "", fmt.Errorf("cannot resolve @: %w", err)
		}
		return branch, nil
	case "-":
		// git rev-parse --abbrev-ref @{-1} gives the previous branch.
		branch, err := git.Cmd(repoRoot, "rev-parse", "--abbrev-ref", "@{-1}")
		if err != nil {
			return "", fmt.Errorf("cannot resolve -: no previous branch")
		}
		return branch, nil
	case "^":
		branch, err := DefaultBranch(repoRoot)
		if err != nil {
			return "", err
		}
		return branch, nil
	default:
		return symbol, nil
	}
}

// DefaultBranch returns the default branch name (main, master, etc).
func DefaultBranch(repoRoot string) (string, error) {
	// Try remote HEAD first.
	ref, err := git.Cmd(repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}
	// Fallback: check common names.
	for _, name := range []string{"main", "master"} {
		if _, err := git.Cmd(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("cannot determine default branch")
}

// IsBranchIntegrated checks if a branch has been merged into the target branch.
// Uses multiple strategies (inspired by Worktrunk):
//  1. Same commit as target
//  2. Branch is ancestor of target
//  3. No diff between branch and target (tree comparison)
func IsBranchIntegrated(repoRoot, branch, target string) (bool, string) {
	branchSHA, err := git.Cmd(repoRoot, "rev-parse", branch)
	if err != nil {
		return false, ""
	}
	targetSHA, err := git.Cmd(repoRoot, "rev-parse", target)
	if err != nil {
		return false, ""
	}

	// Same commit.
	if branchSHA == targetSHA {
		return true, "same commit as " + target
	}

	// Branch is ancestor of target.
	if _, err := git.Cmd(repoRoot, "merge-base", "--is-ancestor", branch, target); err == nil {
		return true, "ancestor of " + target
	}

	// Check if merging branch into target would add nothing.
	cherry, err := git.Cmd(repoRoot, "cherry", target, branch)
	if err == nil {
		hasUnique := false
		for _, line := range strings.Split(strings.TrimSpace(cherry), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "+") {
				hasUnique = true
				break
			}
		}
		if !hasUnique {
			return true, "no unique commits vs " + target
		}
	}

	return false, ""
}

// LoadProjectConfig reads .cms.toml from repoRoot.
func LoadProjectConfig(repoRoot string) config.ProjectConfig {
	return config.LoadProjectConfig(repoRoot)
}

// ResolveWorktreeConfig merges user config with per-repo project config.
// When cwd is inside the repo, prefer .cms.toml discovered from that directory;
// otherwise fall back to the repo root.
func ResolveWorktreeConfig(repoRoot, cwd string, userCfg *config.WorktreeConfig) config.WorktreeConfig {
	projDir := repoRoot
	if cwd != "" {
		projDir = cwd
	}
	proj := LoadProjectConfig(projDir)
	if projDir != repoRoot {
		if _, err := os.Stat(filepath.Join(projDir, ".cms.toml")); os.IsNotExist(err) {
			proj = LoadProjectConfig(repoRoot)
		}
	}
	if projDir == "" {
		proj = LoadProjectConfig(repoRoot)
	}
	merged := *userCfg

	p := proj.Worktree
	if p.BaseDir != "" {
		merged.BaseDir = p.BaseDir
	}
	if p.BaseBranch != "" {
		merged.BaseBranch = p.BaseBranch
	}
	if p.CommitCmd != "" {
		merged.CommitCmd = p.CommitCmd
	}
	if p.GoCmd != "" {
		merged.GoCmd = p.GoCmd
	}
	if len(p.Hooks) > 0 {
		merged.Hooks = p.Hooks
	}
	if len(p.PreRemove) > 0 {
		merged.PreRemove = p.PreRemove
	}
	if len(p.PreCommit) > 0 {
		merged.PreCommit = p.PreCommit
	}
	if len(p.PostCommit) > 0 {
		merged.PostCommit = p.PostCommit
	}
	if len(p.PreMerge) > 0 {
		merged.PreMerge = p.PreMerge
	}
	if len(p.PostMerge) > 0 {
		merged.PostMerge = p.PostMerge
	}

	return merged
}

// ResolveWorktreeBaseDir returns the absolute base directory for worktrees.
func ResolveWorktreeBaseDir(repoRoot string, cfg *config.WorktreeConfig) string {
	baseDir := "../worktrees"
	if cfg != nil && cfg.BaseDir != "" {
		baseDir = cfg.BaseDir
	}
	if !filepath.IsAbs(baseDir) {
		baseDir = filepath.Join(repoRoot, baseDir)
	}
	return filepath.Clean(baseDir)
}

// CreateWorktreeOpts configures worktree creation.
type CreateWorktreeOpts struct {
	NewBranch   bool   // create a new branch (-b)
	ForceBranch bool   // force-create branch even if it exists (-B, mirrors git switch -C)
	Track       string // remote ref to track (e.g. "origin/feature")
	StartPoint  string // commit/branch to start from (e.g. "main")
	Force       bool
}

// CreateWorktree creates a git worktree at the given path for the given branch.
func CreateWorktree(repoRoot, path, branch string, opts CreateWorktreeOpts) error {
	args := []string{"worktree", "add"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.NewBranch || opts.ForceBranch {
		branchFlag := "-b"
		if opts.ForceBranch {
			branchFlag = "-B"
		}
		args = append(args, branchFlag, branch)
		if opts.Track != "" {
			args = append(args, "--track", path, opts.Track)
		} else if opts.StartPoint != "" {
			args = append(args, path, opts.StartPoint)
		} else {
			args = append(args, path)
		}
	} else {
		if opts.Track != "" {
			args = append(args, "--track", path, opts.Track)
		} else {
			args = append(args, path, branch)
		}
	}
	_, err := git.Cmd(repoRoot, args...)
	return err
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(repoRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := git.Cmd(repoRoot, args...)
	return err
}

// DeleteBranch deletes a local branch.
func DeleteBranch(repoRoot, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := git.Cmd(repoRoot, "branch", flag, branch)
	return err
}

// ResolveBranch checks if a branch exists locally or on a remote.
// Returns (true, "", nil) for local, (false, "origin/branch", nil) for remote,
// or an error if not found or ambiguous.
func ResolveBranch(repoRoot, branch string) (local bool, remote string, err error) {
	// Check local.
	_, err = git.Cmd(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, "", nil
	}

	// Check remotes.
	out, err := git.Cmd(repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/remotes/*/"+branch)
	if err != nil || out == "" {
		return false, "", fmt.Errorf("branch %q not found locally or on any remote", branch)
	}

	refs := strings.Split(out, "\n")
	if len(refs) > 1 {
		return false, "", fmt.Errorf("branch %q found on multiple remotes: %s", branch, strings.Join(refs, ", "))
	}
	return false, refs[0], nil
}

// RunHooks executes hooks in order. Each hook is a shell command run in the
// target worktree with CMS_WORKTREE_PATH and CMS_REPO_ROOT env vars set.
func RunHooks(label, mainWorktree, targetWorktree string, hooks []config.WorktreeHook) error {
	for i, h := range hooks {
		cmd := exec.Command("sh", "-c", h.Command)
		cmd.Dir = targetWorktree
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(),
			"CMS_WORKTREE_PATH="+targetWorktree,
			"CMS_REPO_ROOT="+mainWorktree,
		)
		for k, v := range h.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s hook %d (%q): %w", label, i+1, h.Command, err)
		}
	}
	return nil
}

// RunPostCreateHooks executes hooks after worktree creation.
func RunPostCreateHooks(mainWorktree, newWorktree string, hooks []config.WorktreeHook) error {
	return RunHooks("post-create", mainWorktree, newWorktree, hooks)
}

// FindRepoRoot resolves the canonical repo root from any directory --
// whether inside the main worktree, a linked worktree, or a bare repo.
// Always returns the main worktree root (where .cms.toml lives and
// base_dir resolves from), not the linked worktree you happen to be in.
func FindRepoRoot(dir string) (string, error) {
	// --git-common-dir gives the shared git dir across all worktrees.
	commonDir, err := git.Cmd(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}

	// If common dir ends with .git, the repo root is its parent (normal repo).
	// Otherwise it IS the repo root (bare repo).
	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	return commonDir, nil
}

// FindMainWorktree returns the path of the main worktree for a repo.
func FindMainWorktree(repoRoot string) (string, error) {
	wts, err := git.ListWorktrees(repoRoot)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.IsMain {
			return wt.Path, nil
		}
	}
	return repoRoot, nil
}

// ShortenHome replaces the user's home directory prefix with "~".
func ShortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return filepath.Join("~", path[len(home):])
	}
	return path
}
