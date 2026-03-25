package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
)

// MergeOpts configures the merge workflow.
type MergeOpts struct {
	Squash  bool   // squash all commits into one before merge
	NoFF    bool   // create a merge commit even for fast-forward
	Force   bool   // skip integration checks
	Message string // commit message (for squash); empty = auto-generate
	NoEdit  bool   // don't open editor for commit message
	Keep    bool   // keep worktree after merge (don't remove)
}

// Merge implements the full merge workflow inspired by Worktrunk:
//  1. Validate we're in a worktree (not main)
//  2. Run pre-commit hooks
//  3. Squash commits if requested
//  4. Commit (with optional LLM-generated message)
//  5. Run post-commit hooks
//  6. Rebase onto target
//  7. Run pre-merge hooks
//  8. Merge into target (ff or --no-ff)
//  9. Run post-merge hooks
//  10. Clean up: remove worktree + branch + tmux window
func Merge(args []string) error {
	opts := MergeOpts{}
	positional := []string{}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--squash", "-s":
			opts.Squash = true
		case "--no-ff":
			opts.NoFF = true
		case "--force", "-f":
			opts.Force = true
		case "--keep":
			opts.Keep = true
		case "--no-edit":
			opts.NoEdit = true
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
	root, err := FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	cfg := config.Load()
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
		// Default: merge into the default branch.
		target, err = DefaultBranch(root)
		if err != nil {
			return fmt.Errorf("no target specified and cannot determine default branch: %w", err)
		}
	}

	if currentBranch == target {
		return fmt.Errorf("already on target branch %s, nothing to merge", target)
	}

	// Verify target branch exists.
	if _, err := git.Cmd(root, "show-ref", "--verify", "--quiet", "refs/heads/"+target); err != nil {
		return fmt.Errorf("target branch %q does not exist", target)
	}

	// Check for uncommitted changes.
	status, _ := git.Cmd(cwd, "status", "--porcelain")
	hasChanges := strings.TrimSpace(status) != ""

	// Find the current worktree info.
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

	// Find the target worktree (needed for switching and post-merge hooks).
	var targetWt *git.Worktree
	for i := range wts {
		if wts[i].Branch == target {
			targetWt = &wts[i]
			break
		}
	}

	fmt.Fprintf(os.Stderr, "merging %s into %s\n", currentBranch, target)

	// Step 1: Handle uncommitted changes.
	if hasChanges {
		if !opts.Squash {
			return fmt.Errorf("uncommitted changes present; commit them first or use --squash")
		}
		fmt.Fprintf(os.Stderr, "staging uncommitted changes\n")
		if _, err := git.Cmd(cwd, "add", "-A"); err != nil {
			return fmt.Errorf("git add failed: %w", err)
		}
	}

	// Step 2: Pre-commit hooks.
	if len(wtCfg.PreCommit) > 0 && (opts.Squash || hasChanges) {
		fmt.Fprintf(os.Stderr, "running %d pre-commit hooks\n", len(wtCfg.PreCommit))
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
		fmt.Fprintf(os.Stderr, "running %d post-commit hooks\n", len(wtCfg.PostCommit))
		if err := RunHooks("post-commit", mainWt, root, wtCfg.PostCommit); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-commit hook failed: %v\n", err)
		}
	}

	// Step 5: Rebase onto target.
	fmt.Fprintf(os.Stderr, "rebasing onto %s\n", target)
	if _, err := git.Cmd(cwd, "rebase", target); err != nil {
		return fmt.Errorf("rebase failed: %w\nresolve conflicts and run: git rebase --continue", err)
	}

	// Step 6: Pre-merge hooks.
	if len(wtCfg.PreMerge) > 0 {
		fmt.Fprintf(os.Stderr, "running %d pre-merge hooks\n", len(wtCfg.PreMerge))
		if err := RunHooks("pre-merge", mainWt, root, wtCfg.PreMerge); err != nil {
			return fmt.Errorf("pre-merge hook failed: %w", err)
		}
	}

	// Step 7: Switch to target and merge.
	mergeDir := root
	if targetWt != nil {
		mergeDir = targetWt.Path
	} else {
		// Target has no worktree -- checkout in main worktree.
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

	fmt.Fprintf(os.Stderr, "merging %s into %s\n", currentBranch, target)
	if _, err := git.Cmd(mergeDir, mergeArgs...); err != nil {
		if !opts.NoFF {
			// ff-only failed, try with --no-ff if forced.
			fmt.Fprintf(os.Stderr, "fast-forward not possible, trying merge commit\n")
			mergeArgs = []string{"merge", "--no-ff", currentBranch}
			if _, err := git.Cmd(mergeDir, mergeArgs...); err != nil {
				return fmt.Errorf("merge failed: %w", err)
			}
		} else {
			return fmt.Errorf("merge failed: %w", err)
		}
	}

	// Step 8: Post-merge hooks (run in target worktree).
	if len(wtCfg.PostMerge) > 0 {
		hookDir := mergeDir
		fmt.Fprintf(os.Stderr, "running %d post-merge hooks\n", len(wtCfg.PostMerge))
		if err := RunHooks("post-merge", mainWt, hookDir, wtCfg.PostMerge); err != nil {
			fmt.Fprintf(os.Stderr, "warning: post-merge hook failed: %v\n", err)
		}
	}

	// Step 9: Clean up worktree + branch.
	if !opts.Keep && currentWt != nil && !currentWt.IsMain {
		fmt.Fprintf(os.Stderr, "cleaning up worktree %s\n", ShortenHome(currentWt.Path))

		// Run pre-remove hooks.
		if len(wtCfg.PreRemove) > 0 {
			RunHooks("pre-remove", mainWt, currentWt.Path, wtCfg.PreRemove)
		}

		// Remove worktree.
		if err := RemoveWorktree(root, currentWt.Path, true); err != nil {
			fmt.Fprintf(os.Stderr, "warning: worktree remove failed: %v\n", err)
		}

		// Delete branch (safe -- it's been merged).
		fmt.Fprintf(os.Stderr, "deleting branch %s\n", currentBranch)
		if err := DeleteBranch(root, currentBranch, false); err != nil {
			fmt.Fprintf(os.Stderr, "warning: branch delete failed: %v\n", err)
		}

		// Kill the tmux window for the removed worktree.
		CleanupTmuxWindow(currentWt.Path)
	}

	// Step 10: Switch to target worktree.
	if targetWt != nil && os.Getenv("TMUX") != "" {
		windowName := SanitizeBranch(target)
		SwitchToTmuxWindow(windowName)
	}

	fmt.Fprintf(os.Stderr, "merged %s into %s\n", currentBranch, target)
	return nil
}

