package proc

import (
	"os/exec"
	"strconv"
	"strings"
)

// Entry is a single row from `ps`.
type Entry struct {
	PID  int
	PPID int
	Comm string // real binary name from the kernel, not argv[0]
	Args string // full command line (argv)
}

// Table maps PID to Entry and tracks parent-to-children relationships.
type Table struct {
	Procs    map[int]Entry
	Children map[int][]int
}

// BuildTable runs `ps` once and builds a lookup table.
func BuildTable() Table {
	pt := Table{
		Procs:    map[int]Entry{},
		Children: map[int][]int{},
	}

	cmd := exec.Command("ps", "-eo", "pid,ppid,comm,args")
	out, err := cmd.Output()
	if err != nil {
		return pt
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		comm := fields[2]
		args := strings.Join(fields[3:], " ")
		// Strip leading dash from login shells (e.g. "-fish" -> "fish")
		comm = strings.TrimLeft(comm, "-")
		// Take basename in case it's a full path
		if idx := strings.LastIndex(comm, "/"); idx >= 0 {
			comm = comm[idx+1:]
		}

		pt.Procs[pid] = Entry{PID: pid, PPID: ppid, Comm: comm, Args: args}
		pt.Children[ppid] = append(pt.Children[ppid], pid)
	}

	return pt
}

// ResolveCommand figures out the real command running in a pane.
// The pane PID is typically a shell. We find its first child (the foreground job)
// and return that child's real binary name from ps.
// Falls back to tmuxCmd if we can't resolve.
func ResolveCommand(pt Table, panePID int, tmuxCmd string) string {
	entry, ok := pt.Procs[panePID]
	if !ok {
		return tmuxCmd
	}

	if !IsShellCommand(entry.Comm) {
		return entry.Comm
	}

	kids := pt.Children[panePID]
	if len(kids) == 0 {
		return entry.Comm
	}

	if child, ok := pt.Procs[kids[0]]; ok {
		return child.Comm
	}

	return tmuxCmd
}

// IsShellCommand returns true if cmd is a known shell name.
func IsShellCommand(cmd string) bool {
	switch cmd {
	case "fish", "bash", "zsh", "sh", "dash", "tcsh", "ksh":
		return true
	default:
		return false
	}
}

// FindInTree walks a pane's process tree and returns the first matching descendant.
func FindInTree(pt Table, panePID int, match func(Entry) bool, extractArgs func(string) string) (bool, string) {
	queue := []int{panePID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, childPID := range pt.Children[current] {
			child := pt.Procs[childPID]
			if match(child) {
				if extractArgs == nil {
					return true, ""
				}
				return true, extractArgs(child.Args)
			}
			queue = append(queue, childPID)
		}
	}
	return false, ""
}

// ExtractArgsAfterBinary strips the binary name from a full command line
// and returns just the arguments.
func ExtractArgsAfterBinary(fullArgs string) string {
	parts := strings.Fields(fullArgs)
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[1:], " ")
}
