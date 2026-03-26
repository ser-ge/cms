package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
)

// ANSI color helpers for CLI output.
var (
	cReset = "\033[0m"
	cRed   = "\033[31m"
	cGreen = "\033[32m"
	cDim   = "\033[2m"
	cBold  = "\033[1m"
)

func init() {
	// Disable colors when stderr is not a terminal.
	if fi, err := os.Stderr.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		cReset, cRed, cGreen, cDim, cBold = "", "", "", "", ""
	}
}

func red(s string) string   { return cRed + s + cReset }
func green(s string) string { return cGreen + s + cReset }
func dim(s string) string   { return cDim + s + cReset }
func bold(s string) string  { return cBold + s + cReset }

// LandOpts configures the land workflow.
type LandOpts struct {
	Squash   bool   // squash all commits into one before landing
	NoFF     bool   // create a merge commit even for fast-forward
	Message  string // commit message (for squash); empty = auto-generate
	NoEdit   bool   // don't open editor for commit message
	Keep     bool   // keep worktree after landing (don't remove)
	Abort    bool   // abort an in-progress rebase
	Continue bool   // resume after resolving rebase conflicts
}

// Land implements the full land workflow:
//  1. Stage uncommitted changes  (if --squash)
//  2. Run pre-commit hooks
//  3. Squash commits             (if --squash)
//  4. Run post-commit hooks
//  5. Rebase onto target
//  6. Run pre-land hooks
//  7. Fast-forward merge into target (or --no-ff merge commit)
//  8. Run post-land hooks
//  9. Remove worktree + branch + tmux window (unless --keep)
func Land(args []string) error {
	opts := LandOpts{}
	positional := []string{}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--squash":
			opts.Squash = true
		case "--no-ff":
			opts.NoFF = true
		case "--keep":
			opts.Keep = true
		case "--no-edit":
			opts.NoEdit = true
		case "--abort":
			opts.Abort = true
		case "--continue":
			opts.Continue = true
		case "-m":
			if i+1 < len(args) {
				i++
				opts.Message = args[i]
			}
		default:
			positional = append(positional, args[i])
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Handle --abort early.
	if opts.Abort {
		fmt.Fprintf(os.Stderr, "%s rebase\n", red("aborting"))
		if _, err := git.Cmd(cwd, "rebase", "--abort"); err != nil {
			return fmt.Errorf("rebase --abort failed: %w", err)
		}
		return nil
	}

	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	resolved := ResolveWorktreeConfig(root, cwd, &cfg.Worktree)
	wtCfg := &resolved
	mainWt, _ := FindMainWorktree(root)

	// Determine current branch (use cwd, not root -- root may be a bare repo).
	currentBranch, err := git.Cmd(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot determine current branch: %w", err)
	}

	// Determine target branch.
	var target string
	if len(positional) > 0 {
		target, err = ResolveWorktreeSymbol(root, positional[0])
		if err != nil {
			return err
		}
	} else {
		// Default: use configured base_branch, then auto-detect.
		target = wtCfg.BaseBranch
		if target == "" {
			target, err = DefaultBranch(root)
			if err != nil {
				return fmt.Errorf("no target specified and cannot determine default branch: %w", err)
			}
		}
	}

	if currentBranch == target {
		return fmt.Errorf("already on target branch %s, nothing to land", target)
	}

	// Verify target branch exists.
	if _, err := git.Cmd(root, "show-ref", "--verify", "--quiet", "refs/heads/"+target); err != nil {
		return fmt.Errorf("target branch %q does not exist", target)
	}

	// Find worktree info.
	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}
	var currentWt *git.Worktree
	for i := range wts {
		if wts[i].Branch == currentBranch {
			currentWt = &wts[i]
			break
		}
	}
	var targetWt *git.Worktree
	for i := range wts {
		if wts[i].Branch == target {
			targetWt = &wts[i]
			break
		}
	}

	// Handle --continue: resume from step 6 (rebase --continue, then merge).
	if opts.Continue {
		fmt.Fprintf(os.Stderr, "%s rebase\n", green("continuing"))
		if err := git.RunInteractive(cwd, "rebase", "--continue"); err != nil {
			return fmt.Errorf("rebase --continue failed: %w\nresolve remaining conflicts and run: cms land --continue", err)
		}
		return landMergeAndCleanup(cwd, root, currentBranch, target, mainWt, currentWt, targetWt, opts, wtCfg)
	}

	fmt.Fprintf(os.Stderr, "%s %s into %s\n", green("landing"), bold(currentBranch), bold(target))

	// Check for uncommitted changes.
	status, _ := git.Cmd(cwd, "status", "--porcelain")
	hasChanges := strings.TrimSpace(status) != ""

	// Step 1: Handle uncommitted changes.
	if hasChanges {
		if !opts.Squash {
			return fmt.Errorf("uncommitted changes present; commit them first or use --squash")
		}
		fmt.Fprintf(os.Stderr, "%s uncommitted changes\n", dim("staging"))
		if _, err := git.Cmd(cwd, "add", "-A"); err != nil {
			return fmt.Errorf("git add failed: %w", err)
		}
	}

	// Step 2: Pre-commit hooks.
	if len(wtCfg.PreCommit) > 0 && (opts.Squash || hasChanges) {
		fmt.Fprintf(os.Stderr, "%s %d pre-commit hooks\n", dim("running"), len(wtCfg.PreCommit))
		if err := RunHooks("pre-commit", mainWt, root, wtCfg.PreCommit); err != nil {
			return fmt.Errorf("pre-commit hook failed: %w", err)
		}
	}

	// Step 3: Squash if requested.
	if opts.Squash {
		if err := squashCommits(cwd, target, currentBranch, opts, wtCfg); err != nil {
			return err
		}
	}

	// Step 4: Post-commit hooks.
	if len(wtCfg.PostCommit) > 0 && opts.Squash {
		fmt.Fprintf(os.Stderr, "%s %d post-commit hooks\n", dim("running"), len(wtCfg.PostCommit))
		if err := RunHooks("post-commit", mainWt, root, wtCfg.PostCommit); err != nil {
			fmt.Fprintf(os.Stderr, "%s post-commit hook failed: %v\n", red("warning:"), err)
		}
	}

	// Step 5: Rebase onto target.
	fmt.Fprintf(os.Stderr, "%s onto %s\n", dim("rebasing"), bold(target))
	if _, err := git.Cmd(cwd, "rebase", target); err != nil {
		return fmt.Errorf("rebase failed: %w\nresolve conflicts and run: cms land --continue\nor abort with: cms land --abort", err)
	}

	// Steps 6-9: Merge and cleanup.
	return landMergeAndCleanup(cwd, root, currentBranch, target, mainWt, currentWt, targetWt, opts, wtCfg)
}

