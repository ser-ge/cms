package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/mark"
	"github.com/serge/cms/internal/session"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/tui"
	"github.com/serge/cms/internal/watcher"
	"github.com/serge/cms/internal/worktree"
)

// version is set at build time via ldflags.
var version = "dev"


func main() {
	initDebugLogger()

	args := os.Args[1:]

	// Version flag — runs before config loading.
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-V") {
		fmt.Println("cms", version)
		return
	}

	// Internal commands (e.g. hook forwarding) must run before config loading.
	// Hook commands are called by Claude Code and must never fail due to
	// config validation errors — they must be unconditionally reliable.
	if len(args) >= 1 && args[0] == "internal" {
		exitIfErr(runInternal(args[1:]))
		return
	}

	cfg, err := config.Load()
	if err != nil {
		exitErr(err)
	}
	tui.InitStyles(cfg)

	initial := tui.ScreenFinder
	sections := cfg.Finder.Include // default: config-driven

	// Global help.
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Print(renderHelp())
		return
	}

	// Shell completion.
	if len(args) > 0 && args[0] == "completion" {
		exitIfErr(runCompletion(args[1:]))
		return
	}

	// Per-command help: cms <command> --help
	if len(args) >= 2 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") {
		fmt.Print(renderCommandHelp(args[0]))
		return
	}

	var plainMode, watchMode bool

	// Parse short and long flags before subcommand.
	// Short flags compose: -s = sessions, -swa = sessions+worktrees+agents.
	// Long flags: --plain, --watch (can be mixed with short flags in any order).
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if strings.HasPrefix(args[0], "--") {
			switch args[0] {
			case "--plain":
				plainMode = true
			case "--watch":
				watchMode = true
			default:
				exitErr(fmt.Errorf("%s", unknownFlagMsg(args[0])))
			}
			args = args[1:]
			continue
		}
		if len(args[0]) > 1 {
			parsed := parseShortFlags(args[0][1:])
			if parsed == nil {
				exitErr(fmt.Errorf("%s", unknownFlagMsg(args[0])))
			}
			sections = parsed
			args = args[1:]
			continue
		}
		break
	}

	if len(args) > 0 {
		switch args[0] {

		// Views.
		case "dash":
			initial = tui.ScreenDashboard

		// Headless navigation.
		case "next":
			secs := sections
			if len(secs) == 0 {
				secs = cfg.Finder.Include
			}
			exitIfErr(jumpNext(secs, cfg))
			return
		case "mark":
			exitIfErr(runMark(args[1:]))
			return
		case "jump":
			exitIfErr(runJump(args[1:]))
			return

		// Worktree operations (top-level).
		case "switch", "add":
			exitIfErr(worktree.RunSwitch(args[1:]))
			return
		case "go":
			exitIfErr(worktree.RunGo(args[1:]))
			return
		case "rm":
			exitIfErr(worktree.RunRemove(args[1:]))
			return
		case "land", "merge":
			exitIfErr(worktree.Land(args[1:]))
			return
		case "ls":
			exitIfErr(worktree.RunList())
			return

		// Config.
		case "config":
			if len(args) > 1 {
				switch args[1] {
				case "init":
					path, err := config.WriteDefaultConfigFile()
					if err != nil {
						if err == os.ErrExist {
							exitErr(fmt.Errorf("config already exists at %s", path))
						}
						exitErr(err)
					}
					fmt.Println(path)
					return
				case "default":
					data, err := config.DefaultConfigTOML()
					exitIfErr(err)
					os.Stdout.Write(data)
					return
				}
			}
			exitErr(fmt.Errorf("usage: cms config {init|default}"))

		// TUI screens.
		case "new":
			initial = tui.ScreenNewWorktree
		case "hook-print":
			hook.RunSetup()
			return
		case "hook-install":
			exitIfErr(hook.RunInstall())
			return
		case "hook-uninstall":
			exitIfErr(hook.RunUninstall())
			return

		// "internal" is handled before config load (see top of main).
		case "session":
			if len(os.Args) > 2 && os.Args[2] == "save" {
				target, err := tmux.FetchCurrentTarget()
				if err != nil || target.Session == "" {
					fmt.Fprintln(os.Stderr, "error: no tmux session to save")
					os.Exit(1)
				}
				// Resolve repo root from the current pane's working directory.
				repoRoot, err := worktree.FindRepoRoot(".")
				if err != nil {
					fmt.Fprintln(os.Stderr, "error: not inside a git repository")
					os.Exit(1)
				}
				if err := session.SaveSnapshot(target.Session, repoRoot); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "saved snapshot for session %q\n", target.Session)
				return
			}
			fmt.Fprintln(os.Stderr, "usage: cms session save")
			os.Exit(1)

		default:
			// Try to parse as comma-separated section names (e.g. "sessions,worktrees").
			parsed := parseSections(args[0])
			if parsed != nil {
				sections = parsed
				// Check for trailing --plain/--watch after section name.
				for _, a := range args[1:] {
					switch a {
					case "--plain":
						plainMode = true
					case "--watch":
						watchMode = true
					default:
						exitErr(fmt.Errorf("%s", unknownFlagMsg(a)))
					}
				}
			} else {
				exitErr(fmt.Errorf("%s", unknownCommandMsg(args[0])))
			}
		}
	}

	// Plain/watch mode: headless finder with text output.
	if plainMode || watchMode {
		runPlainMode(sections, cfg, watchMode)
		return
	}

	w := watcher.New()
	w.ApplyConfig(cfg.General)
	w.BootstrapSync() // pre-fill CachedState so finder has sessions+agents on first render
	m := tui.NewRootModel(initial, sections, cfg, w)
	p := tea.NewProgram(m, tea.WithAltScreen())
	w.Start(p.Send)
	result, err := p.Run()
	w.Stop()
	session.SaveAllSnapshots() // best-effort, errors logged
	if err != nil {
		exitErr(err)
	}
	if rm, ok := result.(tui.RootModel); ok && rm.PostAction() != nil {
		exitIfErr(executePostAction(rm.PostAction(), cfg))
	}
}

