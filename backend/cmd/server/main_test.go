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

func TestBackendSelfURLNormalizesWildcardHosts(t *testing.T) {
	// 监听地址常写成通配形式，子进程无法直连，必须补成 127.0.0.1。
	cases := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:20081", "http://127.0.0.1:20081"},
		{":20081", "http://127.0.0.1:20081"},
		{"0.0.0.0:20089", "http://127.0.0.1:20089"},
		{"192.168.1.11:20081", "http://192.168.1.11:20081"},
		{"localhost:20081", "http://localhost:20081"},
	}
	for _, item := range cases {
		if got := backendSelfURL(item.addr); got != item.want {
			t.Fatalf("backendSelfURL(%q) = %q, want %q", item.addr, got, item.want)
		}
	}
}

func TestBackendSelfURLPrefersExplicitOverride(t *testing.T) {
	t.Setenv("A_STOCK_SELF_URL", "https://stock.example.com/")
	if got := backendSelfURL("127.0.0.1:20081"); got != "https://stock.example.com" {
		t.Fatalf("backendSelfURL() = %q, want 显式覆盖值且去掉尾斜杠", got)
	}
}

func TestBackendSelfURLFallsBackOnUnparsableAddress(t *testing.T) {
	t.Setenv("A_STOCK_SELF_URL", "")
	if got := backendSelfURL("not-an-address"); got != "http://127.0.0.1:20081" {
		t.Fatalf("backendSelfURL() = %q, want 默认回退地址", got)
	}
}
