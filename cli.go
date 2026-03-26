package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// command describes a CLI command for help, completion, and suggestions.
type command struct {
	Name    string   // primary name, e.g. "go"
	Aliases []string // short flags, e.g. ["-s"]
	Args    string   // placeholder, e.g. "<branch> [path]"
	Short   string   // one-line description
	Long    string   // extended help (optional)
	Hidden  bool     // exclude from help and completion
	Group   string   // visual group in help output
}

var commands = []command{
	// Finder modes.
	{Name: "sessions", Aliases: []string{"-s"}, Short: "Sessions only", Group: "Finder"},
	{Name: "projects", Aliases: []string{"-p"}, Short: "Projects only", Group: "Finder"},
	{Name: "queue", Aliases: []string{"-q"}, Short: "Attention queue (urgency-sorted agent panes)", Group: "Finder"},
	{Name: "marks", Aliases: []string{"-m"}, Short: "Marks only", Group: "Finder"},
	{Name: "worktrees", Short: "Worktrees only (current repo)", Group: "Finder"},
	{Name: "windows", Short: "Windows only (all sessions)", Group: "Finder"},
	{Name: "panes", Short: "Panes only (all sessions)", Group: "Finder"},

	// Views.
	{Name: "dash", Short: "Dashboard (session/pane grid with agent status)", Group: "View"},
	{Name: "new", Short: "Create new worktree (interactive)", Group: "View"},

	// Navigation.
	{Name: "next", Short: "Jump to next waiting/idle agent pane", Group: "Navigation"},
	{Name: "mark", Args: "<label> [pane]", Short: "Mark current pane with label", Group: "Navigation"},
	{Name: "jump", Args: "<label>", Short: "Switch to marked pane", Group: "Navigation"},

	// Worktree operations.
	{Name: "go", Args: "<branch> [path]", Short: "Switch to worktree (create if needed)", Long: "Switch to an existing worktree for the given branch, or create\na new one if it doesn't exist.", Group: "Worktree"},
	{Name: "add", Args: "[--no-open] <branch> [path]", Short: "Create worktree", Long: "Create a new git worktree for the given branch.\n\nFlags:\n  --no-open   Don't open a tmux window for the worktree", Group: "Worktree"},
	{Name: "rm", Args: "<branch>", Short: "Remove worktree", Long: "Remove a worktree by branch name or path.\n\nFlags:\n  -f, --force        Force removal even with changes\n  --keep-branch      Don't delete the branch after removing the worktree", Group: "Worktree"},
	{Name: "merge", Args: "[flags] [branch]", Short: "Merge worktree", Long: "Merge a worktree branch into the base branch.\n\nFlags:\n  --no-delete   Don't delete worktree after merge\n  --squash      Squash commits before merging", Group: "Worktree"},
	{Name: "ls", Short: "List worktrees (paths, branches, merge status)", Group: "Worktree"},

	// Config.
	{Name: "config", Args: "init", Short: "Write default config file", Group: "Config"},
	{Name: "hook-setup", Short: "Print Claude Code hook config", Group: "Config"},
	{Name: "completion", Args: "<fish|bash|zsh>", Short: "Print shell completion script", Group: "Config"},

	// Hidden.
	{Name: "internal", Hidden: true},
	{Name: "session", Hidden: true},
}

// groupOrder controls the display order in help output.
var groupOrder = []string{"Finder", "View", "Navigation", "Worktree", "Config"}

// --- Help rendering ---

