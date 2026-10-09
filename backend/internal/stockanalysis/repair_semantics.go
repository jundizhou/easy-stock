package stockanalysis

import (
	"strings"
)

// [ES_REPAIR_SEMANTICS_PATCH] §3 结构修复语义收紧。
//
// 背景（现场复现，见 repair_semantics_patch_test.go 顶部注释）：
// 旧实现用 `len(stack) > 1` 作为「对象闭括号被误写成 ']'」的唯一判据，
// 对 `{"a":[{"x":1],"b":2}]}` 会静默产出 `{"a":[{"x":1}],"b":2}`，
// 把数组元素内的 `"b":2` **提升为顶层字段**——字段归属被改变。
//
// §3 要求：
//   - 不得把 len(stack)>1 当作唯一语义判据；
//   - 无法消除歧义时**拒绝并重新生成**；
//   - 区分正常结束 / 长度截断 / 超时 / 流中断；
//     不得通过补齐括号把截断输出标为成功；
//   - 记录修复动作、位置、原始/修复后 hash（由调用方取 hash）。
//
// 本实现把旧逻辑拆成「带判定的」版本：
//   sanitizeJSONStructureChecked(raw) (out string, ok bool)
//     ok=true  表示可安全接受（合法原文，或无二义地被修复）
//     ok=false 表示存在二义/截断/多根等**不可安全自动修复**的情形，调用方须重新生成
//   sanitizeJSONStructureWithActions(raw) (out string, actions []jsonRepairAction)
//     在可接受时给出修复动作清单（位置 + 类型），用于审计。

// jsonRepairAction 记录一次结构修复动作（位置与类型），供审计与 hash 比对。
type jsonRepairAction struct {
	Index        int    `json:"index"`         // 在原始文本中的字节位置
	Kind         string `json:"kind"`          // close-container / insert-close / drop-comma / ...
	Detail       string `json:"detail"`        // 人类可读细节
	OriginalByte string `json:"original_byte"` // 被处理的原字符（可为空）
}

// jsonStructureKind 描述一次「判定结果」，用于区分失败原因。
type jsonStructureKind string

const (
	jsonStructureLegal      jsonStructureKind = "legal"
	jsonStructureRepaired   jsonStructureKind = "repaired"
	jsonStructureAmbiguous  jsonStructureKind = "ambiguous_ownership"
	jsonStructureTruncated  jsonStructureKind = "truncated"
	jsonStructureMultipleRt jsonStructureKind = "multiple_roots"
)

// sanitizeJSONStructureChecked 是 sanitizeJSONStructure 的「带判定」版本。
// 返回 (修复/原文, 是否可安全接受)。ok=false 时 out==raw（不动原文）。
func sanitizeJSONStructureChecked(raw string) (string, bool) {
	out, actions, kind := sanitizeJSONStructureAnalyze(raw)
	if kind == jsonStructureLegal || kind == jsonStructureRepaired {
		return out, true
	}
	_ = actions
	return raw, false
}

// sanitizeJSONStructureWithActions 返回修复结果与动作清单（不可接受时动作为空）。
func sanitizeJSONStructureWithActions(raw string) (string, []jsonRepairAction) {
	out, actions, kind := sanitizeJSONStructureAnalyze(raw)
	if kind == jsonStructureLegal || kind == jsonStructureRepaired {
		return out, actions
	}
	return raw, nil
}

