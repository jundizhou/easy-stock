package stockanalysis

import (
	"encoding/json"
	"sort"
)

// [ES_EVIDENCE_CONTRACT_PATCH]
// 证据引用契约（版本化）。
//
// 背景（现场取证，见 C:\quant\es-eval\LINK_EVIDENCE.md）：
//   - 研究主调用走 OpenAI 兼容 /v1/chat/completions，且**完全没有带 response_format**；
//     辅助小调用却已实际使用 response_format:{"type":"json_schema"} 并返回 200。
//     ⇒ Schema 通道是 response_format，链路已具备，属接线缺口而非能力缺口。
//   - evidence pack 把 rule_baseline 与 evidence 平铺在同一层，
//     模型据此把 "rule_baseline" 当成合法 source id 引用 →
//     校验报「不存在的来源编号：rule_baseline」（000001.SZ 首次与纠错后均失败）。
//
// 本文件只做三件事，且**不改变对外 API 与落盘格式**：
//   1. 版本常量（prompt + schema），便于回溯哪一版契约产出的结果。
//   2. 从「本次运行实际证据」动态生成 allowed_source_ids（不得写死归档案里的 5 个编号）。
//   3. 生成受约束 JSON Schema，供 response_format 透传与后端一致性校验共用。

const (
	// ResearchEvidenceContractVersion 证据引用契约版本（prompt 侧）。
	ResearchEvidenceContractVersion = "evidence-contract-v1"
	// ResearchOutputSchemaVersion 输出 Schema 版本（response_format 侧）。
	ResearchOutputSchemaVersion = "research-output-schema-v1"
)

// baselineContextKey 是「基线上下文」在 prompt payload 中的键名。
// 刻意**不叫 rule_baseline**，避免与 source id 空间混淆；
// 也不再与 evidence 平铺，而是收进独立的 analytical_context 子对象。
const baselineContextKey = "analytical_context"

// researchAllowedSources 汇总本次运行真正可引用的来源编号。
//
// 规则（必须与 validateResearch 的 `sources` 查找同源）：
//   - 只收录能被 validateResearch 解析为 source 的编号：
//     snapshot.Sources（含 m-*/f-*/s-*）；
//   - **不含** anchors：anchor_id 属于另一个标识空间，
//     只在 conditions/price_plan 的 anchor_id 字段使用，写进 source_ids 会被拒；
//   - **不含** rule_baseline / 任何基线字段。
//
// 注意 evidence 卡片的 id 也应全部出现在 snapshot.Sources 中；
// 若个别卡片来自合成计算而未登记，务必以 Sources 为准，避免
// 「prompt 允许但后端拒绝」的自相矛盾。
func researchAllowedSources(snapshot ResearchSnapshot, pack researchEvidencePack) []string {
	set := map[string]bool{}
	// 以 snapshot.Sources 为唯一权威来源集合（与校验同源）。
	for _, source := range snapshot.Sources {
		if id := trimSpace(source.ID); id != "" {
			set[id] = true
		}
	}
	// 证据卡片若携带未登记的 id，则一并纳入（校验端会因 sources 缺失而拒绝，
	// 这里同步纳入会导致不一致，故仅当它确实在 Sources 中才保留——上面的循环
	// 已覆盖；此处显式说明：不额外添加 pack 专属 id）。
	_ = pack
	// 兜底：基线字段永不入白名单。
	delete(set, "rule_baseline")
	delete(set, baselineContextKey)

	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isJSONSpace(s[start]) {
		start++
	}
	for end > start && isJSONSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

// isBaselineIdentifier 判定某个「来源编号」实为基线/上下文标识，而非证据来源。
//
// 现场证据：000001.SZ 首次与纠错后均报「不存在的来源编号：rule_baseline」。
// 仅靠「不在 sources 里」拒绝，反馈太弱，模型容易反复；这里识别为**语义类别错误**，
// 给出「整条重写或删除」的明确指令。
func isBaselineIdentifier(id string) bool {
	switch trimSpace(id) {
	case "rule_baseline", baselineContextKey, "analytical_context.rule_baseline",
		"baseline", "baseline_relation", "baseline_reason":
		return true
	}
	return false
}

// SchemaFallbackReason 说明为什么本链路**没有**使用原生 Schema 约束。
//
// 现场取证（非独立探针，见 LINK_EVIDENCE.md）：
//   - easy-stock 研究路径经 Hermes 的 `prompt.submit` JSON-RPC 提交，
//     该方法的参数只有 {session_id, text}（+ 图片/截断控制），
//     已安装版 Hermes 中 `tui_gateway/methods_prompt.py` 对 `json_schema`
//     的引用数为 0 → **该接口不承载 Schema**。
//   - `response_format:{"type":"json_schema"}` 只在 Hermes 的插件级
//     `PluginLlm.complete_structured(json_schema=...)` 路径出现
//     （实测辅助小调用确实携带该字段并 200）——但研究路径不走插件。
//   - 因此本版本按实际接口实现：Schema 以**版本化文本块**随 prompt 下发，
//     并同时用 `format` 之外的既有通道保持兼容；不伪造「已透传」结论。
const SchemaFallbackReason = "hermes prompt.submit 不承载原生 Schema（已安装版无 json_schema 参数）；" +
	"研究路径改用版本化 in-prompt JSON 契约 + 后端严格校验兜底"

// ResearchSchemaBlock 返回版本化的输出契约文本块，追加在提示词末尾。
//
// 它由「本次运行实际证据」驱动：allowed_source_ids 动态注入，
// 因此不会把归档案里的 5 个编号写死为全系统白名单。
func ResearchSchemaBlock(pack researchEvidencePack) string {
	allowed := pack.allowedSourceIDs
	if len(allowed) == 0 {
		allowed = []string{}
	}
	ids, _ := json.Marshal(allowed)
	return "[输出契约 " + ResearchOutputSchemaVersion + "]\n" +
		"1) 顶层只能是一个 JSON 对象；不要 Markdown 代码块、不要前后解释。\n" +
		"2) 所有 source_ids 只能取自下方白名单（本次运行的实际证据编号）：\n" +
		"   allowed_source_ids=" + string(ids) + "\n" +
		"3) 白名单之外的任何编号（尤其 analytical_context.rule_baseline、" +
		"\"baseline\"、字段名）都**不是来源**，出现即视为校验失败。\n" +
		"4) 引文 quote 若填写，必须与对应来源原文逐字一致；不确定就省略 quote。\n" +
		"5) 字符串不得截断；JSON 必须完整闭合（含所有 ] 与 }）。\n"
}

// researchAllowedSourceSet 生成白名单集合，供后端严格校验复用（与 prompt 同源）。
func researchAllowedSourceSet(snapshot ResearchSnapshot, pack researchEvidencePack) map[string]bool {
	set := map[string]bool{}
	for _, id := range researchAllowedSources(snapshot, pack) {
		set[id] = true
	}
	return set
}
