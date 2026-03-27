package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/proc"
)

// --- Project config (.cms.toml) tests ---

func TestLoadProjectConfig_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg := LoadProjectConfig(dir)
	if cfg.Worktree.BaseDir != "" {
		t.Errorf("expected empty base_dir, got %q", cfg.Worktree.BaseDir)
	}
	if len(cfg.Worktree.Hooks) != 0 {
		t.Errorf("expected no hooks, got %d", len(cfg.Worktree.Hooks))
	}
}

func TestLoadProjectConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".cms.toml"), []byte(`
[worktree]
base_dir = "../wt"
commit_cmd = "llm -m haiku"

[[worktree.hooks]]
command = "cp $CMS_REPO_ROOT/.env .env"

[[worktree.hooks]]
command = "npm install"

[[worktree.pre_remove]]
command = "cleanup.sh"
`), 0o644)

	cfg := LoadProjectConfig(dir)
	if cfg.Worktree.BaseDir != "../wt" {
		t.Errorf("base_dir = %q, want %q", cfg.Worktree.BaseDir, "../wt")
	}
	if cfg.Worktree.CommitCmd != "llm -m haiku" {
		t.Errorf("commit_cmd = %q, want %q", cfg.Worktree.CommitCmd, "llm -m haiku")
	}
	if len(cfg.Worktree.Hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(cfg.Worktree.Hooks))
	}
	if cfg.Worktree.Hooks[1].Command != "npm install" {
		t.Errorf("hooks[1].Command = %q", cfg.Worktree.Hooks[1].Command)
	}
	if len(cfg.Worktree.PreRemove) != 1 {
		t.Fatalf("got %d pre_remove hooks, want 1", len(cfg.Worktree.PreRemove))
	}
}

func TestLoadProjectConfig_HookEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".cms.toml"), []byte(`
[[worktree.hooks]]
command = "cp \"$ENV_FILE\" .env"
[worktree.hooks.env]
ENV_FILE = "/secrets/.env"
EXTRA = "value"
`), 0o644)

	cfg := LoadProjectConfig(dir)
	if len(cfg.Worktree.Hooks) != 1 {
		t.Fatalf("got %d hooks, want 1", len(cfg.Worktree.Hooks))
	}
	h := cfg.Worktree.Hooks[0]
	if h.Env == nil {
		t.Fatal("hook Env is nil")
	}
	if h.Env["ENV_FILE"] != "/secrets/.env" {
		t.Errorf("ENV_FILE = %q, want %q", h.Env["ENV_FILE"], "/secrets/.env")
	}
	if h.Env["EXTRA"] != "value" {
		t.Errorf("EXTRA = %q, want %q", h.Env["EXTRA"], "value")
	}
}

func TestLoadProjectConfig_MultipleHooksWithEnv(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".cms.toml"), []byte(`
[[worktree.hooks]]
command = "cp \"$SRC\" .env"
[worktree.hooks.env]
SRC = "/secrets/.env"

[[worktree.hooks]]
command = "npm install"
`), 0o644)

	cfg := LoadProjectConfig(dir)
	if len(cfg.Worktree.Hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(cfg.Worktree.Hooks))
	}
	// First hook should have env.
	if cfg.Worktree.Hooks[0].Env["SRC"] != "/secrets/.env" {
		t.Errorf("hooks[0].Env[SRC] = %q, want %q", cfg.Worktree.Hooks[0].Env["SRC"], "/secrets/.env")
	}
	// Second hook should have no env.
	if len(cfg.Worktree.Hooks[1].Env) != 0 {
		t.Errorf("hooks[1].Env should be empty, got %v", cfg.Worktree.Hooks[1].Env)
	}
}

