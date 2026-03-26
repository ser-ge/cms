package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
)

// providerStyles holds provider-specific lipgloss styles.
type providerStyles struct {
	accent, plan, accept, danger, safe lipgloss.Style
}

// Styles -- initialized from config via InitStyles().
var (
	sessionStyle    lipgloss.Style
	windowStyle     lipgloss.Style
	dimStyle        lipgloss.Style
	selectedStyle   lipgloss.Style
	currentStyle    lipgloss.Style
	moveSrcStyle    lipgloss.Style
	workingStyle    lipgloss.Style
	waitingStyle    lipgloss.Style
	idleStyle       lipgloss.Style
	helpStyle       lipgloss.Style
	separatorStyle  lipgloss.Style
	footerRuleStyle lipgloss.Style
	attachLabel     string

	providerStyleMap map[agent.Provider]providerStyles

	ctxLowStyle  lipgloss.Style
	ctxMidStyle  lipgloss.Style
	ctxHighStyle lipgloss.Style

	workingFramesUI   []string
	waitingIndicator  string
	idleIndicator     string
	unknownIndicator  string
	columnSeparatorUI string
	footerSeparatorUI string

	// Picker styles.
	pickerSelectedStyle lipgloss.Style
	pickerNormalStyle   lipgloss.Style
	pickerDescStyle     lipgloss.Style
	pickerMatchStyle    lipgloss.Style
	pickerTitleStyle    lipgloss.Style
	pickerCountStyle    lipgloss.Style
	pickerConfirmStyle  lipgloss.Style

	// Section icon padding (1 cell space for items with no icon).
	sectionIconPadding string
)

// InitStyles initializes all shared styles from a loaded config.
func InitStyles(cfg config.Config) {
	c := cfg.Colors
	sessionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Shared.Session))
	windowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Window))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Dim))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color(c.Shared.Selected))
	currentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Current))
	moveSrcStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.MoveSrc)).Bold(true)
	workingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Working))
	waitingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Waiting))
	idleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Idle))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Dim)).Faint(true)
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Separator)).Faint(true)
	footerRuleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.FooterRule)).Faint(true)
	attachLabel = dimStyle.Render(" (attached)")

	providerStyleMap = map[agent.Provider]providerStyles{}
	for _, pc := range []struct {
		provider agent.Provider
		cfg      config.ProviderColorsConfig
	}{
		{agent.ProviderClaude, c.Claude},
		{agent.ProviderCodex, c.Codex},
	} {
		providerStyleMap[pc.provider] = providerStyles{
			accent: lipgloss.NewStyle().Foreground(lipgloss.Color(pc.cfg.Accent)).Bold(true),
			plan:   lipgloss.NewStyle().Foreground(lipgloss.Color(pc.cfg.Plan)),
			accept: lipgloss.NewStyle().Foreground(lipgloss.Color(pc.cfg.Accept)),
			danger: lipgloss.NewStyle().Foreground(lipgloss.Color(pc.cfg.Danger)).Bold(true),
			safe:   lipgloss.NewStyle().Foreground(lipgloss.Color(pc.cfg.Safe)),
		}
	}

	ctxLowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxLow))
	ctxMidStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxMid))
	ctxHighStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.CtxHigh))

	// Picker styles.
	pickerSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color(c.Shared.Selected)).Foreground(lipgloss.Color(c.Shared.Current)).Bold(true)
	pickerNormalStyle = lipgloss.NewStyle()
	pickerDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Window))
	pickerMatchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Working)).Bold(true)
	pickerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Shared.Session))
	pickerCountStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Dim))
	pickerConfirmStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Shared.Waiting)).Bold(true)

	// Section icon padding — single space matching a 1-cell icon.
	sectionIconPadding = " "

	workingFramesUI = append([]string(nil), cfg.Icons.WorkingFrames...)
	waitingIndicator = cfg.Icons.Waiting
	idleIndicator = cfg.Icons.Idle
	unknownIndicator = cfg.Icons.Unknown
	columnSeparatorUI = cfg.Icons.ColumnSeparator
	footerSeparatorUI = cfg.Icons.FooterSeparator
}

// RenderSectionIcon renders a section icon with the given lipgloss style.
func RenderSectionIcon(icon string, style lipgloss.Style) string {
	return style.Render(icon)
}

// SectionIconPadding returns a space matching a 1-cell icon width.
func SectionIconPadding() string {
	return sectionIconPadding
}

// ActivityStyle returns the lipgloss style for the given agent activity.
func ActivityStyle(a agent.Activity) lipgloss.Style {
	switch a {
	case agent.ActivityWorking:
		return workingStyle
	case agent.ActivityWaitingInput:
		return waitingStyle
	case agent.ActivityCompleted:
		return waitingStyle
	case agent.ActivityIdle:
		return idleStyle
	default:
		return dimStyle
	}
}

// MostUrgentActivity returns the most urgent activity from a list,
// using the provided state_order (first = most urgent).
func MostUrgentActivity(activities []agent.Activity, stateOrder []string) agent.Activity {
	best := agent.Activity(-1)
	bestRank := len(stateOrder) + 1
	for _, a := range activities {
		name := a.String()
		rank := len(stateOrder) // default: least urgent
		for i, s := range stateOrder {
			if s == name {
				rank = i
				break
			}
		}
		if rank < bestRank {
			bestRank = rank
			best = a
		}
	}
	if best < 0 {
		return agent.ActivityIdle
	}
	return best
}

// ProviderAccent returns the accent style for a given provider.
func ProviderAccent(p agent.Provider) lipgloss.Style {
	if ps, ok := providerStyleMap[p]; ok {
		return ps.accent
	}
	return dimStyle
}

// ContextStyle returns a style based on context usage percentage.
func ContextStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 80:
		return ctxHighStyle
	case pct >= 50:
		return ctxMidStyle
	default:
		return ctxLowStyle
	}
}

// ModeStyle returns the style for a given agent status's mode.
func ModeStyle(status agent.AgentStatus) lipgloss.Style {
	ps, ok := providerStyleMap[status.Provider]
	if !ok {
		return dimStyle
	}
	switch status.Mode {
	case agent.ModePlan:
		return ps.plan
	case agent.ModeAcceptEdits:
		return ps.accept
	case agent.ModeBypassPermissions, agent.ModeDangerFullAccess:
		return ps.danger
	case agent.ModeReadOnly, agent.ModeWorkspaceWrite:
		return ps.safe
	default:
		return ps.accent
	}
}

// RenderMode renders the mode label for a given agent status.
func RenderMode(status agent.AgentStatus) string {
	if status.ModeLabel == "" {
		return ""
	}
	return ModeStyle(status).Render(status.ModeLabel)
}

func RenderActivity(a agent.Activity) string {

	switch a {
	case agent.ActivityIdle:
		return idleStyle.Render(agent.ActivityIdle.String())
	case agent.ActivityWorking:
		return workingStyle.Render(agent.ActivityWorking.String())
	case agent.ActivityWaitingInput:
		return waitingStyle.Render(agent.ActivityWaitingInput.String())
	case agent.ActivityCompleted:
		return waitingStyle.Render(agent.ActivityCompleted.String())
	default:
		return "unknown"
	}

}
