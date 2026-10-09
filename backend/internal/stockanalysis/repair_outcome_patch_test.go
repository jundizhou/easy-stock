package stockanalysis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// [ES_REPAIR_SEMANTICS_PATCH] §3 失败归因与审计。
//
// §3 要求「区分正常结束 / 长度截断 / 超时 / 流中断」，并「记录修复动作、位置、
// 原始/修复后 hash」。本测试锁定这三件事，防止回归。
//
// 为什么必须区分：现场唯一未解决的 000001.SZ 失败，日志里只留下
// "JSON object does not contain expected fields"——这是**契约错误**的措辞，
// 却可能是由截断/二义引起的。归因不清会直接误导修复方向。

// 1) 长度截断必须与「正常结束但字段缺失」区分开。
func TestFailureClassDistinguishesTruncation(t *testing.T) {
	// 截断：字符串未闭合。
	if got := classifyDecodeFailure(`{"headline":"这是被截`, nil); got != jsonFailureTruncated {
		t.Errorf("未闭合字符串归类 = %s（应 truncated）", got)
	}
	// 截断：容器未闭合且尾部是残缺 token。
	if got := classifyDecodeFailure(`{"a":[1,2`, nil); got != jsonFailureTruncated {
		t.Errorf("未闭合数组归类 = %s（应 truncated）", got)
	}
	// 非截断：结构完整但字段不符。
	if got := classifyDecodeFailure(`{"other":1}`, errors.New("JSON object does not contain expected fields")); got != jsonFailureSchema {
		t.Errorf("字段不符归类 = %s（应 schema_mismatch）", got)
	}
}

// 2) 空响应单独成类，不与截断混同。
func TestFailureClassEmptyResponse(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		if got := classifyDecodeFailure(in, nil); got != jsonFailureEmpty {
			t.Errorf("%q 归类 = %s（应 empty_response）", in, got)
		}
	}
}

// 3) 超时 / 流中断必须分开（二者处置不同：超时考虑放宽预算，流中断考虑重连）。
func TestFailureClassTransport(t *testing.T) {
	if got := classifyTransportFailure(context.DeadlineExceeded); got != jsonFailureTimeout {
		t.Errorf("ctx 超时归类 = %s", got)
	}
	if got := classifyTransportFailure(errors.New("Post \"http://127.0.0.1:30000\": context deadline exceeded")); got != jsonFailureTimeout {
		t.Errorf("deadline exceeded 文本归类 = %s", got)
	}
	if got := classifyTransportFailure(errors.New("unexpected EOF")); got != jsonFailureStreamInterrupted {
		t.Errorf("unexpected EOF 归类 = %s", got)
	}
	if got := classifyTransportFailure(errors.New("read tcp: connection reset by peer")); got != jsonFailureStreamInterrupted {
		t.Errorf("connection reset 归类 = %s", got)
	}
	if got := classifyTransportFailure(nil); got != jsonFailureNone {
		t.Errorf("nil 错误归类 = %s（应 none）", got)
	}
}

// 4) 二义 / 多根 / 重复键 各自成类。
func TestFailureClassStructural(t *testing.T) {
	if got := classifyDecodeFailure(`{"a":[{"x":1},"b":2]}`, nil); got != jsonFailureAmbiguous {
		t.Errorf("二义归类 = %s（应 ambiguous_ownership）", got)
	}
	if got := classifyDecodeFailure(`{"a":1}{"b":2}`, nil); got != jsonFailureMultipleRoots {
		t.Errorf("多根归类 = %s（应 multiple_roots）", got)
	}
	if got := classifyDecodeFailure(`{"a":1,"a":2}`, nil); got != jsonFailureDuplicateKeys {
		t.Errorf("重复键归类 = %s（应 duplicate_keys）", got)
	}
}

// 5) 审计记录必须含原始/修复后 hash 与动作位置。
func TestRepairAuditRecordsHashesAndActions(t *testing.T) {
	// 用可安全修复的缺陷（多余闭括号）验证审计。
	in := `{"a":[{"t":"a"}]],"b":[{"t":"c"}]}`
	out, actions := sanitizeJSONStructureWithActions(in)
	if len(actions) == 0 {
		t.Fatalf("真实缺陷未记录修复动作: %s", in)
	}
	audit := newRepairAudit(in, out, actions, jsonFailureNone)

	if !audit.Repaired {
		t.Error("审计未标记 repaired")
	}
	if audit.RawSHA256 == audit.SanitizedSHA256 {
		t.Error("修复后 hash 与原始相同（说明未修复或未记录）")
	}
	if len(audit.RawSHA256) != 64 || len(audit.SanitizedSHA256) != 64 {
		t.Errorf("hash 长度异常: %d / %d", len(audit.RawSHA256), len(audit.SanitizedSHA256))
	}
	for _, a := range actions {
		if a.Index < 0 {
			t.Errorf("动作位置为负: %+v", a)
		}
		if a.Kind == "" {
			t.Errorf("动作类型为空: %+v", a)
		}
	}
}

// 5b) 被**拒绝**的二义输入，审计必须反映「未修复 + 归因二义」。
func TestRepairAuditRefusedAmbiguous(t *testing.T) {
	in := `{"a":[{"x":1},"b":2]}`
	out, actions := sanitizeJSONStructureWithActions(in)
	if len(actions) != 0 {
		t.Errorf("被拒绝的输入不应产生动作: %+v", actions)
	}
	if out != in {
		t.Errorf("被拒绝时不应改动原文: %s", out)
	}
	audit := newRepairAudit(in, out, actions, classifyDecodeFailure(in, nil))
	if audit.Repaired {
		t.Error("被拒绝的二义输入被标记为 repaired")
	}
	if audit.Class != jsonFailureAmbiguous {
		t.Errorf("审计归因 = %s（应 ambiguous_ownership）", audit.Class)
	}
	if audit.RawSHA256 != audit.SanitizedSHA256 {
		t.Error("被拒绝时原始/修复后 hash 应相同")
	}
}