func TestResolveWorktreeConfig_ProjectOverridesUser(t *testing.T) {
	dir := t.TempDir()
	// Write a .cms.toml with project-specific hooks.
	os.WriteFile(filepath.Join(dir, ".cms.toml"), []byte(`
[worktree]
base_dir = "../project-wt"
commit_cmd = "project-llm"

[[worktree.hooks]]
command = "project-hook"
`), 0o644)

	userCfg := &config.WorktreeConfig{
		BaseDir:   "../user-wt",
		CommitCmd: "user-llm",
		Hooks:     []config.WorktreeHook{{Command: "user-hook"}},
	}

	merged := ResolveWorktreeConfig(dir, dir, userCfg)
	if merged.BaseDir != "../project-wt" {
		t.Errorf("base_dir should be project value, got %q", merged.BaseDir)
	}
	if merged.CommitCmd != "project-llm" {
		t.Errorf("commit_cmd should be project value, got %q", merged.CommitCmd)
	}
	if len(merged.Hooks) != 1 || merged.Hooks[0].Command != "project-hook" {
		t.Errorf("hooks should be project hooks, got %v", merged.Hooks)
	}
}

func TestResolveWorktreeConfig_UserFallback(t *testing.T) {
	dir := t.TempDir() // no .cms.toml

	userCfg := &config.WorktreeConfig{
		BaseDir:   "../user-wt",
		CommitCmd: "user-llm",
		Hooks:     []config.WorktreeHook{{Command: "user-hook"}},
	}

	merged := ResolveWorktreeConfig(dir, dir, userCfg)
	if merged.BaseDir != "../user-wt" {
		t.Errorf("base_dir should fall back to user, got %q", merged.BaseDir)
	}
	if merged.CommitCmd != "user-llm" {
		t.Errorf("commit_cmd should fall back to user, got %q", merged.CommitCmd)
	}
	if len(merged.Hooks) != 1 || merged.Hooks[0].Command != "user-hook" {
		t.Errorf("hooks should fall back to user, got %v", merged.Hooks)
	}
}

func TestResolveWorktreeConfig_PartialOverride(t *testing.T) {
	dir := t.TempDir()
	// Project only sets hooks, not base_dir.
	os.WriteFile(filepath.Join(dir, ".cms.toml"), []byte(`
[[worktree.hooks]]
command = "project-hook"
`), 0o644)

	userCfg := &config.WorktreeConfig{
		BaseDir: "../user-wt",
		Hooks:   []config.WorktreeHook{{Command: "user-hook"}},
	}

	merged := ResolveWorktreeConfig(dir, dir, userCfg)
	if merged.BaseDir != "../user-wt" {
		t.Errorf("base_dir should stay as user value, got %q", merged.BaseDir)
	}
	if len(merged.Hooks) != 1 || merged.Hooks[0].Command != "project-hook" {
		t.Errorf("hooks should be overridden by project, got %v", merged.Hooks)
	}
}

