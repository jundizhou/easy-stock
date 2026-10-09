package stockanalysis

import (
	"encoding/json"
	"strings"
	"testing"
)

// [ES_EVIDENCE_CONTRACT_PATCH] §2 证据引用契约回归测试。
//
// 现场依据（非独立探针）：
//   - 000001.SZ 首次与纠错后均报「不存在的来源编号：rule_baseline」
//     （_es_14b_v2_stability.log，快照 sha 14685c04…）。
//   - 归档「1/4 → 3/4」核实：旧契约 1/4、新契约 3/4，唯一残余失败即本项。

// 1) prompt payload 必须把基线放进 analytical_context，且不再与 evidence 平铺。
func TestEvidenceContractPayloadSeparatesBaseline(t *testing.T) {
	_, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	pack := buildResearchEvidencePack(snapshot, request, researchPromptSynthesis, nil)
	if pack.Baseline == nil {
		t.Fatal("fixture 未产生基线，无法验证契约")
	}
	prompt := researchSynthesisPromptWithPack(snapshot, request, ResearchOutline{}, pack)

	// 取 payload 段：payload 是提示词末尾的 JSON 对象。
	// 指令正文里也含 `{`（schema 示例），故按「括号配平且含 contract_version」
	// 从后往前找最外层的那个对象。
	kv := strings.Index(prompt, `"contract_version"`)
	if kv < 0 {
		t.Fatalf("payload 未带 contract_version：\n%.600s", prompt)
	}
	doc := map[string]any{}
	found := false
	for start := 0; start < kv; start++ {
		if prompt[start] != '{' {
			continue
		}
		depth, end := 0, -1
		for i := start; i < len(prompt); i++ {
			switch prompt[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i + 1
				}
			}
			if end > 0 {
				break
			}
		}
		if end <= kv {
			continue // 没包住 contract_version
		}
		candidate := map[string]any{}
		if json.Unmarshal([]byte(prompt[start:end]), &candidate) != nil {
			continue
		}
		if _, ok := candidate["contract_version"]; !ok {
			continue
		}
		doc, found = candidate, true
		break
	}
	if !found {
		t.Fatalf("未能在提示词中定位 payload 对象：\n%.600s", prompt)
	}
	// 关键：顶层不得出现 rule_baseline
	if _, bad := doc["rule_baseline"]; bad {
		t.Error("payload 顶层仍出现 rule_baseline（应与 evidence 分离）")
	}
	ctx, ok := doc[baselineContextKey].(map[string]any)
	if !ok {
		t.Fatalf("缺少 %s 子对象", baselineContextKey)
	}
	if ctx["kind"] != "rule_baseline" {
		t.Errorf("analytical_context.kind = %v，应为 rule_baseline", ctx["kind"])
	}
	if _, ok := doc["evidence"]; !ok {
		t.Error("payload 缺少 evidence")
	}
	// 基线不得出现在白名单里
	allowed, _ := doc["allowed_source_ids"].([]any)
	for _, id := range allowed {
		if isBaselineIdentifier(id.(string)) {
			t.Errorf("白名单混入基线标识: %v", id)
		}
	}
}

// 2) 白名单来自本次运行实际证据，且**不**等于归档案里写死的 5 个编号。
func TestEvidenceContractWhitelistIsDynamic(t *testing.T) {
	_, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	pack := buildResearchEvidencePack(snapshot, request, researchPromptSynthesis, nil)
	allowed := researchAllowedSources(snapshot, pack)
	if len(allowed) == 0 {
		t.Fatal("白名单为空")
	}
	// 必须包含 fixture 实际证据编号
	has := func(id string) bool {
		for _, a := range allowed {
			if a == id {
				return true
			}
		}
		return false
	}
	if !has("m-price") || !has("f-business") {
		t.Errorf("白名单缺少实际证据编号: %v", allowed)
	}
	// 必须**不含**基线
	for _, a := range allowed {
		if isBaselineIdentifier(a) {
			t.Errorf("白名单不应含基线标识: %s", a)
		}
	}
	// 白名单与后端校验同源：必须严格等于 snapshot.Sources 的编号集合。
	srcSet := map[string]bool{}
	for _, s := range snapshot.Sources {
		srcSet[s.ID] = true
	}
	if len(allowed) != len(srcSet) {
		t.Errorf("白名单大小 %d ≠ snapshot.Sources 大小 %d：%v", len(allowed), len(srcSet), allowed)
	}
	for _, a := range allowed {
		if !srcSet[a] {
			t.Errorf("白名单编号 %s 不在 snapshot.Sources 中（会导致 prompt 允许但校验拒绝）", a)
		}
	}
	// anchor 属于另一个标识空间，绝不能进 source 白名单
	for _, anchor := range snapshot.Anchors {
		for _, a := range allowed {
			if a == anchor.ID {
				t.Errorf("anchor %s 混入 source 白名单（source_ids 不接受 anchor_id）", anchor.ID)
			}
		}
	}
}

