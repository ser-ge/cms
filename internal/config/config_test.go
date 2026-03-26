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
	if !strings.Contains(text, "[general]") {
		t.Fatalf("default config missing [general]: %q", text)
	}
	if !strings.Contains(text, "[finder]") {
		t.Fatalf("default config missing [finder]: %q", text)
	}
	if strings.Contains(text, "[dashboard]") || strings.Contains(text, "[icons]") {
		t.Fatalf("default config should only expose [general] and [finder], got %q", text)
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
