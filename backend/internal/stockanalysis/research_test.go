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
	for _, request := range []ResearchRequest{{Symbol: "bad"}, {Symbol: "600519", Purpose: "trade"}, {Symbol: "600519", Horizon: "forever"}, {Symbol: "600519", CostPrice: floatPtr(10)}, {Symbol: "600519", Purpose: "holding", CostPrice: floatPtr(-1)}} {
		if _, err := NormalizeResearchRequest(request); err == nil {
			t.Errorf("accepted invalid request: %+v", request)
		}
	}
	r, err := NormalizeResearchRequest(ResearchRequest{Symbol: "600519"})
	if err != nil || r.Symbol != "600519.SH" || r.Horizon != "swing" || r.Purpose != "observe" {
		t.Fatalf("normalization: %+v %v", r, err)
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
	if prompter.calls != 3 || queries != 3 || len(analysis.ResearchReport.Attempts) != 3 || analysis.ResearchReport.Decision.Horizon != "medium" {
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