// 3) prompt 必须带版本化 Schema 契约块，且明确禁止引用基线。
func TestEvidenceContractSchemaBlock(t *testing.T) {
	_, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	pack := buildResearchEvidencePack(snapshot, request, researchPromptSynthesis, nil)
	block := ResearchSchemaBlock(pack)
	if !strings.Contains(block, ResearchOutputSchemaVersion) {
		t.Error("Schema 块缺少版本号")
	}
	if !strings.Contains(block, "allowed_source_ids") {
		t.Error("Schema 块未下发动态白名单")
	}
	if !strings.Contains(block, "rule_baseline") {
		t.Error("Schema 块未明确禁止引用 rule_baseline")
	}
	if !strings.Contains(block, "闭合") {
		t.Error("Schema 块未要求 JSON 完整闭合")
	}
	// 回退原因必须可查（§2 要求显式记录）
	if !strings.Contains(SchemaFallbackReason, "prompt.submit") {
		t.Error("回退原因未说明实际接口")
	}
}

// 4) ★ 核心：把 rule_baseline 当来源引用必须**硬失败**，且报错可操作。
func TestEvidenceContractRejectsBaselineAsSource(t *testing.T) {
	_, snapshot := researchFixture(t)
	result := validResearch()
	result.Thesis.SourceIDs = []string{"rule_baseline"}
	_, err := validateRequestedResearch(&result, snapshot, ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep})
	if err == nil {
		t.Fatal("把 rule_baseline 当来源竟然通过校验")
	}
	msg := err.Error()
	if !strings.Contains(msg, "基线字段而非证据") {
		t.Errorf("错误信息未识别为语义类别错误: %s", msg)
	}
	// 必须给出「整条重写/删除」的可操作指令，而不是只让改编号
	if !strings.Contains(msg, "整条重写或删除") {
		t.Errorf("错误信息未给出可操作指令: %s", msg)
	}
	if !strings.Contains(msg, "allowed_source_ids") {
		t.Errorf("错误信息未指向白名单: %s", msg)
	}
}

// 5) 同理，baseline / baseline_relation 等变体也要被拒。
func TestEvidenceContractRejectsBaselineVariants(t *testing.T) {
	_, snapshot := researchFixture(t)
	for _, bad := range []string{"rule_baseline", "baseline", "baseline_relation", "analytical_context", "baseline_reason"} {
		result := validResearch()
		result.Support[0].SourceIDs = []string{bad}
		if _, err := validateRequestedResearch(&result, snapshot, ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}); err == nil {
			t.Errorf("变体 %q 未被拒绝", bad)
		}
	}
}

// 6) 合法输出仍然通过（契约不得误伤正常研究）。
func TestEvidenceContractAcceptsLegalOutput(t *testing.T) {
	_, snapshot := researchFixture(t)
	result := validResearch()
	if _, err := validateRequestedResearch(&result, snapshot, ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}); err != nil {
		t.Fatalf("合法研究被拒: %v", err)
	}
}

// 7) 契约版本常量存在且稳定（供回溯）。
func TestEvidenceContractVersions(t *testing.T) {
	if ResearchEvidenceContractVersion == "" || ResearchOutputSchemaVersion == "" {
		t.Fatal("契约版本不能为空")
	}
	if !strings.Contains(ResearchEvidenceContractVersion, "v1") {
		t.Errorf("契约版本缺少版本后缀: %s", ResearchEvidenceContractVersion)
	}
}
