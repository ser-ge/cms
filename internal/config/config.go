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

	CompletedDecayMs int   `toml:"completed_decay_ms"` // Completed->Idle auto-decay in ms (default 30000)
	AlwaysHooksForStatus *bool `toml:"always_hooks_for_status"` // when true (default), hooks never go stale; observer skips transitions while any hook has been seen

	// Transition smoothing: delay state changes to suppress flicker.
	// Global override applies to all transitions if > 0.
	TransitionSmoothingMs int              `toml:"transition_smoothing_ms"`
	Smoothing             SmoothingConfig  `toml:"smoothing"`
}

// SmoothingConfig holds per-transition smoothing delays in milliseconds.
// A value of 0 means no smoothing for that transition.
type SmoothingConfig struct {
	WorkingToIdleMs      int `toml:"working_to_idle_ms"`      // suppress brief idle flickers during multi-step work
	WorkingToCompletedMs int `toml:"working_to_completed_ms"` // don't show "completed" too eagerly
	IdleToWorkingMs      int `toml:"idle_to_working_ms"`      // going TO working (usually instant)
	CompletedToIdleMs    int `toml:"completed_to_idle_ms"`    // controlled by completed_decay_ms already
}

// SmoothingMs returns the smoothing delay in ms for a transition,
// applying the global override if set.
func (g GeneralConfig) SmoothingMs(from, to string) int {
	if g.TransitionSmoothingMs > 0 {
		return g.TransitionSmoothingMs
	}
	key := from + "_to_" + to
	switch key {
	case "working_to_idle":
		return g.Smoothing.WorkingToIdleMs
	case "working_to_completed":
		return g.Smoothing.WorkingToCompletedMs
	case "idle_to_working":
		return g.Smoothing.IdleToWorkingMs
	case "completed_to_idle":
		return g.Smoothing.CompletedToIdleMs
	}
	return 0
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
	PromoteActive *bool `toml:"promote_active"` // sort active/open items first
}

// AgentDisplayConfig holds agent-specific display settings used by queue
// and session agent summaries.
type AgentDisplayConfig struct {
	ProviderOrder         []string `toml:"provider_order"`
	StateOrder            []string `toml:"state_order"`
	ShowContextPercentage bool     `toml:"show_context_percentage"`
	UseSeenInRanking      bool     `toml:"use_seen_in_ranking"` // unseen attention events boost queue ranking
}

// ActiveIndicatorConfig controls the visual indicator for active/open items.
type ActiveIndicatorConfig struct {
	Icon       string `toml:"icon"`       // default "▪"
	Color      string `toml:"color"`      // foreground color (ANSI), default "" (inherits)
	Background string `toml:"background"` // background color (ANSI), default "" (none)
	Bold       *bool  `toml:"bold"`       // default nil (false)
}

type FinderConfig struct {
	// What appears in bare `cms` and in what order.
	Include []string `toml:"include"`

	// Global sort defaults (per-picker sections override).
	DemoteCurrent bool `toml:"demote_current"`
	PromoteRecent bool `toml:"promote_recent"`
	PromoteActive bool `toml:"promote_active"` // sort active/open items first

	// Agent display (queue + summaries).
	Agents AgentDisplayConfig `toml:"agents"`

	// Active item visual indicator.
	ActiveIndicator ActiveIndicatorConfig `toml:"active_indicator"`

	// Per-section sort overrides.
	Sessions  PickerSortConfig `toml:"sessions"`
	Projects  PickerSortConfig `toml:"projects"`
	Worktrees PickerSortConfig `toml:"worktrees"`
	Branches  PickerSortConfig `toml:"branches"`
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

// ShouldPromoteActive returns whether active/open items should be
// promoted above inactive items in the given section.
func (f FinderConfig) ShouldPromoteActive(pickerType string) bool {
	if psc := f.pickerSort(pickerType); psc.PromoteActive != nil {
		return *psc.PromoteActive
	}
	return f.PromoteActive
}

func (f FinderConfig) pickerSort(pickerType string) PickerSortConfig {
	switch pickerType {
	case "sessions":
		return f.Sessions
	case "projects":
		return f.Projects
	case "worktrees":
		return f.Worktrees
	case "branches":
		return f.Branches
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
	GoCmd      string         `toml:"go_cmd"`     // command to run with prompt after "cms go <branch> <prompt>"
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
		Include:       []string{"sessions", "queue", "worktrees", "marks", "projects"},
		DemoteCurrent: true,
		PromoteRecent: false,
		Agents: AgentDisplayConfig{
			ProviderOrder:         []string{"claude", "codex"},
			StateOrder:            []string{"idle", "working", "waiting"},
			ShowContextPercentage: true,
		},
		ActiveIndicator: ActiveIndicatorConfig{
			Icon:  "\u25aa", // ▪
			Color: "2",      // green
		},
		Sessions: PickerSortConfig{PromoteRecent: boolPtr(true)},
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
		CompletedDecayMs: 300000,
		AlwaysHooksForStatus: boolPtr(true),
		Smoothing: SmoothingConfig{
			WorkingToIdleMs:      3000,
			WorkingToCompletedMs: 2000,
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
	defaultSlice(&c.Finder.Agents.ProviderOrder, df.Agents.ProviderOrder)
	defaultSlice(&c.Finder.Agents.StateOrder, df.Agents.StateOrder)
	defaultSlice(&c.Finder.Include, df.Include)
	defaultStr(&c.Finder.ActiveIndicator.Icon, df.ActiveIndicator.Icon)

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
	if err := toml.NewEncoder(&buf).Encode(DefaultConfig()); err != nil {
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

// LoadProjectConfig reads .cms.toml from the given directory (typically the
// repo root). Returns a zero-value config when the file is missing.
func LoadProjectConfig(dir string) ProjectConfig {
	var proj ProjectConfig
	if dir == "" {
		return proj
	}
	path := filepath.Join(dir, ".cms.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return proj
	}
	toml.Unmarshal(data, &proj) //nolint:errcheck
	return proj
}
