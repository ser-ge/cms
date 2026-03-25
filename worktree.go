package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// tomlUnmarshal wraps toml.Unmarshal, ignoring errors for convenience
// when loading optional config files.
func tomlUnmarshal(data []byte, v any) {
	toml.Unmarshal(data, v) //nolint:errcheck
}

// WorktreeConfig holds worktree settings. Loaded from user config
// (~/.config/cms/config.toml [worktree]) and per-repo project config
// (.cms.toml [worktree]). Project config overrides user config.
type WorktreeConfig struct {
	BaseDir    string         `toml:"base_dir"`
	Hooks      []WorktreeHook `toml:"hooks"`       // post-create
	PreRemove  []WorktreeHook `toml:"pre_remove"`
	PreCommit  []WorktreeHook `toml:"pre_commit"`
	PostCommit []WorktreeHook `toml:"post_commit"`
	PreMerge   []WorktreeHook `toml:"pre_merge"`
	PostMerge  []WorktreeHook `toml:"post_merge"`
	AutoOpen   bool           `toml:"auto_open"`
	CommitCmd  string         `toml:"commit_cmd"` // LLM commit message command (e.g. "llm -m claude-haiku")
}

// ProjectConfig is the per-repo config loaded from .cms.toml at the repo root.
// Currently only holds worktree settings; extensible for future repo-level config.
type ProjectConfig struct {
	Worktree WorktreeConfig `toml:"worktree"`
}

// WorktreeHook is a shell command that runs at a lifecycle point.
type WorktreeHook struct {
	Command string            `toml:"command"`
	Env     map[string]string `toml:"env"`
}

// sanitizeBranch replaces characters that break filesystem paths.
// "feature/auth" → "feature-auth", "bug\fix" → "bug-fix"
func sanitizeBranch(branch string) string {
	s := strings.NewReplacer("/", "-", "\\", "-").Replace(branch)
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// resolveWorktreeSymbol expands special symbols:
//
//	"@" → current branch (from cwd)
//	"-" → previous branch (from git reflog)
//	"^" → default branch (main/master)
func resolveWorktreeSymbol(repoRoot, symbol string) (string, error) {
	switch symbol {
	case "@":
		branch, err := gitCmd(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return "", fmt.Errorf("cannot resolve @: %w", err)
		}
		return branch, nil
	case "-":
		// git rev-parse --abbrev-ref @{-1} gives the previous branch.
		branch, err := gitCmd(repoRoot, "rev-parse", "--abbrev-ref", "@{-1}")
		if err != nil {
			return "", fmt.Errorf("cannot resolve -: no previous branch")
		}
		return branch, nil
	case "^":
		branch, err := defaultBranch(repoRoot)
		if err != nil {
			return "", err
		}
		return branch, nil
	default:
		return symbol, nil
	}
}

// defaultBranch returns the default branch name (main, master, etc).
func defaultBranch(repoRoot string) (string, error) {
	// Try remote HEAD first.
	ref, err := gitCmd(repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}
	// Fallback: check common names.
	for _, name := range []string{"main", "master"} {
		if _, err := gitCmd(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("cannot determine default branch")
}

// isBranchIntegrated checks if a branch has been merged into the target branch.
// Uses multiple strategies (inspired by Worktrunk):
//  1. Same commit as target
//  2. Branch is ancestor of target
//  3. No diff between branch and target (tree comparison)
func isBranchIntegrated(repoRoot, branch, target string) (bool, string) {
	branchSHA, err := gitCmd(repoRoot, "rev-parse", branch)
	if err != nil {
		return false, ""
	}
	targetSHA, err := gitCmd(repoRoot, "rev-parse", target)
	if err != nil {
		return false, ""
	}

	// Same commit.
	if branchSHA == targetSHA {
		return true, "same commit as " + target
	}

	// Branch is ancestor of target.
	if _, err := gitCmd(repoRoot, "merge-base", "--is-ancestor", branch, target); err == nil {
		return true, "ancestor of " + target
	}

	// Check if merging branch into target would add nothing.
	// git merge-tree finds the merge result; if it produces no diff, branch is already integrated.
	// Simpler: compare the tree of target with a trial merge.
	// Use cherry: if no commits are unique to branch vs target, it's integrated.
	cherry, err := gitCmd(repoRoot, "cherry", target, branch)
	if err == nil && strings.TrimSpace(cherry) == "" {
		return true, "no unique commits vs " + target
	}

	return false, ""
}

// loadProjectConfig reads .cms.toml from the repo root.
// Returns a zero ProjectConfig if the file doesn't exist.
func loadProjectConfig(repoRoot string) ProjectConfig {
	var proj ProjectConfig
	path := filepath.Join(repoRoot, ".cms.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return proj
	}
	tomlUnmarshal(data, &proj)
	return proj
}

// resolveWorktreeConfig merges user config (from ~/.config/cms/config.toml)
// with per-repo project config (from .cms.toml). Project overrides user:
// if the project sets hooks, they replace user hooks entirely.
// Scalars (base_dir, commit_cmd) use project value if non-empty.
func resolveWorktreeConfig(repoRoot string, userCfg *WorktreeConfig) WorktreeConfig {
	proj := loadProjectConfig(repoRoot)
	merged := *userCfg

	p := proj.Worktree
	if p.BaseDir != "" {
		merged.BaseDir = p.BaseDir
	}
	if p.CommitCmd != "" {
		merged.CommitCmd = p.CommitCmd
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

// resolveWorktreeBaseDir returns the absolute base directory for worktrees.
func resolveWorktreeBaseDir(repoRoot string, cfg *WorktreeConfig) string {
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
	NewBranch bool   // create a new branch (-b)
	Track     string // remote ref to track (e.g. "origin/feature")
	Force     bool
}

// CreateWorktree creates a git worktree at the given path for the given branch.
func CreateWorktree(repoRoot, path, branch string, opts CreateWorktreeOpts) error {
	args := []string{"worktree", "add"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.NewBranch {
		args = append(args, "-b", branch)
		if opts.Track != "" {
			args = append(args, "--track", path, opts.Track)
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
	_, err := gitCmd(repoRoot, args...)
	return err
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(repoRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := gitCmd(repoRoot, args...)
	return err
}

// DeleteBranch deletes a local branch.
func DeleteBranch(repoRoot, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitCmd(repoRoot, "branch", flag, branch)
	return err
}

// ResolveBranch checks if a branch exists locally or on a remote.
// Returns (true, "", nil) for local, (false, "origin/branch", nil) for remote,
// or an error if not found or ambiguous.
func ResolveBranch(repoRoot, branch string) (local bool, remote string, err error) {
	// Check local.
	_, err = gitCmd(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, "", nil
	}

	// Check remotes.
	out, err := gitCmd(repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/remotes/*/"+branch)
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
func RunHooks(label, mainWorktree, targetWorktree string, hooks []WorktreeHook) error {
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
func RunPostCreateHooks(mainWorktree, newWorktree string, hooks []WorktreeHook) error {
	return RunHooks("post-create", mainWorktree, newWorktree, hooks)
}

// findRepoRoot resolves the git repo root from the current directory.
func findRepoRoot(dir string) (string, error) {
	root, err := gitCmd(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	return root, nil
}

// findMainWorktree returns the path of the main worktree for a repo.
func findMainWorktree(repoRoot string) (string, error) {
	wts, err := listWorktrees(repoRoot)
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
