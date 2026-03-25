package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeConfig holds per-repo worktree settings from .wtp.yml or cms config.
type WorktreeConfig struct {
	BaseDir string          `toml:"base_dir" yaml:"base_dir"`
	Hooks   []WorktreeHook  `toml:"hooks" yaml:"-"`
	YAMLRaw *wtpYAMLConfig  // parsed from .wtp.yml if present
}

type wtpYAMLConfig struct {
	Version  string `yaml:"version"`
	Defaults struct {
		BaseDir string `yaml:"base_dir"`
	} `yaml:"defaults"`
	Hooks struct {
		PostCreate []WorktreeHook `yaml:"post_create"`
	} `yaml:"hooks"`
}

// WorktreeHook defines a post-create action.
type WorktreeHook struct {
	Type    string            `toml:"type" yaml:"type"`       // "copy", "symlink", "command"
	From    string            `toml:"from" yaml:"from"`       // source (relative to main worktree)
	To      string            `toml:"to" yaml:"to"`           // dest (relative to new worktree)
	Command string            `toml:"command" yaml:"command"`
	Env     map[string]string `toml:"env" yaml:"env"`
}

// resolveWorktreeBaseDir returns the absolute base directory for worktrees.
// Checks .wtp.yml first, then cms config, then defaults to "../worktrees".
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

// resolveWorktreeHooks returns hooks from .wtp.yml if present, else from cms config.
func resolveWorktreeHooks(repoRoot string, cfg *WorktreeConfig) []WorktreeHook {
	// Try .wtp.yml first.
	wtpCfg, err := loadWTPConfig(repoRoot)
	if err == nil && wtpCfg != nil {
		return wtpCfg.Hooks.PostCreate
	}
	if cfg != nil {
		return cfg.Hooks
	}
	return nil
}

// loadWTPConfig reads .wtp.yml from repo root if it exists.
func loadWTPConfig(repoRoot string) (*wtpYAMLConfig, error) {
	path := filepath.Join(repoRoot, ".wtp.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &wtpYAMLConfig{}
	if err := yamlUnmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// yamlUnmarshal is a minimal YAML parser for .wtp.yml. We only need a few
// fields so we avoid pulling in a full YAML dependency. Handles the subset
// of YAML that .wtp.yml uses: scalars, maps, and sequences of maps.
func yamlUnmarshal(data []byte, cfg *wtpYAMLConfig) error {
	lines := strings.Split(string(data), "\n")
	var section string    // "defaults", "hooks", "post_create"
	var currentHook *WorktreeHook

	flushHook := func() {
		if currentHook != nil {
			cfg.Hooks.PostCreate = append(cfg.Hooks.PostCreate, *currentHook)
			currentHook = nil
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		// Top-level keys.
		if indent == 0 {
			flushHook()
			if strings.HasPrefix(trimmed, "version:") {
				cfg.Version = unquote(strings.TrimPrefix(trimmed, "version:"))
			} else if trimmed == "defaults:" {
				section = "defaults"
			} else if trimmed == "hooks:" {
				section = "hooks"
			} else {
				section = ""
			}
			continue
		}

		switch section {
		case "defaults":
			if strings.HasPrefix(trimmed, "base_dir:") {
				cfg.Defaults.BaseDir = unquote(strings.TrimPrefix(trimmed, "base_dir:"))
			}
		case "hooks":
			if trimmed == "post_create:" {
				section = "post_create"
			}
		case "post_create":
			if strings.HasPrefix(trimmed, "- type:") {
				flushHook()
				currentHook = &WorktreeHook{
					Type: unquote(strings.TrimPrefix(trimmed, "- type:")),
				}
			} else if currentHook != nil {
				kv := strings.SplitN(trimmed, ":", 2)
				if len(kv) == 2 {
					k := strings.TrimSpace(strings.TrimPrefix(kv[0], "- "))
					v := unquote(kv[1])
					switch k {
					case "from":
						currentHook.From = v
					case "to":
						currentHook.To = v
					case "command":
						currentHook.Command = v
					}
				}
			}
		}
	}
	flushHook()
	return nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		s = s[1 : len(s)-1]
	}
	return s
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

// RunPostCreateHooks executes hooks after worktree creation.
func RunPostCreateHooks(mainWorktree, newWorktree string, hooks []WorktreeHook) error {
	for i, h := range hooks {
		switch h.Type {
		case "copy":
			src := resolvePath(mainWorktree, h.From)
			dst := resolvePath(newWorktree, h.To)
			if dst == "" {
				dst = resolvePath(newWorktree, h.From)
			}
			if err := copyPath(src, dst); err != nil {
				return fmt.Errorf("hook %d (copy %s): %w", i+1, h.From, err)
			}
		case "symlink":
			src := resolvePath(mainWorktree, h.From)
			dst := resolvePath(newWorktree, h.To)
			if dst == "" {
				dst = resolvePath(newWorktree, h.From)
			}
			if _, err := os.Lstat(src); err != nil {
				return fmt.Errorf("hook %d (symlink %s): source not found", i+1, h.From)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(src, dst); err != nil {
				return fmt.Errorf("hook %d (symlink %s): %w", i+1, h.From, err)
			}
		case "command":
			cmd := exec.Command("sh", "-c", h.Command)
			cmd.Dir = newWorktree
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Env = append(os.Environ(),
				"GIT_WTP_WORKTREE_PATH="+newWorktree,
				"GIT_WTP_REPO_ROOT="+mainWorktree,
			)
			for k, v := range h.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("hook %d (command %q): %w", i+1, h.Command, err)
			}
		default:
			return fmt.Errorf("hook %d: unknown type %q", i+1, h.Type)
		}
	}
	return nil
}

func resolvePath(base, rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

// copyPath copies a file or directory recursively.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
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
