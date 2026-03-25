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
