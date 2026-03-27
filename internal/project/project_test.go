package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/serge/cms/internal/config"
)

func TestScanSkipsSubmodulesByDefault(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	submodule := filepath.Join(root, "submodule")

	mustMkdirAll(t, filepath.Join(parent, ".git"))
	mustWriteFile(t, filepath.Join(submodule, ".git"), "gitdir: ../.git/modules/submodule\n")

	cfg := config.DefaultConfig()
	cfg.General.SearchPaths = []config.SearchPath{{Path: root, MaxDepth: 2}}

	projects := Scan(cfg)
	if containsProjectPath(projects, submodule) {
		t.Fatalf("submodule path %q should be skipped by default", submodule)
	}
	if !containsProjectPath(projects, parent) {
		t.Fatalf("normal repo path %q should be discovered", parent)
	}
}

func TestScanIncludesSubmodulesWhenEnabled(t *testing.T) {
	root := t.TempDir()
	submodule := filepath.Join(root, "submodule")
	mustWriteFile(t, filepath.Join(submodule, ".git"), "gitdir: ../.git/modules/submodule\n")

	cfg := config.DefaultConfig()
	cfg.General.SearchPaths = []config.SearchPath{{Path: root, MaxDepth: 2}}
	cfg.General.SearchSubmodules = true

	projects := Scan(cfg)
	if !containsProjectPath(projects, submodule) {
		t.Fatalf("submodule path %q should be discovered when enabled", submodule)
	}
}

func TestScanRespectsExclusions(t *testing.T) {
	root := t.TempDir()

	// visible/repo and archive/hidden — both are git repos.
	mustMkdirAll(t, filepath.Join(root, "visible", ".git"))
	mustMkdirAll(t, filepath.Join(root, "archive", "hidden", ".git"))

	cfg := config.DefaultConfig()
	cfg.General.SearchPaths = []config.SearchPath{{
		Path:       root,
		MaxDepth:   3,
		Exclusions: []string{"archive"},
	}}

	projects := Scan(cfg)
	if !containsProjectPath(projects, filepath.Join(root, "visible")) {
		t.Fatalf("visible repo should be discovered")
	}
	if containsProjectPath(projects, filepath.Join(root, "archive", "hidden")) {
		t.Fatalf("repo under excluded dir should not be discovered")
	}
}

func TestScanExclusionsNormalizeGlobSuffix(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "archive", "hidden", ".git"))

	for _, pattern := range []string{"archive", "archive/*", "archive/**"} {
		cfg := config.DefaultConfig()
		cfg.General.SearchPaths = []config.SearchPath{{
			Path:       root,
			MaxDepth:   3,
			Exclusions: []string{pattern},
		}}

		projects := Scan(cfg)
		if containsProjectPath(projects, filepath.Join(root, "archive", "hidden")) {
			t.Errorf("pattern %q should exclude archive/hidden", pattern)
		}
	}
}

func containsProjectPath(projects []Project, path string) bool {
	for _, p := range projects {
		if p.Path == path {
			return true
		}
	}
	return false
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
