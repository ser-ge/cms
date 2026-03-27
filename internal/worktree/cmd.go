package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/proc"
	"github.com/serge/cms/internal/tmux"
)

// RunCmd dispatches worktree subcommands (kept for cms internal worktree).
func RunCmd(args []string) error {
	if len(args) == 0 {
		return RunList()
	}
	switch args[0] {
	case "list", "ls":
		return RunList()
	case "switch", "add", "a":
		return RunSwitch(args[1:])
	case "go":
		return RunGo(args[1:])
	case "remove", "rm":
		return RunRemove(args[1:])
	case "land", "merge", "m":
		return Land(args[1:])
	default:
		return fmt.Errorf("unknown worktree command: %s\nusage: cms worktree [list|switch|go|remove|land]", args[0])
	}
}

// RunList prints the worktree table.
func RunList() error {
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

// SwitchOpts configures the switch command (git switch semantics).
type SwitchOpts struct {
	NoOpen      bool   // skip tmux window creation/switch
	NewBranch   string // -c: create new branch (error if exists)
	ForceBranch string // -C: force-create branch (reset if exists)
	Force       bool   // --force: force worktree creation
	Path        string // --path: override auto-resolved worktree dir
	StartPoint  string // positional start-point (only with -c/-C)
	Prompt      string // prompt string to pass to go_cmd after worktree setup
}

// SwitchWorktree switches to a branch's worktree, creating it if needed.
// With strict git switch semantics: unknown branches require -c/-C.
func SwitchWorktree(root, branch string, opts SwitchOpts) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	wtCfg := ResolveWorktreeConfig(root, cwd, &cfg.Worktree)

	// Check if worktree already exists for this branch — switch to it.
	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}
	for _, wt := range wts {
		if wt.Branch == branch || SanitizeBranch(wt.Branch) == branch ||
			filepath.Base(wt.Path) == branch {
			if !opts.NoOpen && os.Getenv("TMUX") != "" {
				switchOrOpenTmuxWindow(wt.Path, wt.Branch)
			}
			fmt.Println(wt.Path)
			return nil
		}
	}

	// Resolve path.
	path := opts.Path
	if path == "" {
		baseDir := ResolveWorktreeBaseDir(root, &wtCfg)
		path = filepath.Join(baseDir, SanitizeBranch(branch))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	// Build create options based on mode.
	createOpts := CreateWorktreeOpts{Force: opts.Force}

	if opts.ForceBranch != "" {
		// -C: force-create branch.
		createOpts.NewBranch = true
		createOpts.ForceBranch = true
		createOpts.StartPoint = opts.StartPoint
	} else if opts.NewBranch != "" {
		// -c: create new branch, error if exists.
		if local, _, _ := ResolveBranch(root, branch); local {
			return fmt.Errorf("branch %q already exists (use -C to force-reset)", branch)
		}
		createOpts.NewBranch = true
		createOpts.StartPoint = opts.StartPoint
	} else {
		// No -c/-C: strict mode — branch must exist.
		local, remote, err := ResolveBranch(root, branch)
		if err != nil {
			return fmt.Errorf("branch %q not found (use -c to create a new branch)", branch)
		}
		if !local && remote != "" {
			createOpts.Track = remote
		}
	}

	fmt.Fprintf(os.Stderr, "creating worktree at %s for branch %s\n", ShortenHome(path), branch)
	if err := CreateWorktree(root, path, branch, createOpts); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	runPostCreateHooksAndOpen(path, branch, &wtCfg, opts.NoOpen)
	fmt.Println(path)
	return nil
}