// squashCommits squashes all branch commits into a single commit.
func squashCommits(wtDir, target, branch string, opts MergeOpts, wtCfg *config.WorktreeConfig) error {
	// Find merge base.
	mergeBase, err := git.Cmd(wtDir, "merge-base", target, branch)
	if err != nil {
		return fmt.Errorf("cannot find merge base between %s and %s: %w", target, branch, err)
	}

	// Soft reset to merge base (keeps all changes staged).
	fmt.Fprintf(os.Stderr, "squashing commits since %s\n", mergeBase[:8])
	if _, err := git.Cmd(wtDir, "reset", "--soft", mergeBase); err != nil {
		return fmt.Errorf("git reset --soft failed: %w", err)
	}

	// Determine commit message.
	message := opts.Message
	if message == "" {
		message = generateCommitMessage(wtDir, target, branch, wtCfg)
	}

	// Commit.
	commitArgs := []string{"commit"}
	if message != "" {
		commitArgs = append(commitArgs, "-m", message)
	} else if !opts.NoEdit {
		// Open editor for message.
		return commitInteractive(wtDir)
	}

	if _, err := git.Cmd(wtDir, commitArgs...); err != nil {
		return fmt.Errorf("squash commit failed: %w", err)
	}

	return nil
}

// generateCommitMessage creates a commit message, optionally via LLM.
func generateCommitMessage(wtDir, target, branch string, wtCfg *config.WorktreeConfig) string {
	// Try LLM generation if configured.
	if wtCfg.CommitCmd != "" {
		msg := generateCommitMessageLLM(wtDir, wtCfg.CommitCmd)
		if msg != "" {
			return msg
		}
		fmt.Fprintf(os.Stderr, "warning: LLM commit message generation failed, using default\n")
	}

	// Default message: summarize the branch.
	diffStat, _ := git.Cmd(wtDir, "diff", "--stat", "HEAD~1")
	subject := fmt.Sprintf("Merge branch '%s'", branch)
	if diffStat != "" {
		return subject + "\n\n" + diffStat
	}
	return subject
}

// generateCommitMessageLLM pipes the staged diff to an LLM command and returns its output.
func generateCommitMessageLLM(wtDir, llmCmd string) string {
	// Get the staged diff.
	diff, err := git.Cmd(wtDir, "diff", "--cached", "--stat")
	if err != nil || diff == "" {
		diff, _ = git.Cmd(wtDir, "diff", "HEAD~1", "--stat")
	}
	if diff == "" {
		return ""
	}

	// Get detailed diff for context (limit to avoid token overload).
	detailedDiff, _ := git.Cmd(wtDir, "diff", "--cached")
	if detailedDiff == "" {
		detailedDiff, _ = git.Cmd(wtDir, "diff", "HEAD~1")
	}

	// Truncate large diffs.
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
