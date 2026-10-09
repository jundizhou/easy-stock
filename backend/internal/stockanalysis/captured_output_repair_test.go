package stockanalysis

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// [ES_JSON_STRUCTURE_REPAIR_PATCH_V2] 用真实抓取的模型输出做端到端回归。
// 样本缺失时跳过（CI/他人机器上 _es_debug_raw 不存在）。
func TestSanitizeJSONStructureOnCapturedModelOutputs(t *testing.T) {
	dir := `C:\quant\market_hub\_es_debug_raw`
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("跳过：抓取目录不可用 %v", err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "resp_") || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		raw, err := os.ReadFile(dir + `\` + e.Name())
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		content := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(content, "{") {
			continue
		}
		checked++
		fixed := sanitizeJSONStructure(content)
		var probe any
		if err := json.Unmarshal([]byte(fixed), &probe); err != nil {
			t.Fatalf("%s 修复后仍非法: %v\nfixed=%s", e.Name(), err, fixed)
		}
		if content == fixed {
			t.Logf("%s: 原文已合法，未被改动（符合预期）", e.Name())
		} else {
			t.Logf("%s: 已修复，修复前 %d 字节 → 修复后 %d 字节",
				e.Name(), len(content), len(fixed))
		}
	}
	if checked == 0 {
		t.Skip("跳过：未找到可用的 JSON 抓取样本")
	}
}
