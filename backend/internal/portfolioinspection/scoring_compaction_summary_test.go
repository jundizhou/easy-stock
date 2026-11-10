package portfolioinspection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactionSummaryResponseFormats(t *testing.T) {
	summary := "已核验披露持仓，主要风险仍需结合原研究与来源进一步确认。"
	object, _ := json.Marshal(map[string]string{"summary": summary})
	quoted, _ := json.Marshal(summary)
	for _, tc := range []struct {
		name, response, want string
	}{
		{"object", string(object), summary},
		{"string", string(quoted), summary},
		{"short_string", `"现金流偏弱"`, "现金流偏弱"},
		{"bare", summary, summary},
		{"fenced_object", "```json\n" + string(object) + "\n```", summary},
		{"fenced_bare", "```text\n" + summary + "\n```", summary},
		{"wrapped_object", "以下是研究摘要：\n" + string(object) + "\n以上仅供核验。", summary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := normalizeCompactionSummary(tc.response, 2328)
			if got != tc.want || reason != "" {
				t.Fatalf("valid summary rejected or altered: got %q reason=%q", got, reason)
			}
		})
	}
}

func TestCompactionRejectsWrappedInvalidJSON(t *testing.T) {
	quotedError, _ := json.Marshal(`以下是压缩失败的原因，请检查原研究是否完整：{"error":"无法读取输入资料"}`)
	for _, tc := range []struct{ name, response string }{
		{"empty_object", `{}`},
		{"wrapped_empty", "以下是本次压缩请求的处理结果，请稍后重新尝试：{}"},
		{"wrapped_error", `以下是压缩失败的原因，请检查原研究是否完整：{"error":"无法读取输入资料"}`},
		{"wrapped_empty_summary", `本次资料整理的完整处理结果如下，请核对：{"summary":"  "}`},
		{"wrapped_wrong_type", `本次资料整理的完整处理结果如下，请核对：{"summary":["并非摘要字符串"]}`},
		{"incomplete_object", `{"summary":"这是一段没有完成输出且超过二十四个字符的研究资料`},
		{"wrapped_incomplete", `以下是压缩后的研究内容，详细说明如下：{"summary":"部分资料`},
		{"incomplete_string", `"这是一段没有完成输出且超过二十四个字符的研究资料`},
		{"quoted_error", string(quotedError)},
		{"fenced_error", "```json\n以下是压缩失败的原因，请检查原研究是否完整：{\"error\":\"无法读取输入资料\"}\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary, reason := normalizeCompactionSummary(tc.response, 2328)
			if summary != "" || reason != "no_summary" {
				t.Fatalf("invalid structured answer became research: %q reason=%q", summary, reason)
			}
		})
	}
}

func TestCompactionClampKeepsCompleteSentences(t *testing.T) {
	sentence := "来源e-profile：参考溢价为12.34%，属于历史观察，不代表实时成交条件。"
	prefix := strings.Repeat(sentence, 5)
	encoded, _ := json.Marshal(prefix + compactionTruncatedMarker)
	limit := len(encoded)
	response, _ := json.Marshal(map[string]string{"summary": prefix + sentence})
	summary, reason := normalizeCompactionSummary(string(response), limit)
	if summary != prefix+compactionTruncatedMarker || reason != "" {
		t.Fatalf("safe complete-sentence truncation failed: %q reason=%q", summary, reason)
	}
	if !validCompactionSummaryWithin(summary, limit) {
		t.Fatal("truncated summary exceeded its JSON budget")
	}
}

func TestCompactionRejectsUnsafeTruncation(t *testing.T) {
	prefix := strings.Repeat("研究依据已核实，", 80)
	for _, tc := range []struct{ name, fragment, tail string }{
		{"decimal", "参考溢价为12", ".34%，但本次数字来自历史公告，不能视作当前实时溢价。"},
		{"source_id", "来源e-financial-20", "26-11-06仅确认报告期盈利，不证明当前交易条件已经满足。"},
		{"early_sentence", "很早的完整句。" + prefix, "后续长句仍须结合原始资料完整核验，不能只保留前半段的有利判断。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kept := prefix + tc.fragment
			budget, _ := json.Marshal(kept + compactionTruncatedMarker)
			response, _ := json.Marshal(map[string]string{"summary": kept + tc.tail})
			summary, reason := normalizeCompactionSummary(string(response), len(budget))
			if summary != "" || reason != "over_limit" {
				t.Fatalf("summary was cut inside evidence instead of rejected: %d bytes, reason=%q", len(summary), reason)
			}
		})
	}
}