// sanitizeJSONStructureAnalyze 执行一次带完整判定的结构分析。
func sanitizeJSONStructureAnalyze(raw string) (string, []jsonRepairAction, jsonStructureKind) {
	if raw == "" {
		return raw, nil, jsonStructureLegal
	}

	out := make([]byte, 0, len(raw))
	stack := make([]byte, 0, 32)
	states := make([]uint8, 0, 32)
	actions := make([]jsonRepairAction, 0, 8)
	inString := false
	escaped := false
	sawValue := false // 是否已见过一个根值
	terminatedRoot := false
	ambiguous := false

	const (
		stateKey uint8 = iota
		stateColon
		stateValue
		stateAfterValue
	)
	topState := func() uint8 {
		if len(states) == 0 {
			return stateKey
		}
		return states[len(states)-1]
	}
	setTopState := func(s uint8) {
		if len(states) > 0 {
			states[len(states)-1] = s
		}
	}
	closeContainer := func() {
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
			states = states[:len(states)-1]
		}
		if len(states) > 0 {
			setTopState(stateAfterValue)
		}
	}
	record := func(index int, kind, detail string, orig byte) {
		ob := ""
		if orig != 0 {
			ob = string(orig)
		}
		actions = append(actions, jsonRepairAction{Index: index, Kind: kind, Detail: detail, OriginalByte: ob})
	}

	for index := 0; index < len(raw); index++ {
		current := raw[index]

		if inString {
			out = append(out, current)
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
				if len(stack) == 0 {
					sawValue = true
				}
			}
			continue
		}

		switch current {
		case '"':
			// 前瞻：若后随 ':' 则是对象键。
			look := index + 1
			for look < len(raw) {
				if raw[look] == '\\' {
					look += 2
					continue
				}
				if raw[look] == '"' {
					break
				}
				look++
			}
			look++
			for look < len(raw) && isJSONSpace(raw[look]) {
				look++
			}
			isKey := look < len(raw) && raw[look] == ':'
			if isKey {
				// 键只能出现在对象里。栈顶若为数组，则该数组漏了闭括号。
				// 但**必须先判定**「这个键究竟属于谁」是否存在二义（§3）。
				//
				// 关键区分（实测缺陷 [1] vs V2 二义案例）：
				//   · 若栈顶是 '{'（对象在等键）——键本就该属于它，
				//     前面的数组多写了一个 ']'，丢掉即可，**归属不变**，可修；
				//   · 若栈顶是 '['（数组在等元素）——键既可能是
				//     「数组元素对象的后续键」也可能是「外层对象的键」，
				//     两种读法都配平，无法从字符流消歧 → **拒绝**。
				if len(stack) > 0 && stack[len(stack)-1] == '[' {
					if hasOwnershipAmbiguity(raw, index, stack) {
						ambiguous = true
					} else {
						for len(stack) > 0 && stack[len(stack)-1] == '[' {
							out = dropTrailingComma(out)
							out = append(out, ']')
							record(index, "insert-close", "补回缺失的数组闭括号 ']'", 0)
							closeContainer()
						}
						if topState() == stateAfterValue {
							out = dropTrailingComma(out)
							out = append(out, ',')
							record(index, "insert-comma", "补回元素分隔逗号", 0)
						}
					}
				}
			}
			inString = true
			out = append(out, current)
		case '{', '[':
			if len(stack) == 0 {
				if sawValue || terminatedRoot {
					return raw, nil, jsonStructureMultipleRt
				}
			}
			stack = append(stack, current)
			if current == '{' {
				states = append(states, stateKey)
			} else {
				states = append(states, stateValue)
			}
			out = append(out, current)
		case '}', ']':
			if len(stack) == 0 {
				// 多余闭合符。若根值已完整且又见闭合符 → 多根/多余。
				if sawValue || terminatedRoot {
					return raw, nil, jsonStructureMultipleRt
				}
				record(index, "drop-close", "丢弃多余的闭合符", current)
				continue
			}
			expected := byte('{')
			if current == ']' {
				expected = '['
			}
			if stack[len(stack)-1] != expected {
				if current == ']' && stack[len(stack)-1] == '{' {
					// 栈顶是对象、却读到 ']'：两种可能读法——
					//   (i) 对象闭括号被误写成 ']' → 应**改写**为 '}'；
					//   (ii) 这是一个**多余**的 ']' → 应**丢弃**。
					//
					// 旧实现用 `len(stack) > 1` 一律选 (i)，对
					// `{"a":[{"x":1],"b":2}]}` 会把 `"b":2` 提升为顶层字段。
					//
					// §3 收紧后：只有当 (i) 有**旁证**时才改写；
					// 否则选 (ii) 丢弃——丢弃**不改变任何字段归属**，
					// 是两种读法里唯一安全的一种（缺陷 [1] 的 `]]` 正属此类）。
					if objectCloseMiswriteIsUnambiguous(raw, index, stack) {
						out = dropTrailingComma(out)
						out = append(out, '}')
						record(index, "fix-close", "对象闭括号误写为 ']'，改为 '}'", current)
						closeContainer()
						continue
					}
					record(index, "drop-close", "丢弃多余的 ']'（无改写旁证，保留字段归属）", current)
					continue
				}
				// 其他不匹配：多余闭合符，丢弃（保守，不改归属）。
				record(index, "drop-close", "丢弃不匹配的闭合符", current)
				continue
			}
			out = dropTrailingComma(out)
			out = append(out, current)
			// 正常闭合**不是修复动作**，不记账（否则合法 JSON 也会产生动作）。
			closeContainer()
			if len(stack) == 0 {
				terminatedRoot = true
			}
		case ':':
			if topState() == stateColon {
				setTopState(stateValue)
			}
			out = append(out, current)
		case ',':
			if topState() == stateAfterValue {
				if len(stack) > 0 && stack[len(stack)-1] == '{' {
					setTopState(stateKey)
				} else {
					setTopState(stateValue)
				}
			}
			out = append(out, current)
		case ' ', '\t', '\n', '\r':
			out = append(out, current)
		default:
			if topState() == stateValue {
				setTopState(stateAfterValue)
			}
			out = append(out, current)
		}
	}

	// 截断判定：字符串未闭合 = 明确截断，不能补引号蒙混过去。
	if inString {
		return raw, nil, jsonStructureTruncated
	}
	// 数字/字面量被截断：尾部是 '-'、'.'、'e'、'E'、'+' 等不完整 token。
	if truncatedScalarTail(out) {
		return raw, nil, jsonStructureTruncated
	}
	if ambiguous {
		return raw, nil, jsonStructureAmbiguous
	}
	// ★ [ES_REPAIR_SEMANTICS_PATCH] §3 核心：**残留未闭合容器 = 长度截断**。
	//
	// 旧实现到这里会用 "tail-close" 补齐 `]`/`}` 并返回「已修复」——
	// 这正是 §3 明令禁止的「通过补齐括号把截断输出标为成功」。
	// 健全的输出应当自带全部闭括号；缺了就是被截断，必须拒绝并重新生成。
	//
	// 注意：**类型不匹配**的补齐（缺陷 [2]：数组漏闭括号后紧跟新键）
	// 已在上面按「无二义」判定修复，不走这条；能走到这里的只有「读到文本末尾
	// 仍有容器开着」，即真正的尾部截断。
	if len(stack) > 0 {
		return raw, nil, jsonStructureTruncated
	}

	if len(actions) == 0 {
		return string(out), nil, jsonStructureLegal
	}
	return string(out), actions, jsonStructureRepaired
}

