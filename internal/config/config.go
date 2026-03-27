package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds cms settings loaded from ~/.config/cms/config.toml.
type Config struct {
	General   GeneralConfig   `toml:"general"`
	Status    StatusConfig    `toml:"status"`
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
	Completed  string `toml:"completed"`
	Idle       string `toml:"idle"`
	Active     string `toml:"active"` // non-agent "open/present" indicator
	MoveSrc    string `toml:"move_src"`
	Match      string `toml:"match"` // fuzzy match highlight in picker
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
	Path       string   `toml:"path"`
	MaxDepth   int      `toml:"max_depth"`
	Exclusions []string `toml:"exclusions"`
}

type GeneralConfig struct {
	DefaultSession   string       `toml:"default_session"`
	SwitchPriority   []string     `toml:"switch_priority"`
	EscapeChord      string       `toml:"escape_chord"`
	EscapeChordMs    int          `toml:"escape_chord_ms"`
	SearchSubmodules bool         `toml:"search_submodules"`
	SearchPaths      []SearchPath `toml:"search_paths"`

	Restore         *bool `toml:"restore"`            // master switch for snapshot restore on project open (default: true)
	CompletedDecayS int   `toml:"completed_decay_s"` // Completed->Idle auto-decay in seconds (default 30000)
}

// StatusConfig holds agent status tracking tuning — smoothing and hook behavior.
// These are internal tuning knobs, not exported in the minimal default config.
type StatusConfig struct {
	AlwaysHooksForStatus *bool `toml:"always_hooks_for_status"` // when true, hooks never go stale; observer skips transitions while any hook has been seen

	// Transition smoothing: delay state changes to suppress flicker.
	// Global override applies to all transitions if > 0.
	TransitionSmoothingMs int             `toml:"transition_smoothing_ms"`
	Smoothing             SmoothingConfig `toml:"smoothing"`
}

// SmoothingConfig holds per-transition smoothing delays in milliseconds.
// A value of 0 means no smoothing for that transition.
type SmoothingConfig struct {
	WorkingToIdleMs      int `toml:"working_to_idle_ms"`      // suppress brief idle flickers during multi-step work
	WorkingToCompletedMs int `toml:"working_to_completed_ms"` // don't show "completed" too eagerly
	IdleToWorkingMs      int `toml:"idle_to_working_ms"`      // going TO working (usually instant)
	CompletedToIdleMs    int `toml:"completed_to_idle_ms"`    // controlled by completed_decay_s already
}

// SmoothingMs returns the smoothing delay in ms for a transition,
// applying the global override if set.
func (s StatusConfig) SmoothingMs(from, to string) int {
	if s.TransitionSmoothingMs > 0 {
		return s.TransitionSmoothingMs
	}
	key := from + "_to_" + to
	switch key {
	case "working_to_idle":
		return s.Smoothing.WorkingToIdleMs
	case "working_to_completed":
		return s.Smoothing.WorkingToCompletedMs
	case "idle_to_working":
		return s.Smoothing.IdleToWorkingMs
	case "completed_to_idle":
		return s.Smoothing.CompletedToIdleMs
	}
	return 0
}

// ShouldRestore reports whether session snapshots should be restored on project open.
func (g GeneralConfig) ShouldRestore() bool {
	if g.Restore == nil {
		return true
	}
	return *g.Restore
}

