package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SetupResult holds the outcome of the first-run setup wizard.
type SetupResult struct {
	ConfigPath   string // path to written config file (empty if cancelled)
	InstallHooks bool   // user opted into Claude Code hooks
	Cancelled    bool
}

// RunSetup runs an interactive first-run wizard that prompts for essential
// settings and writes the config file.
func RunSetup() (SetupResult, error) {
	p := tea.NewProgram(newSetupModel())
	result, err := p.Run()
	if err != nil {
		return SetupResult{}, err
	}
	m := result.(setupModel)
	if m.cancelled {
		return SetupResult{Cancelled: true}, nil
	}
	if m.writeErr != nil {
		return SetupResult{}, m.writeErr
	}
	return SetupResult{
		ConfigPath:   m.writtenPath,
		InstallHooks: m.installHooks,
	}, nil
}

// --- bubbletea model ---

type setupStep int

const (
	stepSearchPath setupStep = iota
	stepHooks
	stepConfirm
)

type setupModel struct {
	step         setupStep
	input        textinput.Model
	searchPath   string
	installHooks bool
	cancelled    bool
	writtenPath  string
	writeErr     error

	// Tab completion state.
	completions []string // current completions
	compIdx     int      // index into completions (-1 = none)

	dim  lipgloss.Style
	bold lipgloss.Style
}

func newSetupModel() setupModel {
	home, _ := os.UserHomeDir()
	defaultPath := home

	ti := textinput.New()
	ti.Placeholder = abbreviateHome(defaultPath)
	ti.Focus()
	ti.Width = 60
	ti.SetValue(abbreviateHome(defaultPath))
	ti.SetCursor(len(ti.Value()))

	return setupModel{
		step:    stepSearchPath,
		input:   ti,
		compIdx: -1,
		dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		bold:    lipgloss.NewStyle().Bold(true),
	}
}

func (m setupModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyTab:
			if m.step == stepSearchPath {
				return m.handleTab()
			}

		case tea.KeyEnter:
			switch m.step {
			case stepSearchPath:
				val := strings.TrimSpace(m.input.Value())
				if val == "" {
					home, _ := os.UserHomeDir()
					val = home
				}
				m.searchPath = val
				m.step = stepHooks
				return m, nil

			case stepHooks:
				// Enter = yes (default)
				m.installHooks = true
				m.step = stepConfirm
				return m, nil

			case stepConfirm:
				m.writtenPath, m.writeErr = m.writeConfig()
				return m, tea.Quit
			}

		default:
			switch m.step {
			case stepHooks:
				switch msg.String() {
				case "y", "Y":
					m.installHooks = true
					m.step = stepConfirm
					return m, nil
				case "n", "N":
					m.installHooks = false
					m.step = stepConfirm
					return m, nil
				}
				return m, nil

			case stepConfirm:
				switch msg.String() {
				case "n", "N":
					m.cancelled = true
					return m, tea.Quit
				case "y", "Y":
					m.writtenPath, m.writeErr = m.writeConfig()
					return m, tea.Quit
				}
				return m, nil
			}

			// Any non-tab key resets completions.
			m.completions = nil
			m.compIdx = -1
		}
	}

	if m.step == stepSearchPath {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m setupModel) View() string {
	var b strings.Builder

	b.WriteString("cms — first-run setup\n\n")

	switch m.step {
	case stepSearchPath:
		b.WriteString("Directory to scan for git projects:\n")
		b.WriteString(m.input.View())
		b.WriteString("\n")
		if len(m.completions) > 1 {
			b.WriteString(m.dim.Render(fmt.Sprintf("  tab: %d matches", len(m.completions))))
			b.WriteString("\n")
		}
		b.WriteString(m.dim.Render("  tab to complete · enter to confirm · esc to cancel"))
		b.WriteString("\n")

	case stepHooks:
		b.WriteString(fmt.Sprintf("  search path: %s\n\n", m.bold.Render(abbreviateHome(ExpandHome(m.searchPath)))))
		b.WriteString("Install Claude Code hooks for faster agent status?\n")
		b.WriteString(m.dim.Render("  Adds hooks to ~/.claude/settings.json for real-time status updates."))
		b.WriteString("\n")
		b.WriteString(m.dim.Render("  Without hooks, cms detects activity by observing pane output (slower)."))
		b.WriteString("\n\n")
		b.WriteString("Install hooks? [Y/n] ")

	case stepConfirm:
		expanded := ExpandHome(m.searchPath)

		b.WriteString("The following will be created:\n\n")
		b.WriteString(fmt.Sprintf("  1. Write %s\n", m.bold.Render(abbreviateHome(configPath()))))
		b.WriteString(fmt.Sprintf("     search path: %s (depth 3)\n", abbreviateHome(expanded)))

		if m.installHooks {
			b.WriteString(fmt.Sprintf("\n  2. Add cms hooks to %s\n", m.bold.Render(filepath.Join("~", ".claude", "settings.json"))))
			b.WriteString("     events: SessionStart, Stop, SessionEnd, Notification, PreToolUse, UserPromptSubmit\n")
		}

		if info, err := os.Stat(expanded); err != nil || !info.IsDir() {
			b.WriteString(fmt.Sprintf("\n  %s %s is not a directory\n",
				lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("warning:"), expanded))
		}

		b.WriteString("\nProceed? [Y/n] ")
	}

	return b.String()
}

// handleTab cycles through directory completions for the current input.
func (m setupModel) handleTab() (setupModel, tea.Cmd) {
	val := m.input.Value()

	// First tab press: compute completions.
	if len(m.completions) == 0 {
		m.completions = completeDir(val)
		m.compIdx = 0
	} else {
		m.compIdx = (m.compIdx + 1) % len(m.completions)
	}

	if len(m.completions) > 0 {
		m.input.SetValue(m.completions[m.compIdx])
		m.input.SetCursor(len(m.input.Value()))
	}

	return m, nil
}

const maxCompletions = 50

// completeDir returns directory completions for a partial path.
func completeDir(partial string) []string {
	expanded := ExpandHome(partial)

	// If it's already a directory, list its children.
	if info, err := os.Stat(expanded); err == nil && info.IsDir() {
		if !strings.HasSuffix(expanded, "/") {
			expanded += "/"
		}
		return listChildDirs(expanded)
	}

	// Otherwise complete the last component.
	dir := filepath.Dir(expanded)
	prefix := filepath.Base(expanded)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			full := filepath.Join(dir, e.Name())
			matches = append(matches, abbreviateHome(full))
			if len(matches) >= maxCompletions {
				break
			}
		}
	}
	sort.Strings(matches)
	return matches
}

// listChildDirs lists immediate subdirectories of dir.
func listChildDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var results []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		results = append(results, abbreviateHome(full))
		if len(results) >= maxCompletions {
			break
		}
	}
	sort.Strings(results)
	return results
}

func (m setupModel) writeConfig() (string, error) {
	expanded := ExpandHome(m.searchPath)

	cfg := DefaultConfig()
	cfg.General.SearchPaths = []SearchPath{{
		Path:     expanded,
		MaxDepth: cfg.General.SearchPaths[0].MaxDepth,
	}}

	data, err := defaultConfigTOMLFrom(cfg.General, cfg.Finder)
	if err != nil {
		return "", err
	}

	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