func TestResolveWorktreeBaseDir(t *testing.T) {
	tests := []struct {
		name string
		root string
		cfg  *config.WorktreeConfig
		want string
	}{
		{"nil config uses default", "/repo", nil, "/worktrees"},
		{"relative base_dir", "/repo", &config.WorktreeConfig{BaseDir: "../wt"}, "/wt"},
		{"absolute base_dir", "/repo", &config.WorktreeConfig{BaseDir: "/tmp/wt"}, "/tmp/wt"},
		{"empty base_dir uses default", "/repo", &config.WorktreeConfig{}, "/worktrees"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWorktreeBaseDir(tt.root, tt.cfg)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeBranch(t *testing.T) {
	tests := []struct{ in, want string }{
		{"feature/auth", "feature-auth"},
		{"bug\\fix", "bug-fix"},
		{"simple", "simple"},
		{"a/b/c", "a-b-c"},
		{"//leading", "leading"},
		{"trailing//", "trailing"},
		{"no-change", "no-change"},
	}
	for _, tt := range tests {
		got := SanitizeBranch(tt.in)
		if got != tt.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRunPostCreateHooks_Command(t *testing.T) {
	tmp := t.TempDir()
	newwt := filepath.Join(tmp, "new")
	os.MkdirAll(newwt, 0o755)

	hooks := []config.WorktreeHook{{Command: "echo done > marker.txt"}}
	if err := RunPostCreateHooks(tmp, newwt, hooks); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(newwt, "marker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "done") {
		t.Errorf("got %q, want 'done'", data)
	}
}

func TestRunHooks_PreRemoveCommand(t *testing.T) {
	tmp := t.TempDir()
	targetWt := filepath.Join(tmp, "target")
	os.MkdirAll(targetWt, 0o755)

	hooks := []config.WorktreeHook{{Command: "echo cleanup > done.txt"}}
	if err := RunHooks("pre-remove", tmp, targetWt, hooks); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(targetWt, "done.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cleanup") {
		t.Errorf("got %q, want 'cleanup'", data)
	}
}

func TestRunHooks_FailingCommand(t *testing.T) {
	hooks := []config.WorktreeHook{{Command: "exit 1"}}
	err := RunHooks("pre-remove", "/tmp", "/tmp", hooks)
	if err == nil || !strings.Contains(err.Error(), "pre-remove") {
		t.Errorf("expected error with 'pre-remove' label, got %v", err)
	}
}

// Integration tests that use real git repos.

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s (%v)", args, out, err)
		}
	}
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s (%v)", args, out, err)
	}
}

func TestListWorktrees_SingleRepo(t *testing.T) {
	repo := initTestRepo(t)
	wts, err := git.ListWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 {
		t.Fatalf("got %d worktrees, want 1", len(wts))
	}
	if !wts[0].IsMain {
		t.Error("expected first worktree to be main")
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "feature")

	err := CreateWorktree(repo, wtPath, "feature", CreateWorktreeOpts{NewBranch: true})
	if err != nil {
		t.Fatal(err)
	}

	wts, _ := git.ListWorktrees(repo)
	if len(wts) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(wts))
	}

	found := false
	for _, wt := range wts {
		if wt.Branch == "feature" {
			found = true
		}
	}
	if !found {
		t.Error("feature worktree not found in list")
	}

	// Remove it.
	err = RemoveWorktree(repo, wtPath, false)
	if err != nil {
		t.Fatal(err)
	}

	wts, _ = git.ListWorktrees(repo)
	if len(wts) != 1 {
		t.Fatalf("got %d worktrees after remove, want 1", len(wts))
	}
}

func TestResolveBranch_Local(t *testing.T) {
	repo := initTestRepo(t)
	// Default branch should be local.
	branch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")
	local, remote, err := ResolveBranch(repo, branch)
	if err != nil {
		t.Fatal(err)
	}
	if !local {
		t.Error("expected local=true")
	}
	if remote != "" {
		t.Errorf("expected empty remote, got %q", remote)
	}
}

func TestResolveBranch_NotFound(t *testing.T) {
	repo := initTestRepo(t)
	_, _, err := ResolveBranch(repo, "nonexistent-branch-xyz")
	if err == nil {
		t.Error("expected error for nonexistent branch")
	}
}

func TestDeleteBranch(t *testing.T) {
	repo := initTestRepo(t)

	// Create a branch.
	git.Cmd(repo, "branch", "to-delete")

	err := DeleteBranch(repo, "to-delete", false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's gone.
	_, err = git.Cmd(repo, "show-ref", "--verify", "--quiet", "refs/heads/to-delete")
	if err == nil {
		t.Error("branch should have been deleted")
	}
}

func TestFindRepoRoot(t *testing.T) {
	repo := initTestRepo(t)
	sub := filepath.Join(repo, "sub", "dir")
	os.MkdirAll(sub, 0o755)

	root, err := FindRepoRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks (macOS /var → /private/var).
	wantReal, _ := filepath.EvalSymlinks(repo)
	gotReal, _ := filepath.EvalSymlinks(root)
	if gotReal != wantReal {
		t.Errorf("got %q, want %q", gotReal, wantReal)
	}
}

func TestFindRepoRoot_NotRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := FindRepoRoot(dir)
	if err == nil {
		t.Error("expected error for non-repo dir")
	}
}

