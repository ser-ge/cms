package tui

import "testing"

func TestCompactPath(t *testing.T) {
	tests := []struct {
		path   string
		maxLen int
		want   string
	}{
		{"~/projects/cms/worktrees/feature", 0, "~/p/c/w/feature"},
		{"~/projects/gather_git", 0, "~/p/gather_git"},
		{"~/notes", 0, "~/notes"},
		{"/usr/local/bin", 0, "/u/l/bin"},
		{"simple", 0, "simple"},
		{"~/a/b", 0, "~/a/b"},
		// maxLen: don't abbreviate if under threshold.
		{"~/projects/cms", 20, "~/projects/cms"},
		// maxLen: abbreviate if over threshold.
		{"~/projects/cms", 10, "~/p/cms"},
	}
	for _, tt := range tests {
		got := CompactPath(tt.path, tt.maxLen)
		if got != tt.want {
			t.Errorf("CompactPath(%q, %d) = %q, want %q", tt.path, tt.maxLen, got, tt.want)
		}
	}
}