// shortFlagMap maps single-letter flags to section names.
var shortFlagMap = map[byte]string{
	's': "sessions",
	'p': "projects",
	'a': "agents",
	'm': "marks",
	'w': "worktrees",
	'b': "branches",
	'W': "windows",
	'P': "panes",
}

// parseShortFlags expands a string of single-letter flags into section names.
// Returns nil if any letter is unknown. E.g. "swa" → ["sessions","worktrees","agents"].
func parseShortFlags(letters string) []string {
	seen := map[string]bool{}
	var result []string
	for i := 0; i < len(letters); i++ {
		section, ok := shortFlagMap[letters[i]]
		if !ok {
			return nil
		}
		if !seen[section] {
			seen[section] = true
			result = append(result, section)
		}
	}
	return result
}

// parseSections splits a comma-separated string into section names.
// Returns nil if any name is not a valid section.
func parseSections(arg string) []string {
	valid := make(map[string]bool, len(tui.ValidSections))
	for _, s := range tui.ValidSections {
		valid[s] = true
	}

	parts := strings.Split(arg, ",")
	for _, p := range parts {
		if !valid[p] {
			return nil
		}
	}
	return parts
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func exitIfErr(err error) {
	if err != nil {
		exitErr(err)
	}
}

func executePostAction(a *tui.PostAction, cfg config.Config) error {
	// Direct pane switch (from dashboard, agents queue, pane picker, marks).
	if a.PaneID != "" {
		return session.SwitchToPane(a.PaneID)
	}
	switch a.Kind {
	case tui.KindSession:
		sessions, pt, err := tmux.FetchState()
		if err != nil {
			return session.Switch(a.SessionName)
		}
		agents := agent.DetectAll(sessions, pt)
		return session.SmartSwitch(a.SessionName, a.Priority, sessions, agents)
	case tui.KindProject:
		return session.OpenProject(a.ProjectPath, cfg.General.ShouldRestore())
	case tui.KindWorktree:
		if a.BranchName != "" {
			return createWorktreeFromTUI(a.BranchName)
		}
		return switchToWorktreeWindow(a.WorktreePath, a.WorktreeBranch)
	}
	return nil
}

// switchToWorktreeWindow finds an existing tmux window for the worktree, or creates one.
func switchToWorktreeWindow(wtPath, branch string) error {
	sessions, _, err := tmux.FetchState()
	if err == nil {
		for _, sess := range sessions {
			for _, win := range sess.Windows {
				for _, pane := range win.Panes {
					if strings.HasPrefix(pane.WorkingDir, wtPath) {
						return session.SwitchToPane(pane.ID)
					}
				}
			}
		}
	}
	target, err := tmux.FetchCurrentTarget()
	if err != nil {
		return fmt.Errorf("not inside tmux")
	}
	windowName := worktree.SanitizeBranch(branch)
	_, err = tmux.Run("new-window", "-a", "-t", target.Session, "-n", windowName, "-c", wtPath)
	return err
}

// --- Headless commands ---

// runMark implements `cms mark <label> [pane]`.
func runMark(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cms mark <label> [pane-id]")
	}
	label := args[0]

	var paneID, sessName, winName string
	if len(args) > 1 {
		paneID = args[1]
	} else {
		// Resolve from current tmux pane.
		current, err := tmux.FetchCurrentTarget()
		if err != nil {
			return fmt.Errorf("cannot determine current pane (not in tmux?)")
		}
		sessions, _, err := tmux.FetchState()
		if err != nil {
			return err
		}
		for _, sess := range sessions {
			if sess.Name != current.Session {
				continue
			}
			sessName = sess.Name
			for _, win := range sess.Windows {
				if win.Index != current.Window {
					continue
				}
				winName = win.Name
				for _, pane := range win.Panes {
					if pane.Index == current.Pane {
						paneID = pane.ID
					}
				}
			}
		}
		if paneID == "" {
			return fmt.Errorf("cannot find current pane")
		}
	}

	if err := mark.Set(label, mark.Mark{
		PaneID:  paneID,
		Session: sessName,
		Window:  winName,
	}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "marked %s → %s (%s:%s)\n", label, paneID, sessName, winName)
	return nil
}