func TestFindRepoRoot_FromLinkedWorktree(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "linked")
	CreateWorktree(repo, wtPath, "linked", CreateWorktreeOpts{NewBranch: true})
	defer RemoveWorktree(repo, wtPath, true)

	// FindRepoRoot from inside the linked worktree should return the main repo root.
	root, err := FindRepoRoot(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	repoReal, _ := filepath.EvalSymlinks(repo)
	rootReal, _ := filepath.EvalSymlinks(root)
	if rootReal != repoReal {
		t.Errorf("from linked worktree: got %q, want main repo %q", rootReal, repoReal)
	}
}

func TestFindRepoRoot_BareRepo(t *testing.T) {
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	root, err := FindRepoRoot(bare)
	if err != nil {
		t.Fatal(err)
	}
	bareReal, _ := filepath.EvalSymlinks(bare)
	rootReal, _ := filepath.EvalSymlinks(root)
	if rootReal != bareReal {
		t.Errorf("bare repo: got %q, want %q", rootReal, bareReal)
	}
}

func TestResolveWorktreeSymbol_At(t *testing.T) {
	repo := initTestRepo(t)
	branch, err := ResolveWorktreeSymbol(repo, "@")
	if err != nil {
		t.Fatal(err)
	}
	// Should return the current branch (whatever init created).
	if branch == "" || branch == "@" {
		t.Errorf("expected resolved branch, got %q", branch)
	}
}

func TestResolveWorktreeSymbol_Caret(t *testing.T) {
	repo := initTestRepo(t)
	// Create a "main" branch so ^ resolves.
	currentBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	branch, err := ResolveWorktreeSymbol(repo, "^")
	if err != nil {
		t.Fatal(err)
	}
	if branch != currentBranch {
		// The default branch is whatever was created by init.
		t.Logf("default branch resolved to %q (current is %q)", branch, currentBranch)
	}
}

func TestResolveWorktreeSymbol_Passthrough(t *testing.T) {
	repo := initTestRepo(t)
	branch, err := ResolveWorktreeSymbol(repo, "my-feature")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "my-feature" {
		t.Errorf("expected passthrough, got %q", branch)
	}
}

func TestDefaultBranch(t *testing.T) {
	repo := initTestRepo(t)
	branch, err := DefaultBranch(repo)
	if err != nil {
		t.Fatal(err)
	}
	// git init typically creates "main" or "master".
	if branch != "main" && branch != "master" {
		t.Errorf("unexpected default branch %q", branch)
	}
}

func TestIsBranchIntegrated(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Create a branch at the same commit — should be "integrated".
	git.Cmd(repo, "branch", "same-commit")
	integrated, reason := IsBranchIntegrated(repo, "same-commit", defBranch)
	if !integrated {
		t.Error("same-commit branch should be integrated")
	}
	if reason == "" {
		t.Error("expected a reason")
	}
}

func TestIsBranchIntegrated_Diverged(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Create a branch with a new commit — should NOT be integrated.
	git.Cmd(repo, "checkout", "-b", "diverged")
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "diverge")
	cmd.Run()
	git.Cmd(repo, "checkout", defBranch)

	integrated, _ := IsBranchIntegrated(repo, "diverged", defBranch)
	if integrated {
		t.Error("diverged branch should not be integrated")
	}
}

func TestHasAgentProcess(t *testing.T) {
	pt := proc.Table{
		Procs: map[int]proc.Entry{
			100: {PID: 100, PPID: 1, Comm: "fish", Args: "fish"},
			101: {PID: 101, PPID: 100, Comm: "node", Args: "node /usr/local/bin/claude"},
		},
		Children: map[int][]int{
			100: {101},
		},
	}

	if !HasAgentProcess(pt, 100) {
		t.Error("should detect claude agent in args")
	}

	pt2 := proc.Table{
		Procs: map[int]proc.Entry{
			200: {PID: 200, PPID: 1, Comm: "fish", Args: "fish"},
			201: {PID: 201, PPID: 200, Comm: "vim", Args: "vim file.go"},
		},
		Children: map[int][]int{
			200: {201},
		},
	}
	if HasAgentProcess(pt2, 200) {
		t.Error("should not detect agent for vim")
	}
}