// GoWorktree switches to a branch's worktree, auto-creating from base_branch if needed.
// This is the opinionated shortcut — unknown branches are created automatically.
func GoWorktree(root, branch string, opts SwitchOpts) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	wtCfg := ResolveWorktreeConfig(root, cwd, &cfg.Worktree)

	// Check if worktree already exists — switch to it.
	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}
	for _, wt := range wts {
		if wt.Branch == branch || SanitizeBranch(wt.Branch) == branch ||
			filepath.Base(wt.Path) == branch {
			if !opts.NoOpen && os.Getenv("TMUX") != "" {
				switchOrOpenTmuxWindow(wt.Path, wt.Branch)
			}
			fmt.Println(wt.Path)
			return maybeRunGoCmd(wt.Path, root, opts.Prompt, &wtCfg)
		}
	}

	// Resolve path.
	path := opts.Path
	if path == "" {
		baseDir := ResolveWorktreeBaseDir(root, &wtCfg)
		path = filepath.Join(baseDir, SanitizeBranch(branch))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	// Resolve branch: check local, then remote, then create new.
	createOpts := CreateWorktreeOpts{Force: opts.Force}
	local, remote, err := ResolveBranch(root, branch)
	if err != nil {
		// Branch doesn't exist — create from start-point or base_branch.
		createOpts.NewBranch = true
		if opts.StartPoint != "" {
			createOpts.StartPoint = opts.StartPoint
		} else {
			// Use configured base_branch or default branch.
			baseBranch := wtCfg.BaseBranch
			if baseBranch == "" {
				baseBranch, _ = DefaultBranch(root)
			}
			if baseBranch != "" {
				createOpts.StartPoint = baseBranch
			}
		}
	} else if !local && remote != "" {
		createOpts.Track = remote
	}

	fmt.Fprintf(os.Stderr, "creating worktree at %s for branch %s\n", ShortenHome(path), branch)
	if err := CreateWorktree(root, path, branch, createOpts); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	runPostCreateHooksAndOpen(path, branch, &wtCfg, opts.NoOpen)
	fmt.Println(path)
	return maybeRunGoCmd(path, root, opts.Prompt, &wtCfg)
}

// runPostCreateHooksAndOpen runs post-create hooks and opens a tmux window.
func runPostCreateHooksAndOpen(path, branch string, wtCfg *config.WorktreeConfig, noOpen bool) {
	var mainWt string
	if root, err := FindRepoRoot(path); err == nil {
		mainWt, _ = FindMainWorktree(root)
	}
	if len(wtCfg.Hooks) > 0 {
		fmt.Fprintf(os.Stderr, "running %d post-create hooks\n", len(wtCfg.Hooks))
		if err := RunPostCreateHooks(mainWt, path, wtCfg.Hooks); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook failed: %v\n", err)
		}
	}
	if !noOpen && os.Getenv("TMUX") != "" {
		openTmuxWindow(branch, path)
	}
}

// switchOrOpenTmuxWindow switches to an existing tmux window for the worktree,
// or creates a new one if none exists.
func switchOrOpenTmuxWindow(wtPath, branch string) {
	sessions, _, err := tmux.FetchState()
	if err == nil {
		for _, sess := range sessions {
			for _, win := range sess.Windows {
				for _, pane := range win.Panes {
					if strings.HasPrefix(pane.WorkingDir, wtPath) {
						tmux.Run("switch-client", "-t", pane.ID)
						return
					}
				}
			}
		}
	}
	openTmuxWindow(branch, wtPath)
}

