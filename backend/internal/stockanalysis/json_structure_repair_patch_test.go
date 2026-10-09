package stockanalysis

import (
	"encoding/json"
	"strings"
	"testing"
)

// [ES_JSON_STRUCTURE_REPAIR_PATCH] 契约测试。
//
// 真实样本（Fin-R1-7B 核心判断阶段，落在 _es_debug_raw）：
//
//	"counter":[{"...","source_ids":["m-quote"]],\n{"text":"..."...
//	                                 ^^ 这里模型把对象的 '}' 写成了 ']'
//
// 该缺陷使 json.Unmarshal 整体失败，hasKnownJSONField 也随之失效，
// 最终对外报出误导性的 "JSON object does not contain expected fields"。
func TestSanitizeJSONStructureRepairsRealFinR1Defect(t *testing.T) {
	broken := `{"headline":"标题","counter":[{"text":"A","kind":"inference","source_ids":["m-quote"]],` + "\n" +
		`{"text":"B","kind":"calculation","source_ids":["m-quote"]}],"alternatives":[]}`

	// 该样本用标准 JSON 解析器必然失败。
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(broken), &probe); err == nil {
		t.Fatal("缺陷样本本应无法被标准解析器接受")
	}
	// 未经修复时，decodeJSONObjectRaw 报出的正是误导性的「不含期望字段」。
	if err := decodeJSONObjectRaw(broken, new(ResearchCoreSynthesis)); err == nil {
		t.Fatal("缺陷样本在未开修复通道时本应失败")
	}

	var after ResearchCoreSynthesis
	if err := decodeJSONObject(broken, &after); err != nil {
		t.Fatalf("结构修复后应能解码：%v", err)
	}
	if after.Headline != "标题" || len(after.Counter) != 2 {
		t.Fatalf("修复后字段不完整：%+v", after)
	}
	if after.Counter[1].Text != "B" {
		t.Fatalf("第二个反对理由丢失：%+v", after.Counter)
	}
}

// 合法 JSON 必须逐字节原样通过——修复器绝不能「顺手」改动正常响应。
func TestSanitizeJSONStructurePreservesValidJSON(t *testing.T) {
	cases := []string{
		`{"a":1,"b":[1,2,3],"c":{"d":"e"}}`,
		`{"text":"含,逗号和]括号以及}花括号的字符串","n":[1,2]}`,
		`{"esc":"a\"b]c\\","x":[],"y":{}}`,
		`{"a":1}`,
		`[]`,
		`{}`,
		`{"nested":[{"k":"v"},{"k2":"v2"}]}`,
	}
	for _, c := range cases {
		if got := sanitizeJSONStructure(c); got != c {
			t.Fatalf("合法JSON被改动:\n in=%q\nout=%q", c, got)
		}
	}
}

// 两类确定性缺陷 + 结果必须仍是合法 JSON。
func TestSanitizeJSONStructureRepairs(t *testing.T) {
	cases := map[string]string{
		// 对象闭合符误写成 ]
		`{"a":[1,2]]}`: `{"a":[1,2]}`,
		// 多余的闭合括号
		`{"a":[1,2]}}`: `{"a":[1,2]}`,
		// 尾随逗号（数组 / 对象）
		`{"a":[1,2,],"b":1}`: `{"a":[1,2],"b":1}`,
		`{"a":1,}`:           `{"a":1}`,
	}
	for in, want := range cases {
		got := sanitizeJSONStructure(in)
		if got != want {
			t.Fatalf("修复不符：\n in=%q\nout=%q\n期望=%q", in, got, want)
		}
		var v any
		if err := json.Unmarshal([]byte(got), &v); err != nil {
			t.Fatalf("修复结果仍非法：%s err=%v", got, err)
		}
	}
}

// 幂等：修复结果再修复不应变化。
func TestSanitizeJSONStructureIsIdempotent(t *testing.T) {
	broken := `{"a":[1,2]],"b":{"c":1,}}`
	once := sanitizeJSONStructure(broken)
	if twice := sanitizeJSONStructure(once); twice != once {
		t.Fatalf("非幂等：\n once=%q\ntwice=%q", once, twice)
	}
	if !strings.Contains(once, `{"c":1}`) {
		t.Fatalf("嵌套对象尾随逗号未清理：%q", once)
	}
}