// === Additional edge-case tests ===

// --- §1: sanitizeBranch edge cases ---

func TestSanitizeBranch_EdgeCases(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"feature/auth/v2", "feature-auth-v2"},
		{"feature//double", "feature-double"},
		{"refs/heads/main", "refs-heads-main"},
		{"feature/日本語", "feature-日本語"}, // unicode survives
		{"---", ""},                          // all dashes collapse and trim
	}
	for _, tt := range tests {
		got := SanitizeBranch(tt.in)
		if got != tt.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- §2: resolveWorktreeSymbol edge cases ---

func TestResolveWorktreeSymbol_Dash(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Checkout a new branch, then switch back, so reflog has a previous entry.
	runGit(t, repo, "checkout", "-b", "temp-branch")
	runGit(t, repo, "checkout", defBranch)

	branch, err := ResolveWorktreeSymbol(repo, "-")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "temp-branch" {
		t.Errorf("expected 'temp-branch', got %q", branch)
	}
}

func TestResolveWorktreeSymbol_DashNoPrevious(t *testing.T) {
	// Fresh repo with no checkout history → "-" should error.
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init")
	cmd.Run()
	exec.Command("git", "-C", dir, "config", "user.email", "t@t.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "T").Run()
	exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run()

	_, err := ResolveWorktreeSymbol(dir, "-")
	if err == nil {
		t.Error("expected error for '-' with no previous branch")
	}
}

func TestResolveWorktreeSymbol_AtDetachedHead(t *testing.T) {
	repo := initTestRepo(t)
	sha, _ := git.Cmd(repo, "rev-parse", "HEAD")
	runGit(t, repo, "checkout", "--detach", sha)

	branch, err := ResolveWorktreeSymbol(repo, "@")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "HEAD" {
		t.Errorf("expected 'HEAD' for detached, got %q", branch)
	}
}

func TestResolveWorktreeSymbol_CaretNoDefault(t *testing.T) {
	// Repo where the branch is neither "main" nor "master" and no remote.
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-b", "develop").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "t@t.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "T").Run()
	exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run()

	_, err := ResolveWorktreeSymbol(dir, "^")
	if err == nil {
		t.Error("expected error for '^' when no main/master exists")
	}
}

// --- §3: isBranchIntegrated edge cases ---

func TestIsBranchIntegrated_Ancestor(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Create feature branch, go back to default, add commit on default.
	// Feature is an ancestor of default.
	runGit(t, repo, "checkout", "-b", "old-feature")
	runGit(t, repo, "commit", "--allow-empty", "-m", "feature commit")
	runGit(t, repo, "checkout", defBranch)
	runGit(t, repo, "merge", "old-feature")
	runGit(t, repo, "commit", "--allow-empty", "-m", "after merge")

	integrated, reason := IsBranchIntegrated(repo, "old-feature", defBranch)
	if !integrated {
		t.Error("ancestor branch should be integrated")
	}
	if !strings.Contains(reason, "ancestor") {
		t.Errorf("expected 'ancestor' in reason, got %q", reason)
	}
}

func TestIsBranchIntegrated_CherryPicked(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Create a feature branch with one commit.
	runGit(t, repo, "checkout", "-b", "cherry-feature")
	os.WriteFile(filepath.Join(repo, "cherry.txt"), []byte("cherry"), 0o644)
	runGit(t, repo, "add", "cherry.txt")
	runGit(t, repo, "commit", "-m", "cherry commit")
	featureSHA, _ := git.Cmd(repo, "rev-parse", "HEAD")

	// Cherry-pick onto default.
	runGit(t, repo, "checkout", defBranch)
	runGit(t, repo, "cherry-pick", featureSHA)

	// git cherry should show the commit as already applied.
	integrated, _ := IsBranchIntegrated(repo, "cherry-feature", defBranch)
	if !integrated {
		t.Error("cherry-picked branch should be integrated")
	}
}

func TestIsBranchIntegrated_PartialCherryPick(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	// Feature with two commits, only cherry-pick the first.
	runGit(t, repo, "checkout", "-b", "partial-feature")
	os.WriteFile(filepath.Join(repo, "one.txt"), []byte("one"), 0o644)
	runGit(t, repo, "add", "one.txt")
	runGit(t, repo, "commit", "-m", "first commit")
	firstSHA, _ := git.Cmd(repo, "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(repo, "two.txt"), []byte("two"), 0o644)
	runGit(t, repo, "add", "two.txt")
	runGit(t, repo, "commit", "-m", "second commit")

	runGit(t, repo, "checkout", defBranch)
	runGit(t, repo, "cherry-pick", firstSHA)

	// Second commit not cherry-picked → NOT integrated.
	integrated, _ := IsBranchIntegrated(repo, "partial-feature", defBranch)
	if integrated {
		t.Error("partially cherry-picked branch should NOT be integrated")
	}
}

// --- §4: CreateWorktree edge cases ---

func TestCreateWorktree_PathAlreadyExists(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "existing")
	os.MkdirAll(wtPath, 0o755)
	os.WriteFile(filepath.Join(wtPath, "file.txt"), []byte("occupied"), 0o644)

	err := CreateWorktree(repo, wtPath, "feature", CreateWorktreeOpts{NewBranch: true})
	// Git should error because the path exists and has files.
	if err == nil {
		t.Error("expected error when path already exists with files")
	}
}

func TestCreateWorktree_BranchAlreadyCheckedOut(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	wtPath := filepath.Join(t.TempDir(), "conflict")
	// Try to create a worktree for a branch that's already checked out.
	err := CreateWorktree(repo, wtPath, defBranch, CreateWorktreeOpts{})
	if err == nil {
		t.Error("expected error when branch is already checked out")
	}
}

func TestCreateWorktree_PathWithSpaces(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "path with spaces")

	err := CreateWorktree(repo, wtPath, "spaced", CreateWorktreeOpts{NewBranch: true})
	if err != nil {
		t.Fatalf("worktree creation with spaces in path failed: %v", err)
	}

	// Verify it works.
	wts, _ := git.ListWorktrees(repo)
	found := false
	for _, wt := range wts {
		if wt.Branch == "spaced" {
			found = true
		}
	}
	if !found {
		t.Error("worktree with spaces in path not found")
	}
	RemoveWorktree(repo, wtPath, true)
}