type IconsConfig struct {
	WorkingFrames   []string `toml:"working_frames"`
	Working         string   `toml:"working"` // static icon for counts (distinct from animated WorkingFrames)
	Waiting         string   `toml:"waiting"`
	Completed       string   `toml:"completed"`
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

// PickerSortConfig controls sort behavior for a single picker/section type.
// An empty Sort list inherits from FinderConfig.Sort.
type PickerSortConfig struct {
	Sort       []string `toml:"sort"`        // sort key priority list (e.g. ["recent", "-current"])
	StateOrder []string `toml:"state_order"` // agents queue urgency order (e.g. ["waiting","completed","working","idle"])
}

// SectionIconsConfig holds the icon glyph for each finder section.
// The icon's color is determined by state (activity-colored for agent-bearing
// sections, active/dim for others).
type SectionIconsConfig struct {
	Sessions  string `toml:"sessions"`  // default "S"
	AgentsQueue string `toml:"agents_queue"` // default "*"
	Worktrees string `toml:"worktrees"` // default "⎇"
	Branches  string `toml:"branches"`  // default "B"
	Panes     string `toml:"panes"`     // default ">"
	Windows   string `toml:"windows"`   // default "W"
	Marks     string `toml:"marks"`     // default "M"
	Projects  string `toml:"projects"`  // default "P"
}

type FinderConfig struct {
	// What appears in bare `cms` and in what order.
	Include []string `toml:"include"`

	// Global sort key priority list. Per-section overrides below.
	// Keys evaluated left-to-right; first difference wins.
	// Prefix "-" demotes (e.g. "-current" pushes current item to bottom).
	// Valid keys: active, current, recent, state, unseen, oldest, newest.
	Sort       []string `toml:"sort"`
	StateOrder []string `toml:"state_order"` // agents queue urgency order (used by "state" sort key)

	ShowContextPercentage bool `toml:"show_context_percentage"`

	// Per-section icons (glyph identifies type, color encodes state).
	SectionIcons SectionIconsConfig `toml:"section_icons"`

	// Per-section sort overrides.
	Sessions  PickerSortConfig `toml:"sessions"`
	Projects  PickerSortConfig `toml:"projects"`
	AgentsQueue PickerSortConfig `toml:"agents_queue"`
	Worktrees PickerSortConfig `toml:"worktrees"`
	Branches  PickerSortConfig `toml:"branches"`
	Windows   PickerSortConfig `toml:"windows"`
	Panes     PickerSortConfig `toml:"panes"`
	Marks     PickerSortConfig `toml:"marks"`
}

// SortKeys returns the sort key priority list for the given section.
// Falls back to the global FinderConfig.Sort if the section has no override.
func (f FinderConfig) SortKeys(section string) []string {
	if psc := f.sectionConfig(section); len(psc.Sort) > 0 {
		return psc.Sort
	}
	return f.Sort
}

// GetStateOrder returns the agents queue urgency sort order for the given section.
func (f FinderConfig) GetStateOrder(section string) []string {
	if psc := f.sectionConfig(section); len(psc.StateOrder) > 0 {
		return psc.StateOrder
	}
	return f.StateOrder
}

func (f FinderConfig) sectionConfig(pickerType string) PickerSortConfig {
	switch pickerType {
	case "sessions":
		return f.Sessions
	case "projects":
		return f.Projects
	case "agents":
		return f.AgentsQueue
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
	CommitCmd  string         `toml:"commit_cmd"` // LLM commit message command (e.g. "claude -p --model=haiku ...")
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
			Current:    "15",
			Working:    "3",
			Waiting:    "1",
			Completed:  "208",
			Idle:       "12",
			Active:     "2",
			MoveSrc:    "5",
			Match:      "13",
			CtxLow:     "2",
			CtxMid:     "3",
			CtxHigh:    "9",
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
		Working:         "\u26a1",
		Waiting:         "?",
		Completed:       "\u2713",
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
		Include:    []string{"agents", "worktrees", "sessions", "projects"},
		Sort:       []string{"active", "-current"},
		StateOrder:            []string{"waiting", "completed", "idle", "working"},
		ShowContextPercentage: true,

		SectionIcons: SectionIconsConfig{
			Sessions:  "S",
			AgentsQueue: "*",
			Worktrees: "\u2387", // ⎇
			Branches:  "B",
			Panes:     ">",
			Windows:   "W",
			Marks:     "M",
			Projects:  "P",
		},
		Sessions: PickerSortConfig{Sort: []string{"recent", "-current"}},
		AgentsQueue: PickerSortConfig{Sort: []string{"state", "unseen", "oldest"}},
	}
}

func DefaultGeneralConfig() GeneralConfig {
	home, _ := os.UserHomeDir()
	return GeneralConfig{
		DefaultSession:   "",
		SwitchPriority:   []string{"waiting", "completed", "idle", "default", "working"},
		EscapeChord:      "jj",
		EscapeChordMs:    250,
		SearchSubmodules: false,
		SearchPaths: []SearchPath{
			{Path: home, MaxDepth: 3},
		},
		Restore:         boolPtr(true),
		CompletedDecayS: 30000,
	}
}

func DefaultStatusConfig() StatusConfig {
	return StatusConfig{
		AlwaysHooksForStatus: boolPtr(false),
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
		Status:    DefaultStatusConfig(),
		Colors:    DefaultColors(),
		Icons:     DefaultIcons(),
		Dashboard: DefaultDashboardConfig(),
		Finder:    DefaultFinderConfig(),
	}
}

// Load reads config from ~/.config/cms/config.toml.
// Returns DefaultConfig and firstRun=true if the file doesn't exist.
// Returns an error if the config is invalid or contains unknown keys.
func Load() (cfg Config, firstRun bool, err error) {
	path := configPath()
	cfg = DefaultConfig()

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return cfg, true, nil
	}

	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, false, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		var keys []string
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return cfg, false, fmt.Errorf("%s: unknown config keys: %s", path, strings.Join(keys, ", "))
	}

	// Expand ~ in all search paths.
	for i := range cfg.General.SearchPaths {
		cfg.General.SearchPaths[i].Path = ExpandHome(cfg.General.SearchPaths[i].Path)
		if cfg.General.SearchPaths[i].MaxDepth == 0 {
			cfg.General.SearchPaths[i].MaxDepth = 3
		}
	}

	cfg.normalize()

	return cfg, false, nil
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
	defaultStr(&c.Icons.Working, di.Working)
	defaultStr(&c.Icons.Waiting, di.Waiting)
	defaultStr(&c.Icons.Completed, di.Completed)
	defaultStr(&c.Icons.Idle, di.Idle)
	defaultStr(&c.Icons.Unknown, di.Unknown)
	defaultStr(&c.Icons.ColumnSeparator, di.ColumnSeparator)
	defaultStr(&c.Icons.FooterSeparator, di.FooterSeparator)

	dd := DefaultDashboardConfig()
	defaultSlice(&c.Dashboard.Columns, dd.Columns)
	defaultStr(&c.Dashboard.WindowHeaders, dd.WindowHeaders)

	df := DefaultFinderConfig()
	defaultSlice(&c.Finder.Include, df.Include)
	defaultSlice(&c.Finder.Sort, df.Sort)
	defaultSlice(&c.Finder.StateOrder, df.StateOrder)
	defaultStr(&c.Finder.SectionIcons.Sessions, df.SectionIcons.Sessions)
	defaultStr(&c.Finder.SectionIcons.AgentsQueue, df.SectionIcons.AgentsQueue)
	defaultStr(&c.Finder.SectionIcons.Worktrees, df.SectionIcons.Worktrees)
	defaultStr(&c.Finder.SectionIcons.Branches, df.SectionIcons.Branches)
	defaultStr(&c.Finder.SectionIcons.Panes, df.SectionIcons.Panes)
	defaultStr(&c.Finder.SectionIcons.Windows, df.SectionIcons.Windows)
	defaultStr(&c.Finder.SectionIcons.Marks, df.SectionIcons.Marks)
	defaultStr(&c.Finder.SectionIcons.Projects, df.SectionIcons.Projects)

	dg := DefaultGeneralConfig()
	defaultSlice(&c.General.SearchPaths, dg.SearchPaths)
	defaultSlice(&c.General.SwitchPriority, dg.SwitchPriority)
	defaultStr(&c.General.EscapeChord, dg.EscapeChord)
	defaultInt(&c.General.EscapeChordMs, dg.EscapeChordMs)
	defaultInt(&c.General.CompletedDecayS, dg.CompletedDecayS)
}

