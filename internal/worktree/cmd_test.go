package worktree

import (
	"os"
	"strings"
	"testing"
)

// cmdTestDir creates a temp directory outside any git repo and chdirs into it.
// The original working directory is restored on cleanup.
func cmdTestDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// --- §11: CLI command routing and flag parsing ---

func TestRunWorktreeCmd_UnknownCommand(t *testing.T) {
	cmdTestDir(t)
	err := RunCmd([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("error should mention 'unknown worktree command', got: %v", err)
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error should include usage, got: %v", err)
	}
}

func TestRunWorktreeCmd_AddNoArgs(t *testing.T) {
	cmdTestDir(t)
	err := RunCmd([]string{"add"})
	if err == nil {
		t.Fatal("expected error for 'add' with no branch")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error should include usage, got: %v", err)
	}
}

func TestRunWorktreeCmd_RemoveNoArgs(t *testing.T) {
	cmdTestDir(t)
	err := RunCmd([]string{"rm"})
	if err == nil {
		t.Fatal("expected error for 'rm' with no target")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error should include usage, got: %v", err)
	}
}

func TestRunWorktreeCmd_MergeAlias(t *testing.T) {
	cmdTestDir(t)
	// "m" should route to merge, which will fail because we're not in a repo,
	// but the error should NOT be "unknown worktree command".
	err := RunCmd([]string{"m"})
	if err == nil {
		t.Fatal("merge should fail outside a git repo")
	}
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'m' should be recognized as merge alias, got: %v", err)
	}
}

func TestRunWorktreeCmd_ListAlias(t *testing.T) {
	cmdTestDir(t)
	// "ls" should be recognized — it will fail (not a repo) but not as "unknown command".
	err := RunCmd([]string{"ls"})
	if err == nil {
		t.Fatal("list should fail outside a git repo")
	}
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'ls' should be recognized as list alias, got: %v", err)
	}
}

func TestRunWorktreeCmd_AddAlias(t *testing.T) {
	cmdTestDir(t)
	// "a" should be recognized.
	err := RunCmd([]string{"a"})
	if err == nil {
		t.Fatal("add should fail with no args")
	}
	// Should be a usage error, not "unknown command".
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'a' should be recognized as add alias, got: %v", err)
	}
}