func TestCreateWorktree_ForceOverridesCheckedOut(t *testing.T) {
	repo := initTestRepo(t)
	defBranch, _ := git.Cmd(repo, "rev-parse", "--abbrev-ref", "HEAD")

	wtPath := filepath.Join(t.TempDir(), "forced")
	err := CreateWorktree(repo, wtPath, defBranch, CreateWorktreeOpts{Force: true})
	if err != nil {
		t.Errorf("expected force to override 'already checked out': %v", err)
	}
	if err == nil {
		RemoveWorktree(repo, wtPath, true)
	}
}

// --- §4: ResolveBranch with remote ---

func initTestRepoWithRemote(t *testing.T) (string, string) {
	t.Helper()
	// Create bare "remote".
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	// Clone it.
	clone := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", bare, clone)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %s (%v)", out, err)
	}
	runGit(t, clone, "config", "user.email", "test@test.com")
	runGit(t, clone, "config", "user.name", "Test")
	runGit(t, clone, "commit", "--allow-empty", "-m", "init")
	runGit(t, clone, "push", "-u", "origin", "HEAD")

	return clone, bare
}

func TestResolveBranch_Remote(t *testing.T) {
	clone, _ := initTestRepoWithRemote(t)

	// Get the actual default branch name from the clone.
	defBranch, _ := git.Cmd(clone, "rev-parse", "--abbrev-ref", "HEAD")

	// Create a branch on remote only.
	runGit(t, clone, "checkout", "-b", "remote-only")
	runGit(t, clone, "commit", "--allow-empty", "-m", "remote commit")
	runGit(t, clone, "push", "origin", "remote-only")
	// Delete the local branch, keep only remote.
	runGit(t, clone, "checkout", defBranch)
	runGit(t, clone, "branch", "-D", "remote-only")

	local, remote, err := ResolveBranch(clone, "remote-only")
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Error("should not be local")
	}
	if !strings.Contains(remote, "remote-only") {
		t.Errorf("expected remote ref containing 'remote-only', got %q", remote)
	}
}