// [ES_JSON_STRUCTURE_REPAIR_PATCH_V2] 第 2 类缺陷：容器闭合符「整体缺失」。
//
// 真实样本（qwen3:14b-64k 核心判断阶段，落在 _es_debug_raw/resp_核心判断_a1_*.txt）：
//
//	"support":[{...},{...},{...},"counter":[{...
//	                                  ^ 此处应为 `],` —— support 数组的 ']'
//	                                    与分隔逗号一起被模型吞掉了
//
// [ES_REPAIR_SEMANTICS_PATCH] §3 裁定（2026-10 收紧）：该形态虽然真实且高频，
// 但字符流上**无法区分** `"counter"` 属于「support 的数组元素对象」还是「外层对象」
// —— 两种读法都合法配平。按 §3「无法消除歧义时拒绝并重新生成」，
// 本形态**不再自动修复**，改由调用方带反馈重试。
//
// 依据：现场 V2 案例 `{"a":[{"x":1],"b":2}]}` 被旧实现静默产出
// `{"a":[{"x":1}],"b":2}` —— 元素字段被提升为顶层字段，归属被改。
// 本形态与之同类，因此一并纳入拒绝。
func TestSanitizeJSONStructureRefusesAmbiguousMissingArrayClose(t *testing.T) {
	broken := `{"headline":"标题","support":[{"text":"A"},{"text":"B"},"counter":[{"text":"C"}],"main_conflict":"X"}`

	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(broken), &probe); err == nil {
		t.Fatal("缺陷样本本应无法被标准解析器接受")
	}

	// ★ §3：必须**原样返回**（拒绝修复），绝不能产出改变归属的文本。
	got := sanitizeJSONStructure(broken)
	if got != broken {
		t.Fatalf("二义形态被自动修复（应原样返回）：\nOUT=%s", got)
	}
	// 归因必须是「归属二义」，便于调用方走重新生成。
	if class := classifyDecodeFailure(broken, nil); class != jsonFailureAmbiguous {
		t.Fatalf("归因 = %s（应 ambiguous_ownership）", class)
	}
	// 离线可解码的 checked 版本也必须拒绝。
	if out, ok := sanitizeJSONStructureChecked(broken); ok {
		t.Fatalf("checked 版本应拒绝，却返回 ok=true: %s", out)
	}
}

// V2 修复器必须对既有的 9 类合法 JSON 仍然逐字节恒等（含深层嵌套与转义）。
func TestSanitizeJSONStructureV2PreservesValidJSON(t *testing.T) {
	cases := []string{
		`{"a":{"b":[1,2,{"c":3}]},"d":[]}`,
		`{"a":[{"x":1},{"x":2}],"b":{"c":{"d":1}},"e":"s"}`,
		`{}`,
		`[]`,
		`{"a":"}{[]","b":1}`,
		`{"a":"he said \"hi\"","b":2}`,
		`{"a":[[1],[2,[3]]],"b":{}}`,
		`{"a":true,"b":false,"c":null,"d":0.5}`,
		`{"headline":"H","thesis":{"text":"T","kind":"inference","source_ids":["f-financial"]},"support":[{"text":"a","kind":"fact","source_ids":["f-financial"]}],"counter":[{"text":"c","kind":"inference","source_ids":["f-financial"]}],"alternatives":[],"main_conflict":"X","evidence_level":"limited","limitations":["L"],"baseline_relation":"disagree","baseline_reason":"R"}`,
	}
	for _, c := range cases {
		if got := sanitizeJSONStructure(c); got != c {
			t.Fatalf("合法JSON被改动:\n in=%q\nout=%q", c, got)
		}
	}
}

// [ES_REPAIR_SEMANTICS_PATCH] §3：第 2 类缺陷（数组闭括号整体缺失）不得自动修复，
// 因此「修复幂等」不再适用于该形态；此处锁定的是**拒绝的稳定性**：
// 多次调用必须得到同一原文（不产出任何变动）。
func TestSanitizeJSONStructureV2IdempotentMissingClose(t *testing.T) {
	broken := `{"support":[{"t":"a"},{"t":"b"},"counter":[{"t":"c"}],"x":1}`
	once := sanitizeJSONStructure(broken)
	twice := sanitizeJSONStructure(once)
	if once != broken || twice != broken {
		t.Fatalf("二义形态未被稳定拒绝：\n in=%q\n1x=%q\n2x=%q", broken, once, twice)
	}
	// 对照：checked 版本也必须一致拒绝（不产生任何修复文本）。
	if _, ok := sanitizeJSONStructureChecked(broken); ok {
		t.Fatal("checked 版本应拒绝该二义形态")
	}
}
