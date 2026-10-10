package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

// countingRealtime 每次都返回同一批「滚动窗口」报价，模拟数据源行为。
type countingRealtime struct{ batch []foundation.Quote }

func (c *countingRealtime) Realtime(context.Context, []string) ([]foundation.Quote, error) {
	out := make([]foundation.Quote, len(c.batch))
	copy(out, c.batch)
	return out, nil
}

// TestArchiveDedupAcrossSchedulerRestarts 复现真实故障场景并验证整改效果。
//
// 故障背景（V0.2 任务五实测）：
//   - 10-09 归档调度器当天启动 8 次，实际采集 17 次
//   - 每次启动 New() 后内存 seen 清零 → 同一批滚动数据反复落盘
//   - realtime: 794 行 / 66 唯一键 = 重复率 91.7%
//
// 期望：启用持久化去重后，无论重启多少次，落盘行数恒等于唯一记录数。
func TestArchiveDedupAcrossSchedulerRestarts(t *testing.T) {
	root := t.TempDir()
	batch := []foundation.Quote{
		{Symbol: "600408.SH", Price: 10.11},
		{Symbol: "000723.SZ", Price: 5.55},
		{Symbol: "601011.SH", Price: 8.88},
	}
	inner := &countingRealtime{batch: batch}

	const restarts = 8 // 与实测一致

	for i := 0; i < restarts; i++ {
		// 每次「进程重启」都重新构造，内存 seen 必然清零
		ar, err := NewRealtimeArchiver(inner, root, Options{})
		if err != nil {
			t.Fatalf("restart #%d NewRealtimeArchiver: %v", i, err)
		}
		if _, err := ar.Realtime(context.Background(), nil); err != nil {
			t.Fatalf("restart #%d Realtime: %v", i, err)
		}
	}

	lines := businessLines(t, filepath.Join(root, "realtime"))
	if lines != len(batch) {
		t.Fatalf("重启 %d 次后落盘行数 = %d, want %d（重复率应为 0）",
			restarts, lines, len(batch))
	}
}

// businessLines 统计业务数据行数（排除 .seen-* 键文件与 latest.json）。
func businessLines(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %q: %v", dir, err)
	}
	total := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, ".seen-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("ReadFile %q: %v", name, err)
		}
		total += countLines(data)
	}
	return total
}

// TestArchiveDedupDisabledKeepsLegacyBehavior 验证关闭开关后确实退回旧的
// 重复落盘行为 —— 只有当「开启」与「关闭」结果不同时，上面的测试才有意义。
func TestArchiveDedupDisabledKeepsLegacyBehavior(t *testing.T) {
	root := t.TempDir()
	batch := []foundation.Quote{{Symbol: "600408.SH", Price: 10.11}}
	inner := &countingRealtime{batch: batch}

	const restarts = 8
	for i := 0; i < restarts; i++ {
		ar, err := NewRealtimeArchiver(inner, root, Options{NoPersistentDedup: true})
		if err != nil {
			t.Fatalf("restart #%d: %v", i, err)
		}
		if _, err := ar.Realtime(context.Background(), nil); err != nil {
			t.Fatalf("restart #%d Realtime: %v", i, err)
		}
	}

	lines := businessLines(t, filepath.Join(root, "realtime"))
	if lines != restarts {
		t.Fatalf("关闭持久化去重后落盘行数 = %d, want %d（每次重启都会重复写）",
			lines, restarts)
	}
}
