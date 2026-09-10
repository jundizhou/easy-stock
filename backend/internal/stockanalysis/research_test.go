package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
)

func researchFixture(t *testing.T) (Analysis, ResearchSnapshot) {
	t.Helper()
	lines := syntheticTrendLines("600519.SH", 80, 10, .05, 1_000_000_000)
	cutoff := lines[len(lines)-1].Time.Add(18 * time.Hour)
	input := Input{Symbol: "600519.SH", Quote: foundation.Quote{Symbol: "600519.SH", Name: "测试股票", Price: lines[len(lines)-1].Close}, KLines: lines, Business: "生产测试产品"}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	return analysis, BuildResearchSnapshot(input, analysis, cutoff)
}

func validResearch() ResearchSynthesis {
	return ResearchSynthesis{
		Headline: "观察量价与披露能否一致", Thesis: ResearchClaim{Text: "历史上行不是未来上涨的证明", Kind: "inference", SourceIDs: []string{"m-price"}},
		Support:      []ResearchClaim{{Text: "有日线记录", Kind: "fact", SourceIDs: []string{"m-price"}}, {Text: "有业务资料", Kind: "fact", SourceIDs: []string{"f-business"}}},
		MainConflict: "价格强弱与业务兑现尚未相互验证", EvidenceLevel: "limited", BaselineRelation: "disagree", BaselineReason: "量价评分不证明业务兑现",
		Conditions: []ResearchCondition{{ID: "c1", Text: "验证结构失效", Metric: "close", AnchorID: "ma20", Operator: "lte", Window: "next_close", SourceIDs: []string{"m-price"}}}, InvalidationIDs: []string{"c1"},
		Decision: ResearchDecision{Status: "conditional", Mode: "non_short", Horizon: "swing", NewPosition: "等待条件确认", ExistingPosition: "先核实已有仓位风险", Reason: "只使用已有价格锚点", PricePlan: &AnchoredPricePlan{EntryAnchor: "last_close", StopAnchor: "ma20", Reason: "以历史均价为失效参考", SourceIDs: []string{"m-price"}}},
	}
}

