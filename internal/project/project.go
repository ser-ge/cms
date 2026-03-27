package project

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/git"
)

// Project represents a discovered git repository.
type Project struct {
	Name string   // display name (may include parent dir for dedup)
	Path string   // absolute path
	Git  git.Info // branch, dirty, etc.
}

// Scan walks the configured search paths and returns all git repositories found.
// Uses BFS with depth limiting, respects per-path exclusion lists.
func Scan(cfg config.Config) []Project {
	type searchEntry struct {
		path  string
		depth int
	}

	var repoPaths []string
	seen := map[string]bool{}

	for _, sp := range cfg.General.SearchPaths {
		excluded := buildExclusionSet(sp.Exclusions)
		queue := []searchEntry{{path: sp.Path, depth: sp.MaxDepth}}

		for len(queue) > 0 {
			entry := queue[0]
			queue = queue[1:]

			if seen[entry.path] {
				continue
			}
			seen[entry.path] = true

			// Check if this directory is a git repo.
			gitDir := filepath.Join(entry.path, ".git")
			if info, err := os.Lstat(gitDir); err == nil {
				if info.IsDir() {
					// Normal repo (.git is a directory).
					repoPaths = append(repoPaths, entry.path)
				} else if git.IsWorktreeCheckout(entry.path) {
					// Linked worktree checkout — skip, main repo will enumerate it.
				} else if cfg.General.SearchSubmodules {
					// Submodule checkout (.git is a file) — optionally include.
					repoPaths = append(repoPaths, entry.path)
				}
				continue
			}

			// Check for bare repo (HEAD + refs/ at top level, no .git).
			headFile := filepath.Join(entry.path, "HEAD")
			refsDir := filepath.Join(entry.path, "refs")
			if _, err := os.Stat(headFile); err == nil {
				if info, err := os.Stat(refsDir); err == nil && info.IsDir() {
					repoPaths = append(repoPaths, entry.path)
					continue // don't recurse into bare repos
				}
			}

			if entry.depth <= 0 {
				continue
			}

			// Read children and enqueue.
			entries, err := os.ReadDir(entry.path)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if excluded[name] || name[0] == '.' {
					continue
				}
				queue = append(queue, searchEntry{
					path:  filepath.Join(entry.path, name),
					depth: entry.depth - 1,
				})
			}
		}
	}

	// Build projects with deduped names. Skip git info for speed —
	// the picker only needs name + path.
	projects := make([]Project, 0, len(repoPaths))
	for _, p := range repoPaths {
		projects = append(projects, Project{
			Name: filepath.Base(p),
			Path: p,
		})
	}

	deduplicateNames(projects)

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Name < projects[j].Name
	})

	return projects
}

// deduplicateNames appends parent directory components to disambiguate
// projects that share the same basename.
func deduplicateNames(projects []Project) {
	// Group by name.
	groups := map[string][]int{}
	for i, p := range projects {
		groups[p.Name] = append(groups[p.Name], i)
	}

	for _, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		// Add parent dir components until all names are unique.
		for depth := 2; depth <= 5; depth++ {
			names := map[string]bool{}
			allUnique := true
			for _, idx := range indices {
				parts := splitPath(projects[idx].Path)
				start := len(parts) - depth
				if start < 0 {
					start = 0
				}
				name := filepath.Join(parts[start:]...)
				if names[name] {
					allUnique = false
					break
				}
				names[name] = true
				projects[idx].Name = name
			}
			if allUnique {
				break
			}
		}
	}
}

// buildExclusionSet normalizes exclusion patterns into a set of directory
// basenames. Trailing glob suffixes (/* or /**) are stripped so that
// "archive", "archive/*", and "archive/**" all exclude the same directory.
func buildExclusionSet(patterns []string) map[string]bool {
	m := make(map[string]bool, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSuffix(p, "/**")
		p = strings.TrimSuffix(p, "/*")
		if p != "" {
			m[p] = true
		}
	}
	return m
}

func splitPath(path string) []string {
	var parts []string
	for path != "/" && path != "." && path != "" {
		parts = append([]string{filepath.Base(path)}, parts...)
		path = filepath.Dir(path)
	}
	return parts
}
