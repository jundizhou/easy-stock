package appsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// assertPrivatePermissions 校验 settings.json 未被组/其他用户读写。
//
// Windows 不实现 POSIX 权限位：os.Chmod 仅切换「只读」开关，os.FileMode.Perm()
// 对任何可写文件恒为 0666。因此这里在 Windows 上只确认文件未被误设为只读，
// 在类 Unix 上才严格断言 0600（生产环境实际运行于 Windows，Unix 分支用于 CI/容器）。
func assertPrivatePermissions(t *testing.T, info os.FileInfo) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if info.Mode().Perm()&0o200 == 0 {
			t.Fatalf("settings file unexpectedly read-only on windows: %v", info.Mode())
		}
		return
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions = %o, want 600", info.Mode().Perm())
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
	assertPrivatePermissions(t, info)
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