// --- §5: Removal edge cases ---

func TestRemoveWorktree_Dirty(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "dirty")

	CreateWorktree(repo, wtPath, "dirty-branch", CreateWorktreeOpts{NewBranch: true})
	// Make the worktree dirty.
	os.WriteFile(filepath.Join(wtPath, "untracked.txt"), []byte("dirty"), 0o644)
	runGit(t, wtPath, "add", "untracked.txt")

	// Without force → should error.
	err := RemoveWorktree(repo, wtPath, false)
	if err == nil {
		t.Error("expected error removing dirty worktree without force")
	}

	// With force → should succeed.
	err = RemoveWorktree(repo, wtPath, true)
	if err != nil {
		t.Errorf("expected force remove to succeed: %v", err)
	}
}

// --- §6: hasAgentProcess edge cases ---

func TestHasAgentProcess_ClaudeViaComm(t *testing.T) {
	pt := proc.Table{
		Procs: map[int]proc.Entry{
			1: {PID: 1, PPID: 0, Comm: "bash", Args: "bash"},
			2: {PID: 2, PPID: 1, Comm: "claude", Args: "claude"},
		},
		Children: map[int][]int{1: {2}},
	}
	if !HasAgentProcess(pt, 1) {
		t.Error("should detect 'claude' via comm")
	}
}

func TestHasAgentProcess_CodexViaComm(t *testing.T) {
	pt := proc.Table{
		Procs: map[int]proc.Entry{
			1: {PID: 1, PPID: 0, Comm: "zsh", Args: "zsh"},
			2: {PID: 2, PPID: 1, Comm: "codex", Args: "codex --model o4-mini"},
		},
		Children: map[int][]int{1: {2}},
	}
	if !HasAgentProcess(pt, 1) {
		t.Error("should detect 'codex' via comm")
	}
}

func TestHasAgentProcess_NoChildren(t *testing.T) {
	pt := proc.Table{
		Procs:    map[int]proc.Entry{1: {PID: 1, PPID: 0, Comm: "fish", Args: "fish"}},
		Children: map[int][]int{},
	}
	if HasAgentProcess(pt, 1) {
		t.Error("shell with no children should not be detected as agent")
	}
}

func TestHasAgentProcess_EmptyProcTable(t *testing.T) {
	pt := proc.Table{Procs: map[int]proc.Entry{}, Children: map[int][]int{}}
	if HasAgentProcess(pt, 999) {
		t.Error("empty proc table should return false")
	}
}

// --- §7: Hook edge cases ---

func TestRunHooks_CommandEnvVars(t *testing.T) {
	tmp := t.TempDir()
	targetWt := filepath.Join(tmp, "target")
	os.MkdirAll(targetWt, 0o755)

	hooks := []config.WorktreeHook{{
		Command: "echo $CMS_WORKTREE_PATH > env.txt && echo $CMS_REPO_ROOT >> env.txt",
	}}
	if err := RunHooks("test", tmp, targetWt, hooks); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(targetWt, "env.txt"))
	lines := strings.TrimSpace(string(data))
	if !strings.Contains(lines, targetWt) {
		t.Errorf("CMS_WORKTREE_PATH not found in output: %s", lines)
	}
	if !strings.Contains(lines, tmp) {
		t.Errorf("CMS_REPO_ROOT not found in output: %s", lines)
	}
}

