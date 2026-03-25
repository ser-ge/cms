package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepoWithBranch creates a test repo with a feature branch that has
// commits diverged from the default branch. Returns (repoRoot, worktreePath, defaultBranch).
func initTestRepoWithBranch(t *testing.T) (string, string, string) {
	t.Helper()
	repo := initTestRepo(t)

	defBranch, _ := gitCmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Add a file on the default branch so there's content.
	os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base"), 0o644)
	runGit(t, repo, "add", "base.txt")
	runGit(t, repo, "commit", "-m", "add base.txt")

	// Create a feature worktree with commits.
	wtPath := filepath.Join(t.TempDir(), "feature")
	err := CreateWorktree(repo, wtPath, "feature", CreateWorktreeOpts{NewBranch: true})
	if err != nil {
		t.Fatal(err)
	}

	// Add commits on the feature branch.
	os.WriteFile(filepath.Join(wtPath, "feat.txt"), []byte("feature work"), 0o644)
	runGit(t, wtPath, "add", "feat.txt")
	runGit(t, wtPath, "commit", "-m", "add feat.txt")

	os.WriteFile(filepath.Join(wtPath, "feat2.txt"), []byte("more work"), 0o644)
	runGit(t, wtPath, "add", "feat2.txt")
	runGit(t, wtPath, "commit", "-m", "add feat2.txt")

	return repo, wtPath, defBranch
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s (%v)", args, out, err)
	}
}

func TestSquashCommits(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Count commits before squash.
	logBefore, _ := gitCmd(wtPath, "log", "--oneline")
	linesBefore := len(strings.Split(strings.TrimSpace(logBefore), "\n"))

	opts := MergeOpts{Squash: true, Message: "squashed feature"}
	wtCfg := &WorktreeConfig{}
	err := squashCommits(wtPath, defBranch, "feature", opts, wtCfg)
	if err != nil {
		t.Fatal(err)
	}

	// After squash: should have fewer commits.
	logAfter, _ := gitCmd(wtPath, "log", "--oneline")
	linesAfter := len(strings.Split(strings.TrimSpace(logAfter), "\n"))

	if linesAfter >= linesBefore {
		t.Errorf("squash didn't reduce commits: before=%d, after=%d", linesBefore, linesAfter)
	}

	// The squash commit message should be what we specified.
	lastMsg, _ := gitCmd(wtPath, "log", "-1", "--format=%s")
	if lastMsg != "squashed feature" {
		t.Errorf("commit message = %q, want %q", lastMsg, "squashed feature")
	}

	// Files should still be present.
	if _, err := os.Stat(filepath.Join(wtPath, "feat.txt")); err != nil {
		t.Error("feat.txt missing after squash")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "feat2.txt")); err != nil {
		t.Error("feat2.txt missing after squash")
	}

	_ = repo // keep linter happy
}

