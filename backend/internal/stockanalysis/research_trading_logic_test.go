package stockanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtureTradingLogic() *ResearchTradingLogic {
	return &ResearchTradingLogic{
		Business:  &ResearchClaim{Text: "公司研发端侧NPU与SoC产品", Kind: "fact", SourceIDs: []string{"f-business"}},
		Mainlines: []ResearchLogicItem{{Name: "端侧AI", Explanation: ResearchClaim{Text: "端侧NPU新品提供增长预期，是否主导近期价格仍待核实", Kind: "fact", SourceIDs: []string{"f-business"}}, EvidenceLevel: "sufficient", MarketStatus: "supported", MarketEvidence: &ResearchClaim{Text: "个股上涨不能证明端侧AI板块共振", Kind: "inference", SourceIDs: []string{"m-price"}}}},
		Secondary: []ResearchLogicItem{}, Catalysts: []ResearchClaim{}, Gaps: []string{},
	}
}

func TestTradingLogicSurvivesEveryResearchLevelWithoutExtraModelCalls(t *testing.T) {
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		t.Run(string(level), func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			for i := range snapshot.Sources {
				if snapshot.Sources[i].ID == "f-business" {
					snapshot.Sources[i].Content = "公司研发端侧NPU与SoC产品"
				}
			}
			baseline := analysis.Scorecard
			result := validResearch()
			result.TradingLogic = fixtureTradingLogic()
			encoded, _ := json.Marshal(result)
			prompter := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
				if level == ResearchLevelDeep && call == 1 {
					return `{"questions":[{"question":"核实新品进展","why":"影响增长预期","tool":"news","query":"端侧AI"}]}`, nil
				}
				return string(encoded), nil
			}}
			supplements := 0
			err := RunResearch(context.Background(), prompter, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: level}, "fixture", func(context.Context, ResearchSnapshot, ResearchQuestion) ([]ResearchSource, error) {
				supplements++
				return nil, nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := map[ResearchLevel]int{ResearchLevelQuick: 1, ResearchLevelStandard: 2, ResearchLevelDeep: 3}[level]
			if prompter.calls != want || (level == ResearchLevelDeep && supplements != 1) {
				t.Fatalf("calls=%d supplements=%d", prompter.calls, supplements)
			}
			logic := analysis.ResearchReport.TradingLogic
			if logic == nil || len(logic.Mainlines) != 1 || logic.Mainlines[0].Name != "端侧AI" || logic.Mainlines[0].MarketStatus != "unverified" || logic.Mainlines[0].EvidenceLevel != "sufficient" || logic.Mainlines[0].Explanation.Kind != "inference" {
				t.Fatalf("lost or overstated logic: %+v", logic)
			}
			if !reflect.DeepEqual(baseline, analysis.Scorecard) {
				t.Fatal("AI changed quantitative scores")
			}
		})
	}
}

func TestTradingLogicDropsBadReferencesWithoutDiscardingGoodReport(t *testing.T) {
	_, snapshot := researchFixture(t)
	result := validResearch()
	result.TradingLogic = fixtureTradingLogic()
	result.TradingLogic.Secondary = []ResearchLogicItem{{Name: "CPO", Explanation: ResearchClaim{Text: "无证据业务映射", SourceIDs: []string{"made-up"}}}}
	result.TradingLogic.Catalysts = []ResearchClaim{{Text: "不存在的上市事件", SourceIDs: []string{"made-up"}}}
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if len(result.TradingLogic.Secondary) != 0 || len(result.TradingLogic.Catalysts) != 0 || len(result.TradingLogic.Gaps) == 0 || len(result.TradingLogic.Mainlines) != 1 {
		t.Fatalf("invalid optional logic contaminated report: %+v", result.TradingLogic)
	}
	news := NewResearchSource("news", "新品报道", "端侧NPU新品进展", "test", "https://example.com/news", snapshot.CutoffAt.Add(-time.Hour), snapshot.CutoffAt)
	AppendResearchSources(&snapshot, []ResearchSource{news})
	result = validResearch()
	result.TradingLogic = fixtureTradingLogic()
	result.TradingLogic.Mainlines[0].Explanation.SourceIDs = []string{news.ID}
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.TradingLogic.Mainlines[0].EvidenceLevel != "limited" {
		t.Fatal("third-party news treated as verified company disclosure")
	}
}

func TestTradingLogicMarketConfirmationRequiresMatchingFreshUsableTopic(t *testing.T) {
	cutoff := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, date          string
		usable, carry, want bool
	}{
		{"端侧AI", "2026-09-30", true, false, true},
		{"集成电路", "2026-09-30", true, false, false},
		{"端侧AI", "2026-09-30", true, true, false},
		{"端侧AI", "2026-09-30", false, false, false},
		{"端侧AI", "2026-09-01", true, false, false},
		{"端侧AI", "2026-10-04", true, false, false},
	} {
		source := ResearchSource{ID: "m-themes", Content: fmt.Sprintf(`[{"name":%q,"trade_date":%q,"usable_for_current_move":%t,"carry_forward":%t}]`, tc.name, tc.date, tc.usable, tc.carry)}
		if got := tradingLogicMarketUsable(source, "端侧AI", cutoff); got != tc.want {
			t.Fatalf("%+v got=%v", tc, got)
		}
	}
	source := ResearchSource{ID: "m-sector", Content: `{"as_of":"2026-09-30","peer_group":"行业目录：集成电路","selection_scope":"industry_directory","windows":{"2d":{"sample_size":10,"end_date":"2026-09-30"}}}`}
	if tradingLogicMarketUsable(source, "端侧AI", cutoff) {
		t.Fatal("broad industry confirmed specific AI theme")
	}
}

func TestTargetedNewsSurvivesEvidenceCompressionAndPrompt(t *testing.T) {
	_, snapshot := researchFixture(t)
	for i := 0; i < 18; i++ {
		AppendResearchSources(&snapshot, []ResearchSource{NewResearchSource("announcement", fmt.Sprintf("公告%d", i), "公司项目经营风险与投资公告正文。", "fixture", fmt.Sprintf("https://example.com/a%d", i), snapshot.CutoffAt.Add(-time.Hour), snapshot.CutoffAt)})
	}
	news := NewResearchSource("news", "新品研发", "测试股票端侧NPU新品研发进展。新闻检索摘要非全文。", "eastmoney:stock-news-search:fixture", "https://example.com/npu", snapshot.CutoffAt.Add(-time.Hour), snapshot.CutoffAt)
	AppendResearchSources(&snapshot, []ResearchSource{news})
	for _, level := range []ResearchLevel{ResearchLevelStandard, ResearchLevelDeep} {
		pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{AnalysisLevel: level}, ResearchOutline{})
		found := false
		for _, card := range pack.Evidence {
			found = found || card.ID == news.ID
		}
		if !found {
			t.Fatalf("%s discarded targeted company story", level)
		}
	}
	if prompt := ResearchSynthesisPrompt(snapshot, ResearchRequest{}, ResearchOutline{}); !strings.Contains(prompt, `"trading_logic"`) || !strings.Contains(prompt, "不能自动等同") {
		t.Fatal("missing structured logic instructions")
	}
}
