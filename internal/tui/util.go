package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// ShortenHome replaces the user's home directory prefix with ~.
func ShortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return filepath.Join("~", path[len(home):])
	}
	return path
}

// CompactPath shortens a path by abbreviating each intermediate directory
// to its first character when the full path exceeds maxLen. The last path
// component is always kept in full. If maxLen is 0, always abbreviate.
//
// Examples (maxLen=0):
//
//	~/projects/cms/worktrees/feature → ~/p/c/w/feature
//	~/projects/gather_git            → ~/p/gather_git
//	~/notes                          → ~/notes
func CompactPath(path string, maxLen int) string {
	if maxLen > 0 && len(path) <= maxLen {
		return path
	}

	sep := string(filepath.Separator)
	parts := strings.Split(path, sep)
	if len(parts) <= 2 {
		return path
	}

	// Abbreviate from left, keeping ~ and the last component intact.
	start := 0
	if parts[0] == "~" {
		start = 1
	}
	for i := start; i < len(parts)-1; i++ {
		if len(parts[i]) > 0 {
			// Use first rune to handle UTF-8 names.
			r := []rune(parts[i])
			parts[i] = string(r[0])
		}
	}
	return strings.Join(parts, sep)
}

// JoinParts joins display parts with a middle-dot separator.
func JoinParts(parts []string) string {
	return strings.Join(parts, " \u00b7 ")
}

// normalizeName makes a name safe for tmux (dots and colons become underscores).
func normalizeName(name string) string {
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}
