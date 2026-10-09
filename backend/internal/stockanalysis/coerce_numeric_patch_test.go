package stockanalysis

// ES_JSON_NUMERIC_COERCION_PATCH 验证：
// 1) 真实 dump（threshold 为字符串 "39"）现在能解码成功
// 2) 数值容错不会误伤真正的字符串字段
// 3) 错误保真：真实解码错误不再被 "does not contain expected fields" 覆盖
import (
	"os"
	"strings"
	"testing"
)

func TestCoerceRealRepairDump(t *testing.T) {
	b, err := os.ReadFile(`C:\quant\market_hub\_es_debug_raw\resp_研究结构修复_a1_1791338038501.txt`)
	if err != nil {
		t.Skip("dump 不存在，跳过:", err)
	}
	var target ResearchSynthesis
	if err := decodeJSONObject(string(b), &target); err != nil {
		t.Fatalf("真实 repair dump 仍解码失败: %v", err)
	}
	if len(target.Conditions) != 3 {
		t.Fatalf("conditions 数 = %d, want 3", len(target.Conditions))
	}
	th := target.Conditions[2].Threshold
	if th == nil || *th != 39 {
		t.Fatalf("conditions[2].threshold = %v, want 39", th)
	}
	if target.Headline == "" || target.EvidenceLevel == "" {
		t.Fatalf("关键字段为空: headline=%q evidence_level=%q", target.Headline, target.EvidenceLevel)
	}
	t.Logf("OK: headline=%q threshold=%.0f conditions=%d", target.Headline, *th, len(target.Conditions))
}

func TestCoercePreservesStrings(t *testing.T) {
	cases := []struct {
		in       string
		wantSame bool
	}{
		// 纯数字字符串 → 去引号
		{`{"threshold":"39"}`, false},
		{`{"threshold":"-1.5"}`, false},
		{`{"threshold":"1e3"}`, false},
		// 文本 → 原样
		{`{"text":"2026年Q3营收增长"}`, true},
		{`{"text":"  39  "}`, true}, // 含空格
		{`{"id":"c1"}`, true},       // 键名不动
		{`{"text":"007"}`, true},    // 前导零，视为字符串
		{`{"text":"+39"}`, true},    // 带正号，非标准 JSON 数字
		{`{"anchor_id":"range_low"}`, true},
	}
	for _, c := range cases {
		got := coerceJSONNumbers(c.in)
		same := got == c.in
		if same != c.wantSame {
			t.Errorf("coerce(%s) = %s (same=%v, want same=%v)", c.in, got, same, c.wantSame)
		}
	}
}

func TestCoerceDoesNotBreakValidJSON(t *testing.T) {
	in := `{"a":1,"b":{"threshold":"2.5","text":"39元"},"c":[{"threshold":"7"}]}`
	out := coerceJSONNumbers(in)
	if !strings.Contains(out, `"threshold":2.5`) {
		t.Errorf("嵌套对象内数字未还原: %s", out)
	}
	if !strings.Contains(out, `"threshold":7`) {
		t.Errorf("数组内数字未还原: %s", out)
	}
	if !strings.Contains(out, `"text":"39元"`) {
		t.Errorf("文本字段被误改: %s", out)
	}
	if !strings.Contains(out, `"b":`) || !strings.Contains(out, `"c":[`) {
		t.Errorf("结构被破坏: %s", out)
	}
}

// 错误保真：含错误类型但字段命中的 JSON，应报具体错误而非 "does not contain expected fields"
func TestSpecificErrorSurfaced(t *testing.T) {
	bad := `{"conditions":[{"id":"c1","text":"x","metric":"close","operator":"gte","window":"next_close","source_ids":[],"status":"pending","threshold":{"nested":1}}]}`
	var target ResearchSynthesis
	err := decodeJSONObject(bad, &target)
	if err == nil {
		t.Fatal("期望报错")
	}
	if strings.Contains(err.Error(), "does not contain expected fields") {
		t.Fatalf("错误被误导性信息覆盖: %v", err)
	}
	t.Logf("OK 报出具体错误: %v", err)
}
