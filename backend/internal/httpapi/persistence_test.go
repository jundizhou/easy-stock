package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictPersistenceReportsInvalidSettings(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write invalid settings: %v", err)
	}

	server := NewServer(Config{SettingsPath: settingsPath, StrictPersistence: true})
	t.Cleanup(func() { _ = server.Close() })
	if err := server.StartupError(); err == nil || !strings.Contains(err.Error(), "open settings") {
		t.Fatalf("StartupError() = %v, want settings persistence error", err)
	}
}

func TestStrictPersistenceReportsInvalidDatabasePaths(t *testing.T) {
	invalidDatabasePath := t.TempDir()
	server := NewServer(Config{
		ReviewDBPath:        invalidDatabasePath,
		MarketEmotionDBPath: invalidDatabasePath,
		ThemeRadarDBPath:    invalidDatabasePath,
		StrictPersistence:   true,
	})
	t.Cleanup(func() { _ = server.Close() })

	err := server.StartupError()
	if err == nil {
		t.Fatal("StartupError() = nil, want database persistence errors")
	}
	for _, message := range []string{"review database", "market emotion database", "theme radar database"} {
		if !strings.Contains(err.Error(), message) {
			t.Errorf("StartupError() = %q, want %q", err, message)
		}
	}
}

func TestNonStrictPersistenceKeepsInMemoryFallback(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write invalid settings: %v", err)
	}

	server := NewServer(Config{SettingsPath: settingsPath})
	t.Cleanup(func() { _ = server.Close() })
	if err := server.StartupError(); err != nil {
		t.Fatalf("StartupError() = %v, want nil for compatibility fallback", err)
	}
}