// landMergeAndCleanup handles steps 6-9 of the land workflow (shared by normal and --continue paths).
func landMergeAndCleanup(cwd, root, currentBranch, target, mainWt string, currentWt, targetWt *git.Worktree, opts LandOpts, wtCfg *config.WorktreeConfig) error {
	// Step 6: Pre-land hooks.
	if len(wtCfg.PreMerge) > 0 {
		fmt.Fprintf(os.Stderr, "%s %d pre-land hooks\n", dim("running"), len(wtCfg.PreMerge))
		if err := RunHooks("pre-land", mainWt, root, wtCfg.PreMerge); err != nil {
			return fmt.Errorf("pre-land hook failed: %w", err)
		}
	}

	// Step 7: Switch to target and merge.
	mergeDir := root
	if targetWt != nil {
		mergeDir = targetWt.Path
	} else {
		mergeDir = mainWt
		if _, err := git.Cmd(mergeDir, "checkout", target); err != nil {
			return fmt.Errorf("cannot checkout target %s: %w", target, err)
		}
	}

	mergeArgs := []string{"merge"}
	if opts.NoFF {
		mergeArgs = append(mergeArgs, "--no-ff")
	} else {
		mergeArgs = append(mergeArgs, "--ff-only")
	}
	mergeArgs = append(mergeArgs, currentBranch)

	fmt.Fprintf(os.Stderr, "%s %s into %s\n", dim("merging"), bold(currentBranch), bold(target))
	if _, err := git.Cmd(mergeDir, mergeArgs...); err != nil {
		if !opts.NoFF {
			fmt.Fprintf(os.Stderr, "%s fast-forward not possible, trying merge commit\n", red("warning:"))
			mergeArgs = []string{"merge", "--no-ff", currentBranch}
			if _, err := git.Cmd(mergeDir, mergeArgs...); err != nil {
				return fmt.Errorf("merge failed: %w", err)
			}
		} else {
			return fmt.Errorf("merge failed: %w", err)
		}
	}

	// Step 8: Post-land hooks.
	if len(wtCfg.PostMerge) > 0 {
		hookDir := mergeDir
		fmt.Fprintf(os.Stderr, "%s %d post-land hooks\n", dim("running"), len(wtCfg.PostMerge))
		if err := RunHooks("post-land", mainWt, hookDir, wtCfg.PostMerge); err != nil {
			fmt.Fprintf(os.Stderr, "%s post-land hook failed: %v\n", red("warning:"), err)
		}
	}

	fmt.Fprintf(os.Stderr, "%s landed %s into %s\n", green("✓"), bold(currentBranch), bold(target))

	// Wait for confirmation before closing the window.
	willCleanup := !opts.Keep && currentWt != nil && !currentWt.IsMain
	if willCleanup {
		fmt.Fprintf(os.Stderr, "%s to switch to %s and close this worktree...", dim("press enter"), bold(target))
		fmt.Scanln()
	}

	// Switch to target worktree BEFORE cleanup — CleanupTmuxWindow kills
	// the current window (and this process with it), so the switch must
	// happen first.
	if os.Getenv("TMUX") != "" {
		if targetWt != nil {
			switchOrOpenTmuxWindow(targetWt.Path, target)
		} else if mainWt != "" {
			switchOrOpenTmuxWindow(mainWt, target)
		}
	}

	// Step 9: Clean up worktree + branch.
	if willCleanup {
		fmt.Fprintf(os.Stderr, "%s worktree %s\n", dim("cleaning up"), ShortenHome(currentWt.Path))

		if len(wtCfg.PreRemove) > 0 {
			RunHooks("pre-remove", mainWt, currentWt.Path, wtCfg.PreRemove)
		}

		if err := RemoveWorktree(root, currentWt.Path, true); err != nil {
			fmt.Fprintf(os.Stderr, "%s worktree remove failed: %v\n", red("warning:"), err)
		}

		fmt.Fprintf(os.Stderr, "%s branch %s\n", dim("deleting"), currentBranch)
		if err := DeleteBranch(root, currentBranch, false); err != nil {
			fmt.Fprintf(os.Stderr, "%s branch delete failed: %v\n", red("warning:"), err)
		}

		CleanupTmuxWindow(currentWt.Path)
	}

	return nil
}

