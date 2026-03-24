package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds cms settings loaded from ~/.config/cms/config.toml.
type Config struct {
	General   GeneralConfig   `toml:"general"`
	Colors    ColorsConfig    `toml:"colors"`
	Icons     IconsConfig     `toml:"icons"`
	Dashboard DashboardConfig `toml:"dashboard"`
	Finder    FinderConfig    `toml:"finder"`
}

// ColorsConfig holds shared UI colors and provider-specific accents.
type ColorsConfig struct {
	Shared SharedColorsConfig   `toml:"shared"`
	Claude ProviderColorsConfig `toml:"claude"`
	Codex  ProviderColorsConfig `toml:"codex"`
}

type SharedColorsConfig struct {
	Session    string `toml:"session"`
	Window     string `toml:"window"`
	Dim        string `toml:"dim"`
	Selected   string `toml:"selected"`
	Current    string `toml:"current"`
	Working    string `toml:"working"`
	Waiting    string `toml:"waiting"`
	Idle       string `toml:"idle"`
	MoveSrc    string `toml:"move_src"`
	CtxLow     string `toml:"ctx_low"`
	CtxMid     string `toml:"ctx_mid"`
	CtxHigh    string `toml:"ctx_high"`
	Separator  string `toml:"separator"`
	FooterRule string `toml:"footer_rule"`
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

type GeneralConfig struct {
	DefaultSession   string       `toml:"default_session"`
	SwitchPriority   []string     `toml:"switch_priority"`
	EscapeChord      string       `toml:"escape_chord"`
	EscapeChordMs    int          `toml:"escape_chord_ms"`
	Exclusions       []string     `toml:"exclusions"`
	AttachedLast     bool         `toml:"attached_last"`
	LastSessionFirst bool         `toml:"last_session_first"`
	SearchSubmodules bool         `toml:"search_submodules"`
	SearchPaths      []SearchPath `toml:"search_paths"`
}

type IconsConfig struct {
	WorkingFrames   []string `toml:"working_frames"`
	Waiting         string   `toml:"waiting"`
	Idle            string   `toml:"idle"`
	Unknown         string   `toml:"unknown"`
	ColumnSeparator string   `toml:"column_separator"`
	FooterSeparator string   `toml:"footer_separator"`
}

type DashboardConfig struct {
	Columns               []string `toml:"columns"`
	WindowHeaders         string   `toml:"window_headers"`
	FooterPadding         bool     `toml:"footer_padding"`
	FooterSeparator       bool     `toml:"footer_separator"`
	ShowContextPercentage bool     `toml:"show_context_percentage"`
}

type FinderConfig struct {
	ProviderOrder         []string `toml:"provider_order"`
	StateOrder            []string `toml:"state_order"`
	ShowContextPercentage bool     `toml:"show_context_percentage"`
}

// DefaultColors returns the default color scheme.
func DefaultColors() ColorsConfig {
	return ColorsConfig{
		Shared: SharedColorsConfig{
			Session:    "15",
			Window:     "245",
			Dim:        "240",
			Selected:   "236",
			Current:    "2",
			Working:    "208",
			Waiting:    "1",
			Idle:       "12",
			MoveSrc:    "5",
			CtxLow:     "2",
			CtxMid:     "3",
			CtxHigh:    "1",
			Separator:  "240",
			FooterRule: "240",
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

func DefaultIcons() IconsConfig {
	return IconsConfig{
		WorkingFrames:   []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		Waiting:         "?",
		Idle:            "●",
		Unknown:         "·",
		ColumnSeparator: " │ ",
		FooterSeparator: "╌",
	}
}

func DefaultDashboardConfig() DashboardConfig {
	return DashboardConfig{
		Columns:               []string{"name", "branch", "command", "activity", "context", "mode"},
		WindowHeaders:         "auto",
		FooterPadding:         true,
		FooterSeparator:       true,
		ShowContextPercentage: true,
	}
}

func DefaultFinderConfig() FinderConfig {
	return FinderConfig{
		ProviderOrder:         []string{"claude", "codex"},
		StateOrder:            []string{"idle", "working", "waiting"},
		ShowContextPercentage: true,
	}
}

func DefaultGeneralConfig() GeneralConfig {
	home, _ := os.UserHomeDir()
	return GeneralConfig{
		DefaultSession:   "",
		SwitchPriority:   []string{"waiting", "idle", "default", "working"},
		EscapeChord:      "jj",
		EscapeChordMs:    250,
		Exclusions:       []string{},
		AttachedLast:     true,
		LastSessionFirst: true,
		SearchSubmodules: false,
		SearchPaths: []SearchPath{
			{Path: filepath.Join(home, "projects"), MaxDepth: 3},
		},
	}
}

// DefaultConfig returns sensible defaults when no config file exists.
func DefaultConfig() Config {
	return Config{
		General:   DefaultGeneralConfig(),
		Colors:    DefaultColors(),
		Icons:     DefaultIcons(),
		Dashboard: DefaultDashboardConfig(),
		Finder:    DefaultFinderConfig(),
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
	for i := range cfg.General.SearchPaths {
		cfg.General.SearchPaths[i].Path = expandHome(cfg.General.SearchPaths[i].Path)
		if cfg.General.SearchPaths[i].MaxDepth == 0 {
			cfg.General.SearchPaths[i].MaxDepth = 3
		}
	}

	cfg.normalize()

	return cfg
}

func (c *Config) normalize() {
	defIcons := DefaultIcons()
	if len(c.Icons.WorkingFrames) == 0 {
		c.Icons.WorkingFrames = defIcons.WorkingFrames
	}
	if c.Icons.Waiting == "" {
		c.Icons.Waiting = defIcons.Waiting
	}
	if c.Icons.Idle == "" {
		c.Icons.Idle = defIcons.Idle
	}
	if c.Icons.Unknown == "" {
		c.Icons.Unknown = defIcons.Unknown
	}
	if c.Icons.ColumnSeparator == "" {
		c.Icons.ColumnSeparator = defIcons.ColumnSeparator
	}
	if c.Icons.FooterSeparator == "" {
		c.Icons.FooterSeparator = defIcons.FooterSeparator
	}

	defDash := DefaultDashboardConfig()
	if len(c.Dashboard.Columns) == 0 {
		c.Dashboard.Columns = defDash.Columns
	}
	if c.Dashboard.WindowHeaders == "" {
		c.Dashboard.WindowHeaders = defDash.WindowHeaders
	}

	defFinder := DefaultFinderConfig()
	if len(c.Finder.ProviderOrder) == 0 {
		c.Finder.ProviderOrder = defFinder.ProviderOrder
	}
	if len(c.Finder.StateOrder) == 0 {
		c.Finder.StateOrder = defFinder.StateOrder
	}

	defGeneral := DefaultGeneralConfig()
	if len(c.General.SearchPaths) == 0 {
		c.General.SearchPaths = defGeneral.SearchPaths
	}
	if len(c.General.SwitchPriority) == 0 {
		c.General.SwitchPriority = defGeneral.SwitchPriority
	}
	if c.General.EscapeChord == "" {
		c.General.EscapeChord = defGeneral.EscapeChord
	}
	if c.General.EscapeChordMs == 0 {
		c.General.EscapeChordMs = defGeneral.EscapeChordMs
	}
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

func DefaultConfigTOML() ([]byte, error) {
	var buf bytes.Buffer
	out := struct {
		General GeneralConfig `toml:"general"`
	}{
		General: DefaultGeneralConfig(),
	}
	if err := toml.NewEncoder(&buf).Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteDefaultConfigFile() (string, error) {
	path := configPath()
	if _, err := os.Stat(path); err == nil {
		return path, os.ErrExist
	} else if !os.IsNotExist(err) {
		return path, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}

	data, err := DefaultConfigTOML()
	if err != nil {
		return path, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return path, err
	}
	return path, nil
}