func TestResearchRequestBoundaries(t *testing.T) {
	for _, request := range []ResearchRequest{{Symbol: "bad"}, {Symbol: "600519", Purpose: "trade"}, {Symbol: "600519", Horizon: "forever"}, {Symbol: "600519", CostPrice: floatPtr(10)}, {Symbol: "600519", Purpose: "holding", CostPrice: floatPtr(-1)}, {Symbol: "600519", AnalysisLevel: "invalid"}} {
		if _, err := NormalizeResearchRequest(request); err == nil {
			t.Errorf("accepted invalid request: %+v", request)
		}
	}
	r, err := NormalizeResearchRequest(ResearchRequest{Symbol: "600519"})
	if err != nil || r.Symbol != "600519.SH" || r.Horizon != "swing" || r.Purpose != "observe" || r.AnalysisLevel != ResearchLevelDeep {
		t.Fatalf("normalization: %+v %v", r, err)
	}
	for _, level := range []ResearchLevel{ResearchLevelQuantitative, ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		if got, err := NormalizeResearchRequest(ResearchRequest{Symbol: "600519", AnalysisLevel: level}); err != nil || got.AnalysisLevel != level {
			t.Fatalf("level normalization: %q %+v %v", level, got, err)
		}
	}
	if ResearchStageTimeout(ResearchRequest{AnalysisLevel: ResearchLevelDeep}) != 6*time.Minute || ResearchTotalTimeout(ResearchRequest{AnalysisLevel: ResearchLevelDeep}) != 18*time.Minute {
		t.Fatal("deep research timeouts were not expanded")
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestResearchPlannerIsIndependentAndSourceCutoffIsEnforced(t *testing.T) {
	_, snapshot := researchFixture(t)
	snapshot.Baseline.PositiveSignals = []string{"SECRET_BASELINE_REASON"}
	prompt := ResearchOutlinePrompt(snapshot, ResearchRequest{})
	if strings.Contains(prompt, "SECRET_BASELINE_REASON") || strings.Contains(prompt, `"rule_baseline"`) {
		t.Fatal("planner saw heuristic recommendation")
	}
	future := NewResearchSource("announcement", "future", "future disclosure", "test", "https://example.com/future", snapshot.CutoffAt.Add(time.Hour), snapshot.CutoffAt)
	unknown := NewResearchSource("announcement", "unknown", "undated statement", "test", "javascript:alert(1)", time.Time{}, snapshot.CutoffAt)
	version := snapshot.Version
	if added := AppendResearchSources(&snapshot, []ResearchSource{future, unknown, unknown}); added != 1 {
		t.Fatalf("added %d sources", added)
	}
	if snapshot.Version != version+1 || snapshot.Sources[len(snapshot.Sources)-1].URL != "" || unknown.TimeStatus != "publication_unknown" {
		t.Fatal("source provenance broken")
	}
}

func TestResearchPromptsUseBoundedEvidencePacks(t *testing.T) {
	_, snapshot := researchFixture(t)
	for index := 0; index < 36; index++ {
		kind := "news"
		if index%6 == 0 {
			kind = "announcement"
		}
		content := fmt.Sprintf("样本公告%d：公司项目合作与订单进展。风险提示：经营现金流为负，应收账款和存货变化需要继续核实。", index)
		snapshot.Sources = append(snapshot.Sources, NewResearchSource(kind, fmt.Sprintf("样本材料%d", index), strings.Repeat(content, 20), "test", fmt.Sprintf("https://example.com/%d", index), snapshot.CutoffAt.Add(-time.Duration(index+1)*time.Hour), snapshot.CapturedAt))
	}
	outlinePack := buildResearchEvidencePack(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, researchPromptOutline, nil)
	synthesisPack := buildResearchEvidencePack(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, researchPromptSynthesis, nil)
	t.Logf("outline evidence bytes %d -> %d, sources %d -> %d; synthesis evidence bytes %d -> %d, sources %d -> %d", outlinePack.Stats.OriginalContentBytes, outlinePack.Stats.SelectedContentBytes, outlinePack.Stats.OriginalSourceCount, outlinePack.Stats.SelectedSourceCount, synthesisPack.Stats.OriginalContentBytes, synthesisPack.Stats.SelectedContentBytes, synthesisPack.Stats.OriginalSourceCount, synthesisPack.Stats.SelectedSourceCount)
	if outlinePack.Stats.SelectedContentBytes >= outlinePack.Stats.OriginalContentBytes || synthesisPack.Stats.SelectedContentBytes >= synthesisPack.Stats.OriginalContentBytes {
		t.Fatalf("evidence pack did not shrink content: outline=%+v synthesis=%+v", outlinePack.Stats, synthesisPack.Stats)
	}
	if len(outlinePack.Evidence) > 24 || len(synthesisPack.Evidence) > 32 {
		t.Fatalf("evidence card limits exceeded: outline=%d synthesis=%d", len(outlinePack.Evidence), len(synthesisPack.Evidence))
	}
	outlinePrompt := ResearchOutlinePrompt(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"})
	synthesisPrompt := ResearchSynthesisPrompt(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, ResearchOutline{})
	for label, prompt := range map[string]string{"outline": outlinePrompt, "synthesis": synthesisPrompt} {
		if strings.Contains(prompt, `"daily_bars"`) || strings.Contains(prompt, `"meta"`) {
			t.Fatalf("%s prompt contains uncompressed fields", label)
		}
		if !strings.Contains(prompt, "风险提示") || !strings.Contains(prompt, "f-business") || !strings.Contains(prompt, "m-price") {
			t.Fatalf("%s prompt lost key evidence: %s", label, prompt)
		}
	}
}

func TestResearchAnnouncementSummaryIsBounded(t *testing.T) {
	_, snapshot := researchFixture(t)
	content := strings.Repeat("公司项目合作进展良好，但经营现金流为负，存在减值风险。", 80)
	snapshot.Sources = []ResearchSource{NewResearchSource("announcement", "公告标题", content, "test", "https://example.com", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)}
	pack := buildResearchEvidencePack(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, researchPromptSynthesis, nil)
	if len(pack.Evidence) != 1 {
		t.Fatalf("expected one evidence card, got %d", len(pack.Evidence))
	}
	if got := len([]rune(pack.Evidence[0].Text)); got > 100 {
		t.Fatalf("announcement summary length=%d, want <=100", got)
	}
}

func TestResearchLevelEvidenceBudgets(t *testing.T) {
	if ResearchCompressionVersion != "evidence-pack-v2" {
		t.Fatalf("compression version = %q", ResearchCompressionVersion)
	}
	lines := syntheticTrendLines("600519.SH", 180, 10, .05, 1_000_000_000)
	input := Input{Symbol: "600519.SH", Quote: foundation.Quote{Symbol: "600519.SH", Name: "测试股票", Price: lines[len(lines)-1].Close}, KLines: lines, Business: "生产测试产品"}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := BuildResearchSnapshot(input, analysis, lines[len(lines)-1].Time.Add(18*time.Hour))
	longAnnouncement := NewResearchSource("announcement", "风险公告标题", strings.Repeat("公司经营现金流为负，存在减值风险，项目进展需要继续核实。", 20), "test", "https://example.com", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, longAnnouncement)
	for _, tc := range []struct {
		level ResearchLevel
		bars  int
		chars int
		bytes int
	}{
		{ResearchLevelQuick, 60, 30, 4_000},
		{ResearchLevelStandard, 100, 50, 8_000},
		{ResearchLevelDeep, 300, 100, 16_000},
	} {
		request := ResearchRequest{Purpose: "observe", Horizon: "swing", AnalysisLevel: tc.level}
		pack := buildResearchCoreEvidencePack(snapshot, request, ResearchOutline{})
		if pack.Stats.SelectedContentBytes > tc.bytes || len(pack.Evidence) > researchLevelPolicyFor(tc.level).MaxCards {
			t.Fatalf("%s budget exceeded: cards=%d bytes=%d", tc.level, len(pack.Evidence), pack.Stats.SelectedContentBytes)
		}
		for _, card := range pack.Evidence {
			if card.Kind == "announcement" && len([]rune(card.Text)) > tc.chars {
				t.Fatalf("%s announcement summary=%d, want <=%d", tc.level, len([]rune(card.Text)), tc.chars)
			}
			if card.ID == "m-price" {
				var value map[string]any
				if json.Unmarshal([]byte(card.Text), &value) != nil {
					t.Fatalf("%s price evidence is not JSON", tc.level)
				}
				if rows, ok := value["recent_bars"].([]any); !ok || len(rows) > tc.bars {
					t.Fatalf("%s daily bars=%v, want <=%d", tc.level, value["recent_bars"], tc.bars)
				}
			}
		}
	}
}

func TestResearchTradeEvidencePackStaysFocused(t *testing.T) {
	_, snapshot := researchFixture(t)
	for index := 0; index < 24; index++ {
		snapshot.Sources = append(snapshot.Sources, NewResearchSource("announcement", fmt.Sprintf("公告%d", index), "公司经营风险、减值和现金流需要继续核实。"+strings.Repeat("补充正文。", 30), "test", fmt.Sprintf("https://example.com/%d", index), snapshot.CutoffAt.Add(-time.Duration(index+1)*time.Hour), snapshot.CapturedAt))
	}
	core := ResearchCoreSynthesis{Thesis: ResearchClaim{SourceIDs: []string{"m-price", "s-0"}}, Support: []ResearchClaim{{SourceIDs: []string{"f-business"}}}}
	pack := buildResearchTradeEvidencePack(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, ResearchOutline{}, core)
	if len(pack.Evidence) > 12 || pack.Stats.SelectedContentBytes > 12000 {
		t.Fatalf("trade evidence pack too large: cards=%d bytes=%d", len(pack.Evidence), pack.Stats.SelectedContentBytes)
	}
	for _, id := range []string{"m-price", "m-quote", "f-business"} {
		found := false
		for _, card := range pack.Evidence {
			if card.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("trade pack dropped required source %s", id)
		}
	}
}

func TestResearchSoftensUnsupportedCapitalAttribution(t *testing.T) {
	result := ResearchSynthesis{
		Headline:       "更可能由题材预期与资金驱动",
		MainConflict:   "价格上涨由资金驱动",
		BaselineReason: "量化反映资金动能强",
		Thesis:         ResearchClaim{Text: "量价显示增量资金和资金动能强", Kind: "fact", SourceIDs: []string{"m-price"}},
		Decision:       ResearchDecision{Reason: "题材资金脉冲，资金性质待确认"},
	}
	if !softenUnsupportedAttribution(&result, map[string]ResearchSource{"m-price": {Kind: "calculation"}}) {
		t.Fatal("unsupported attribution was not softened")
	}
	for _, text := range []string{result.Headline, result.MainConflict, result.BaselineReason, result.Thesis.Text, result.Decision.Reason} {
		if strings.Contains(text, "资金驱动") || strings.Contains(text, "资金动能强") || strings.Contains(text, "题材资金脉冲") {
			t.Fatalf("unsupported attribution remains: %s", text)
		}
	}
	if result.Thesis.Kind != "inference" {
		t.Fatal("softened attribution must not remain a fact")
	}
	for _, text := range []string{"缺少增量资金净流入证据", "量价不能证明资金驱动", "资金行为仅为待验证假设"} {
		cautious := ResearchSynthesis{Headline: text}
		if softenUnsupportedAttribution(&cautious, nil) || cautious.Headline != text {
			t.Fatalf("cautious wording changed: %s", cautious.Headline)
		}
	}
}

func TestResearchCoreEvidencePackStaysFocused(t *testing.T) {
	_, snapshot := researchFixture(t)
	for index := 0; index < 24; index++ {
		snapshot.Sources = append(snapshot.Sources, NewResearchSource("announcement", fmt.Sprintf("公告%d", index), "公司业绩变化与经营风险需要继续核实。"+strings.Repeat("补充正文。", 40), "test", fmt.Sprintf("https://example.com/core-%d", index), snapshot.CutoffAt.Add(-time.Duration(index+1)*time.Hour), snapshot.CapturedAt))
	}
	pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{Purpose: "observe", Horizon: "swing"}, ResearchOutline{})
	if len(pack.Evidence) > 12 || pack.Stats.SelectedContentBytes > 16000 {
		t.Fatalf("core evidence pack too large: cards=%d bytes=%d", len(pack.Evidence), pack.Stats.SelectedContentBytes)
	}
}

func TestResearchRejectsFabricatedReferencesAndQuotes(t *testing.T) {
	_, snapshot := researchFixture(t)
	for _, modify := range []func(*ResearchSynthesis){
		func(r *ResearchSynthesis) { r.Thesis.SourceIDs = []string{"invented"} },
		func(r *ResearchSynthesis) { r.Thesis.Quote = "this quote does not exist" },
		func(r *ResearchSynthesis) { r.Conditions[0].AnchorID = "invented_price" },
		func(r *ResearchSynthesis) { r.InvalidationIDs = []string{"c999"} },
		func(r *ResearchSynthesis) {
			r.Scenarios = []ResearchScenario{{Key: "base", ConditionIDs: []string{"c999"}}}
		},
	} {
		result := validResearch()
		modify(&result)
		if _, err := validateResearch(&result, snapshot); err == nil {
			t.Fatal("fabricated structure accepted")
		}
	}
}

func TestResearchRepairsUniqueOneEditSourceIDs(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	announcement := NewResearchSource("announcement", "公告", "公告内容", "test", "https://example.com/announcement", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	AppendResearchSources(&snapshot, []ResearchSource{announcement})
	result := validResearch()
	shortID := announcement.ID[:len(announcement.ID)-1]
	result.Thesis = ResearchClaim{Text: "公告内容", Kind: "fact", SourceIDs: []string{shortID}, Quote: "公告内容"}
	result.Support[0].SourceIDs = []string{shortID}
	result.Conditions[0].SourceIDs = []string{shortID}
	result.Decision.PricePlan.SourceIDs = []string{shortID}
	notes, err := validateResearch(&result, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{result.Thesis.SourceIDs, result.Support[0].SourceIDs, result.Conditions[0].SourceIDs, result.Decision.PricePlan.SourceIDs} {
		found := false
		for _, id := range ids {
			if id == announcement.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("source id was not repaired: %#v, want %q", ids, announcement.ID)
		}
	}
	if !strings.Contains(strings.Join(notes, "\n"), shortID) || !strings.Contains(strings.Join(notes, "\n"), announcement.ID) {
		t.Fatalf("repair note missing: %#v", notes)
	}
	if analysis.Scorecard.Overall == 0 {
		t.Fatal("fixture baseline unexpectedly empty")
	}
}

func TestResearchDoesNotRepairAmbiguousOrNonGeneratedSourceIDs(t *testing.T) {
	_, snapshot := researchFixture(t)
	first := ResearchSource{ID: "s-aaaaaaaaaaaaaaa0", Kind: "announcement", Title: "公告1", Content: "内容1"}
	second := ResearchSource{ID: "s-aaaaaaaaaaaaaaa1", Kind: "announcement", Title: "公告2", Content: "内容2"}
	AppendResearchSources(&snapshot, []ResearchSource{first, second})
	ambiguous := "s-aaaaaaaaaaaaaaa"
	result := validResearch()
	result.Thesis.SourceIDs = []string{ambiguous}
	if _, err := validateResearch(&result, snapshot); err == nil {
		t.Fatal("ambiguous source id was repaired")
	}
	result = validResearch()
	result.Thesis.SourceIDs = []string{"m-pric"}
	if _, err := validateResearch(&result, snapshot); err == nil {
		t.Fatal("non-generated source id was repaired")
	}
}

func TestResearchAbstainsWithoutChangingQuantitativeBaseline(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	baseline := analysis.Scorecard
	result := validResearch()
	result.Support = nil
	result.Decision.ExistingPosition = "立即加仓"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.Decision.Status != "no_plan" || result.Decision.PricePlan != nil || strings.Contains(result.Decision.ExistingPosition, "立即加仓") {
		t.Fatalf("dependent actions not revoked: %+v", result.Decision)
	}
	report := ResearchReport{ResearchSynthesis: result, Request: ResearchRequest{Purpose: "holding"}}
	ApplyResearch(&analysis, &report, snapshot)
	if !reflect.DeepEqual(baseline, analysis.Scorecard) {
		t.Fatal("AI modified baseline score")
	}
	if analysis.RiskControl.StopPrice != 0 || analysis.RiskControl.TakeProfitFirst != 0 || analysis.RiskControl.SuggestedPositionMax != 0 || analysis.ActionPlan.Entry.PriceLow != 0 {
		t.Fatal("abstention retained executable prices")
	}
	if analysis.Conclusion.Action != result.Decision.ExistingPosition {
		t.Fatal("holding purpose ignored")
	}
}

func TestResearchPricesOnlyUseValidatedAnchors(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	result := validResearch()
	result.Conditions[0].Threshold = floatPtr(99999)
	result.Conditions[0].Text = "跌到99999元"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.Decision.PricePlan == nil {
		t.Fatal("valid entry/stop rejected")
	}
	if strings.Contains(result.Conditions[0].Text, "99999") || *result.Conditions[0].Threshold == 99999 {
		t.Fatal("invented threshold survived")
	}
	ApplyResearch(&analysis, &ResearchReport{ResearchSynthesis: result}, snapshot)
	if analysis.RiskControl.StopPrice != *result.Conditions[0].Threshold || analysis.RiskControl.TakeProfitFirst != 0 || analysis.RiskControl.RiskReward != 0 {
		t.Fatal("missing target replaced with fabricated profit target")
	}
	result = validResearch()
	result.Decision.PricePlan.TargetAnchor = "ma20"
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.Decision.PricePlan != nil || result.Decision.Status != "no_plan" {
		t.Fatal("invalid target accepted")
	}
}

func TestResearchConditionOperatorSynonymsAreNormalized(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{">=", "gte"}, {"至少", "gte"}, {"<=", "lte"}, {"至多", "lte"}, {"gte", "gte"},
	} {
		got, ok := normalizeResearchConditionOperator(tc.input)
		if !ok || got != tc.want {
			t.Fatalf("normalizeResearchConditionOperator(%q) = %q, %v; want %q, true", tc.input, got, ok, tc.want)
		}
	}
	for _, operator := range []string{"maybe", "above", "below", "confirmed"} {
		if _, ok := normalizeResearchConditionOperator(operator); ok {
			t.Fatalf("ambiguous or nonnumeric operator %q must remain unchanged", operator)
		}
	}
}

func TestResearchStalePricesAndOpinionsCannotCreateConfidentPlan(t *testing.T) {
	_, snapshot := researchFixture(t)
	opinion := NewResearchSource("opinion", "broker", "利润可能改善", "broker", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CutoffAt)
	AppendResearchSources(&snapshot, []ResearchSource{opinion})
	result := validResearch()
	result.Thesis = ResearchClaim{Text: "利润可能改善", Kind: "fact", SourceIDs: []string{opinion.ID}, Quote: "利润可能改善"}
	snapshot.CutoffAt = snapshot.CutoffAt.Add(10 * 24 * time.Hour)
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.Thesis.Kind == "fact" || result.EvidenceLevel != "insufficient" || result.Decision.Status != "no_plan" {
		t.Fatal("opinion/stale report overstated")
	}
}

type researchTestPrompter struct {
	calls   int
	options []hermes.PromptOptions
	respond func(int, string) (string, error)
}

func (p *researchTestPrompter) Prompt(ctx context.Context, prompt string) (hermes.PromptResult, error) {
	return p.PromptWithOptions(ctx, prompt, hermes.PromptOptions{})
}
func (p *researchTestPrompter) PromptWithOptions(_ context.Context, prompt string, options hermes.PromptOptions) (hermes.PromptResult, error) {
	p.calls++
	p.options = append(p.options, options)
	text, err := p.respond(p.calls, prompt)
	return hermes.PromptResult{Content: text}, err
}

func TestQuickResearchSucceedsAfterUniqueSourceIDRepair(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	announcement := NewResearchSource("announcement", "公告", "公告内容", "test", "https://example.com/quick", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	AppendResearchSources(&snapshot, []ResearchSource{announcement})
	shortID := announcement.ID[:len(announcement.ID)-1]
	result := validResearch()
	result.Thesis = ResearchClaim{Text: "公告内容", Kind: "fact", SourceIDs: []string{shortID}, Quote: "公告内容"}
	result.Support[0].SourceIDs = []string{shortID}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	prompter := &researchTestPrompter{respond: func(int, string) (string, error) {
		return string(encoded), nil
	}}
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelQuick}
	if err := RunResearch(context.Background(), prompter, &snapshot, &analysis, request, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	if analysis.ResearchReport == nil || analysis.ResearchReport.Validation != "references_checked" {
		t.Fatalf("quick research was not accepted: %+v", analysis.ResearchReport)
	}
	if analysis.ResearchReport.Thesis.SourceIDs[0] != announcement.ID {
		t.Fatalf("quick research kept malformed source id: %#v", analysis.ResearchReport.Thesis.SourceIDs)
	}
}

func TestResearchUsesBoundedSupplementAndRepairBudget(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	out := ResearchOutline{}
	for i := 0; i < 5; i++ {
		out.Questions = append(out.Questions, ResearchQuestion{Question: "核实事项", Why: "可能改变判断", Tool: "announcements", Query: strings.Repeat("甲", i+1)})
	}
	outline, _ := json.Marshal(out)
	final, _ := json.Marshal(validResearch())
	prompter := &researchTestPrompter{respond: func(call int, _ string) (string, error) {
		switch call {
		case 1:
			return string(outline), nil
		case 2:
			return `{"headline":"malformed"}`, nil
		default:
			return string(final), nil
		}
	}}
	queries := 0
	err := RunResearch(context.Background(), prompter, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "medium"}, "test", func(ctx context.Context, _ ResearchSnapshot, q ResearchQuestion) ([]ResearchSource, error) {
		queries++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 13*time.Second {
			t.Fatal("unbounded supplement")
		}
		return nil, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prompter.calls != 4 || queries != 3 || len(analysis.ResearchReport.Attempts) != 4 || analysis.ResearchReport.Decision.Horizon != "medium" {
		t.Fatalf("budget/request not preserved: calls=%d queries=%d", prompter.calls, queries)
	}
	for _, options := range prompter.options {
		if !options.DisableTools || !options.Sandbox {
			t.Fatal("unattended model gained tools")
		}
	}
	for _, q := range analysis.ResearchReport.Questions {
		if q.Status != "not_found" || !strings.Contains(q.Outcome, "不代表") {
			t.Fatal("absence misrepresented as evidence")
		}
	}
}

func TestResearchFailureDoesNotApplyPartialAdvice(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	before := analysis.Conclusion
	p := &researchTestPrompter{respond: func(int, string) (string, error) { return "", errors.New("offline") }}
	if err := RunResearch(context.Background(), p, &snapshot, &analysis, ResearchRequest{}, "test", nil, nil); err == nil {
		t.Fatal("expected failure")
	}
	if analysis.ResearchReport != nil || analysis.Conclusion != before || p.calls > 3 {
		t.Fatal("failed research changed analysis or exhausted unbounded calls")
	}
}

func TestResearchVerificationRejectsMissingSessionsAndAdjustedPrices(t *testing.T) {
	_, snapshot := researchFixture(t)
	result := validResearch()
	_, _ = validateResearch(&result, snapshot)
	report := ResearchReport{ResearchSynthesis: result}
	loc := time.FixedZone("CST", 8*3600)
	last := snapshot.DailyBars[len(snapshot.DailyBars)-1]
	day, _ := time.ParseInLocation("2006-01-02", last.Date, loc)
	snapshot.CutoffAt = day.Add(16 * time.Hour)
	base := foundation.KLine{Time: day, Close: last.Close, High: last.High, Low: last.Low, Volume: 100}
	next := foundation.KLine{Time: day.AddDate(0, 0, 1), Close: 1, High: 2, Low: 1, Volume: 100}
	third := next
	third.Time = day.AddDate(0, 0, 2)
	calendar := []foundation.KLine{base, next, third}
	now := third.Time.Add(16 * time.Hour)
	check := func(lines []foundation.KLine) ConditionCheck {
		return VerifyResearch(snapshot, report, lines, now, calendar).Checks[0]
	}
	if got := check([]foundation.KLine{base, next, third}); got.Status != "met" || got.AsOf != next.Time.Format("2006-01-02") {
		t.Fatalf("next close: %+v", got)
	}
	if got := check([]foundation.KLine{base, third}); got.Status != "unavailable" {
		t.Fatalf("silently skipped missing next session: %+v", got)
	}
	base.Close *= .8
	if got := check([]foundation.KLine{base, next, third}); got.Status != "unavailable" {
		t.Fatalf("compared incompatible adjusted prices: %+v", got)
	}
}
