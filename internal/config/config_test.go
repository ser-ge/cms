package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigTOML(t *testing.T) {
	data, err := DefaultConfigTOML()
	if err != nil {
		t.Fatalf("DefaultConfigTOML: %v", err)
	}
	text := string(data)
	for _, section := range []string{"[general]", "[dashboard]", "[finder]", "[icons]", "[worktree]"} {
		if !strings.Contains(text, section) {
			t.Fatalf("default config missing %s: %q", section, text)
		}
	}
}

func TestWriteDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := WriteDefaultConfigFile()
	if err != nil {
		t.Fatalf("WriteDefaultConfigFile: %v", err)
	}
	want := filepath.Join(dir, "cms", "config.toml")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "[general]") {
		t.Fatalf("written config missing [general]: %q", string(data))
	}

	_, err = WriteDefaultConfigFile()
	if !os.IsExist(err) {
		t.Fatalf("second WriteDefaultConfigFile error = %v, want os.ErrExist", err)
	}
}
