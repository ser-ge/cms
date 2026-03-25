package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/config"
)

// newWorktreeModel is a minimal text-input screen for creating a new worktree.
// Enter submits, Escape cancels.
type newWorktreeModel struct {
	input  textinput.Model
	cfg    config.Config
	done   bool
	action *PostAction
	err    string
}

func newNewWorktreeModel(cfg config.Config) newWorktreeModel {
	ti := textinput.New()
	ti.Placeholder = "branch-name"
	ti.Prompt = "  new worktree " + dimStyle.Render("› ")
	ti.CharLimit = 128
	ti.Focus()
	return newWorktreeModel{input: ti, cfg: cfg}
}

func (m newWorktreeModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m newWorktreeModel) Update(msg tea.Msg) (newWorktreeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.input.Value())
			if name == "" {
				m.err = "branch name cannot be empty"
				return m, nil
			}
			m.action = &PostAction{
				Kind:       KindWorktree,
				BranchName: name,
			}
			m.done = true
			return m, nil
		case "esc":
			m.done = true
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.err = ""
	return m, cmd
}

func (m newWorktreeModel) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(fmt.Sprintf("  %s\n", waitingStyle.Render(m.err)))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter confirm · esc cancel"))
	b.WriteString("\n")
	return b.String()
}
