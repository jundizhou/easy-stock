package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Preferences belong to the monitor database, independently of system settings.
type Preferences struct {
	Settings      Settings `json:"settings"`
	NotifyEnabled bool     `json:"notify_enabled"`
}

type PreferencesStore struct {
	mu    sync.RWMutex
	store *Store
	value Preferences
}

func OpenPreferences(store *Store) (*PreferencesStore, error) {
	if store == nil || store.db == nil {
		return nil, errors.New("monitor store unavailable")
	}
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS monitor_preferences (
		id INTEGER PRIMARY KEY CHECK (id = 1), value TEXT NOT NULL
	)`); err != nil {
		return nil, err
	}
	prefs := &PreferencesStore{store: store, value: Preferences{Settings: DefaultSettings()}}
	var payload string
	err := store.db.QueryRow(`SELECT value FROM monitor_preferences WHERE id=1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return prefs, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(payload), &prefs.value); err != nil {
		return nil, fmt.Errorf("decode monitor preferences: %w", err)
	}
	prefs.value.Settings = NormalizeSettings(prefs.value.Settings)
	return prefs, nil
}

func (p *PreferencesStore) Snapshot() Preferences {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.value
}

func (p *PreferencesStore) Update(ctx context.Context, update func(*Preferences)) (Preferences, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	next := p.value
	update(&next)
	next.Settings = NormalizeSettings(next.Settings)
	payload, err := json.Marshal(next)
	if err != nil {
		return p.value, err
	}
	if _, err := p.store.db.ExecContext(ctx, `INSERT INTO monitor_preferences(id,value) VALUES(1,?)
		ON CONFLICT(id) DO UPDATE SET value=excluded.value`, string(payload)); err != nil {
		return p.value, err
	}
	p.value = next
	return next, nil
}

func NormalizeSettings(settings Settings) Settings {
	if settings.QuoteIntervalSec <= 0 {
		settings.QuoteIntervalSec = 3
	}
	settings.QuoteIntervalSec = min(60, max(1, settings.QuoteIntervalSec))
	if settings.SnapshotIntervalSec <= 0 {
		settings.SnapshotIntervalSec = 15
	}
	settings.SnapshotIntervalSec = min(300, max(10, settings.SnapshotIntervalSec))
	if settings.DigestSeconds <= 0 {
		settings.DigestSeconds = 300
	}
	settings.DigestSeconds = min(1800, max(30, settings.DigestSeconds))
	settings.QuietHours = normalizeQuietHours(settings.QuietHours)
	return settings
}

func normalizeQuietHours(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return ""
	}
	for _, part := range parts {
		var hour, minute int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%02d:%02d", &hour, &minute); err != nil || hour < 0 || hour >= 24 || minute < 0 || minute >= 60 {
			return ""
		}
	}
	return parts[0] + "-" + parts[1]
}