func configPath() string {
	if dir := os.Getenv("CMS_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.toml")
	}
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
	return defaultConfigTOMLFrom(DefaultGeneralConfig(), DefaultFinderConfig())
}

func defaultConfigTOMLFrom(g GeneralConfig, f FinderConfig) ([]byte, error) {
	var buf bytes.Buffer
	w := func(s string) { buf.WriteString(s) }

	w("[general]\n")
	w(fmt.Sprintf("default_session = %q\n", g.DefaultSession))
	w("# Priority order for pane selection when switching to a session or window.\n")
	w(fmt.Sprintf("switch_priority = %s\n", tomlStringArray(g.SwitchPriority)))
	w("# Two-key chord to exit insert mode in the TUI.\n")
	w(fmt.Sprintf("escape_chord = %q\n", g.EscapeChord))
	w(fmt.Sprintf("escape_chord_ms = %d\n", g.EscapeChordMs))
	w("# Scan git submodules when discovering projects.\n")
	w(fmt.Sprintf("search_submodules = %v\n", g.SearchSubmodules))
	w("# Restore tmux session snapshots when opening a project.\n")
	w(fmt.Sprintf("restore = %v\n", g.ShouldRestore()))
	w("# Seconds before a Completed agent decays to Idle (0 = never).\n")
	w(fmt.Sprintf("completed_decay_s = %d\n", g.CompletedDecayS))
	w("# Directories to scan for git projects.\n")
	for _, sp := range g.SearchPaths {
		w("[[general.search_paths]]\n")
		w(fmt.Sprintf("path = %q\n", abbreviateHome(sp.Path)))
		w(fmt.Sprintf("max_depth = %d\n", sp.MaxDepth))
		w(fmt.Sprintf("exclusions = %s\n", tomlStringArray(sp.Exclusions)))
	}

	w("\n")

	w("[finder]\n")
	w("# What bare `cms` shows and in what order.\n")
	w(fmt.Sprintf("include = %s\n", tomlStringArray(f.Include)))
	w("\n# Global sort key priority list. Per-section overrides below.\n")
	w("# Keys evaluated left-to-right; first difference wins.\n")
	w("# Prefix \"-\" demotes (pushes matching items to bottom).\n")
	w(fmt.Sprintf("sort = %s\n", tomlStringArray(f.Sort)))
	w("\n# Agents queue urgency order (used by \"state\" sort key).\n")
	w(fmt.Sprintf("state_order = %s\n", tomlStringArray(f.StateOrder)))
	w("\n# Show max context percentage in aggregate session/worktree summaries.\n")
	w(fmt.Sprintf("show_context_percentage = %v\n", f.ShowContextPercentage))
	w("\n")

	w("[finder.section_icons]\n")
	w(fmt.Sprintf("sessions = %q\n", f.SectionIcons.Sessions))
	w(fmt.Sprintf("agents_queue = %q\n", f.SectionIcons.AgentsQueue))
	w(fmt.Sprintf("worktrees = %q\n", f.SectionIcons.Worktrees))
	w(fmt.Sprintf("branches = %q\n", f.SectionIcons.Branches))
	w(fmt.Sprintf("panes = %q\n", f.SectionIcons.Panes))
	w(fmt.Sprintf("windows = %q\n", f.SectionIcons.Windows))
	w(fmt.Sprintf("marks = %q\n", f.SectionIcons.Marks))
	w(fmt.Sprintf("projects = %q\n", f.SectionIcons.Projects))
	w("\n")

	w("# Per-section sort overrides — only specify what differs from global.\n")
	w("[finder.sessions]\n")
	w(fmt.Sprintf("sort = %s  # last-visited first, attached last\n", tomlStringArray(f.Sessions.Sort)))
	w("\n")

	w("[finder.agents_queue]\n")
	w(fmt.Sprintf("sort = %s  # urgency sort\n", tomlStringArray(f.AgentsQueue.Sort)))
	w("\n")

	w("# [finder.worktrees]\n")
	w("# sort = [\"active\", \"-current\"]\n")
	w("# [finder.branches]\n")
	w("# sort = [\"active\"]\n")

	return buf.Bytes(), nil
}