var (
	boldStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

func renderHelp() string {
	var b strings.Builder

	b.WriteString(boldStyle.Render("cms") + dimStyle.Render(" — tmux session picker and dashboard with agent awareness") + "\n\n")
	b.WriteString(boldStyle.Render("Usage:") + "\n")
	b.WriteString("  cms                        " + dimStyle.Render("Open finder (universal fuzzy switcher)") + "\n")
	b.WriteString("  cms " + accentStyle.Render("<command>") + " [args]         " + dimStyle.Render("Run a command") + "\n")
	b.WriteString("  cms " + accentStyle.Render("-s|-p|-q|-m") + "             " + dimStyle.Render("Open finder with filter") + "\n\n")

	// Group commands.
	grouped := map[string][]command{}
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		grouped[c.Group] = append(grouped[c.Group], c)
	}

	for _, group := range groupOrder {
		cmds, ok := grouped[group]
		if !ok {
			continue
		}
		b.WriteString(headingStyle.Render(group) + "\n")
		for _, c := range cmds {
			name := c.Name
			if c.Args != "" {
				name += " " + c.Args
			}
			alias := ""
			if len(c.Aliases) > 0 {
				alias = " " + dimStyle.Render("("+strings.Join(c.Aliases, ", ")+")")
			}
			// Pad to 30 chars for alignment.
			pad := 30 - len(name)
			if pad < 2 {
				pad = 2
			}
			b.WriteString("  " + accentStyle.Render(name) + strings.Repeat(" ", pad) + c.Short + alias + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render("Run 'cms <command> --help' for more information on a command.") + "\n")
	return b.String()
}

func renderCommandHelp(name string) string {
	for _, c := range commands {
		if c.Name != name {
			continue
		}
		if c.Hidden {
			break
		}
		var b strings.Builder
		usage := "cms " + c.Name
		if c.Args != "" {
			usage += " " + c.Args
		}
		b.WriteString(boldStyle.Render("Usage:") + " " + usage + "\n\n")
		b.WriteString(c.Short + "\n")
		if c.Long != "" {
			b.WriteString("\n" + c.Long + "\n")
		}
		if len(c.Aliases) > 0 {
			b.WriteString("\n" + boldStyle.Render("Aliases:") + " " + strings.Join(c.Aliases, ", ") + "\n")
		}
		return b.String()
	}
	// Unknown command — show hint.
	msg := fmt.Sprintf("unknown command: %s\n", name)
	if s := suggestCommand(name); s != "" {
		msg += fmt.Sprintf("\nDid you mean %s?\n", accentStyle.Render("cms "+s))
	}
	msg += "\n" + dimStyle.Render("Run 'cms --help' for a list of commands.") + "\n"
	return msg
}

// --- "Did you mean?" ---

func suggestCommand(input string) string {
	best := ""
	bestDist := 4 // only suggest if distance <= 3
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		d := levenshtein(input, c.Name)
		if d < bestDist {
			bestDist = d
			best = c.Name
		}
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev = curr
	}
	return prev[lb]
}

func unknownCommandMsg(name string) string {
	msg := fmt.Sprintf("unknown command: %s", name)
	if s := suggestCommand(name); s != "" {
		msg += fmt.Sprintf("\n\nDid you mean %s?", accentStyle.Render("cms "+s))
	}
	msg += "\n\n" + dimStyle.Render("Run 'cms --help' for a list of commands.")
	return msg
}

func unknownFlagMsg(flag string) string {
	msg := fmt.Sprintf("unknown flag: %s", flag)
	msg += "\n\n" + dimStyle.Render("Available flags: -s (sessions), -p (projects), -q (queue), -m (marks)")
	msg += "\n" + dimStyle.Render("Run 'cms --help' for a list of commands.")
	return msg
}

// --- Shell completion ---

func runCompletion(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cms completion <fish|bash|zsh>")
	}
	switch args[0] {
	case "fish":
		fmt.Print(completionFish())
	case "bash":
		fmt.Print(completionBash())
	case "zsh":
		fmt.Print(completionZsh())
	default:
		return fmt.Errorf("unsupported shell: %s (supported: fish, bash, zsh)", args[0])
	}
	return nil
}

func completionFish() string {
	var b strings.Builder
	b.WriteString("# cms fish completions — generated by 'cms completion fish'\n")
	b.WriteString("# Install: cms completion fish > ~/.config/fish/completions/cms.fish\n\n")

	// Disable file completion by default.
	b.WriteString("complete -c cms -f\n\n")

	// Subcommands.
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		b.WriteString(fmt.Sprintf("complete -c cms -n '__fish_use_subcommand' -a %s -d %q\n", c.Name, c.Short))
	}
	b.WriteString("\n")

	// Short flags.
	for _, c := range commands {
		for _, a := range c.Aliases {
			if strings.HasPrefix(a, "-") && len(a) == 2 {
				b.WriteString(fmt.Sprintf("complete -c cms -n '__fish_use_subcommand' -s %s -d %q\n", a[1:], c.Short))
			}
		}
	}
	b.WriteString("\n")

	// Dynamic branch/worktree completion for go, add, rm, merge.
	b.WriteString("# Dynamic branch completion for worktree commands.\n")
	branchCmds := []string{"go", "add", "rm", "merge"}
	for _, cmd := range branchCmds {
		// Complete with existing worktree branches.
		b.WriteString(fmt.Sprintf(
			"complete -c cms -n '__fish_seen_subcommand_from %s' -a '(git worktree list --porcelain 2>/dev/null | string match \"branch *\" | string replace \"branch refs/heads/\" \"\")'\n",
			cmd,
		))
		// Complete with local branches.
		b.WriteString(fmt.Sprintf(
			"complete -c cms -n '__fish_seen_subcommand_from %s' -a '(git branch --format=\"%%(refname:short)\" 2>/dev/null)'\n",
			cmd,
		))
	}
	b.WriteString("\n")

	// Dynamic mark label completion for jump.
	b.WriteString("# Dynamic mark completion for jump.\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from jump' -a '(test -f ~/.config/cms/marks.json; and string match -r '\"(\\w+)\"' < ~/.config/cms/marks.json | string replace -r '\"(\\w+)\".*' '$1')'\n")
	b.WriteString("\n")

	// Flags for specific commands.
	b.WriteString("# Command-specific flags.\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from add' -l no-open -d 'Don\\'t open tmux window'\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from rm' -s f -l force -d 'Force removal'\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from rm' -l keep-branch -d 'Keep the branch after removal'\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from merge' -l no-delete -d 'Don\\'t delete worktree after merge'\n")
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from merge' -l squash -d 'Squash commits before merging'\n")
	b.WriteString("\n")

	// Completion for 'config' subcommand.
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from config' -a init -d 'Write default config file'\n")

	// Completion for 'completion' subcommand.
	b.WriteString("complete -c cms -n '__fish_seen_subcommand_from completion' -a 'fish bash zsh' -d 'Shell type'\n")

	return b.String()
}

