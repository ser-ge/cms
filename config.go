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
	Colors         ColorsConfig `toml:"colors"`
}

// ColorsConfig holds shared UI colors and provider-specific accents.
type ColorsConfig struct {
	Shared SharedColorsConfig   `toml:"shared"`
	Claude ProviderColorsConfig `toml:"claude"`
	Codex  ProviderColorsConfig `toml:"codex"`
}

type SharedColorsConfig struct {
	Session  string `toml:"session"`
	Window   string `toml:"window"`
	Dim      string `toml:"dim"`
	Selected string `toml:"selected"`
	Current  string `toml:"current"`
	Working  string `toml:"working"`
	Waiting  string `toml:"waiting"`
	Idle     string `toml:"idle"`
	MoveSrc  string `toml:"move_src"`
	CtxLow   string `toml:"ctx_low"`
	CtxMid   string `toml:"ctx_mid"`
	CtxHigh  string `toml:"ctx_high"`
}

type ProviderColorsConfig struct {
	Accent string `toml:"accent"`
	Plan   string `toml:"plan"`
	Accept string `toml:"accept"`
	Safe   string `toml:"safe"`
	Danger string `toml:"danger"`
}

// SearchPath defines a directory to scan for projects.
type SearchPath struct {
	Path     string `toml:"path"`
	MaxDepth int    `toml:"max_depth"`
}

// DefaultColors returns the default color scheme.
func DefaultColors() ColorsConfig {
	return ColorsConfig{
		Shared: SharedColorsConfig{
			Session:  "15",
			Window:   "245",
			Dim:      "240",
			Selected: "236",
			Current:  "2",
			Working:  "208",
			Waiting:  "1",
			Idle:     "240",
			MoveSrc:  "5",
			CtxLow:   "2",
			CtxMid:   "3",
			CtxHigh:  "1",
		},
		Claude: ProviderColorsConfig{
			Accent: "5",
			Plan:   "4",
			Accept: "5",
			Safe:   "6",
			Danger: "1",
		},
		Codex: ProviderColorsConfig{
			Accent: "6",
			Plan:   "12",
			Accept: "14",
			Safe:   "10",
			Danger: "9",
		},
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