// FullConfigTOML returns a commented TOML document with every configurable option,
// including internal tuning knobs (status, colors, icons, dashboard).
func FullConfigTOML() ([]byte, error) {
	base, err := DefaultConfigTOML()
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	s := cfg.Status
	c := cfg.Colors
	i := cfg.Icons
	d := cfg.Dashboard

	var buf bytes.Buffer
	w := func(s string) { buf.WriteString(s) }

	buf.Write(base)
	w("\n")

	w("# Agent status tracking tuning. Most users won't need to change these.\n")
	w("[status]\n")
	w("# When true, hooks never go stale; observer skips transitions while any hook has been seen.\n")
	w(fmt.Sprintf("always_hooks_for_status = %v\n", *s.AlwaysHooksForStatus))
	w("# Global smoothing delay (ms) for all state transitions (0 = use per-transition values).\n")
	w("# transition_smoothing_ms = 0\n")
	w("\n")

	w("# Per-transition smoothing delays (ms). Suppresses flicker from rapid state changes.\n")
	w("[status.smoothing]\n")
	w(fmt.Sprintf("working_to_idle_ms = %d\n", s.Smoothing.WorkingToIdleMs))
	w(fmt.Sprintf("working_to_completed_ms = %d\n", s.Smoothing.WorkingToCompletedMs))
	w(fmt.Sprintf("idle_to_working_ms = %d\n", s.Smoothing.IdleToWorkingMs))
	w(fmt.Sprintf("completed_to_idle_ms = %d\n", s.Smoothing.CompletedToIdleMs))
	w("\n")

	w("# UI color scheme. Values are ANSI color numbers (0-255) or hex (#rrggbb).\n")
	w("[colors.shared]\n")
	w(fmt.Sprintf("session = %q\n", c.Shared.Session))
	w(fmt.Sprintf("window = %q\n", c.Shared.Window))
	w(fmt.Sprintf("dim = %q\n", c.Shared.Dim))
	w(fmt.Sprintf("selected = %q\n", c.Shared.Selected))
	w(fmt.Sprintf("current = %q\n", c.Shared.Current))
	w(fmt.Sprintf("working = %q\n", c.Shared.Working))
	w(fmt.Sprintf("waiting = %q\n", c.Shared.Waiting))
	w(fmt.Sprintf("completed = %q\n", c.Shared.Completed))
	w(fmt.Sprintf("idle = %q\n", c.Shared.Idle))
	w(fmt.Sprintf("active = %q\n", c.Shared.Active))
	w(fmt.Sprintf("move_src = %q\n", c.Shared.MoveSrc))
	w(fmt.Sprintf("match = %q\n", c.Shared.Match))
	w(fmt.Sprintf("ctx_low = %q\n", c.Shared.CtxLow))
	w(fmt.Sprintf("ctx_mid = %q\n", c.Shared.CtxMid))
	w(fmt.Sprintf("ctx_high = %q\n", c.Shared.CtxHigh))
	w(fmt.Sprintf("separator = %q\n", c.Shared.Separator))
	w(fmt.Sprintf("footer_rule = %q\n", c.Shared.FooterRule))
	w("\n")

	writeProviderColors(w, "claude", c.Claude)
	writeProviderColors(w, "codex", c.Codex)

	w("# Icon glyphs used in the TUI.\n")
	w("[icons]\n")
	w(fmt.Sprintf("working_frames = %s\n", tomlStringArray(i.WorkingFrames)))
	w(fmt.Sprintf("working = %q\n", i.Working))
	w(fmt.Sprintf("waiting = %q\n", i.Waiting))
	w(fmt.Sprintf("completed = %q\n", i.Completed))
	w(fmt.Sprintf("idle = %q\n", i.Idle))
	w(fmt.Sprintf("unknown = %q\n", i.Unknown))
	w(fmt.Sprintf("column_separator = %q\n", i.ColumnSeparator))
	w(fmt.Sprintf("footer_separator = %q\n", i.FooterSeparator))
	w("\n")

	w("# Dashboard layout.\n")
	w("[dashboard]\n")
	w(fmt.Sprintf("columns = %s\n", tomlStringArray(d.Columns)))
	w(fmt.Sprintf("window_headers = %q\n", d.WindowHeaders))
	w(fmt.Sprintf("footer_padding = %v\n", d.FooterPadding))
	w(fmt.Sprintf("footer_separator = %v\n", d.FooterSeparator))
	w(fmt.Sprintf("show_context_percentage = %v\n", d.ShowContextPercentage))
	w("\n")

	w("# Worktree management. Also configurable per-repo in .cms.toml.\n")
	w("[worktree]\n")
	w("# base_dir = \"../worktrees\"\n")
	w("# base_branch = \"main\"\n")
	w(fmt.Sprintf("auto_open = %v\n", cfg.Worktree.AutoOpen))
	w("# commit_cmd = \"claude -p ...\"\n")
	w("# go_cmd = \"claude -p \\\"$CMS_PROMPT\\\"\"\n")

	return buf.Bytes(), nil
}

