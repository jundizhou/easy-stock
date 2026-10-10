package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPersistentSeenSurvivesRestart 验证：模拟进程重启后，
// 同一批记录不会被重复落盘（V0.2 任务五 P0 整改的验收测试）。
func TestPersistentSeenSurvivesRestart(t *testing.T) {
	root := t.TempDir()

	type rec struct {
		Symbol string `json:"symbol"`
		Time   string `json:"time"`
	}
	keyOf := func(r rec) string { return r.Symbol + "|" + r.Time }

	batch := []rec{
		{"600408.SH", "15:34:59"},
		{"000723.SZ", "15:35:30"},
		{"601011.SH", "15:34:59"},
	}

	// 业务日必须显式绑定：归档批次携带交易日，可能与系统当天不同。
	// 若不绑定，重构时两侧会指向不同键文件，跨进程去重静默失效。
	day := time.Now().Format("2006-01-02")

	// --- 第一次运行 ---
	{
		wr, err := New[rec](root, "realtime")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ps, err := NewPersistentSeenForDay(wr.Dir(), day)
		if err != nil {
			t.Fatalf("NewPersistentSeen: %v", err)
		}
		wr.WithKeyPersistent(keyOf, ps)
		for _, r := range batch {
			if _, err := wr.Append(r); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
		if got := ps.Count(); got != len(batch) {
			t.Fatalf("首次运行后键数 = %d, want %d", got, len(batch))
		}
	}

	// --- 模拟进程重启：全新 Writer + 全新 PersistentSeen（从磁盘恢复） ---
	{
		wr2, err := New[rec](root, "realtime")
		if err != nil {
			t.Fatalf("New(2): %v", err)
		}
		ps2, err := NewPersistentSeenForDay(wr2.Dir(), day)
		if err != nil {
			t.Fatalf("NewPersistentSeen(2): %v", err)
		}
		// 关键断言：重启后应能恢复出之前写过的键
		if got := ps2.Count(); got != len(batch) {
			t.Fatalf("重启后恢复键数 = %d, want %d", got, len(batch))
		}
		wr2.WithKeyPersistent(keyOf, ps2)
		written := 0
		for _, r := range batch {
			before := wr2.SeenStats().Skipped
			if _, err := wr2.Append(r); err != nil {
				t.Fatalf("Append(2): %v", err)
			}
			if wr2.SeenStats().Skipped > before {
				continue
			}
			written++
		}
		if written != 0 {
			t.Fatalf("重启后重复写入 %d 条，want 0（去重应跨进程生效）", written)
		}
	}

	// 校验业务数据文件总行数仍等于唯一记录数。
	// 注意：必须排除 `.seen-*.jsonl`（去重键文件）与 latest.json —— 前者
	// 后缀同为 .jsonl，会被 Glob("*.jsonl") 一并匹配，导致误判为重复行。
	files, _ := filepath.Glob(filepath.Join(root, "realtime", "*.jsonl"))
	total := 0
	for _, f := range files {
		base := filepath.Base(f)
		if base == "latest.json" || strings.HasPrefix(base, ".seen-") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		total += countLines(data)
	}
	if total != len(batch) {
		t.Fatalf("落盘总行数 = %d, want %d（不应有重复）", total, len(batch))
	}
}

// TestPersistentSeenFallsBackToMemory 验证：持久化层不可用时，
// 内存去重仍生效（降级安全）。
func TestPersistentSeenFallsBackToMemory(t *testing.T) {
	root := t.TempDir()
	type rec struct {
		ID string `json:"id"`
	}
	keyOf := func(r rec) string { return r.ID }

	wr, err := New[rec](root, "news")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wr.WithKey(keyOf) // 纯内存（未启用持久化）

	items := []rec{{"a"}, {"b"}, {"a"}}
	kept := 0
	for _, r := range items {
		before := wr.SeenStats().Skipped
		if _, err := wr.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if wr.SeenStats().Skipped == before {
			kept++
		}
	}
	if kept != 2 {
		t.Fatalf("内存去重后写入 %d 条, want 2", kept)
	}
}

// TestPersistentSeenEmptyKeySkipped 验证空 key 被跳过（与原语义一致）。
func TestPersistentSeenEmptyKeySkipped(t *testing.T) {
	dir := t.TempDir()
	ps, err := NewPersistentSeen(dir)
	if err != nil {
		t.Fatalf("NewPersistentSeen: %v", err)
	}
	if admitted, ok := ps.Admit(""); admitted || !ok {
		t.Fatalf("空 key 应返回 (false, true)，实际 (%v, %v)", admitted, ok)
	}
	if admitted, _ := ps.Admit("x"); !admitted {
		t.Fatal("首次出现应 admitted=true")
	}
	if admitted, _ := ps.Admit("x"); admitted {
		t.Fatal("重复出现应 admitted=false")
	}
}
