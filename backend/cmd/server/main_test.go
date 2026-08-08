package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferredDataDirUsesLegacyWhenRenamedDirectoryIsUnconfigured(t *testing.T) {
	configDir := t.TempDir()
	legacy := filepath.Join(configDir, "a-stock-ai")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := preferredDataDir(configDir); got != legacy {
		t.Fatalf("preferredDataDir() = %q, want %q", got, legacy)
	}
}

func TestPreferredDataDirUsesRenamedDirectoryWhenConfigured(t *testing.T) {
	configDir := t.TempDir()
	current := filepath.Join(configDir, "easy-stock")
	legacy := filepath.Join(configDir, "a-stock-ai")
	for _, directory := range []string{current, legacy} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "settings.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := preferredDataDir(configDir); got != current {
		t.Fatalf("preferredDataDir() = %q, want %q", got, current)
	}
}
