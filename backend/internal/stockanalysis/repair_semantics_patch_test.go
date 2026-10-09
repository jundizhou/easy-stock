package stockanalysis

import (
	"encoding/json"
	"strings"
	"testing"
)

// [ES_REPAIR_SEMANTICS_PATCH] §3 结构修复语义收紧。
//
// 现场取证（本次会话复现）：
//
//	sanitizeJSONStructure(`{"a":[{"x":1],"b":2}]}`) == `{"a":[{"x":1}],"b":2}`
//
// 即把数组元素内部的 `"b":2` 提升为**顶层字段**——字段归属被静默改写，
// 唯一判据是 `len(stack) > 1`。这在研究 JSON 上会把 counter/support 的某个
// 数组元素错误地挪进顶层对象，产物「看起来合法」但语义已变。
//
// §3 明确要求：不得把 len(stack)>1 当作唯一语义判据；无法消除歧义时
// 拒绝并重新生成。

// 1) ★ 归属二义时必须**拒绝**，而不是静默选一种。
func TestRepairRefusesAmbiguousFieldOwnership(t *testing.T) {
	in := `{"a":[{"x":1],"b":2}]}`
	out, ok := sanitizeJSONStructureChecked(in)
	if ok {
		t.Fatalf("二义输入被静默修复（应拒绝）：\n IN=%s\nOUT=%s", in, out)
	}
	// 拒绝时原文必须原样返回（调用方据此判失败并重新生成）
	if out != in {
		t.Errorf("拒绝时不应改动原文：%s", out)
	}
}

// 2) ★ §3 裁定：数组缺闭括号且紧跟一个新键时，字符流上**无法**区分
// 该键属于「数组元素对象」还是「外层对象」——两种读法都合法配平。
//
// 因此即使是真实高频缺陷 [2]（qwen3:14b-64k），也必须**拒绝并重新生成**，
// 而不是静默选一种归属。选择「归外层」正是 V2 案例被改坏字段归属的成因。
//
// 处置：拒绝 → 由调用方带反馈重试（§3「无法消除歧义时拒绝并重新生成」）。
func TestRepairRefusesAmbiguousMissingArrayClose(t *testing.T) {
	// qwen3:14b-64k 真实形态：support 数组漏了 `]`，紧跟 "counter" 键。
	in := `{"headline":"H","support":[{"text":"a","kind":"inference","source_ids":["m-price"]},"counter":[{"text":"c","kind":"inference","source_ids":["m-price"]}]}`
	out, ok := sanitizeJSONStructureChecked(in)
	if ok {
		t.Fatalf("归属二义（数组漏闭括号+新键）被静默修复（应拒绝）：\nOUT=%s", out)
	}
	if out != in {
		t.Errorf("拒绝时不应改动原文：%s", out)
	}
	// 归因必须是「归属二义」，而不是「契约不符」之类误导性措辞。
	if got := classifyDecodeFailure(in, nil); got != jsonFailureAmbiguous {
		t.Errorf("归因 = %s（应 ambiguous_ownership）", got)
	}
}

// 2b) 但「数组自身闭合后才出现的新键」是**唯一确定**的读法，此刻的
// 多余 `]` 可以安全丢弃（缺陷 [1]：`..."]"]]`，多余闭合符）。
func TestRepairDropsExtraCloseBracketSafely(t *testing.T) {
	in := `{"headline":"H","support":[{"text":"a","source_ids":["m-price"]}]],"counter":[]}`
	out, ok := sanitizeJSONStructureChecked(in)
	if !ok {
		t.Fatalf("多余闭括号应可安全丢弃：%s", in)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("修复结果非法: %v\n%s", err, out)
	}
	// 关键：顶层字段集合与支持元素必须**完全不变**，未发生归属漂移。
	if _, ok := v["headline"]; !ok {
		t.Errorf("headline 丢失: %s", out)
	}
	if _, ok := v["support"]; !ok {
		t.Errorf("support 丢失: %s", out)
	}
	if _, ok := v["counter"]; !ok {
		t.Errorf("counter 丢失: %s", out)
	}
	if _, drifted := v["text"]; drifted {
		t.Errorf("字段 text 被提升到顶层（归属漂移）: %s", out)
	}
	sup, _ := v["support"].([]any)
	if len(sup) != 1 {
		t.Errorf("support 元素数 = %d（应 1）: %s", len(sup), out)
	}
}

// 3) 合法 JSON 必须逐字节不变（既有保证不得破坏）。
func TestRepairLegalJSONByteIdentical(t *testing.T) {
	for _, in := range []string{
		`{"a":[{"x":1}],"b":2}`,
		`{"headline":"H","support":[{"text":"a","source_ids":["m-price"]}],"counter":[]}`,
		`{"n":1.5,"e":1e-3,"z":null,"t":true,"f":false,"s":"a]b}c\"d"}`,
		`[]`,
		`{}`,
	} {
		out, ok := sanitizeJSONStructureChecked(in)
		if !ok {
			t.Errorf("合法 JSON 被判为需拒绝: %s", in)
			continue
		}
		if out != in {
			t.Errorf("合法 JSON 被改动:\n IN=%s\nOUT=%s", in, out)
		}
	}
}