// squashCommits squashes all branch commits into a single commit.
func squashCommits(wtDir, target, branch string, opts LandOpts, wtCfg *config.WorktreeConfig) error {
	mergeBase, err := git.Cmd(wtDir, "merge-base", target, branch)
	if err != nil {
		return fmt.Errorf("cannot find merge base between %s and %s: %w", target, branch, err)
	}

	fmt.Fprintf(os.Stderr, "%s commits since %s\n", dim("squashing"), mergeBase[:8])
	if _, err := git.Cmd(wtDir, "reset", "--soft", mergeBase); err != nil {
		return fmt.Errorf("git reset --soft failed: %w", err)
	}

	message := opts.Message
	if message == "" {
		message = generateCommitMessage(wtDir, target, branch, wtCfg)
	}

	commitArgs := []string{"commit"}
	if message != "" {
		commitArgs = append(commitArgs, "-m", message)
	} else if !opts.NoEdit {
		return commitInteractive(wtDir)
	}

	if _, err := git.Cmd(wtDir, commitArgs...); err != nil {
		return fmt.Errorf("squash commit failed: %w", err)
	}

	return nil
}

// generateCommitMessage creates a commit message, optionally via LLM.
func generateCommitMessage(wtDir, target, branch string, wtCfg *config.WorktreeConfig) string {
	if wtCfg.CommitCmd != "" {
		msg := generateCommitMessageLLM(wtDir, wtCfg.CommitCmd)
		if msg != "" {
			return msg
		}
		fmt.Fprintf(os.Stderr, "%s LLM commit message generation failed, using default\n", red("warning:"))
	}

	diffStat, _ := git.Cmd(wtDir, "diff", "--stat", "HEAD~1")
	subject := fmt.Sprintf("Merge branch '%s'", branch)
	if diffStat != "" {
		return subject + "\n\n" + diffStat
	}
	return subject
}

// generateCommitMessageLLM pipes the staged diff to an LLM command and returns its output.
func generateCommitMessageLLM(wtDir, llmCmd string) string {
	diff, err := git.Cmd(wtDir, "diff", "--cached", "--stat")
	if err != nil || diff == "" {
		diff, _ = git.Cmd(wtDir, "diff", "HEAD~1", "--stat")
	}
	if diff == "" {
		return ""
	}

	detailedDiff, _ := git.Cmd(wtDir, "diff", "--cached")
	if detailedDiff == "" {
		detailedDiff, _ = git.Cmd(wtDir, "diff", "HEAD~1")
	}

	const maxDiff = 8000
	if len(detailedDiff) > maxDiff {
		detailedDiff = detailedDiff[:maxDiff] + "\n... (truncated)"
	}

	prompt := fmt.Sprintf("Write a concise git commit message (subject line + optional body) for this diff. "+
		"Focus on the 'why' not the 'what'. No quotes around the message.\n\nDiff summary:\n%s\n\nDiff:\n%s",
		diff, detailedDiff)

	cmd := exec.Command("sh", "-c", llmCmd)
	cmd.Dir = wtDir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// commitInteractive opens the user's editor for a commit message.
func commitInteractive(wtDir string) error {
	cmd := exec.Command("git", "-C", wtDir, "commit")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