// RunSwitch parses flags for the switch command (strict git switch semantics).
func RunSwitch(args []string) error {
	opts := SwitchOpts{}
	positional := []string{}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-c":
			if i+1 >= len(args) {
				return fmt.Errorf("-c requires a branch name")
			}
			i++
			opts.NewBranch = args[i]
		case "-C":
			if i+1 >= len(args) {
				return fmt.Errorf("-C requires a branch name")
			}
			i++
			opts.ForceBranch = args[i]
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a directory")
			}
			i++
			opts.Path = args[i]
		case "--force", "-f":
			opts.Force = true
		case "--no-open":
			opts.NoOpen = true
		default:
			positional = append(positional, args[i])
		}
	}

	// Determine branch and start-point from flags + positionals.
	var branch string
	if opts.NewBranch != "" || opts.ForceBranch != "" {
		// -c/-C: branch from flag, positional is start-point.
		if opts.NewBranch != "" {
			branch = opts.NewBranch
		} else {
			branch = opts.ForceBranch
		}
		if len(positional) > 0 {
			opts.StartPoint = positional[0]
		}
		if len(positional) > 1 {
			return fmt.Errorf("too many arguments")
		}
	} else {
		// No -c/-C: single positional is branch, no start-point allowed.
		if len(positional) == 0 {
			return fmt.Errorf("usage: cms switch [-c/-C <branch>] [<start-point>] | cms switch <branch>")
		}
		branch = positional[0]
		if len(positional) > 1 {
			return fmt.Errorf("start-point requires -c or -C (did you mean: cms switch -c %s %s?)", positional[0], positional[1])
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	// Resolve symbols.
	branch, err = ResolveWorktreeSymbol(root, branch)
	if err != nil {
		return err
	}
	if opts.StartPoint != "" {
		opts.StartPoint, err = ResolveWorktreeSymbol(root, opts.StartPoint)
		if err != nil {
			return err
		}
	}

	return SwitchWorktree(root, branch, opts)
}

// RunGo parses flags for the go command (opinionated switch-or-create).
func RunGo(args []string) error {
	opts := SwitchOpts{}
	positional := []string{}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a directory")
			}
			i++
			opts.Path = args[i]
		case "--force", "-f":
			opts.Force = true
		case "--no-open":
			opts.NoOpen = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms go <branch> [<start-point>] [<prompt>]")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	branch, err := ResolveWorktreeSymbol(root, positional[0])
	if err != nil {
		return err
	}

	// Remaining positional args: distinguish start-point (no spaces) from prompt (has spaces).
	// - 1 extra arg with spaces → prompt
	// - 1 extra arg without spaces → start-point
	// - 2+ extra args → first without spaces is start-point, next is prompt
	for _, arg := range positional[1:] {
		if strings.ContainsRune(arg, ' ') {
			opts.Prompt = arg
		} else if opts.StartPoint == "" {
			sp, err := ResolveWorktreeSymbol(root, arg)
			if err != nil {
				return err
			}
			opts.StartPoint = sp
		} else {
			opts.Prompt = arg
		}
	}

	return GoWorktree(root, branch, opts)
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

// maybeRunGoCmd runs the configured go_cmd with the given prompt.
// The prompt is always available as $CMS_PROMPT in the command's environment.
// If go_cmd does not reference $CMS_PROMPT or ${CMS_PROMPT}, the prompt is
// appended as a shell-quoted argument.
// Returns nil if no prompt is provided.
func maybeRunGoCmd(wtPath, repoRoot, prompt string, wtCfg *config.WorktreeConfig) error {
	if prompt == "" {
		return nil
	}
	if wtCfg.GoCmd == "" {
		return fmt.Errorf("prompt provided but [worktree] go_cmd is not configured")
	}

	shellCmd := wtCfg.GoCmd
	if !strings.Contains(shellCmd, "$CMS_PROMPT") &&
		!strings.Contains(shellCmd, "${CMS_PROMPT}") {
		shellCmd += " " + shellQuote(prompt)
	}
	cmd := exec.Command("sh", "-c", shellCmd)
	cmd.Dir = wtPath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"CMS_WORKTREE_PATH="+wtPath,
		"CMS_REPO_ROOT="+repoRoot,
		"CMS_PROMPT="+prompt,
	)
	return cmd.Run()
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// RunRemove parses flags and removes a worktree.
func RunRemove(args []string) error {
	force := false
	forceBranch := false
	keepBranch := false
	dryRun := false
	positional := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--force", "-f":
			force = true
		case "-D":
			forceBranch = true
		case "--keep-branch":
			keepBranch = true
		case "--dry-run":
			dryRun = true
		default:
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("usage: cms rm [-f] [-D] [--keep-branch] [--dry-run] <branch-or-path>")
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

	// Dry-run: show what would happen and exit.
	if dryRun {
		fmt.Fprintf(os.Stderr, "would remove worktree at %s\n", ShortenHome(found.Path))
		if !keepBranch && found.Branch != "" {
			defBranch, _ := DefaultBranch(root)
			integrated, _ := IsBranchIntegrated(root, found.Branch, defBranch)
			if forceBranch || integrated {
				fmt.Fprintf(os.Stderr, "would delete branch %s\n", found.Branch)
			} else {
				fmt.Fprintf(os.Stderr, "would skip branch %s (not merged; use -D to force)\n", found.Branch)
			}
		}
		fmt.Fprintf(os.Stderr, "would kill tmux window for %s\n", ShortenHome(found.Path))
		return nil
	}

	// Run pre-remove hooks.
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
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

	// Delete branch unless --keep-branch.
	if !keepBranch && found.Branch != "" {
		if !forceBranch {
			defBranch, err := DefaultBranch(root)
			if err == nil && defBranch != "" {
				integrated, reason := IsBranchIntegrated(root, found.Branch, defBranch)
				if !integrated {
					fmt.Fprintf(os.Stderr, "warning: branch %s is not merged into %s, skipping deletion\n", found.Branch, defBranch)
					fmt.Fprintf(os.Stderr, "  use -D to delete anyway\n")
					goto cleanup
				}
				fmt.Fprintf(os.Stderr, "branch %s is safe to delete (%s)\n", found.Branch, reason)
			}
		}
		fmt.Fprintf(os.Stderr, "deleting branch %s\n", found.Branch)
		if err := DeleteBranch(root, found.Branch, forceBranch); err != nil {
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
