package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
}
