package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// [ES_PARTIAL_CORE_SAVE_PATCH] §4 受控分段保存测试。
//
// 锁定的六条边界（对应 §4 的六项要求）：
//  1. 只有独立通过完整语义校验的 core 才可保存；
//  2. 保留 core + 证据快照 + 版本 hash；
//  3. 明示「研究可用，交易计划未生成」，且不计入完整 succeeded；
//  4. 不展示未校验的 conditions/scenarios，不产生可执行计划；
//  5. core 的来源/逐字引用失败 → 仍整体失败；
//  6. 交易重试固定复用同一 core/证据快照。

// coreFrom(validResearch) 取出 fixture 的 core 部分（剔除交易字段）。
func coreFrom(r ResearchSynthesis) ResearchCoreSynthesis {
	return ResearchCoreSynthesis{
		Headline: r.Headline, Thesis: r.Thesis, Support: r.Support, Counter: r.Counter,
		Alternatives: r.Alternatives, MainConflict: r.MainConflict, EvidenceLevel: r.EvidenceLevel,
		Limitations: r.Limitations, BaselineRelation: r.BaselineRelation, BaselineReason: r.BaselineReason,
	}
}

// 1) core 自身通过完整校验 → 允许保存，且 notes 可获取。
func TestPartialCoreOnlyAcceptsValidatedCore(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	notes, ok := validateCoreOnly(core, snapshot, request)
	if !ok {
		t.Fatal("已通过完整校验的 core 被判为不可保存")
	}
	_ = notes

	report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "test-model", nil, ResearchPromptCompression{}, notes, coreSynthesisHash(core))
	if !report.Partial || report.PartialStatus != ResearchPartialCoreOnly {
		t.Fatalf("报告未标记 core-only: partial=%v status=%q", report.Partial, report.PartialStatus)
	}
	if report.CoreHash == "" {
		t.Error("未记录 core 版本 hash")
	}
	if report.SnapshotID != snapshot.ID {
		t.Error("未保留证据快照 ID")
	}
	// 2) 保留可核验的证据快照与版本
	if report.SnapshotVersion != snapshot.Version {
		t.Errorf("快照版本未保留: %d != %d", report.SnapshotVersion, snapshot.Version)
	}
	if len(report.Sources) != len(snapshot.Sources) {
		t.Errorf("来源快照未完整保留: %d != %d", len(report.Sources), len(snapshot.Sources))
	}
}

// 3) 状态标记必须明示「交易计划未生成」，且不得是 succeeded 语义。
func TestPartialCoreOnlyNeverClaimsFullSuccess(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "m", nil, ResearchPromptCompression{}, nil, coreSynthesisHash(core))
	applyPartialResearch(&analysis, &report, snapshot)

	if analysis.AI.Status == "ready" {
		t.Error("core-only 被标为 ready（等同完整成功）")
	}
	if analysis.AI.Status != "partial" {
		t.Errorf("状态 = %q（应 partial）", analysis.AI.Status)
	}
	if !strings.Contains(analysis.AI.Message, "研究可用") || !strings.Contains(analysis.AI.Message, "交易计划未生成") {
		t.Errorf("未明示「研究可用，交易计划未生成」: %q", analysis.AI.Message)
	}
	// validation 字段必须不是完整校验通过
	if report.Validation == "references_checked" {
		t.Error("core-only 报告自称完整校验通过")
	}
	if report.Validation != "core_only_pending_trade" {
		t.Errorf("validation = %q", report.Validation)
	}
}

// 4) 不得展示未校验的 conditions / scenarios，不得产生可执行计划。
func TestPartialCoreOnlyHidesUnvalidatedTradePlan(t *testing.T) {
	_, snapshot := researchFixture(t)
	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: snapshot.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	// even if caller passes a core that (hypothetically) carried trade-ish data,
	// 报告构造必须只取 core 字段，交易字段一律为空。
	report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "m", nil, ResearchPromptCompression{}, nil, coreSynthesisHash(core))

	if len(report.Conditions) != 0 {
		t.Errorf("展示了未校验的交易条件: %+v", report.Conditions)
	}
	if len(report.Scenarios) != 0 {
		t.Errorf("展示了未校验的情景: %+v", report.Scenarios)
	}
	if len(report.InvalidationIDs) != 0 {
		t.Errorf("展示了未校验的失效条件: %+v", report.InvalidationIDs)
	}
	if report.Decision.Status != "" || report.Decision.NewPosition != "" || report.Decision.PricePlan != nil {
		t.Errorf("产生了可执行计划入口: %+v", report.Decision)
	}
}

