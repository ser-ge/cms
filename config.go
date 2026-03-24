package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds cms settings loaded from ~/.config/cms/config.toml.
type Config struct {
	SearchPaths    []SearchPath `toml:"search_paths"`
	Exclusions     []string     `toml:"exclusions"`
	DefaultSession string       `toml:"default_session"`
	// SwitchPriority controls which pane to focus when switching to a session.
	// Values: "waiting", "idle", "default", "working".
	// Empty or unset uses the default priority: ["waiting", "idle", "default", "working"].
	// Set to ["default"] to always use tmux's last-active pane.
	SwitchPriority []string `toml:"switch_priority"`
	// EscapeChord is a two-key sequence to exit insert mode in the picker (like vim's jj/jk).
	// Set to "" to disable. Default: "jj".
	EscapeChord string `toml:"escape_chord"`
	// EscapeChordMs is the timeout in milliseconds for the escape chord. Default: 250.
	EscapeChordMs int `toml:"escape_chord_ms"`
}

// SearchPath defines a directory to scan for projects.
type SearchPath struct {
	Path     string `toml:"path"`
	MaxDepth int    `toml:"max_depth"`
}

// DefaultConfig returns sensible defaults when no config file exists.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		SearchPaths: []SearchPath{
			{Path: filepath.Join(home, "projects"), MaxDepth: 3},
		},
		Exclusions:     []string{"node_modules", "vendor", ".git", ".cache"},
		SwitchPriority: []string{"waiting", "idle", "default", "working"},
		EscapeChord:    "jj",
		EscapeChordMs:  250,
	}
}

// LoadConfig reads config from ~/.config/cms/config.toml.
// Returns DefaultConfig if the file doesn't exist.
func LoadConfig() Config {
	path := configPath()
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig()
	}

	// Expand ~ in all search paths.
	for i := range cfg.SearchPaths {
		cfg.SearchPaths[i].Path = expandHome(cfg.SearchPaths[i].Path)
		if cfg.SearchPaths[i].MaxDepth == 0 {
			cfg.SearchPaths[i].MaxDepth = 3
		}
	}

	return cfg
}

func configPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cms", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cms", "config.toml")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
