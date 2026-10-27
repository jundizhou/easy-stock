package monitor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPreferencesPersistIndependentlyAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := OpenPreferences(store)
	if err != nil {
		t.Fatal(err)
	}
	if got := prefs.Snapshot(); got.Settings.Enabled || got.NotifyEnabled {
		t.Fatalf("new monitor must stay disabled: %+v", got)
	}
	saved, err := prefs.Update(context.Background(), func(next *Preferences) {
		next.Settings = Settings{Enabled: true, QuoteIntervalSec: 100, SnapshotIntervalSec: 1, DigestSeconds: 1, QuietHours: "invalid"}
		next.NotifyEnabled = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Settings.QuoteIntervalSec != 60 || saved.Settings.SnapshotIntervalSec != 10 || saved.Settings.DigestSeconds != 30 || saved.Settings.QuietHours != "" {
		t.Fatalf("settings were not normalized: %+v", saved)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := OpenPreferences(reopened)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.Snapshot(); got != saved {
		t.Fatalf("restart lost monitor preferences: got %+v, want %+v", got, saved)
	}
}

func TestPreferencesWriteFailureKeepsLastCommittedState(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := OpenPreferences(store)
	if err != nil {
		t.Fatal(err)
	}
	before := prefs.Snapshot()
	_ = store.Close()
	if _, err := prefs.Update(context.Background(), func(next *Preferences) { next.Settings.Enabled = true }); err == nil {
		t.Fatal("closed database write must fail")
	}
	if got := prefs.Snapshot(); got != before {
		t.Fatalf("failed persistence changed active preferences: %+v", got)
	}
}

func TestPreferencesCorruptStorageDoesNotSilentlyEnable(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := OpenPreferences(store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO monitor_preferences(id,value) VALUES(1,'invalid-json')`); err != nil {
		t.Fatal(err)
	}
	if prefs, err := OpenPreferences(store); err == nil || prefs != nil {
		t.Fatalf("corrupt storage must report unavailable: %+v %v", prefs, err)
	}
}