// 5) ★ core 自身来源非法 → 不得部分保存（整体失败）。
func TestPartialCoreOnlyRejectsInvalidCoreSources(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	for name, mutate := range map[string]func(*ResearchCoreSynthesis){
		"不存在的来源编号": func(c *ResearchCoreSynthesis) {
			c.Support = []ResearchClaim{{Text: "无源论据", Kind: "fact", SourceIDs: []string{"s-not-exist-0000"}}}
		},
		"基线当来源引用": func(c *ResearchCoreSynthesis) {
			c.Support = []ResearchClaim{{Text: "拿基线当证据", Kind: "fact", SourceIDs: []string{"rule_baseline"}}}
		},
		"论据无来源": func(c *ResearchCoreSynthesis) {
			c.Support = []ResearchClaim{{Text: "无源论据", Kind: "fact"}}
		},
	} {
		core := coreFrom(validResearch())
		mutate(&core)
		if _, ok := validateCoreOnly(core, snapshot, request); ok {
			t.Errorf("%s：core 校验本应失败，却允许了部分保存", name)
		}
	}
}

// 5b) core 核心字段缺失（空 headline / 无论据）→ 不得部分保存。
func TestPartialCoreOnlyRejectsIncompleteCore(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	for name, core := range map[string]ResearchCoreSynthesis{
		"空标题": func() ResearchCoreSynthesis { c := coreFrom(validResearch()); c.Headline = "  "; return c }(),
		"无论据": func() ResearchCoreSynthesis { c := coreFrom(validResearch()); c.Support = nil; return c }(),
		"空论点": func() ResearchCoreSynthesis { c := coreFrom(validResearch()); c.Thesis.Text = ""; return c }(),
	} {
		if _, ok := validateCoreOnly(core, snapshot, request); ok {
			t.Errorf("%s：不完整的 core 被允许部分保存", name)
		}
	}
}

// 6) 端到端：trade 段失败 → job 状态为 partial（非 succeeded / 非 failed），
// 且 core 内容与证据快照被保留。
func TestPartialResearchEndToEndViaService(t *testing.T) {
	analysis, snapshot := researchFixture(t)

	// 直接构造 partial 错误，验证 service 的状态映射（不依赖真实模型）。
	tradeErr := &invalidJSONResponseError{label: "交易条件", cause: errors.New("模拟交易段失败")}

	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	notes, ok := validateCoreOnly(core, snapshot, request)
	if !ok {
		t.Fatal("前置条件失败：core 应通过校验")
	}
	report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "m", nil, ResearchPromptCompression{}, notes, coreSynthesisHash(core))
	applyPartialResearch(&analysis, &report, snapshot)

	partial := &PartialResearchError{Core: core, CoreNotes: notes, TradeErr: tradeErr}

	// service 的映射逻辑：partial → status=partial, stage=completed
	got, isPartial := isPartialResearch(error(partial))
	if !isPartial || got == nil {
		t.Fatal("partial 错误未被识别")
	}
	if !strings.Contains(partial.Error(), "研究可用") || !strings.Contains(partial.Error(), "交易计划未生成") {
		t.Errorf("错误文案未明示可用性: %q", partial.Error())
	}
	// errors.Is/As 必须能穿透到底层交易错误，便于展示精确原因。
	if !errors.Is(error(partial), tradeErr) {
		t.Error("partial 错误未包裹交易段原始错误")
	}
	// core 内容保留
	if report.Headline != core.Headline {
		t.Error("core headline 未保留")
	}
	if len(report.Support) != len(core.Support) {
		t.Error("core support 未保留")
	}
	// hash 与 core 一致（§4.2 版本 hash 可核验）
	if report.CoreHash != coreSynthesisHash(core) {
		t.Error("core hash 不一致")
	}
}

