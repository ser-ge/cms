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
	SwitchPriority []string     `toml:"switch_priority"`
	EscapeChord    string       `toml:"escape_chord"`
	EscapeChordMs  int          `toml:"escape_chord_ms"`
	Colors ColorsConfig `toml:"colors"`
}

// ColorsConfig holds all configurable color values (ANSI 0-255 or hex).
type ColorsConfig struct {
	Session    string `toml:"session"`     // session name header
	Window     string `toml:"window"`      // window name
	Dim        string `toml:"dim"`         // dimmed/muted text
	Selected   string `toml:"selected"`    // selected row background
	Current    string `toml:"current"`     // current pane indicator
	Working    string `toml:"working"`     // claude working
	Waiting    string `toml:"waiting"`     // claude waiting for input
	Idle       string `toml:"idle"`        // claude idle
	MoveSrc    string `toml:"move_src"`    // pane being moved
	ModePlan   string `toml:"mode_plan"`   // plan mode
	ModeAccept string `toml:"mode_accept"` // auto-edit mode
	ModeYolo   string `toml:"mode_yolo"`   // yolo mode
	CtxLow     string `toml:"ctx_low"`     // context < 50%
	CtxMid     string `toml:"ctx_mid"`     // context 50-79%
	CtxHigh    string `toml:"ctx_high"`    // context >= 80%
}

// SearchPath defines a directory to scan for projects.
type SearchPath struct {
	Path     string `toml:"path"`
	MaxDepth int    `toml:"max_depth"`
}

// DefaultColors returns the default color scheme.
func DefaultColors() ColorsConfig {
	return ColorsConfig{
		Session:    "15",
		Window:     "245",
		Dim:        "240",
		Selected:   "236",
		Current:    "2",
		Working:    "208",
		Waiting:    "1",
		Idle:       "240",
		MoveSrc:    "5",
		ModePlan:   "4",
		ModeAccept: "5",
		ModeYolo:   "1",
		CtxLow:     "2",
		CtxMid:     "3",
		CtxHigh:    "1",
	}
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
		Colors:         DefaultColors(),
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
