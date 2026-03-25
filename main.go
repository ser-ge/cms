package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
	"github.com/serge/cms/internal/hook"
	"github.com/serge/cms/internal/mark"
	"github.com/serge/cms/internal/session"
	"github.com/serge/cms/internal/tmux"
	"github.com/serge/cms/internal/tui"
	"github.com/serge/cms/internal/watcher"
	"github.com/serge/cms/internal/worktree"
)

type jumpCandidate struct {
	paneID   string
	activity agent.Activity
}

func main() {
	initDebugLogger()
	cfg := config.Load()
	tui.InitStyles(cfg)

	initial := tui.ScreenFinder
	fk := tui.FinderAll

	// Parse flags before subcommand.
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-s":
			fk = tui.FinderSessions
		case "-p":
			fk = tui.FinderProjects
		case "-q":
			fk = tui.FinderQueue
		case "-m":
			fk = tui.FinderMarks
		default:
			exitErr(fmt.Errorf("unknown flag: %s", args[0]))
		}
		args = args[1:]
	}

	if len(args) > 0 {
		switch args[0] {
		// Filtered finders (long forms).
		case "sessions":
			fk = tui.FinderSessions
		case "projects":
			fk = tui.FinderProjects
		case "queue":
			fk = tui.FinderQueue
		case "marks":
			fk = tui.FinderMarks
		case "worktrees":
			fk = tui.FinderWorktrees
		case "panes":
			fk = tui.FinderPanes
		case "windows":
			fk = tui.FinderWindows

		// Views.
		case "dash":
			initial = tui.ScreenDashboard

		// Headless navigation.
		case "next":
			exitIfErr(jumpNext())
			return
		case "mark":
			exitIfErr(runMark(args[1:]))
			return
		case "jump":
			exitIfErr(runJump(args[1:]))
			return

		// Worktree operations (top-level).
		case "go":
			exitIfErr(runGo(args[1:]))
			return
		case "add":
			exitIfErr(worktree.RunAdd(args[1:]))
			return
		case "rm":
			exitIfErr(worktree.RunRemove(args[1:]))
			return
		case "merge":
			exitIfErr(worktree.Merge(args[1:]))
			return
		case "ls":
			exitIfErr(worktree.RunList())
			return

		// Config.
		case "config":
			if len(args) > 1 && args[1] == "init" {
				path, err := config.WriteDefaultConfigFile()
				if err != nil {
					if err == os.ErrExist {
						exitErr(fmt.Errorf("config already exists at %s", path))
					}
					exitErr(err)
				}
				fmt.Println(path)
				return
			}
			exitErr(fmt.Errorf("usage: cms config init"))

		// TUI screens.
		case "new":
			initial = tui.ScreenNewWorktree
		case "hook-setup":
			hook.RunSetup()
			return

		// Internal (hidden from help).
		case "internal":
			exitIfErr(runInternal(args[1:]))
			return
		}
	}

	w := watcher.New()
	w.ApplyConfig(cfg.General)
	m := tui.NewRootModel(initial, fk, cfg, w)
	p := tea.NewProgram(m, tea.WithAltScreen())
	w.Start(p.Send)
	result, err := p.Run()
	w.Stop()
	if err != nil {
		exitErr(err)
	}
	if rm, ok := result.(tui.RootModel); ok && rm.PostAction() != nil {
		exitIfErr(executePostAction(rm.PostAction()))
	}
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

func executePostAction(a *tui.PostAction) error {
	// Direct pane switch (from dashboard, queue, pane picker, marks).
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
		return session.OpenProject(a.ProjectPath)
	case tui.KindWorktree:
		if a.BranchName != "" {
			return createWorktree(a.BranchName)
		}
		return switchToWorktree(a.WorktreePath, a.WorktreeBranch)
	}
	return nil
}

