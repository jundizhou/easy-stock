package stockanalysis

import (
	"os"
	"strings"
	"testing"
)

// [ES_REPAIR_SEMANTICS_PATCH] §3 回归：回显型空壳输出不得被判定为成功。
//
// 现场实证（C:\quant\es-eval\s5\diag_core_1791375179.txt）：
// 14B 模型把输入 evidence-pack 原样回显，其 `analysis` 子对象恰好含 `limitations`
// （与 ResearchCoreSynthesis 同名），旧逻辑只命中一个键就放行，
// decodeJSONObject 返回 nil 但 headline 为空 —— 静默成功。
func TestEchoedFragmentNeverSilentlySucceeds(t *testing.T) {
	echoed := `{
	  "stock": {"symbol":"600519.SH","name":"测试股票","evidence":{}},
	  "analysis": {"overall":61,"opportunity_score":75,"risk_score":46,
	    "grade":"C","direction":"中性","conviction":"较低",
	    "limitations":["财务数据不足","新闻检索有限"]}
	}`
	var target ResearchCoreSynthesis
	if err := decodeJSONObject(echoed, &target); err == nil {
		t.Errorf("回显旁支片段竟解码成功（静默成功缺陷未修）: headline=%q", target.Headline)
	}
}

// 嵌套旁支弱命中：candidateIsUnambiguousForTarget 必须否决。
func TestCandidateIsUnambiguousRejectsWeakNestedHit(t *testing.T) {
	weak := `{"overall":61,"limitations":["a"]}`
	var target ResearchCoreSynthesis
	if candidateIsUnambiguousForTarget(weak, &target) {
		t.Error("嵌套旁支（仅撞 limitations + 无锚点）被误判为明确目标对象")
	}
	strong := `{"headline":"x","support":[],"counter":[]}`
	if !candidateIsUnambiguousForTarget(strong, &target) {
		t.Error("命中锚点 headline 的候选被误判为非目标对象")
	}
}

// 合法输出必须稳定通过（不得因收紧而误杀）。
func TestEchoFixAcceptsRealCoreOutput(t *testing.T) {
	good := `{"headline":"量价与披露能否一致",
		"thesis":{"text":"历史上行不是未来上涨的证明","kind":"inference","source_ids":["m-price"]},
		"support":[{"text":"有日线记录","kind":"fact","source_ids":["m-price"]}],
		"counter":[],"alternatives":[],
		"main_conflict":"价格强弱与业务兑现尚未相互验证",
		"evidence_level":"limited","baseline_relation":"disagree",
		"baseline_reason":"量价评分不证明业务兑现"}`
	var target ResearchCoreSynthesis
	if err := decodeJSONObject(good, &target); err != nil {
		t.Fatalf("合法输出解码失败: %v", err)
	}
	if target.Headline == "" {
		t.Error("合法输出解码后 headline 为空")
	}
}

// 既有契约：最外层单命中（小模型分片作答）仍须接受。
func TestEchoFixKeepsOutermostPartialDecode(t *testing.T) {
	var target aiConclusion
	content := `{"summary":"最终有效结论"}`
	if err := decodeJSONObject(content, &target); err != nil {
		t.Fatalf("最外层分片作答被误杀: %v", err)
	}
	if target.Summary != "最终有效结论" {
		t.Errorf("分片作答未解码: %+v", target)
	}
}

// 既有契约：1 字段小目标（items）仍须接受。
func TestEchoFixKeepsSingleFieldTarget(t *testing.T) {
	var target aiThemeEvidenceResponse
	if err := decodeJSONObject(`{"items":[]}`, &target); err != nil {
		t.Fatalf("1 字段目标被误杀: %v", err)
	}
}

// 既有契约：第二个对象才是真结果时须取第二个，且不得泄漏被拒候选的字段。
func TestEchoFixKeepsSecondCandidateSelection(t *testing.T) {
	decoded := aiConclusion{MainRisk: "旧结果风险"}
	content := `{"headline":"无效候选标题","summary":42}
{"summary":"最终有效结论"}`
	if err := decodeJSONObject(content, &decoded); err != nil {
		t.Fatalf("第二候选未被取到: %v", err)
	}
	if decoded.Summary != "最终有效结论" || decoded.Headline != "" || decoded.MainRisk != "" {
		t.Fatalf("被拒候选或旧目标泄漏进结果: %+v", decoded)
	}
}

// 现场样本回归：逐一断言不再静默成功。
func TestDiagSampleNeverSilentlySucceeds(t *testing.T) {
	dir := `C:\quant\es-eval\s5`
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skip("无现场样本目录")
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "diag_core_") || !strings.HasSuffix(name, ".txt") {
			continue
		}
		raw, err := os.ReadFile(dir + `\` + name)
		if err != nil {
			continue
		}
		n++
		var target ResearchCoreSynthesis
		if err := decodeJSONObject(string(raw), &target); err == nil && target.Headline == "" {
			t.Errorf("%s 仍被静默判定成功（headline 为空）", name)
		}
	}
	if n == 0 {
		t.Skip("无 diag_core_*.txt 样本")
	}
	t.Logf("回归 %d 个现场样本", n)
}