// 6) 合法 JSON 的审计：hash 相等、无动作、未修复。
func TestRepairAuditLegalJSONHasNoActions(t *testing.T) {
	in := `{"a":[{"x":1}],"b":2}`
	out, actions := sanitizeJSONStructureWithActions(in)
	audit := newRepairAudit(in, out, actions, jsonFailureNone)
	if audit.Repaired {
		t.Error("合法 JSON 被标记为已修复")
	}
	if audit.RawSHA256 != audit.SanitizedSHA256 {
		t.Error("合法 JSON 的原始/修复后 hash 不一致")
	}
	if len(audit.Actions) != 0 {
		t.Errorf("合法 JSON 产生动作: %+v", audit.Actions)
	}
}

// 7) ★ 关键回归防线：截断输入**绝不能**被判为可接受。
// 即使补齐括号后能解析成合法 JSON，也必须归为 truncated 并拒绝。
//
// 注意这里选的是「无二义」的补齐形态：截断处在**数组元素对象已正常闭合之后**，
// 只差外层的 `]` 与 `}`，补上后结构唯一确定（不会改变任何字段归属）。
// 这正是旧实现最容易被「补括号蒙混」的形态：补完能解析 → 报成功。
func TestTruncatedNeverAcceptedViaBracketPadding(t *testing.T) {
	truncated := `{"headline":"h","items":[{"text":"a"},{"text":"b"}`
	padded := truncated + `]}`

	// 补齐版本确实是合法 JSON（证明「补括号」这个诱惑真实存在）。
	if out, ok := sanitizeJSONStructureChecked(padded); !ok || out != padded {
		t.Fatalf("前置假设失败：补齐版本应是合法 JSON，得到 ok=%v out=%s", ok, out)
	}
	// 补齐确实能解析（诱惑成立）。
	if err := decodeJSONObject(padded, &map[string]any{}); err != nil {
		t.Fatalf("前置假设失败：补齐版本应可解析: %v", err)
	}

	// ★ 但截断原文必须被拒绝：绝不能靠补 `]`/`}` 返回「已修复」。
	if out, ok := sanitizeJSONStructureChecked(truncated); ok {
		t.Errorf("截断输入被当作可接受（应拒绝），out=%s", out)
	}
	if got := classifyDecodeFailure(truncated, nil); got != jsonFailureTruncated {
		t.Errorf("截断输入归类 = %s（应 truncated，绝不能是成功）", got)
	}
	// 端到端：走真实解码路径也必须失败（不得被静默修复放行，也不得截取内部片段）。
	if err := decodeJSONObject(truncated, &map[string]any{}); err == nil {
		t.Error("截断输入经 decodeJSONObject 竟被放行")
	}
}

// 7b) ★ 纯容器截断（无未闭合字符串）同样必须拒绝——这是旧实现最容易蒙混的形态。
func TestContainerTruncationRejected(t *testing.T) {
	for _, in := range []string{
		`{"a":[1,2`,       // 数组与对象都未闭合
		`{"a":[1,2]`,      // 外层对象未闭合
		`{"a":{"b":1`,     // 嵌套对象未闭合
		`[{"x":1}`,        // 根数组未闭合
		`{"a":[{"b":[1,2`, // 多层未闭合
	} {
		out, ok := sanitizeJSONStructureChecked(in)
		if ok {
			t.Errorf("容器截断 %q 被当作可接受，out=%q", in, out)
		}
		if got := classifyDecodeFailure(in, nil); got != jsonFailureTruncated {
			t.Errorf("容器截断 %q 归类 = %s（应 length_truncated）", in, got)
		}
	}
}

// 8) 归因字符串必须是稳定常量，便于日志聚合与门禁断言。
func TestFailureClassValuesAreStable(t *testing.T) {
	want := map[jsonFailureClass]string{
		jsonFailureNone:              "none",
		jsonFailureEmpty:             "empty_response",
		jsonFailureTruncated:         "length_truncated",
		jsonFailureTimeout:           "timeout",
		jsonFailureStreamInterrupted: "stream_interrupted",
		jsonFailureAmbiguous:         "ambiguous_ownership",
		jsonFailureMultipleRoots:     "multiple_roots",
		jsonFailureDuplicateKeys:     "duplicate_keys",
		jsonFailureSchema:            "schema_mismatch",
		jsonFailureUnknown:           "unknown",
	}
	for class, s := range want {
		if string(class) != s {
			t.Errorf("类别常量值变动: %s != %s", class, s)
		}
	}
}

// 9) 归因不得 panic（任意输入）。
func TestClassificationNeverPanics(t *testing.T) {
	inputs := []string{"", "{", "[", "}", "]", `"`, `\`, `{"a":`, `[1,`, strings.Repeat("{", 500),
		strings.Repeat(`{"a":`, 500), "\x00\xff\xfe", `{"a":"\u0"}`}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", in, r)
				}
			}()
			_ = classifyDecodeFailure(in, fmt.Errorf("x"))
			_ = classifyTransportFailure(errors.New(in))
		}()
	}
}
