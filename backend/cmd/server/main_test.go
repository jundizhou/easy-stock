package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
)

func TestHermesRuntimeDiscoveryChecksBundledSource(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("A_STOCK_HERMES_RUNTIME_ROOT", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(cwd, "desktop", "resources", "hermes-runtime")
	cache := filepath.Join(cwd, ".runtime", "hermes-runtime-0.22.5")
	for _, file := range []string{
		agent.BundledHermesPython(checkout),
		agent.BundledHermesPython(cache),
		filepath.Join(cache, "source", "providers", "__init__.py"),
		filepath.Join(cache, "source", "hermes_cli", "__init__.py"),
		filepath.Join(cache, "source", "tui_gateway", "entry.py"),
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got, resources := resolveHermesRuntimeRoot(); got != cache || resources != filepath.Dir(checkout) {
		t.Fatalf("runtime root and resources = %q, %q, want %q, %q", got, resources, cache, filepath.Dir(checkout))
	}
	t.Setenv("A_STOCK_HERMES_RUNTIME_ROOT", checkout)
	if got, _ := resolveHermesRuntimeRoot(); got != checkout {
		t.Fatalf("configured runtime root = %q, want %q", got, checkout)
	}
	status := agent.NewHermesRuntime(agent.HermesConfig{RuntimeRoot: checkout}).Status()
	if status.Available || !strings.Contains(status.Message, "ensure-hermes-runtime.mjs") {
		t.Fatalf("incomplete runtime status = %+v, want repair instructions", status)
	}
}

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
