package methodology

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLibraryCachesRemoteDocumentsAndSyncsHermes(t *testing.T) {
	var treeCalls atomic.Int32
	var rawCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tree":
			treeCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"sha":"commit-1","truncated":false,"tree":[{"path":"游资心法/测试游资/深度研读报告.md","type":"blob"},{"path":"游资心法/测试游资/学习笔记.md","type":"blob"}]}`)
		case strings.HasPrefix(r.URL.Path, "/raw/"):
			rawCalls.Add(1)
			if strings.Contains(r.URL.Path, "深度研读报告") {
				fmt.Fprint(w, "# 测试游资深度报告\n\n> 市场永远是对的。\n\n## 龙头战法\n只在情绪启动期观察龙头，仓位管理优先。")
				return
			}
			fmt.Fprint(w, "# 测试游资学习笔记\n\n## 风险控制\n退潮期降低仓位，避免情绪化交易。")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	hermesHome := filepath.Join(t.TempDir(), "hermes")
	library := NewLibrary(Config{
		CacheDir:        cacheDir,
		HermesHome:      hermesHome,
		TreeURL:         upstream.URL + "/tree",
		RawBaseURL:      upstream.URL + "/raw/",
		SourceURL:       upstream.URL + "/source",
		RefreshInterval: time.Hour,
		DisableBuiltin:  true,
	})

	snapshot, err := library.Snapshot(context.Background(), false)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(snapshot.Traders) != 1 || snapshot.Traders[0].Name != "测试游资" || snapshot.KnowledgeStatus != "ready" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Traders[0].Quote != "市场永远是对的。" {
		t.Fatalf("unexpected trader quote: %q", snapshot.Traders[0].Quote)
	}
	if treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("unexpected remote calls tree=%d raw=%d", treeCalls.Load(), rawCalls.Load())
	}
	if _, err := library.Snapshot(context.Background(), false); err != nil {
		t.Fatalf("load cached snapshot: %v", err)
	}
	if treeCalls.Load() != 1 || rawCalls.Load() != 2 {
		t.Fatalf("cache was not reused tree=%d raw=%d", treeCalls.Load(), rawCalls.Load())
	}

	skillPath := filepath.Join(hermesHome, "skills", "trading", skillName, "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(skill), "测试游资") {
		t.Fatalf("Hermes skill not synced: err=%v content=%s", err, skill)
	}
	memory, err := os.ReadFile(filepath.Join(hermesHome, "memories", "MEMORY.md"))
	if err != nil || !strings.Contains(string(memory), skillName) {
		t.Fatalf("Hermes memory index not synced: err=%v content=%s", err, memory)
	}

	contextText, err := library.ContextForPrompt(context.Background(), "测试游资的龙头战法和仓位管理是什么？", 4000)
	if err != nil || !strings.Contains(contextText, "只在情绪启动期观察龙头") || !strings.Contains(contextText, "历史交易经验") {
		t.Fatalf("unexpected prompt context: err=%v context=%s", err, contextText)
	}
}

func TestLibrarySeedsBundledBaselineWithoutNetwork(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer upstream.Close()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	library := NewLibrary(Config{
		CacheDir:        cacheDir,
		HermesHome:      filepath.Join(t.TempDir(), "hermes"),
		TreeURL:         upstream.URL + "/tree",
		RawBaseURL:      upstream.URL + "/raw/",
		RefreshInterval: 24 * time.Hour,
	})
	snapshot, err := library.Snapshot(context.Background(), false)
	if err != nil {
		t.Fatalf("seed bundled snapshot: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("first load unexpectedly requested remote data %d times", calls.Load())
	}
	if len(snapshot.Traders) != 21 || snapshot.KnowledgeStatus != "ready" {
		t.Fatalf("unexpected bundled snapshot: traders=%d status=%s", len(snapshot.Traders), snapshot.KnowledgeStatus)
	}
	if snapshot.Traders[0].Name != "炒股养家" || snapshot.Traders[0].Quote == "" {
		t.Fatalf("bundled trader metadata missing: %+v", snapshot.Traders[0])
	}
	manifestContent, err := os.ReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil || !strings.Contains(string(manifestContent), `"version": 4`) {
		t.Fatalf("bundled manifest not persisted: err=%v content=%s", err, manifestContent)
	}
	if files, err := os.ReadDir(filepath.Join(cacheDir, "documents")); err != nil || len(files) != 42 {
		t.Fatalf("bundled documents not persisted: count=%d err=%v", len(files), err)
	}
}

func TestExtractQuoteIgnoresMarkdownHeadings(t *testing.T) {
	content := "# 92科比心法_深度研读报告\n\n> **研读说明**：这是一份二次整理材料。\n> **创建时间**：2026年6月11日\n\n普通正文不会成为引用。\n\n> 只做市场最强主线。\n"
	if quote := extractQuote(content); quote != "只做市场最强主线。" {
		t.Fatalf("unexpected quote: %q", quote)
	}
}

func TestLibraryKeepsExistingMemoryOutsideManagedBlock(t *testing.T) {
	value := "用户已有记忆\n\n<!-- easy-stock:short-term-masters:start -->\n旧内容\n<!-- easy-stock:short-term-masters:end -->\n\n其他记忆"
	updated := upsertManagedBlock(value, "<!-- easy-stock:short-term-masters:start -->", "<!-- easy-stock:short-term-masters:end -->", "<!-- easy-stock:short-term-masters:start -->\n新内容\n<!-- easy-stock:short-term-masters:end -->")
	if !strings.Contains(updated, "用户已有记忆") || !strings.Contains(updated, "其他记忆") || !strings.Contains(updated, "新内容") || strings.Contains(updated, "旧内容") {
		t.Fatalf("managed block replacement damaged memory: %s", updated)
	}
}