func TestRunHooks_CustomEnv(t *testing.T) {
	tmp := t.TempDir()
	targetWt := filepath.Join(tmp, "target")
	os.MkdirAll(targetWt, 0o755)

	hooks := []config.WorktreeHook{{
		Command: "echo $MY_VAR > custom.txt",
		Env:     map[string]string{"MY_VAR": "hello-from-env"},
	}}
	if err := RunHooks("test", tmp, targetWt, hooks); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(targetWt, "custom.txt"))
	got := strings.TrimSpace(string(data))
	if got != "hello-from-env" {
		t.Errorf("expected 'hello-from-env', got %q", got)
	}
}

func TestRunHooks_CommandFailure(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(tmp, 0o755)

	hooks := []config.WorktreeHook{{Command: "exit 1"}}
	err := RunHooks("test", tmp, tmp, hooks)
	if err == nil {
		t.Error("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("error should mention the command, got: %v", err)
	}
}

func TestRunHooks_PartialFailure(t *testing.T) {
	tmp := t.TempDir()
	targetWt := filepath.Join(tmp, "target")
	os.MkdirAll(targetWt, 0o755)

	hooks := []config.WorktreeHook{
		{Command: "echo first > first.txt"},
		{Command: "exit 1"},
		{Command: "echo third > third.txt"},
	}
	err := RunHooks("test", tmp, targetWt, hooks)
	if err == nil {
		t.Fatal("expected error on second hook")
	}

	// First hook should have run.
	if _, err := os.Stat(filepath.Join(targetWt, "first.txt")); err != nil {
		t.Error("first hook should have run before failure")
	}
	// Third hook should NOT have run.
	if _, err := os.Stat(filepath.Join(targetWt, "third.txt")); err == nil {
		t.Error("third hook should not run after failure")
	}
}

// --- §13: Config loading (.cms.toml) ---
// (See TestLoadProjectConfig_*, TestResolveWorktreeConfig_* above)

// --- §14: findMainWorktree ---

func TestFindMainWorktree_Single(t *testing.T) {
	repo := initTestRepo(t)
	mainWt, err := FindMainWorktree(repo)
	if err != nil {
		t.Fatal(err)
	}
	repoReal, _ := filepath.EvalSymlinks(repo)
	mainReal, _ := filepath.EvalSymlinks(mainWt)
	if mainReal != repoReal {
		t.Errorf("got %q, want %q", mainReal, repoReal)
	}
}

func TestFindMainWorktree_Multiple(t *testing.T) {
	repo := initTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "linked")
	CreateWorktree(repo, wtPath, "linked", CreateWorktreeOpts{NewBranch: true})
	defer RemoveWorktree(repo, wtPath, true)

	mainWt, err := FindMainWorktree(repo)
	if err != nil {
		t.Fatal(err)
	}
	repoReal, _ := filepath.EvalSymlinks(repo)
	mainReal, _ := filepath.EvalSymlinks(mainWt)
	if mainReal != repoReal {
		t.Errorf("got %q, want %q (should be main, not linked)", mainReal, repoReal)
	}
}

// --- §15: defaultBranch edge cases ---

func TestDefaultBranch_Master(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-b", "master").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "t@t.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "T").Run()
	exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run()

	branch, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" {
		t.Errorf("expected 'master', got %q", branch)
	}
}

func TestDefaultBranch_NeitherMainNorMaster(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-b", "develop").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "t@t.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "T").Run()
	exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "init").Run()

	_, err := DefaultBranch(dir)
	if err == nil {
		t.Error("expected error when no main/master exists")
	}
}

func TestDefaultBranch_WithRemoteHead(t *testing.T) {
	clone, _ := initTestRepoWithRemote(t)

	branch, err := DefaultBranch(clone)
	if err != nil {
		t.Fatal(err)
	}
	// The remote HEAD should resolve to whatever branch was pushed.
	if branch == "" {
		t.Error("expected non-empty branch from remote HEAD")
	}
}