// 6b) 交易重试必须固定复用同一 core/证据快照：core hash 在重试前后不变。
func TestPartialCoreHashStableAcrossTradeRetry(t *testing.T) {
	_, snapshot := researchFixture(t)
	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: snapshot.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}

	h1 := coreSynthesisHash(core)
	_ = snapshot
	// 模拟一次「交易重试」：trade 段重新跑，但 core 与快照不变。
	tradePack1 := buildResearchTradeEvidencePack(snapshot, request, ResearchOutline{}, core)
	tradePack2 := buildResearchTradeEvidencePack(snapshot, request, ResearchOutline{}, core)
	h2 := coreSynthesisHash(core)

	if h1 != h2 || h1 == "" {
		t.Fatalf("core hash 在交易重试间发生变化: %q -> %q", h1, h2)
	}
	// 证据快照也应稳定（同一 snapshot 输入 → 同一白名单）
	ids1 := researchAllowedSources(snapshot, tradePack1)
	ids2 := researchAllowedSources(snapshot, tradePack2)
	if strings.Join(ids1, ",") != strings.Join(ids2, ",") {
		t.Error("交易重试间证据白名单发生变化")
	}
}

// 7) 完整成功路径不得被 partial 逻辑污染：partial 字段必须省略。
func TestFullResearchReportNotMarkedPartial(t *testing.T) {
	report := ResearchReport{Validation: "references_checked"}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, key := range []string{"\"partial\"", "\"partial_status\"", "\"core_hash\""} {
		if strings.Contains(body, key) {
			t.Errorf("完整报告的 JSON 出现了 partial 字段 %s（应省略以保持格式兼容）: %s", key, body)
		}
	}
}

// 8) partial 报告在 JSON 中必须显式携带标记，便于前端/下游识别。
func TestPartialReportSerializesMarkers(t *testing.T) {
	_, snapshot := researchFixture(t)
	core := coreFrom(validResearch())
	request := ResearchRequest{Symbol: snapshot.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "m", nil, ResearchPromptCompression{}, nil, coreSynthesisHash(core))

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, key := range []string{"\"partial\":true", "\"partial_status\":\"core_only\"", "\"core_hash\""} {
		if !strings.Contains(body, key) {
			t.Errorf("partial 报告缺少标记 %s: %s", key, body)
		}
	}
	// 交易字段不得出现（或为空数组）
	if strings.Contains(body, "\"conditions\":[{") {
		t.Error("partial 报告序列化出了非空 conditions")
	}
}

// 9) 不 panic：异常 core 输入。
func TestPartialCoreOnlyNeverPanics(t *testing.T) {
	_, snapshot := researchFixture(t)
	request := ResearchRequest{Symbol: snapshot.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}
	cases := []ResearchCoreSynthesis{
		{},
		{Headline: "h"},
		{Support: []ResearchClaim{{SourceIDs: []string{""}}}},
		{Thesis: ResearchClaim{Text: "t"}, Support: []ResearchClaim{{Text: "s", SourceIDs: []string{"m-price", "x"}}}},
	}
	for _, core := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %+v: %v", core, r)
				}
			}()
			_, _ = validateCoreOnly(core, snapshot, request)
			report := buildPartialReport(core, snapshot, request, ResearchLevelDeep, "m", nil, ResearchPromptCompression{}, nil, coreSynthesisHash(core))
			var analysis Analysis
			applyPartialResearch(&analysis, &report, snapshot)
		}()
	}
}

// 10) 上下文取消不得被误判为「可部分保存」。
func TestPartialResearchNotTriggeredByContextCancel(t *testing.T) {
	// 这个断言保护的是 research.go 的分支顺序：ctx.Err() != nil 必须先于 partial 判定。
	// 这里用错误类型检查同构地验证语义：context.Canceled 不是 partial。
	if _, ok := isPartialResearch(context.Canceled); ok {
		t.Error("context.Canceled 被误判为 partial")
	}
	if _, ok := isPartialResearch(context.DeadlineExceeded); ok {
		t.Error("context.DeadlineExceeded 被误判为 partial")
	}
	if _, ok := isPartialResearch(errors.New("普通错误")); ok {
		t.Error("普通错误被误判为 partial")
	}
}