func TestSquashCommits_PreservesContent(t *testing.T) {
	_, wtPath, defBranch := initTestRepoWithBranch(t)

	err := squashCommits(wtPath, defBranch, "feature", MergeOpts{Squash: true, Message: "all in one"}, &WorktreeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify file contents survived.
	data, _ := os.ReadFile(filepath.Join(wtPath, "feat.txt"))
	if string(data) != "feature work" {
		t.Errorf("feat.txt content = %q, want %q", data, "feature work")
	}
}

func TestGenerateCommitMessage_Default(t *testing.T) {
	_, wtPath, defBranch := initTestRepoWithBranch(t)
	wtCfg := &WorktreeConfig{} // no LLM command

	msg := generateCommitMessage(wtPath, defBranch, "feature", wtCfg)
	if msg == "" {
		t.Error("expected non-empty default commit message")
	}
	if !strings.Contains(msg, "feature") {
		t.Errorf("expected commit message to mention branch name, got %q", msg)
	}
}

func TestGenerateCommitMessageLLM_FallbackOnMissingCmd(t *testing.T) {
	repo := initTestRepo(t)

	// Non-existent command should return empty.
	msg := generateCommitMessageLLM(repo, "nonexistent-llm-binary-xyz")
	if msg != "" {
		t.Errorf("expected empty msg for missing binary, got %q", msg)
	}
}

func TestGenerateCommitMessageLLM_WorkingCmd(t *testing.T) {
	repo, wtPath, _ := initTestRepoWithBranch(t)
	_ = repo

	// Use echo as a "fake LLM" that just echoes a message.
	msg := generateCommitMessageLLM(wtPath, "echo 'fix: resolved the issue'")
	if !strings.Contains(msg, "fix: resolved the issue") {
		t.Errorf("expected LLM output, got %q", msg)
	}
}

func TestMergeWorkflow_FFOnly(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Rebase feature onto default (so ff is possible).
	runGit(t, wtPath, "rebase", defBranch)

	// Now merge from the feature worktree's directory.
	// We need to be in the feature worktree for the merge to work.
	origDir, _ := os.Getwd()
	os.Chdir(wtPath)
	defer os.Chdir(origDir)

	// Do the merge manually (the full worktreeMerge needs tmux).
	// Instead, test the git operations that merge uses.

	// Merge into default branch from the main worktree.
	mainWt, _ := findMainWorktree(repo)
	_, err := gitCmd(mainWt, "merge", "--ff-only", "feature")
	if err != nil {
		t.Fatalf("ff merge failed: %v", err)
	}

	// Verify feature files are now on default branch.
	if _, err := os.Stat(filepath.Join(mainWt, "feat.txt")); err != nil {
		t.Error("feat.txt missing on default branch after merge")
	}
	if _, err := os.Stat(filepath.Join(mainWt, "feat2.txt")); err != nil {
		t.Error("feat2.txt missing on default branch after merge")
	}
}

func TestMergeWorkflow_SquashAndMerge(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Squash feature commits.
	err := squashCommits(wtPath, defBranch, "feature", MergeOpts{Squash: true, Message: "feat: all in one"}, &WorktreeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Rebase onto default.
	if _, err := gitCmd(wtPath, "rebase", defBranch); err != nil {
		t.Fatalf("rebase failed: %v", err)
	}

	// Merge into default.
	mainWt, _ := findMainWorktree(repo)
	if _, err := gitCmd(mainWt, "merge", "--ff-only", "feature"); err != nil {
		t.Fatalf("ff merge after squash failed: %v", err)
	}

	// Should be exactly one new commit on default (the squashed one).
	log, _ := gitCmd(mainWt, "log", "--oneline")
	lines := strings.Split(strings.TrimSpace(log), "\n")
	// init + base.txt + squashed = 3 commits
	if len(lines) != 3 {
		t.Errorf("expected 3 commits after squash merge, got %d:\n%s", len(lines), log)
	}

	// Verify squash commit message.
	lastMsg, _ := gitCmd(mainWt, "log", "-1", "--format=%s")
	if lastMsg != "feat: all in one" {
		t.Errorf("commit message = %q, want %q", lastMsg, "feat: all in one")
	}
}

func TestCleanupTmuxWindow_NoTmux(t *testing.T) {
	// Should not panic outside tmux.
	old := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer os.Setenv("TMUX", old)

	cleanupTmuxWindow("/nonexistent/path")
	// No panic = pass.
}

// === Additional merge edge-case tests ===

// --- §8: Squash edge cases ---

func TestSquashCommits_SingleCommit(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := gitCmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Create feature with just one commit.
	wtPath := filepath.Join(t.TempDir(), "single")
	CreateWorktree(repo, wtPath, "single-commit", CreateWorktreeOpts{NewBranch: true})
	os.WriteFile(filepath.Join(wtPath, "only.txt"), []byte("only"), 0o644)
	runGit(t, wtPath, "add", "only.txt")
	runGit(t, wtPath, "commit", "-m", "single commit")

	err := squashCommits(wtPath, defBranch, "single-commit", MergeOpts{Squash: true, Message: "squashed single"}, &WorktreeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	lastMsg, _ := gitCmd(wtPath, "log", "-1", "--format=%s")
	if lastMsg != "squashed single" {
		t.Errorf("commit message = %q, want %q", lastMsg, "squashed single")
	}
	if _, err := os.Stat(filepath.Join(wtPath, "only.txt")); err != nil {
		t.Error("only.txt missing after single-commit squash")
	}
}

func TestSquashCommits_WithLLMMessage(t *testing.T) {
	_, wtPath, defBranch := initTestRepoWithBranch(t)

	// Use echo as fake LLM.
	wtCfg := &WorktreeConfig{CommitCmd: "echo 'feat: LLM generated message'"}
	err := squashCommits(wtPath, defBranch, "feature", MergeOpts{Squash: true}, wtCfg)
	if err != nil {
		t.Fatal(err)
	}

	lastMsg, _ := gitCmd(wtPath, "log", "-1", "--format=%s")
	if !strings.Contains(lastMsg, "LLM generated message") {
		t.Errorf("expected LLM message, got %q", lastMsg)
	}
}

// --- §9: Commit message edge cases ---

func TestGenerateCommitMessageLLM_EmptyOutput(t *testing.T) {
	_, wtPath, _ := initTestRepoWithBranch(t)

	// Command that outputs nothing.
	msg := generateCommitMessageLLM(wtPath, "true") // true outputs nothing
	if msg != "" {
		t.Errorf("expected empty msg for silent command, got %q", msg)
	}
}

func TestGenerateCommitMessageLLM_FailingCmd(t *testing.T) {
	_, wtPath, _ := initTestRepoWithBranch(t)

	msg := generateCommitMessageLLM(wtPath, "exit 1")
	if msg != "" {
		t.Errorf("expected empty msg for failing command, got %q", msg)
	}
}

func TestGenerateCommitMessage_DefaultFormat(t *testing.T) {
	_, wtPath, defBranch := initTestRepoWithBranch(t)
	msg := generateCommitMessage(wtPath, defBranch, "my-branch", &WorktreeConfig{})
	if !strings.Contains(msg, "my-branch") {
		t.Errorf("default message should contain branch name, got %q", msg)
	}
	if !strings.Contains(msg, "Merge branch") {
		t.Errorf("default message should contain 'Merge branch', got %q", msg)
	}
}

// --- §10: Merge workflow edge cases (git operations, not full worktreeMerge which needs tmux) ---

func TestMergeWorkflow_NoFFFlag(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Rebase first so FF is possible.
	runGit(t, wtPath, "rebase", defBranch)

	mainWt, _ := findMainWorktree(repo)
	// Use --no-ff to force a merge commit.
	_, err := gitCmd(mainWt, "merge", "--no-ff", "-m", "merge commit", "feature")
	if err != nil {
		t.Fatalf("--no-ff merge failed: %v", err)
	}

	// Should have a merge commit (has 2 parents).
	parents, _ := gitCmd(mainWt, "log", "-1", "--format=%P")
	parentList := strings.Fields(parents)
	if len(parentList) < 2 {
		t.Error("expected merge commit with 2 parents for --no-ff")
	}
}

func TestMergeWorkflow_RebaseConflict(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Create a conflicting commit on default branch.
	mainWt, _ := findMainWorktree(repo)
	os.WriteFile(filepath.Join(mainWt, "feat.txt"), []byte("conflicting content"), 0o644)
	runGit(t, mainWt, "add", "feat.txt")
	runGit(t, mainWt, "commit", "-m", "conflict on main")

	// Rebase should fail due to conflict.
	_, err := gitCmd(wtPath, "rebase", defBranch)
	if err == nil {
		t.Error("expected rebase to fail with conflict")
	}

	// Clean up the rebase.
	gitCmd(wtPath, "rebase", "--abort")
}

func TestMergeWorkflow_KeepWorktree(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Rebase and merge manually.
	runGit(t, wtPath, "rebase", defBranch)
	mainWt, _ := findMainWorktree(repo)
	gitCmd(mainWt, "merge", "--ff-only", "feature")

	// Verify worktree still exists (simulating --keep behavior).
	wts, _ := listWorktrees(repo)
	found := false
	for _, wt := range wts {
		if wt.Branch == "feature" {
			found = true
		}
	}
	if !found {
		t.Error("worktree should still exist (--keep)")
	}
}

func TestMergeWorkflow_FullLifecycle(t *testing.T) {
	repo, wtPath, defBranch := initTestRepoWithBranch(t)

	// Squash.
	err := squashCommits(wtPath, defBranch, "feature", MergeOpts{Squash: true, Message: "feat: everything"}, &WorktreeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Rebase.
	if _, err := gitCmd(wtPath, "rebase", defBranch); err != nil {
		t.Fatalf("rebase failed: %v", err)
	}

	// Merge into default.
	mainWt, _ := findMainWorktree(repo)
	if _, err := gitCmd(mainWt, "merge", "--ff-only", "feature"); err != nil {
		t.Fatalf("ff merge failed: %v", err)
	}

	// Remove worktree.
	err = RemoveWorktree(repo, wtPath, true)
	if err != nil {
		t.Fatalf("worktree remove failed: %v", err)
	}

	// Delete branch.
	err = DeleteBranch(repo, "feature", false)
	if err != nil {
		t.Fatalf("branch delete failed: %v", err)
	}

	// Verify: feature files on default, worktree gone, branch gone.
	if _, err := os.Stat(filepath.Join(mainWt, "feat.txt")); err != nil {
		t.Error("feat.txt missing on default after merge")
	}

	wts, _ := listWorktrees(repo)
	if len(wts) != 1 {
		t.Errorf("expected 1 worktree after cleanup, got %d", len(wts))
	}

	_, err = gitCmd(repo, "show-ref", "--verify", "--quiet", "refs/heads/feature")
	if err == nil {
		t.Error("feature branch should be deleted")
	}
}

// --- §12: Tmux no-op tests ---

func TestSwitchToTmuxWindow_NoTmux(t *testing.T) {
	old := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer os.Setenv("TMUX", old)

	// Should not panic outside tmux.
	switchToTmuxWindow("some-window")
}

func TestWorktreeOpenTmuxWindow_NoTmux(t *testing.T) {
	old := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer os.Setenv("TMUX", old)

	// Should not panic outside tmux.
	worktreeOpenTmuxWindow("feature", "/tmp/worktree")
}
