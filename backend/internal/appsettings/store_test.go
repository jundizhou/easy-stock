package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAutoPatrolDefaultsAndPersistence(t *testing.T) {
	for _, legacy := range []string{"", `{}`, `{"llm":{"model":"existing-model"}}`, `{"cockpit_patrol_interval_minutes":0}`, `{"cockpit_patrol_interval_minutes":-5}`} {
		t.Run(legacy, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if legacy != "" {
				if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if !store.Snapshot().CockpitAutoPatrolEnabled || store.Snapshot().CockpitPatrolIntervalMinutes != 30 {
				t.Fatal("new and legacy settings should keep automatic patrol enabled with a 30-minute interval")
			}
			originalModel := store.Snapshot().LLM.Model
			for _, enabled := range []bool{false, true} {
				if _, err := store.Update(func(values *Values) error {
					values.CockpitAutoPatrolEnabled = enabled
					values.CockpitPatrolIntervalMinutes = 15
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				store, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := store.Snapshot(); got.CockpitAutoPatrolEnabled != enabled || got.CockpitPatrolIntervalMinutes != 15 || got.LLM.Model != originalModel {
					t.Fatal("automatic patrol setting or existing model changed after restart")
				}
			}
		})
	}
}

func TestStorePersistsSecretsWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	if _, err := store.Update(func(values *Values) error {
		values.LLM.Provider = "openai"
		values.LLM.APIKey = "sk-secret-value"
		values.Credentials.TushareToken = "tushare-secret"
		return nil
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions = %o, want 600", info.Mode().Perm())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen settings: %v", err)
	}
	values := reopened.Snapshot()
	if values.LLM.APIKey != "sk-secret-value" || values.Credentials.TushareToken != "tushare-secret" {
		t.Fatalf("settings did not persist: %+v", values)
	}
}

func TestStoreMigratesSingleLLMToSelectableProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	legacy := map[string]any{"llm": map[string]any{"provider": "custom", "base_url": "https://model.example/v1", "model": "gpt-5.6-sol", "api_mode": "codex_responses"}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	values := store.Snapshot()
	if len(values.LLMProfiles) != 1 || values.LLMProfiles[0].Model != "gpt-5.6-sol" || values.ActiveLLMProfileID != values.LLMProfiles[0].ID {
		t.Fatalf("migration=%+v", values)
	}
	if values.LLM.ResponseTimeoutSeconds != DefaultLLMResponseTimeoutSeconds {
		t.Fatalf("response timeout = %d, want default %d", values.LLM.ResponseTimeoutSeconds, DefaultLLMResponseTimeoutSeconds)
	}
}
