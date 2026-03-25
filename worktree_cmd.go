package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

func runWorktreeCmd(args []string) error {
	if len(args) == 0 {
		return worktreeList()
	}
	switch args[0] {
	case "list", "ls":
		return worktreeList()
	case "add", "a":
		return worktreeAdd(args[1:])
	case "remove", "rm":
		return worktreeRemove(args[1:])
	default:
		return fmt.Errorf("unknown worktree command: %s\nusage: cms worktree [list|add|remove]", args[0])
	}
}

func worktreeList() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return err
	}

	wts, err := listWorktrees(root)
	if err != nil {
		return err
	}
	if len(wts) == 0 {
		fmt.Println("no worktrees")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, wt := range wts {
		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}
		marker := " "
		if wt.IsMain {
			marker = "*"
		}
		// Show path relative to cwd if possible.
		displayPath := wt.Path
		if rel, err := filepath.Rel(cwd, wt.Path); err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)+"..") {
			displayPath = rel
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", marker, branch, displayPath)
	}
	return w.Flush()
}

func worktreeAdd(args []string) error {
	var branch, path string
	newBranch := false
	force := false

	// Parse flags.
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-b":
			newBranch = true
		case "--force", "-f":
			force = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms worktree add [-b] [-f] <branch> [path]")
	}
	branch = positional[0]
	if len(positional) > 1 {
		path = positional[1]
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve path from config base_dir if not given.
	cfg := LoadConfig()
	wtCfg := &cfg.Worktree
	if path == "" {
		baseDir := resolveWorktreeBaseDir(root, wtCfg)
		path = filepath.Join(baseDir, branch)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	// Resolve branch: check local, then remote for auto-tracking.
	opts := CreateWorktreeOpts{NewBranch: newBranch, Force: force}
	if !newBranch {
		local, remote, err := ResolveBranch(root, branch)
		if err != nil {
			// Branch doesn't exist anywhere — create it.
			opts.NewBranch = true
		} else if !local && remote != "" {
			opts.Track = remote
		}
	}

	fmt.Fprintf(os.Stderr, "creating worktree at %s for branch %s\n", shortenHome(path), branch)
	if err := CreateWorktree(root, path, branch, opts); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	// Run hooks.
	mainWt, _ := findMainWorktree(root)
	hooks := resolveWorktreeHooks(root, wtCfg)
	if len(hooks) > 0 {
		fmt.Fprintf(os.Stderr, "running %d post-create hooks\n", len(hooks))
		if err := RunPostCreateHooks(mainWt, path, hooks); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook failed: %v\n", err)
		}
	}

	fmt.Println(path)
	return nil
}

func worktreeRemove(args []string) error {
	force := false
	withBranch := false
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--force", "-f":
			force = true
		case "--with-branch":
			withBranch = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms worktree remove [-f] [--with-branch] <branch-or-path>")
	}
	target := positional[0]

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return err
	}

	wts, err := listWorktrees(root)
	if err != nil {
		return err
	}

	// Find the target worktree by branch name or path suffix.
	var found *Worktree
	for i := range wts {
		wt := &wts[i]
		if wt.Branch == target || filepath.Base(wt.Path) == target || wt.Path == target {
			found = wt
			break
		}
	}
	if found == nil {
		return fmt.Errorf("worktree %q not found", target)
	}
	if found.IsMain {
		return fmt.Errorf("cannot remove main worktree")
	}

	// Don't remove if we're inside it.
	if strings.HasPrefix(cwd, found.Path) {
		return fmt.Errorf("cannot remove worktree you are currently inside (%s)", found.Path)
	}

	fmt.Fprintf(os.Stderr, "removing worktree at %s\n", shortenHome(found.Path))
	if err := RemoveWorktree(root, found.Path, force); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}

	if withBranch && found.Branch != "" {
		fmt.Fprintf(os.Stderr, "deleting branch %s\n", found.Branch)
		if err := DeleteBranch(root, found.Branch, force); err != nil {
			fmt.Fprintf(os.Stderr, "warning: branch delete failed: %v\n", err)
		}
	}

	return nil
}