// 4) 修复必须幂等：修一次与修两次结果相同。
func TestRepairIdempotent(t *testing.T) {
	inputs := []string{
		// 多余闭括号（缺陷 [1] 家族）——可安全丢弃，须幂等。
		`{"a":[{"x":1}]],"b":[{"y":2}]}`,
		`{"s":[{"t":"a"}]],"c":[{"t":"c"}]}`,
		`{"a":{"b":[1,2]]}}`,
	}
	for _, in := range inputs {
		once, ok1 := sanitizeJSONStructureChecked(in)
		if !ok1 {
			t.Errorf("应可修复却拒绝了: %s", in)
			continue
		}
		twice, ok2 := sanitizeJSONStructureChecked(once)
		if !ok2 || twice != once {
			t.Errorf("非幂等:\n IN=%s\n1x=%s\n2x=%s (ok2=%v)", in, once, twice, ok2)
		}
	}
}

// 5) 截断（未闭合）输入必须被识别为「截断」而非「可修复成功」。
func TestRepairDistinguishesTruncation(t *testing.T) {
	// 明显被截断：字符串没有闭合引号。
	truncated := `{"headline":"这是被截` + "\n"
	out, ok := sanitizeJSONStructureChecked(truncated)
	if ok {
		t.Errorf("未闭合字符串被当作可修复成功:\n IN=%q\nOUT=%q", truncated, out)
	}
	// 数字被截断（`1e` / 结尾 `.`）。
	for _, in := range []string{`{"a":1e`, `{"a":1.`, `{"a":-`} {
		if out, ok := sanitizeJSONStructureChecked(in); ok {
			t.Errorf("截断数字 %q 被当作可修复成功: %s", in, out)
		}
	}
}

// 6) 重复键：合法 JSON 结构上不报错，但重复键必须能被检出（交由后续拒绝）。
func TestRepairDetectsDuplicateKeys(t *testing.T) {
	in := `{"a":1,"a":2}`
	if _, ok := sanitizeJSONStructureChecked(in); !ok {
		t.Fatalf("合法（虽重复键）JSON 不应被结构层拒绝：%s", in)
	}
	if !hasDuplicateJSONKeys(in) {
		t.Error("未检出重复键 a")
	}
	if hasDuplicateJSONKeys(`{"a":1,"b":2}`) {
		t.Error("误报重复键")
	}
}

// 7) 多个根候选：必须拒绝。
func TestRepairRejectsMultipleRoots(t *testing.T) {
	in := `{"a":1}{"b":2}`
	if _, ok := sanitizeJSONStructureChecked(in); ok {
		t.Error("多个根对象未被拒绝")
	}
}

// 8) 括号配平但语义二义的嵌套必须拒绝（不得只看字符级配平）。
func TestRepairRejectsNestedOwnershipAmbiguity(t *testing.T) {
	cases := []string{
		`{"a":[{"x":1},"y":2]}`,
		`{"outer":{"inner":[{"k":1},"z":2]}}`,
	}
	for _, in := range cases {
		if _, ok := sanitizeJSONStructureChecked(in); ok {
			t.Errorf("嵌套二义未被拒绝: %s", in)
		}
	}
}

// 9) 记录修复动作：合法 JSON 不应产生任何修复动作；
// 真正被修复的输入（多余闭括号）必须留下带位置/类型的动作。
func TestRepairActionsEmptyForLegalJSON(t *testing.T) {
	_, actions := sanitizeJSONStructureWithActions(`{"a":[{"x":1}],"b":2}`)
	if len(actions) != 0 {
		t.Errorf("合法 JSON 产生了修复动作: %v", actions)
	}
	// 真实缺陷 [1]：多余 `]`，应留下 drop-close 动作。
	_, actions = sanitizeJSONStructureWithActions(`{"a":[{"x":1}]],"b":2}`)
	if len(actions) == 0 {
		t.Error("真实缺陷（多余闭括号）未记录修复动作")
	}
	for _, a := range actions {
		if a.Kind == "" || a.Index < 0 {
			t.Errorf("修复动作缺少位置/类型: %+v", a)
		}
	}
}

// 10) 不 panic：任意字节输入。
func TestRepairNeverPanics(t *testing.T) {
	inputs := []string{"", "{", "[", "}", "]", `"`, `\`, `{"a":`, `[1,`, "{\"a\":[{\"x\":1},\"b\":2}]}",
		strings.Repeat("[", 200), strings.Repeat("{", 200), strings.Repeat(`{"a":`, 200),
		"\x00\x01\x02", `{"a":"\u00"}`}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", in, r)
				}
			}()
			_, _ = sanitizeJSONStructureChecked(in)
			_, _ = sanitizeJSONStructureWithActions(in)
		}()
	}
}
