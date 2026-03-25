package worktree

import (
	"strings"
	"testing"
)

// --- §11: CLI command routing and flag parsing ---

func TestRunWorktreeCmd_UnknownCommand(t *testing.T) {
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
	err := RunCmd([]string{"add"})
	if err == nil {
		t.Fatal("expected error for 'add' with no branch")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error should include usage, got: %v", err)
	}
}

func TestRunWorktreeCmd_RemoveNoArgs(t *testing.T) {
	err := RunCmd([]string{"rm"})
	if err == nil {
		t.Fatal("expected error for 'rm' with no target")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error should include usage, got: %v", err)
	}
}

func TestRunWorktreeCmd_MergeAlias(t *testing.T) {
	// "m" should route to merge, which will fail because we're not in a repo,
	// but the error should NOT be "unknown worktree command".
	err := RunCmd([]string{"m"})
	if err == nil {
		t.Skip("merge succeeded unexpectedly (probably in a repo)")
	}
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'m' should be recognized as merge alias, got: %v", err)
	}
}

func TestRunWorktreeCmd_ListAlias(t *testing.T) {
	// "ls" should be recognized.
	err := RunCmd([]string{"ls"})
	if err == nil {
		return // worked, we're in a repo
	}
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'ls' should be recognized as list alias, got: %v", err)
	}
}

func TestRunWorktreeCmd_AddAlias(t *testing.T) {
	// "a" should be recognized.
	err := RunCmd([]string{"a"})
	if err == nil {
		t.Skip("add succeeded unexpectedly")
	}
	// Should be a usage error, not "unknown command".
	if strings.Contains(err.Error(), "unknown worktree command") {
		t.Errorf("'a' should be recognized as add alias, got: %v", err)
	}
}