func writeProviderColors(w func(string), name string, p ProviderColorsConfig) {
	w(fmt.Sprintf("[colors.%s]\n", name))
	w(fmt.Sprintf("accent = %q\n", p.Accent))
	w(fmt.Sprintf("plan = %q\n", p.Plan))
	w(fmt.Sprintf("accept = %q\n", p.Accept))
	w(fmt.Sprintf("safe = %q\n", p.Safe))
	w(fmt.Sprintf("danger = %q\n", p.Danger))
	w("\n")
}

// tomlStringArray formats a string slice as a TOML inline array.
func tomlStringArray(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// abbreviateHome replaces the home directory prefix with ~/ for display.
func abbreviateHome(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// stripEmptySections removes TOML section headers that are immediately
// followed by another section header or EOF (i.e. have no key-value pairs).
func stripEmptySections(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		// Check if this is a section header like [foo.bar].
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			// Look ahead: skip blank lines, check if next non-blank is another header or EOF.
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j >= len(lines) || (strings.HasPrefix(strings.TrimSpace(lines[j]), "[") && strings.HasSuffix(strings.TrimSpace(lines[j]), "]")) {
				i = j - 1 // skip this empty section (and its trailing blanks)
				continue
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
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
	proj := ProjectConfig{
		Session: SessionConfig{
			Claude: SessionClaudeConfig{Resume: true},
		},
	}
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