// switchToWorktree finds an existing tmux window for the worktree, or creates one.
func switchToWorktree(wtPath, branch string) error {
	// Look for a pane whose working dir is inside the worktree.
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
	// No existing window — create one.
	target, err := tmux.FetchCurrentTarget()
	if err != nil {
		return fmt.Errorf("not inside tmux")
	}
	windowName := worktree.SanitizeBranch(branch)
	_, err = tmux.Run("new-window", "-t", target.Session, "-n", windowName, "-c", wtPath)
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

// runGo implements `cms go <branch> [path]`.
func runGo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cms go <branch> [path]")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := worktree.FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	branchArg, err := worktree.ResolveWorktreeSymbol(root, args[0])
	if err != nil {
		return err
	}

	// Check if worktree already exists for this branch.
	wts, err := git.ListWorktrees(root)
	if err != nil {
		return err
	}
	for _, wt := range wts {
		if wt.Branch == branchArg || worktree.SanitizeBranch(wt.Branch) == branchArg ||
			filepath.Base(wt.Path) == branchArg {
			// Worktree exists — switch to it.
			return switchToWorktree(wt.Path, wt.Branch)
		}
	}

	// Worktree doesn't exist — create it.
	var path string
	if len(args) > 1 {
		path = args[1]
	}
	return worktree.AddWorktree(root, branchArg, path, worktree.AddOpts{})
}

// createWorktree creates a new worktree for the given branch name, using the
// configured base branch and base directory. Opens a tmux window for it.
func createWorktree(branch string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := worktree.FindRepoRoot(cwd)
	if err != nil {
		return err
	}

	cfg := config.Load()
	wtCfg := worktree.ResolveWorktreeConfig(root, cwd, &cfg.Worktree)

	// Resolve base branch: config > auto-detect.
	baseBranch := wtCfg.BaseBranch
	if baseBranch == "" {
		baseBranch, err = worktree.DefaultBranch(root)
		if err != nil {
			return fmt.Errorf("cannot determine base branch: %w", err)
		}
	}

	baseDir := worktree.ResolveWorktreeBaseDir(root, &wtCfg)
	path := fmt.Sprintf("%s/%s", baseDir, worktree.SanitizeBranch(branch))

	fmt.Fprintf(os.Stderr, "creating worktree at %s for branch %s (from %s)\n",
		worktree.ShortenHome(path), branch, baseBranch)

	if err := worktree.CreateWorktree(root, path, branch, worktree.CreateWorktreeOpts{
		NewBranch:  true,
		StartPoint: baseBranch,
	}); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	// Run post-create hooks.
	mainWt, _ := worktree.FindMainWorktree(root)
	if len(wtCfg.Hooks) > 0 {
		fmt.Fprintf(os.Stderr, "running %d post-create hooks\n", len(wtCfg.Hooks))
		if err := worktree.RunPostCreateHooks(mainWt, path, wtCfg.Hooks); err != nil {
			fmt.Fprintf(os.Stderr, "warning: hook failed: %v\n", err)
		}
	}

	// Open tmux window and switch to it.
	if os.Getenv("TMUX") != "" {
		windowName := worktree.SanitizeBranch(branch)
		target, err := tmux.FetchCurrentTarget()
		if err == nil {
			tmux.Run("new-window", "-t", target.Session, "-n", windowName, "-c", path)
		}
	}

	return nil
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

// jumpNext finds the next agent pane needing attention and switches to it.
func jumpNext() error {
	sessions, pt, err := tmux.FetchState()
	if err != nil {
		return err
	}
	current, _ := tmux.FetchCurrentTarget()
	agents := agent.DetectAll(sessions, pt)

	var all []jumpCandidate
	currentIdx := -1

	for _, sess := range sessions {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				cs, ok := agents[pane.ID]
				if !ok || !cs.Running {
					continue
				}
				if sess.Name == current.Session && win.Index == current.Window && pane.Index == current.Pane {
					currentIdx = len(all)
				}
				all = append(all, jumpCandidate{paneID: pane.ID, activity: cs.Activity})
			}
		}
	}

	if len(all) == 0 {
		return fmt.Errorf("no agent sessions found")
	}

	if paneID := selectNextPane(all, currentIdx); paneID != "" {
		return session.SwitchToPane(paneID)
	}

	return fmt.Errorf("no waiting or idle agent sessions")
}

func selectNextPane(all []jumpCandidate, currentIdx int) string {
	start := currentIdx + 1
	for _, target := range []agent.Activity{agent.ActivityWaitingInput, agent.ActivityCompleted, agent.ActivityIdle} {
		for i := 0; i < len(all); i++ {
			idx := (start + i) % len(all)
			if all[idx].activity == target {
				return all[idx].paneID
			}
		}
	}
	return ""
}
