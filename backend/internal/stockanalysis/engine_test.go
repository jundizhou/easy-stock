package stockanalysis

import (
	"context"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
)

func TestAnalyzeRoutesLiquidUptrendToTrendCapacity(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 180, 10, 0.08, 1_500_000_000)
	analysis, err := Analyze(Input{
		Symbol: "600519.SH",
		Quote: foundation.Quote{
			Symbol: "600519.SH", Name: "测试趋势股", Price: lines[len(lines)-1].Close, ChangePercent: 2.1,
		},
		KLines:   lines,
		Concepts: []string{"消费"},
		Themes: []foundation.ThemeOverview{{
			Theme: "consumer", Name: "消费", TrendScore: 76, TrendStage: "主升", ActiveDays: 8,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType != "trend_capacity" {
		t.Fatalf("primary type = %q, want trend_capacity (trend=%d phase=%s)", analysis.Profile.PrimaryType, analysis.Trend.Score, analysis.Trend.Phase)
	}
	if analysis.Trend.Score < 68 {
		t.Fatalf("trend score = %d, want >= 68", analysis.Trend.Score)
	}
	if len(analysis.Chart) != 120 {
		t.Fatalf("chart points = %d, want 120", len(analysis.Chart))
	}
	if analysis.ActionPlan.Invalidation == "" || len(analysis.Evidence) < 3 {
		t.Fatalf("analysis missing actionable evidence: %+v", analysis)
	}
}

func TestAnalyzeRoutesDowntrendToRisk(t *testing.T) {
	lines := syntheticTrendLines("000001.SZ", 160, 30, -0.11, 300_000_000)
	analysis, err := Analyze(Input{Symbol: "000001.SZ", KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType != "weak_risk" {
		t.Fatalf("primary type = %q, want weak_risk (trend=%d phase=%s)", analysis.Profile.PrimaryType, analysis.Trend.Score, analysis.Trend.Phase)
	}
	if analysis.ActionPlan.CurrentAction != "回避，等待重新筑底" {
		t.Fatalf("action = %q", analysis.ActionPlan.CurrentAction)
	}
	if analysis.RiskControl.SuggestedPositionMax > 10 || analysis.RiskControl.StopPrice >= analysis.Trend.LatestClose {
		t.Fatalf("weak risk control is not defensive: %+v", analysis.RiskControl)
	}
	if analysis.NextDay.Bias != "防守观察" || !strings.Contains(analysis.NextDay.Scenarios[1].Action, "不新增仓位") {
		t.Fatalf("weak next-day plan must stay defensive: %+v", analysis.NextDay)
	}
	if analysis.RiskControl.RiskReward > 3.01 {
		t.Fatalf("risk reward should use an executable target, got %.2f", analysis.RiskControl.RiskReward)
	}
}

func TestAnalyzeBuildsCompleteDecisionWorkspace(t *testing.T) {
	stockLines := syntheticTrendLines("300750.SZ", 220, 30, 0.16, 2_200_000_000)
	benchmarkLines := syntheticTrendLines("399006.SZ", 220, 1000, 0.7, 100_000_000_000)
	analysis, err := Analyze(Input{
		Symbol:          "300750.SZ",
		Quote:           foundation.Quote{Symbol: "300750.SZ", Name: "测试成长股", Price: stockLines[len(stockLines)-1].Close},
		KLines:          stockLines,
		BenchmarkSymbol: "399006.SZ",
		BenchmarkName:   "创业板指",
		BenchmarkKLines: benchmarkLines,
		Concepts:        []string{"新能源"},
		Themes:          []foundation.ThemeOverview{{Name: "新能源", TrendScore: 81, TrendStage: "趋势推进", ActiveDays: 12}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Timeframes) != 5 || len(analysis.Signals) < 7 || len(analysis.Scorecard.Dimensions) < 7 {
		t.Fatalf("complete score workspace missing: timeframes=%d signals=%d dimensions=%d", len(analysis.Timeframes), len(analysis.Signals), len(analysis.Scorecard.Dimensions))
	}
	if !analysis.Relative.Available || analysis.Relative.BenchmarkName != "创业板指" {
		t.Fatalf("relative strength missing: %+v", analysis.Relative)
	}
	if len(analysis.NextDay.Scenarios) != 4 || len(analysis.NextDay.Levels) != 4 {
		t.Fatalf("next-day playbook incomplete: %+v", analysis.NextDay)
	}
	if analysis.RiskControl.StopPrice <= 0 || analysis.RiskControl.StopPrice >= analysis.RiskControl.EntryReference || analysis.RiskControl.PositionFormula == "" {
		t.Fatalf("risk control incomplete: %+v", analysis.RiskControl)
	}
	if last := analysis.Chart[len(analysis.Chart)-1]; last.MA120 == nil {
		t.Fatalf("chart is missing MA120: %+v", last)
	}
}

func TestAnalyzeThemeShortTermRoutePrefersKaipanlaLimitUpCache(t *testing.T) {
	short := ShortTermAnalysis{ExactLimitUpData: true, LimitUpCount20: 1}
	theme := analyzeTheme(
		"003032.SZ",
		short,
		[]foundation.StockThemeAttribution{
			{Symbol: "003032.SZ", Theme: "机器人概念", Concepts: []string{"机器人概念", "职业教育"}, Source: "duanxianxia:kaipanla-limit-up", TradeDate: "2026-08-07"},
			{Symbol: "003032.SZ", Theme: "AI应用", Source: "duanxianxia:kaipanla-theme-leader", TradeDate: "2026-08-07", Role: "龙二"},
		},
		[]string{"教育信息化"},
		"教育",
		[]foundation.ThemeOverview{{Name: "机器人概念", TrendScore: 83, TrendStage: "趋势推进", ActiveDays: 6}},
		nil,
	)
	if theme.Primary != "机器人概念" || theme.Route != "short_term" || !strings.Contains(theme.Source, "kaipanla-limit-up") {
		t.Fatalf("short-term theme route did not prefer limit-up cache: %+v", theme)
	}
	if theme.Primary == "教育" || theme.TrendScore != 83 {
		t.Fatalf("broad industry won or radar enrichment was lost: %+v", theme)
	}
}

func TestAnalyzeThemeTrendRoutePrefersKaipanlaLeaderAttribution(t *testing.T) {
	theme := analyzeTheme(
		"600000.SH",
		ShortTermAnalysis{},
		[]foundation.StockThemeAttribution{
			{Symbol: "600000.SH", Theme: "算力租赁", Source: "duanxianxia:kaipanla-theme-leader", TradeDate: "2026-08-07", Role: "龙一"},
			{Symbol: "600000.SH", Theme: "数据中心", Source: "duanxianxia:kaipanla-limit-up", TradeDate: "2026-08-07"},
		},
		[]string{"国企改革"},
		"银行",
		[]foundation.ThemeOverview{{Name: "算力租赁", TrendScore: 88, TrendStage: "主升"}},
		nil,
	)
	if theme.Primary != "算力租赁" || theme.Route != "trend" || !strings.Contains(theme.Source, "kaipanla-theme-leader") || theme.Role != "龙一" {
		t.Fatalf("trend route did not prefer leader attribution: %+v", theme)
	}
}

func TestAnalyzeThemeUsesBusinessWhenNoHotAttribution(t *testing.T) {
	theme := analyzeTheme(
		"003032.SZ",
		ShortTermAnalysis{},
		nil,
		[]string{"职业教育", "人工智能"},
		"教育",
		nil,
		nil,
	)
	if theme.Primary != "教育" || theme.Business != "教育" || theme.IsHot || theme.Source != "eastmoney-f10-business" {
		t.Fatalf("business should remain primary without hot attribution: %+v", theme)
	}
}

func TestAnalyzeThemeDoesNotPromoteBroadConceptToHotTheme(t *testing.T) {
	theme := analyzeTheme(
		"688297.SH", ShortTermAnalysis{}, nil,
		[]string{"军工", "西部大开发", "无人机"}, "航天装备",
		[]foundation.ThemeOverview{{Name: "西部大开发", TrendScore: 95, TrendStage: "主升"}}, nil,
		"无人机系统", "主要从事无人机系统研发、生产制造、销售和服务", "eastmoney:f10-business",
	)
	if theme.Primary != "无人机系统" || theme.IsHot || theme.HotTheme != "" || theme.TrendScore != 0 {
		t.Fatalf("broad concept was incorrectly promoted to hot theme: %+v", theme)
	}
}

func TestAnalyzeNonShortStockIncludesFundamentalsAndResearch(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 180, 10, 0.08, 1_500_000_000)
	analysis, err := Analyze(Input{
		Symbol: "600519.SH", KLines: lines,
		Fundamentals: &foundation.StockFundamentals{ReportDate: "2026-03-31", ReportName: "2026一季报", RevenueYearOverYear: 18, NetProfitYearOverYear: 25, ROE: 16, GrossMargin: 55, DebtRatio: 25, OperatingCashFlowPerShare: 2, Meta: foundation.SourceMeta{Source: "eastmoney:f10-financials"}},
		Reports:      []foundation.MarketResearchItem{{ID: "r1", Title: "业绩稳健增长", Organization: "测试证券", Rating: "买入", PublishedAt: time.Now()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType == "emotion_leader" || analysis.Fundamental == nil || !analysis.Fundamental.Available || analysis.Research == nil || !analysis.Research.Available {
		t.Fatalf("non-short analysis missing fundamentals or research: %+v", analysis)
	}
	keys := map[string]bool{}
	for _, item := range analysis.Scorecard.Dimensions {
		keys[item.Key] = true
	}
	if !keys["fundamental"] || !keys["research"] {
		t.Fatalf("scorecard missing non-short dimensions: %+v", analysis.Scorecard.Dimensions)
	}
}

func TestAnalyzeEmotionLeaderSkipsFundamentalsAndResearch(t *testing.T) {
	lines := syntheticTrendLines("003032.SZ", 60, 10, 0.1, 800_000_000)
	now := time.Now()
	analysis, err := Analyze(Input{
		Symbol: "003032.SZ", KLines: lines,
		LimitUps:     []foundation.LimitUpEvent{{Symbol: "003032.SZ", Date: now.AddDate(0, 0, -2), Streak: 2}, {Symbol: "003032.SZ", Date: now.AddDate(0, 0, -1), Streak: 3}},
		Fundamentals: &foundation.StockFundamentals{ReportDate: "2026-03-31", ReportName: "2026一季报"},
		Reports:      []foundation.MarketResearchItem{{ID: "r1", Title: "测试研报"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType != "emotion_leader" || analysis.Fundamental != nil || analysis.Research != nil {
		t.Fatalf("emotion leader should keep short-term route: %+v", analysis)
	}
}

func TestBenchmarkForSymbolRoutesByBoard(t *testing.T) {
	tests := map[string]string{
		"600519.SH": "000001.SH",
		"000001.SZ": "399001.SZ",
		"300750.SZ": "399006.SZ",
		"688981.SH": "000688.SH",
		"832000.BJ": "899050.BJ",
	}
	for symbol, want := range tests {
		got, _ := BenchmarkForSymbol(symbol)
		if got != want {
			t.Fatalf("BenchmarkForSymbol(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestEnrichWithAIReplacesOnlyNarrativeConclusion(t *testing.T) {
	analysis := Analysis{
		Symbol:     "600519.SH",
		Name:       "测试股",
		Profile:    Profile{PrimaryType: "trend_capacity", TypeLabel: "趋势容量型"},
		Trend:      TrendAnalysis{Score: 78, Phase: "主升"},
		Conclusion: Conclusion{BestPath: "原始路径", MainRisk: "原始风险"},
	}
	prompter := fakeStockPrompter{content: `{"headline":"趋势仍在但不追高","summary":"中期结构保持向上，当前更适合等待回踩或放量突破确认。","action":"等待回踩确认","best_path":"缩量回踩后重新放量","main_risk":"跌破中期趋势线"}`}
	if err := EnrichWithAI(context.Background(), prompter, &analysis, ""); err != nil {
		t.Fatal(err)
	}
	if analysis.Conclusion.Source != "hermes-ai" || analysis.AI.Status != "ready" {
		t.Fatalf("AI synthesis not applied: %+v", analysis)
	}
	if analysis.Trend.Score != 78 || analysis.Profile.PrimaryType != "trend_capacity" {
		t.Fatalf("AI must not rewrite deterministic fields: %+v", analysis)
	}
}

func syntheticTrendLines(symbol string, count int, start, step, amount float64) []foundation.KLine {
	items := make([]foundation.KLine, 0, count)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	previous := start
	for index := 0; index < count; index++ {
		closePrice := start + float64(index)*step
		if closePrice <= 1 {
			closePrice = 1 + float64(count-index)*0.02
		}
		change := 0.0
		if previous > 0 {
			change = (closePrice/previous - 1) * 100
		}
		items = append(items, foundation.KLine{
			Symbol: symbol, Time: base.AddDate(0, 0, index), Open: closePrice * 0.995,
			High: closePrice * 1.018, Low: closePrice * 0.982, Close: closePrice,
			Volume: 50_000_000, Amount: amount, TurnoverRate: 3.5, ChangePercent: change,
			Meta: foundation.SourceMeta{Source: "test", FetchedAt: base.AddDate(0, 0, index)},
		})
		previous = closePrice
	}
	return items
}

type fakeStockPrompter struct{ content string }

func (p fakeStockPrompter) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return hermes.PromptResult{Content: p.content}, nil
}
