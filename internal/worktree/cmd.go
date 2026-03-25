package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/tmux"
)

// RunCmd dispatches worktree subcommands.
func RunCmd(args []string) error {
	if len(args) == 0 {
		return list()
	}
	switch args[0] {
	case "list", "ls":
		return list()
	case "add", "a":
		return add(args[1:])
	case "remove", "rm":
		return remove(args[1:])
	case "merge", "m":
		return Merge(args[1:])
	default:
		return fmt.Errorf("unknown worktree command: %s\nusage: cms worktree [list|add|remove|merge]", args[0])
	}
}

func list() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}
	if len(wts) == 0 {
		fmt.Println("no worktrees")
		return nil
	}

	// Determine default branch for integration status.
	defBranch, _ := DefaultBranch(root)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, wt := range wts {
		// Skip bare repo entry -- it's not a real worktree.
		if wt.IsBare {
			continue
		}

		branch := wt.Branch
		if branch == "" {
			branch = "(detached)"
		}

		// Mark the worktree that cwd is directly inside.
		marker := " "
		cwdInWt := cwd == wt.Path || strings.HasPrefix(cwd, wt.Path+string(filepath.Separator))
		if cwdInWt {
			marker = "*"
		}

		// Show integration status for non-main worktrees.
		status := ""
		if !wt.IsMain && defBranch != "" && wt.Branch != "" && wt.Branch != defBranch {
			if integrated, reason := IsBranchIntegrated(root, wt.Branch, defBranch); integrated {
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

func add(args []string) error {
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
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve special symbols (@, -, ^).
	branch, err = ResolveWorktreeSymbol(root, positional[0])
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		path = positional[1]
	}

	// Merge user config with per-repo .cms.toml.
	cfg := config.Load()
	wtCfg := ResolveWorktreeConfig(root, cwd, &cfg.Worktree)
	if path == "" {
		baseDir := ResolveWorktreeBaseDir(root, &wtCfg)
		// Sanitize branch for path: feature/auth -> feature-auth
		path = filepath.Join(baseDir, SanitizeBranch(branch))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	// Resolve branch: check local, then remote for auto-tracking.
	opts := CreateWorktreeOpts{NewBranch: newBranch, Force: force}
	if !newBranch {
		local, remote, err := ResolveBranch(root, branch)
		if err != nil {
			// Branch doesn't exist anywhere -- create it.
			opts.NewBranch = true
		} else if !local && remote != "" {
			opts.Track = remote
		}
	}

	fmt.Fprintf(os.Stderr, "creating worktree at %s for branch %s\n", ShortenHome(path), branch)
	if err := CreateWorktree(root, path, branch, opts); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	// Run post-create hooks.
	mainWt, _ := FindMainWorktree(root)
	if len(wtCfg.Hooks) > 0 {
		fmt.Fprintf(os.Stderr, "running %d post-create hooks\n", len(wtCfg.Hooks))
		if err := RunPostCreateHooks(mainWt, path, wtCfg.Hooks); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook failed: %v\n", err)
		}
	}

	// Auto-open tmux window for the new worktree.
	if !noOpen && os.Getenv("TMUX") != "" {
		openTmuxWindow(branch, path)
	}

	fmt.Println(path)
	return nil
}

// openTmuxWindow creates a tmux window for a new worktree in the
// current session, or in the session that owns the repo.
func openTmuxWindow(branch, wtPath string) {
	target, err := tmux.FetchCurrentTarget()
	if err != nil {
		return
	}
	windowName := SanitizeBranch(branch)
	_, _ = tmux.Run("new-window", "-t", target.Session, "-n", windowName, "-c", wtPath)
}

func remove(args []string) error {
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
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve special symbols.
	target, err := ResolveWorktreeSymbol(root, positional[0])
	if err != nil {
		return err
	}

	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}

	// Find the target worktree by branch name, sanitized branch, or path.
	var found *git.Worktree
	for i := range wts {
		wt := &wts[i]
		if wt.Branch == target || SanitizeBranch(wt.Branch) == target ||
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
		if warning := CheckAgentsInWorktree(found.Path); warning != "" {
			return fmt.Errorf("%s\nuse --force to override", warning)
		}
	}

	// Run pre-remove hooks.
	cfg := config.Load()
	wtCfg := ResolveWorktreeConfig(root, cwd, &cfg.Worktree)
	mainWt, _ := FindMainWorktree(root)
	if len(wtCfg.PreRemove) > 0 {
		fmt.Fprintf(os.Stderr, "running %d pre-remove hooks\n", len(wtCfg.PreRemove))
		if err := RunHooks("pre-remove", mainWt, found.Path, wtCfg.PreRemove); err != nil {
			return fmt.Errorf("pre-remove hook failed: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "removing worktree at %s\n", ShortenHome(found.Path))
	if err := RemoveWorktree(root, found.Path, force); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}

	// Delete branch (same as merge: always delete unless --keep-branch).
	if !keepBranch && found.Branch != "" {
		if !force {
			defBranch, err := DefaultBranch(root)
			if err == nil && defBranch != "" {
				integrated, reason := IsBranchIntegrated(root, found.Branch, defBranch)
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
	CleanupTmuxWindow(found.Path)

	return nil
}

// CheckAgentsInWorktree checks if any tmux panes in the given worktree path
// have running agents. Returns a warning message if so, empty string if safe.
func CheckAgentsInWorktree(wtPath string) string {
	sessions, pt, err := tmux.FetchState()
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
				if HasAgentProcess(pt, pane.PID) {
					agentPanes = append(agentPanes, fmt.Sprintf("  %s:%s (pane %s, %s)",
						sess.Name, win.Name, pane.ID, pane.Command))
				}
			}
		}
	}

	if len(agentPanes) > 0 {
		return fmt.Sprintf("agents running in worktree %s:\n%s",
			ShortenHome(wtPath), strings.Join(agentPanes, "\n"))
	}
	return ""
}

// HasAgentProcess checks if a pane's process tree contains a known agent.
func HasAgentProcess(pt proc.Table, panePID int) bool {
	// Walk children of the pane shell.
	for _, childPID := range pt.Children[panePID] {
		child, ok := pt.Procs[childPID]
		if !ok {
			continue
		}
		name := strings.ToLower(child.Comm)
		if strings.Contains(name, "claude") || strings.Contains(name, "codex") {
			return true
		}
		// Check args too for wrapped invocations (e.g. "node claude").
		args := strings.ToLower(child.Args)
		if strings.Contains(args, "claude") || strings.Contains(args, "codex") {
			return true
		}
	}
	return false
}

// CleanupTmuxWindow finds and kills a tmux window whose working directory
// matches the given worktree path.
func CleanupTmuxWindow(wtPath string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	// List all panes to find windows in this worktree.
	out, err := tmux.Run("list-panes", "-a", "-F", "#{session_name}:#{window_index}\t#{pane_current_path}")
	if err != nil {
		return
	}
	killed := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		winTarget, panePath := parts[0], parts[1]
		if strings.HasPrefix(panePath, wtPath) && !killed[winTarget] {
			tmux.Run("kill-window", "-t", winTarget)
			killed[winTarget] = true
		}
	}
}

// SwitchToTmuxWindow switches to a tmux window by name in the current session.
func SwitchToTmuxWindow(windowName string) {
	target, err := tmux.FetchCurrentTarget()
	if err != nil {
		return
	}
	tmux.Run("select-window", "-t", target.Session+":"+windowName)
}
