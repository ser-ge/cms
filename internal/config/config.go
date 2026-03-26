package config

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
	Worktree  WorktreeConfig  `toml:"worktree"`
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

	CompletedDecayMs int `toml:"completed_decay_ms"` // Completed->Idle auto-decay in ms (default 30000)
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

// PickerSortConfig controls sort behavior for a single picker type.
// nil booleans inherit from FinderConfig defaults.
type PickerSortConfig struct {
	DemoteCurrent *bool `toml:"demote_current"`
	PromoteRecent *bool `toml:"promote_recent"`
	PromoteOpen   *bool `toml:"promote_open"` // promote items with active agent/window activity
}

type FinderConfig struct {
	ProviderOrder         []string `toml:"provider_order"`
	StateOrder            []string `toml:"state_order"`
	ShowContextPercentage bool     `toml:"show_context_percentage"`

	// What appears in bare `cms` and in what order.
	Include []string `toml:"include"`

	// Global sort defaults (per-picker sections override).
	DemoteCurrent bool `toml:"demote_current"`
	PromoteRecent bool `toml:"promote_recent"`
	PromoteOpen   bool `toml:"promote_open"` // promote items that are open (have tmux session/window)

	// Per-picker overrides.
	Sessions  PickerSortConfig `toml:"sessions"`
	Projects  PickerSortConfig `toml:"projects"`
	Worktrees PickerSortConfig `toml:"worktrees"`
	Windows   PickerSortConfig `toml:"windows"`
	Panes     PickerSortConfig `toml:"panes"`
	Marks     PickerSortConfig `toml:"marks"`
}

// ShouldDemoteCurrent returns whether the given picker type should push
// the active/current item to the bottom.
func (f FinderConfig) ShouldDemoteCurrent(pickerType string) bool {
	if psc := f.pickerSort(pickerType); psc.DemoteCurrent != nil {
		return *psc.DemoteCurrent
	}
	return f.DemoteCurrent
}

// ShouldPromoteRecent returns whether the given picker type should promote
// the most recently visited item to the top.
func (f FinderConfig) ShouldPromoteRecent(pickerType string) bool {
	if psc := f.pickerSort(pickerType); psc.PromoteRecent != nil {
		return *psc.PromoteRecent
	}
	return f.PromoteRecent
}

// ShouldPromoteOpen returns whether items that are "open" (have a tmux
// session or window) should be promoted above unopened items.
func (f FinderConfig) ShouldPromoteOpen(pickerType string) bool {
	if psc := f.pickerSort(pickerType); psc.PromoteOpen != nil {
		return *psc.PromoteOpen
	}
	return f.PromoteOpen
}

func (f FinderConfig) pickerSort(pickerType string) PickerSortConfig {
	switch pickerType {
	case "sessions":
		return f.Sessions
	case "projects":
		return f.Projects
	case "worktrees":
		return f.Worktrees
	case "windows":
		return f.Windows
	case "panes":
		return f.Panes
	case "marks":
		return f.Marks
	}
	return PickerSortConfig{}
}

// WorktreeConfig holds worktree settings. Loaded from user config
// (~/.config/cms/config.toml [worktree]) and per-repo project config
// (.cms.toml [worktree]). Project config overrides user config.
type WorktreeConfig struct {
	BaseDir    string         `toml:"base_dir"`
	BaseBranch string         `toml:"base_branch"` // branch to fork from (default: auto-detect main/master)
	Hooks      []WorktreeHook `toml:"hooks"`        // post-create
	PreRemove  []WorktreeHook `toml:"pre_remove"`
	PreCommit  []WorktreeHook `toml:"pre_commit"`
	PostCommit []WorktreeHook `toml:"post_commit"`
	PreMerge   []WorktreeHook `toml:"pre_merge"`
	PostMerge  []WorktreeHook `toml:"post_merge"`
	AutoOpen   bool           `toml:"auto_open"`
	CommitCmd  string         `toml:"commit_cmd"` // LLM commit message command (e.g. "llm -m claude-haiku")
}

// SessionConfig holds session bootstrap and restore settings.
// Loaded from per-repo .cms.toml [session].
type SessionConfig struct {
	Name      string              `toml:"name"`
	Bootstrap string              `toml:"bootstrap"`
	Mode      string              `toml:"mode"` // template_only | restore_only | template_then_restore
	Attach    bool                `toml:"attach"`
	Claude    SessionClaudeConfig `toml:"claude"`
}

// SessionClaudeConfig controls Claude Code session resumption.
type SessionClaudeConfig struct {
	Resume            bool   `toml:"resume"`
	Command           string `toml:"command"`
	OnlyIfPaneEmpty   bool   `toml:"only_if_pane_empty"`
	OnlyInMarkedPanes bool   `toml:"only_in_marked_panes"`
}

