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
	case "merge", "m":
		return worktreeMerge(args[1:])
	default:
		return fmt.Errorf("unknown worktree command: %s\nusage: cms worktree [list|add|remove|merge]", args[0])
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

	// Determine default branch for integration status.
	defBranch, _ := defaultBranch(root)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, wt := range wts {
		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}

		// Mark current worktree (cwd is inside it), not just main.
		marker := " "
		if strings.HasPrefix(cwd, wt.Path+string(filepath.Separator)) || cwd == wt.Path {
			marker = "*"
		}

		// Show integration status for non-main worktrees.
		status := ""
		if !wt.IsMain && defBranch != "" && wt.Branch != "" && wt.Branch != defBranch {
			if integrated, reason := isBranchIntegrated(root, wt.Branch, defBranch); integrated {
				status = " [merged: " + reason + "]"
			}
		}

		// Show path relative to cwd if possible.
		displayPath := wt.Path
		if rel, err := filepath.Rel(cwd, wt.Path); err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)+"..") {
			displayPath = rel
		}
		fmt.Fprintf(w, "%s\t%s\t%s%s\n", marker, branch, displayPath, status)
	}
	return w.Flush()
}

func worktreeAdd(args []string) error {
	var branch, path string
	newBranch := false
	force := false
	noOpen := false

	// Parse flags.
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-b":
			newBranch = true
		case "--force", "-f":
			force = true
		case "--no-open":
			noOpen = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms worktree add [-b] [-f] [--no-open] <branch> [path]")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve special symbols (@, -, ^).
	branch, err = resolveWorktreeSymbol(root, positional[0])
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		path = positional[1]
	}

	// Merge user config with per-repo .cms.toml.
	cfg := LoadConfig()
	wtCfg := resolveWorktreeConfig(root, cwd, &cfg.Worktree)
	if path == "" {
		baseDir := resolveWorktreeBaseDir(root, &wtCfg)
		// Sanitize branch for path: feature/auth → feature-auth
		path = filepath.Join(baseDir, sanitizeBranch(branch))
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

	// Run post-create hooks.
	mainWt, _ := findMainWorktree(root)
	if len(wtCfg.Hooks) > 0 {
		fmt.Fprintf(os.Stderr, "running %d post-create hooks\n", len(wtCfg.Hooks))
		if err := RunPostCreateHooks(mainWt, path, wtCfg.Hooks); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook failed: %v\n", err)
		}
	}

	// Auto-open tmux window for the new worktree.
	if !noOpen && insideTmux() {
		worktreeOpenTmuxWindow(branch, path)
	}

	fmt.Println(path)
	return nil
}

// worktreeOpenTmuxWindow creates a tmux window for a new worktree in the
// current session, or in the session that owns the repo.
func worktreeOpenTmuxWindow(branch, wtPath string) {
	target, err := FetchCurrentTarget()
	if err != nil {
		return
	}
	windowName := sanitizeBranch(branch)
	_, _ = runTmux("new-window", "-t", target.Session, "-n", windowName, "-c", wtPath)
}

func worktreeRemove(args []string) error {
	force := false
	keepBranch := false
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--force", "-f":
			force = true
		case "--keep-branch":
			keepBranch = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms worktree remove [-f] [--keep-branch] <branch-or-path>")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve special symbols.
	target, err := resolveWorktreeSymbol(root, positional[0])
	if err != nil {
		return err
	}

	wts, err := listWorktrees(root)
	if err != nil {
		return err
	}

	// Find the target worktree by branch name, sanitized branch, or path.
	var found *Worktree
	for i := range wts {
		wt := &wts[i]
		if wt.Branch == target || sanitizeBranch(wt.Branch) == target ||
			filepath.Base(wt.Path) == target || wt.Path == target {
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

	// Check for running agents in panes that are in this worktree.
	if !force {
		if warning := checkAgentsInWorktree(found.Path); warning != "" {
			return fmt.Errorf("%s\nuse --force to override", warning)
		}
	}

	// Run pre-remove hooks.
	cfg := LoadConfig()
	wtCfg := resolveWorktreeConfig(root, cwd, &cfg.Worktree)
	mainWt, _ := findMainWorktree(root)
	if len(wtCfg.PreRemove) > 0 {
		fmt.Fprintf(os.Stderr, "running %d pre-remove hooks\n", len(wtCfg.PreRemove))
		if err := RunHooks("pre-remove", mainWt, found.Path, wtCfg.PreRemove); err != nil {
			return fmt.Errorf("pre-remove hook failed: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "removing worktree at %s\n", shortenHome(found.Path))
	if err := RemoveWorktree(root, found.Path, force); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}

	// Delete branch (same as merge: always delete unless --keep-branch).
	if !keepBranch && found.Branch != "" {
		if !force {
			defBranch, err := defaultBranch(root)
			if err == nil && defBranch != "" {
				integrated, reason := isBranchIntegrated(root, found.Branch, defBranch)
				if !integrated {
					fmt.Fprintf(os.Stderr, "warning: branch %s is not merged into %s, skipping deletion\n", found.Branch, defBranch)
					fmt.Fprintf(os.Stderr, "  use --force to delete anyway\n")
					goto cleanup
				}
				fmt.Fprintf(os.Stderr, "branch %s is safe to delete (%s)\n", found.Branch, reason)
			}
		}
		fmt.Fprintf(os.Stderr, "deleting branch %s\n", found.Branch)
		if err := DeleteBranch(root, found.Branch, force); err != nil {
			fmt.Fprintf(os.Stderr, "warning: branch delete failed: %v\n", err)
		}
	}

cleanup:
	// Kill tmux window for the removed worktree.
	cleanupTmuxWindow(found.Path)

	return nil
}

// checkAgentsInWorktree checks if any tmux panes in the given worktree path
// have running agents. Returns a warning message if so, empty string if safe.
func checkAgentsInWorktree(wtPath string) string {
	sessions, pt, err := FetchState()
	if err != nil {
		return "" // can't check, allow removal
	}

	var agentPanes []string
	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				if !strings.HasPrefix(pane.WorkingDir, wtPath) {
					continue
				}
				// Check if an agent process is running in this pane.
				if hasAgentProcess(pt, pane.PID) {
					agentPanes = append(agentPanes, fmt.Sprintf("  %s:%s (pane %s, %s)",
						sess.Name, win.Name, pane.ID, pane.Command))
				}
			}
		}
	}

	if len(agentPanes) > 0 {
		return fmt.Sprintf("agents running in worktree %s:\n%s",
			shortenHome(wtPath), strings.Join(agentPanes, "\n"))
	}
	return ""
}

// hasAgentProcess checks if a pane's process tree contains a known agent.
func hasAgentProcess(pt procTable, panePID int) bool {
	// Walk children of the pane shell.
	for _, childPID := range pt.children[panePID] {
		child, ok := pt.procs[childPID]
		if !ok {
			continue
		}
		name := strings.ToLower(child.comm)
		if strings.Contains(name, "claude") || strings.Contains(name, "codex") {
			return true
		}
		// Check args too for wrapped invocations (e.g. "node claude").
		args := strings.ToLower(child.args)
		if strings.Contains(args, "claude") || strings.Contains(args, "codex") {
			return true
		}
	}
	return false
}