// runJump implements `cms jump <label>`.
func runJump(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cms jump <label>")
	}
	label := args[0]

	sessions, _, err := tmux.FetchState()
	if err != nil {
		return err
	}
	m, alive, err := mark.Resolve(label, sessions)
	if err != nil {
		return err
	}
	if m.PaneID == "" {
		return fmt.Errorf("mark %q not found", label)
	}
	if !alive {
		return fmt.Errorf("mark %q points to dead pane %s", label, m.PaneID)
	}
	return session.SwitchToPane(m.PaneID)
}

// createWorktreeFromTUI creates a new worktree using go semantics (auto-create from base_branch).
// Called from the TUI "new worktree" screen's PostAction.
func createWorktreeFromTUI(branch string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := worktree.FindRepoRoot(cwd)
	if err != nil {
		return err
	}
	return worktree.GoWorktree(root, branch, worktree.SwitchOpts{})
}

// runInternal dispatches hidden internal commands.
func runInternal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cms internal <hook|refresh>")
	}
	switch args[0] {
	case "hook":
		return hook.RunCmd(args[1:])
	case "refresh":
		var name string
		if len(args) > 1 {
			name = args[1]
		}
		return session.RefreshWorktrees(name)
	default:
		return fmt.Errorf("unknown internal command: %s", args[0])
	}
}

// jumpNext uses the same build+sort pipeline as the finder to pick the first
// navigable item, then executes the corresponding action (switch to pane,
// session, worktree, etc.). Flags mirror `cms` — same sections, same sort.
func jumpNext(sections []string, cfg config.Config) error {
	w := watcher.New()
	w.ApplyConfig(cfg.General)
	w.BootstrapSync()

	h := tui.RunHeadless(cfg, w, sections)

	current, _ := tmux.FetchCurrentTarget()
	sessions, _, _ := w.CachedState()
	currentPaneID := resolveCurrentPaneID(sessions, current)

	action := h.FirstAction(currentPaneID, current.Session, cfg.General.SwitchPriority)
	if action == nil {
		return fmt.Errorf("no items to navigate to")
	}
	return executePostAction(action, cfg)
}

// resolveCurrentPaneID finds the pane ID (e.g. %7) for the current target.
func resolveCurrentPaneID(sessions []tmux.Session, current tmux.CurrentTarget) string {
	for _, sess := range sessions {
		if sess.Name != current.Session {
			continue
		}
		for _, win := range sess.Windows {
			if win.Index != current.Window {
				continue
			}
			for _, pane := range win.Panes {
				if pane.Index == current.Pane {
					return pane.ID
				}
			}
		}
	}
	return ""
}