// ProjectConfig is the per-repo config loaded from .cms.toml at the repo root.
type ProjectConfig struct {
	Worktree WorktreeConfig `toml:"worktree"`
	Session  SessionConfig  `toml:"session"`
}

// WorktreeHook is a shell command that runs at a lifecycle point.
type WorktreeHook struct {
	Command string            `toml:"command"`
	Env     map[string]string `toml:"env"`
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
		WorkingFrames:   []string{"\u280B", "\u2819", "\u2839", "\u2838", "\u283C", "\u2834", "\u2826", "\u2827", "\u2807", "\u280F"},
		Waiting:         "?",
		Idle:            "\u25CF",
		Unknown:         "\u00B7",
		ColumnSeparator: " \u2502 ",
		FooterSeparator: "\u254C",
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

func boolPtr(b bool) *bool { return &b }

func DefaultFinderConfig() FinderConfig {
	return FinderConfig{
		ProviderOrder:         []string{"claude", "codex"},
		StateOrder:            []string{"idle", "working", "waiting"},
		ShowContextPercentage: true,
		Include:               []string{"sessions", "queue", "worktrees", "marks", "projects"},
		DemoteCurrent:         true,
		PromoteRecent:         false,
		Sessions:              PickerSortConfig{PromoteRecent: boolPtr(true)},
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
		CompletedDecayMs: 30000,
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

// Load reads config from ~/.config/cms/config.toml.
// Returns DefaultConfig if the file doesn't exist.
func Load() Config {
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
		cfg.General.SearchPaths[i].Path = ExpandHome(cfg.General.SearchPaths[i].Path)
		if cfg.General.SearchPaths[i].MaxDepth == 0 {
			cfg.General.SearchPaths[i].MaxDepth = 3
		}
	}

	cfg.normalize()

	return cfg
}

func defaultStr(field *string, def string) {
	if *field == "" {
		*field = def
	}
}

func defaultSlice[T any](field *[]T, def []T) {
	if len(*field) == 0 {
		*field = def
	}
}

func defaultInt(field *int, def int) {
	if *field == 0 {
		*field = def
	}
}

func (c *Config) normalize() {
	di := DefaultIcons()
	defaultSlice(&c.Icons.WorkingFrames, di.WorkingFrames)
	defaultStr(&c.Icons.Waiting, di.Waiting)
	defaultStr(&c.Icons.Idle, di.Idle)
	defaultStr(&c.Icons.Unknown, di.Unknown)
	defaultStr(&c.Icons.ColumnSeparator, di.ColumnSeparator)
	defaultStr(&c.Icons.FooterSeparator, di.FooterSeparator)

	dd := DefaultDashboardConfig()
	defaultSlice(&c.Dashboard.Columns, dd.Columns)
	defaultStr(&c.Dashboard.WindowHeaders, dd.WindowHeaders)

	df := DefaultFinderConfig()
	defaultSlice(&c.Finder.ProviderOrder, df.ProviderOrder)
	defaultSlice(&c.Finder.StateOrder, df.StateOrder)
	defaultSlice(&c.Finder.Include, df.Include)

	// Migrate legacy fields to new finder sort config.
	if c.General.LastSessionFirst && c.Finder.Sessions.PromoteRecent == nil {
		c.Finder.Sessions.PromoteRecent = boolPtr(true)
	}
	if c.General.AttachedLast && c.Finder.Sessions.DemoteCurrent == nil {
		c.Finder.Sessions.DemoteCurrent = boolPtr(true)
	}

	dg := DefaultGeneralConfig()
	defaultSlice(&c.General.SearchPaths, dg.SearchPaths)
	defaultSlice(&c.General.SwitchPriority, dg.SwitchPriority)
	defaultStr(&c.General.EscapeChord, dg.EscapeChord)
	defaultInt(&c.General.EscapeChordMs, dg.EscapeChordMs)
	defaultInt(&c.General.CompletedDecayMs, dg.CompletedDecayMs)
}

func configPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cms", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cms", "config.toml")
}

// ExpandHome expands a leading ~/ in a path to the user's home directory.
func ExpandHome(path string) string {
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

// LoadProjectConfig reads .cms.toml from the provided directories in order,
// returning the first successfully parsed config. Missing files are ignored.
func LoadProjectConfig(dirs ...string) ProjectConfig {
	var proj ProjectConfig
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, ".cms.toml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		toml.Unmarshal(data, &proj) //nolint:errcheck
		return proj
	}
	return proj
}