// hasOwnershipAmbiguity 判断「在数组里遇到一个键」这一步是否**存在归属二义**。
//
// 仅在栈顶为 '[' 时被调用（即：当前在数组里等元素，却读到一个 `"k":`）。
//
// 二义的本质是：这个键既可能是
//
//	(a) 数组**当前元素对象**的后续键（数组漏了 `]`，`"k"` 属于该元素），
//	(b) 数组**之后外层对象**的键（数组与其元素都已闭合，`"k"` 回到外层）。
//
// §3 的要求是「无法消除歧义时拒绝并重新生成」，因此这里的判据取**保守**：
// 只要「数组元素对象的收尾」与「当前键」之间存在不止一种配平读法，就判二义。
//
// 唯一可安全判定的情形是：键**紧跟在数组自身闭合之后**（即 `]` 已经真实出现，
// 键出现在该 `]` 之后），此时键必属外层对象，数组无缺陷。
// 但在本函数被调用的时刻栈顶仍是 '['，说明**数组尚未闭合**——
// 于是「数组提前闭合、键归外层」与「数组未闭合、键归元素」两种读法并存，
// 均为合法配平，无法从字符流消歧 → **一律判为二义**。
//
// 这正是现场 V2 案例 `{"a":[{"x":1},"y":2]}` / `{"a":[{"x":1],"b":2}]}`
// 必须被拒绝而不能静默产出 `{"a":[{"x":1}],"y":2}` 的原因——后者把元素字段
// 提升到了外层，字段归属被改，产物「看起来合法」但语义已变。
func hasOwnershipAmbiguity(raw string, keyIndex int, stack []byte) bool {
	return true
}