func completionBash() string {
	var b strings.Builder
	b.WriteString("# cms bash completions — generated by 'cms completion bash'\n")
	b.WriteString("# Install: eval \"$(cms completion bash)\" or add to ~/.bashrc\n\n")

	// Collect command names.
	var names []string
	for _, c := range commands {
		if !c.Hidden {
			names = append(names, c.Name)
		}
	}

	b.WriteString(`_cms() {
    local cur prev words cword
    _init_completion || return

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "` + strings.Join(names, " ") + `" -- "$cur"))
        return
    fi

    case "${words[1]}" in
        go|add|rm|merge)
            # Complete with branches and worktree branches.
            local branches
            branches=$(git branch --format="%(refname:short)" 2>/dev/null)
            branches+=$'\n'$(git worktree list --porcelain 2>/dev/null | grep '^branch ' | sed 's|branch refs/heads/||')
            COMPREPLY=($(compgen -W "$branches" -- "$cur"))
            ;;
        jump)
            # Complete with mark labels.
            if [[ -f ~/.config/cms/marks.json ]]; then
                local labels
                labels=$(grep -oP '"[^"]+"\s*:' ~/.config/cms/marks.json | tr -d '":' | tr -s ' ')
                COMPREPLY=($(compgen -W "$labels" -- "$cur"))
            fi
            ;;
        config)
            COMPREPLY=($(compgen -W "init" -- "$cur"))
            ;;
        completion)
            COMPREPLY=($(compgen -W "fish bash zsh" -- "$cur"))
            ;;
    esac
}

complete -F _cms cms
`)
	return b.String()
}

func completionZsh() string {
	var b strings.Builder
	b.WriteString("# cms zsh completions — generated by 'cms completion zsh'\n")
	b.WriteString("# Install: cms completion zsh > ~/.zfunc/_cms && fpath+=(~/.zfunc) && compinit\n\n")

	b.WriteString(`#compdef cms

_cms() {
    local -a subcmds
    subcmds=(
`)
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		// Escape single quotes in descriptions.
		desc := strings.ReplaceAll(c.Short, "'", "'\\''")
		b.WriteString(fmt.Sprintf("        '%s:%s'\n", c.Name, desc))
	}
	b.WriteString(`    )

    if (( CURRENT == 2 )); then
        _describe 'command' subcmds
        return
    fi

    case "${words[2]}" in
        go|add|rm|merge)
            local -a branches
            branches=(${(f)"$(git branch --format='%(refname:short)' 2>/dev/null)"})
            branches+=(${(f)"$(git worktree list --porcelain 2>/dev/null | grep '^branch ' | sed 's|branch refs/heads/||')"})
            compadd -a branches
            ;;
        jump)
            if [[ -f ~/.config/cms/marks.json ]]; then
                local -a labels
                labels=(${(f)"$(grep -oP '"[^"]+"\s*:' ~/.config/cms/marks.json | tr -d '\":' | tr -s ' ')"})
                compadd -a labels
            fi
            ;;
        config)
            compadd init
            ;;
        completion)
            compadd fish bash zsh
            ;;
    esac
}

_cms
`)
	return b.String()
}