// objectCloseMiswriteIsUnambiguous 判断「对象闭括号写成 ']'」是否有旁证。
//
// 旧实现判据是 len(stack) > 1。这里改为**必须**满足：
// 该 ']' 之后紧随（跳过空白/逗号）的 token 是「数组的下一个元素或数组闭合」，
// 否则视为无法判定 → 拒绝。
func objectCloseMiswriteIsUnambiguous(raw string, index int, stack []byte) bool {
	if len(stack) <= 1 {
		return false
	}
	// 该对象在数组中（栈上一帧是 '['）才有意义。
	if stack[len(stack)-2] != '[' {
		return false
	}
	// 旁证：`]` 之后（跳过空白）必须是 ',' 或 ']'。
	look := index + 1
	for look < len(raw) && isJSONSpace(raw[look]) {
		look++
	}
	if look >= len(raw) {
		// 到了末尾：说明这个 ']' 既可能是对象闭合也可能是数组闭合，
		// 无法判定 → 拒绝（不能靠补括号蒙）。
		return false
	}
	switch raw[look] {
	case ',', ']':
		return true
	}
	return false
}

// truncatedScalarTail 判断输出尾部是否为被截断的标量 token。
func truncatedScalarTail(out []byte) bool {
	end := len(out)
	for end > 0 && isJSONSpace(out[end-1]) {
		end--
	}
	if end == 0 {
		return false
	}
	switch out[end-1] {
	case '-', '+', '.', 'e', 'E', ':', ',':
		return true
	}
	return false
}

// hasDuplicateJSONKeys 检出对象内的重复键（不做语义覆盖，只报事实）。
// 合法 JSON 在结构层不报错，但研究输出出现重复键必须被后续检查拒绝。
func hasDuplicateJSONKeys(raw string) bool {
	// 用一个极简扫描：逐层收集对象键，遇到重复即返回。
	type frame struct {
		isObject bool
		seen     map[string]bool
		expect   string // 当前等待的键
	}
	stack := []*frame{}
	inStr, esc := false, false
	var cur strings.Builder
	var lastKey string
	pendingKey := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inStr {
			cur.WriteByte(ch)
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
				lastKey = cur.String()
				pendingKey = true
			}
			continue
		}
		switch ch {
		case '"':
			cur.Reset()
			inStr = true
		case '{':
			stack = append(stack, &frame{isObject: true, seen: map[string]bool{}})
			pendingKey = false
		case '[':
			stack = append(stack, &frame{isObject: false})
			pendingKey = false
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pendingKey = false
		case ':':
			if pendingKey && len(stack) > 0 {
				f := stack[len(stack)-1]
				if f.isObject && f.seen != nil {
					if f.seen[lastKey] {
						return true
					}
					f.seen[lastKey] = true
				}
			}
			pendingKey = false
		}
	}
	return false
}
