package stockanalysis

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
)

func TestAnalyzeNewListingWithOneTradingDay(t *testing.T) {
	lines := syntheticTrendLines("688836.SH", 1, 86, 0, 920_000_000)
	lines[0].Open = 72
	lines[0].High = 96
	lines[0].Low = 70
	lines[0].Close = 88
	lines[0].ChangePercent = 22.22
	lines[0].TurnoverRate = 58
	analysis, err := Analyze(Input{Symbol: "688836.SH", Quote: foundation.Quote{Symbol: "688836.SH", Name: "测试新股", Price: 88}, KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType != "new_listing" || analysis.Profile.Confidence >= .5 {
		t.Fatalf("unexpected new-listing profile: %+v", analysis.Profile)
	}
	if analysis.Trend.HistoryDays != 1 || analysis.Trend.ListingHigh != 96 || analysis.Trend.ListingLow != 70 || analysis.Trend.ListingReturn == 0 {
		t.Fatalf("listing-period trend missing: %+v", analysis.Trend)
	}
	if analysis.Trend.MA20 != 0 || analysis.Trend.MA60 != 0 || analysis.Trend.MA120 != 0 || analysis.Trend.ATR14Percent != 0 || analysis.Trend.Return20 != 0 {
		t.Fatalf("new-listing analysis must not fabricate mature indicators: %+v", analysis.Trend)
	}
	if analysis.RiskControl.StopPrice <= 0 || analysis.RiskControl.StopPrice >= analysis.RiskControl.EntryReference || analysis.RiskControl.SuggestedPositionMax > 10 {
		t.Fatalf("new-listing risk controls are invalid: %+v", analysis.RiskControl)
	}
	if analysis.ActionPlan.DecisionLabel != "新股价格发现" || analysis.ActionPlan.Entry.PriceLow <= analysis.RiskControl.StopPrice || analysis.ActionPlan.StopLoss.PriceHigh != analysis.RiskControl.StopPrice {
		t.Fatalf("new-listing action plan is incomplete: %+v", analysis.ActionPlan)
	}
	if len(analysis.Timeframes) != 4 || analysis.Relative.Available || analysis.Scorecard.Conviction != "较低" {
		t.Fatalf("limited-sample workspace is misleading: timeframes=%+v relative=%+v scorecard=%+v", analysis.Timeframes, analysis.Relative, analysis.Scorecard)
	}
	if analysis.Scorecard.AlgorithmVersion != "stock-score-v2" || analysis.Scorecard.Overall > 80 {
		t.Fatalf("new-listing scorecard must use the capped V2 observation scale: %+v", analysis.Scorecard)
	}
	foundLimitedKLine := false
	for _, item := range analysis.DataQuality {
		if item.Key == "kline" && item.Status == "limited" && strings.Contains(item.Message, "新股价格发现模型") {
			foundLimitedKLine = true
		}
	}
	if !foundLimitedKLine {
		t.Fatalf("limited K-line quality missing: %+v", analysis.DataQuality)
	}
}

func TestAnalyzeNewListingBoundaryAtNineteenTradingDays(t *testing.T) {
	lines := syntheticTrendLines("301678.SZ", 19, 20, .3, 650_000_000)
	analysis, err := Analyze(Input{Symbol: "301678.SZ", KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType != "new_listing" || analysis.Trend.HistoryDays != 19 {
		t.Fatalf("19-day stock did not use new-listing flow: profile=%+v trend=%+v", analysis.Profile, analysis.Trend)
	}
	if state := analysis.Timeframes[1].State; !strings.Contains(state, "1个交易日") {
		t.Fatalf("MA20 maturity status = %q", state)
	}
}

func TestAnalyzeUsesMatureFlowAtTwentyTradingDays(t *testing.T) {
	analysis, err := Analyze(Input{Symbol: "600000.SH", KLines: syntheticTrendLines("600000.SH", 20, 10, .1, 500_000_000)})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Profile.PrimaryType == "new_listing" || analysis.Trend.MA20 <= 0 {
		t.Fatalf("20-day stock must use mature flow: profile=%+v trend=%+v", analysis.Profile, analysis.Trend)
	}
}

func TestAnalyzeRejectsNoValidKLines(t *testing.T) {
	_, err := Analyze(Input{Symbol: "688836.SH", KLines: []foundation.KLine{{Close: 0}}})
	if err == nil || !strings.Contains(err.Error(), "至少需要1个有效交易日") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnrichWithAIPreservesNewListingRiskPlan(t *testing.T) {
	lines := syntheticTrendLines("688836.SH", 1, 86, 0, 920_000_000)
	lines[0].Open, lines[0].High, lines[0].Low, lines[0].Close = 72, 96, 70, 88
	analysis, err := Analyze(Input{Symbol: "688836.SH", KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	originalPlan := analysis.ActionPlan
	prompt := ""
	prompter := fakeStockPrompter{prompt: &prompt, content: `{
		"headline":"上市初期继续观察","summary":"交易样本不足二十日，价格仍处于发现阶段，不能据此外推成熟趋势。","action":"等待更多交易日","best_path":"观察波动收敛","main_risk":"样本不足",
		"decision":{"decision_mode":"non_short","decision_label":"成熟趋势定价","decision_confidence":0.99,"horizon":"中期","rationale":"模型尝试覆盖","current_action":"追涨","position_hint":"重仓","non_short_price_plan":{"entry":{"price_low":90,"price_high":92,"reason":"假计划","action":"买入"},"hold":{"price_low":89,"price_high":95,"reason":"假计划","action":"持有"},"take_profit":{"price_low":100,"price_high":110,"reason":"假计划","action":"止盈"},"stop_loss":{"price_high":85,"reason":"假计划","action":"止损"}}}
	}`}
	if err := EnrichWithAI(context.Background(), prompter, &analysis, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "profile.primary_type=new_listing") || !strings.Contains(prompt, "不得推断MA20/60/120") {
		t.Fatalf("new-listing AI constraints missing from prompt: %s", prompt)
	}
	if analysis.ActionPlan.DecisionLabel != originalPlan.DecisionLabel || analysis.ActionPlan.CurrentAction != originalPlan.CurrentAction || analysis.ActionPlan.Entry.PriceLow != originalPlan.Entry.PriceLow || analysis.RiskControl.SuggestedPositionMax > 10 {
		t.Fatalf("AI overrode constrained new-listing plan: before=%+v after=%+v risk=%+v", originalPlan, analysis.ActionPlan, analysis.RiskControl)
	}
}

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
	if analysis.ActionPlan.Entry.PriceLow <= analysis.RiskControl.StopPrice || analysis.ActionPlan.Entry.PriceHigh < analysis.ActionPlan.Entry.PriceLow {
		t.Fatalf("entry price zone is not executable: action=%+v risk=%+v", analysis.ActionPlan, analysis.RiskControl)
	}
	if analysis.ActionPlan.Hold.PriceLow <= analysis.RiskControl.StopPrice || analysis.ActionPlan.Hold.PriceHigh < analysis.ActionPlan.Hold.PriceLow {
		t.Fatalf("hold price zone is not executable: action=%+v risk=%+v", analysis.ActionPlan, analysis.RiskControl)
	}
	if analysis.ActionPlan.StopLoss.PriceHigh != analysis.RiskControl.StopPrice || analysis.ActionPlan.StopLoss.Reason == "" || analysis.ActionPlan.Entry.Action == "" {
		t.Fatalf("price decision reasons are incomplete: action=%+v risk=%+v", analysis.ActionPlan, analysis.RiskControl)
	}
	if analysis.ActionPlan.TakeProfit.PriceLow != analysis.RiskControl.TakeProfitFirst || analysis.ActionPlan.TakeProfit.PriceHigh != analysis.RiskControl.TakeProfitSecond {
		t.Fatalf("take-profit price zone must match risk targets: action=%+v risk=%+v", analysis.ActionPlan, analysis.RiskControl)
	}
	if analysis.ActionPlan.TakeProfit.PriceLow <= analysis.ActionPlan.Entry.PriceHigh {
		t.Fatalf("take-profit zone overlaps entry zone: action=%+v", analysis.ActionPlan)
	}
	if analysis.RiskControl.EntryReference < analysis.ActionPlan.Entry.PriceLow || analysis.RiskControl.EntryReference > analysis.ActionPlan.Entry.PriceHigh {
		t.Fatalf("risk entry reference must use planned entry zone: action=%+v risk=%+v", analysis.ActionPlan, analysis.RiskControl)
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
	if analysis.ActionPlan.Entry.PriceLow <= analysis.Trend.LatestClose || !strings.Contains(analysis.ActionPlan.Entry.Action, "不是当前抄底价") {
		t.Fatalf("weak-risk entry must require a right-side recovery price: %+v", analysis.ActionPlan.Entry)
	}
	if analysis.ActionPlan.StopLoss.PriceText == "" || analysis.ActionPlan.StopLoss.PriceHigh != analysis.RiskControl.StopPrice {
		t.Fatalf("weak-risk stop-loss price is unclear: %+v", analysis.ActionPlan.StopLoss)
	}
	if analysis.ActionPlan.TakeProfit.PriceLow <= analysis.ActionPlan.Entry.PriceHigh {
		t.Fatalf("weak-risk take-profit must be above confirmed entry zone: %+v", analysis.ActionPlan)
	}
}

func TestBuildActionPriceZonesFallsBackWithoutTechnicalLevels(t *testing.T) {
	trend := TrendAnalysis{LatestClose: 10, ATR14Percent: 0}
	risk := RiskControl{EntryReference: 10, StopPrice: 9}
	plan := buildActionPlan(Profile{PrimaryType: "range_watch"}, trend, ShortTermAnalysis{}, ThemeAnalysis{}, nil, RelativeStrength{}, risk, ShortTermQuantitativePlan{}, trend.LatestClose)
	for _, zone := range []ActionPriceZone{plan.Entry, plan.Hold, plan.TakeProfit, plan.StopLoss} {
		if zone.PriceText == "" || zone.Reason == "" || zone.Action == "" || zone.PriceHigh <= 0 {
			t.Fatalf("fallback price zone is incomplete: %+v", zone)
		}
	}
	if plan.Entry.PriceLow <= risk.StopPrice || plan.Entry.PriceHigh < plan.Entry.PriceLow {
		t.Fatalf("fallback entry zone is invalid: %+v", plan.Entry)
	}
	if plan.Hold.PriceLow <= risk.StopPrice || plan.Hold.PriceHigh < plan.Hold.PriceLow {
		t.Fatalf("fallback hold zone is invalid: %+v", plan.Hold)
	}
	if plan.TakeProfit.PriceLow <= plan.Entry.PriceHigh {
		t.Fatalf("fallback take-profit zone overlaps entry: action=%+v", plan)
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
	if len(analysis.Timeframes) != 5 || len(analysis.Signals) < 6 || len(analysis.Scorecard.Dimensions) < 6 {
		t.Fatalf("complete score workspace missing: timeframes=%d signals=%d dimensions=%d", len(analysis.Timeframes), len(analysis.Signals), len(analysis.Scorecard.Dimensions))
	}
	if analysis.Scorecard.AlgorithmVersion != "stock-score-v2" {
		t.Fatalf("score algorithm version = %q", analysis.Scorecard.AlgorithmVersion)
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

func TestScorecardV2CalibratesNeutralEvidenceWithoutInventingMissingDimensions(t *testing.T) {
	signals := []Signal{
		{Key: "trend", Label: "趋势结构", Tone: "neutral", Strength: 50},
		{Key: "timeframe", Label: "周期一致性", Tone: "neutral", Strength: 50},
		{Key: "momentum", Label: "价格动能", Tone: "neutral", Strength: 50},
		{Key: "volume", Label: "量价配合", Tone: "neutral", Strength: 50},
		{Key: "risk", Label: "风险约束", Tone: "neutral", Strength: 50},
	}
	scorecard := buildScorecard(Profile{PrimaryType: "trend_capacity", Confidence: .7}, signals)
	if scorecard.Overall != 58 || scorecard.AlgorithmVersion != "stock-score-v2" {
		t.Fatalf("neutral V2 scorecard = %+v", scorecard)
	}
	weightTotal := 0.0
	for _, dimension := range scorecard.Dimensions {
		weightTotal += dimension.Weight
		if dimension.Key == "fundamental" || dimension.Key == "research" || dimension.Key == "market" {
			t.Fatalf("missing evidence was added to scorecard: %+v", scorecard.Dimensions)
		}
	}
	if math.Abs(weightTotal-1) > .0001 {
		t.Fatalf("effective weights sum to %.4f", weightTotal)
	}
}

func TestStockScoreV2UsesGentleCalibration(t *testing.T) {
	for _, item := range []struct {
		base, want float64
		positive   int
	}{
		{base: 40, positive: 0, want: 50},
		{base: 50, positive: 0, want: 58},
		{base: 70, positive: 3, want: 76},
		{base: 80, positive: 5, want: 87},
	} {
		if got := calibratedStockScore(item.base, item.positive); got != int(item.want) {
			t.Fatalf("calibratedStockScore(%.0f, %d) = %d, want %.0f", item.base, item.positive, got, item.want)
		}
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

func TestAnalyzeThemePromotesAnnouncementBackedCompoundSemiconductor(t *testing.T) {
	lines := syntheticTrendLines("300720.SZ", 80, 10, 0.18, 900_000_000)
	analysis, err := Analyze(Input{
		Symbol: "300720.SZ", Quote: foundation.Quote{Symbol: "300720.SZ", Name: "海川智能", Price: lines[len(lines)-1].Close}, KLines: lines,
		Business: "智能衡器", BusinessDetail: "智能衡器、物联网设备", Concepts: []string{"物联网"},
		Announcements: []foundation.MarketResearchItem{{Title: "关于参股砷化镓半导体企业的公告", Category: "重大事项", PublishedAt: time.Now(), Meta: foundation.SourceMeta{Source: "eastmoney:announcement"}}},
		Themes:        []foundation.ThemeOverview{{Name: "半导体芯片", TrendScore: 82, RisingNodes: 18, MatchedNodes: 24, LimitUpCount: 3, ActiveDays: 5, FiveDayStrengthScore: 78}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(analysis.Theme.Primary, "化合物半导体") || !strings.Contains(analysis.Theme.Primary, "砷化镓") || !analysis.Theme.IsHot || analysis.Theme.HotScore < 55 {
		t.Fatalf("unexpected announcement attribution: %+v", analysis.Theme)
	}
	if !analysis.Theme.Resonance.Available || analysis.Theme.Resonance.Score <= 0 {
		t.Fatalf("theme resonance missing: %+v", analysis.Theme.Resonance)
	}
}

func TestAnalyzeThemeFallsBackToBusinessWhenHotThemePriceDoesNotMatch(t *testing.T) {
	lines := syntheticTrendLines("688521.SH", 80, 260, -1, 1_500_000_000)
	analysis, err := Analyze(Input{
		Symbol:   "688521.SH",
		Quote:    foundation.Quote{Symbol: "688521.SH", Name: "芯原股份", Price: lines[len(lines)-1].Close},
		KLines:   lines,
		Business: "集成电路", BusinessDetail: "公司主营半导体IP授权和芯片定制服务", BusinessSource: "eastmoney:f10-business",
		Announcements: []foundation.MarketResearchItem{{
			Title:       "关于提质增效重回报行动方案的半年度评估报告",
			Content:     "公司为卫星通信客户开发芯片IP和集成电路解决方案。",
			PublishedAt: time.Now(),
			Meta:        foundation.SourceMeta{Source: "eastmoney:announcement"},
		}},
		Catalog: []foundation.StockCatalogEntry{
			{BoardStock: foundation.BoardStock{Symbol: "600001.SH", Name: "航天甲", ChangePercent: 8, FiveDayChangePercent: 18}, Concepts: []string{"商业航天"}},
			{BoardStock: foundation.BoardStock{Symbol: "600002.SH", Name: "航天乙", ChangePercent: 6, FiveDayChangePercent: 14}, Concepts: []string{"商业航天"}},
			{BoardStock: foundation.BoardStock{Symbol: "600003.SH", Name: "航天丙", ChangePercent: 4, FiveDayChangePercent: 10}, Concepts: []string{"商业航天"}},
		},
		Themes: []foundation.ThemeOverview{{Name: "商业航天", TrendScore: 82, RisingNodes: 5, MatchedNodes: 5, LimitUpCount: 5, ActiveDays: 5, FiveDayStrengthScore: 80}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Theme.IsHot || analysis.Theme.HotTheme != "" || analysis.Theme.Primary != "集成电路" || analysis.Theme.Source != "eastmoney:f10-business" {
		t.Fatalf("price-divergent hot theme should fall back to main business: %+v", analysis.Theme)
	}
	if analysis.Theme.Resonance.State != "价格未确认" || !strings.Contains(analysis.Theme.Description, "5日涨幅未跟随") || !strings.Contains(analysis.Theme.Description, "公司主业集成电路") {
		t.Fatalf("price rejection reason missing: %+v", analysis.Theme)
	}
	if len(analysis.Theme.ConfirmedThemes) != 1 || analysis.Theme.ConfirmedThemes[0].Name != "商业航天" {
		t.Fatalf("rejected theme evidence should remain inspectable: %+v", analysis.Theme.ConfirmedThemes)
	}
	foundThemeQuality := false
	for _, item := range analysis.DataQuality {
		if item.Key == "theme" && strings.Contains(item.Message, "未通过个股涨幅验证") && strings.Contains(item.Message, "集成电路") {
			foundThemeQuality = true
		}
	}
	if !foundThemeQuality {
		t.Fatalf("theme price validation quality missing: %+v", analysis.DataQuality)
	}
}

func TestAnalyzeThemeIgnoresPublicationTitleKeyword(t *testing.T) {
	lines := syntheticTrendLines("601858.SH", 80, 18, 0.08, 650_000_000)
	analysis, err := Analyze(Input{
		Symbol: "601858.SH", Quote: foundation.Quote{Symbol: "601858.SH", Name: "中国科传", Price: lines[len(lines)-1].Close}, KLines: lines,
		Business: "科技出版、期刊出版与知识服务", Industry: "平面媒体", Concepts: []string{"知识产权", "数据要素", "在线教育", "央国企改革", "数字经济"},
		Announcements: []foundation.MarketResearchItem{{
			Title:       "中国科技出版传媒股份有限公司2025年年度股东会会议材料",
			Category:    "股东大会资料",
			Content:     "公司坚持科技出版主责主业。公司‘智能机器人基础理论与关键技术丛书’‘6G信息通信网络丛书’等9个项目入选国家出版基金项目。",
			PublishedAt: time.Now(),
			Meta:        foundation.SourceMeta{Source: "eastmoney:announcement"},
		}},
		Themes: []foundation.ThemeOverview{{Name: "机器人概念", TrendScore: 96, RisingNodes: 30, MatchedNodes: 32, LimitUpCount: 8, ActiveDays: 9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Theme.IsHot || analysis.Theme.HotTheme != "" || analysis.Theme.Primary != "科技出版、期刊出版与知识服务" {
		t.Fatalf("publication title keyword was incorrectly promoted: %+v", analysis.Theme)
	}
}

func TestAnalyzeThemeDoesNotPromoteModelMappingFromStrongMarketAlone(t *testing.T) {
	lines := syntheticTrendLines("601858.SH", 80, 18, 0.08, 650_000_000)
	analysis, err := Analyze(Input{
		Symbol: "601858.SH", Quote: foundation.Quote{Symbol: "601858.SH", Name: "中国科传", Price: lines[len(lines)-1].Close}, KLines: lines,
		Business: "科技出版、期刊出版与知识服务", Industry: "平面媒体",
		ModelThemeEvidence: []ThemeEvidence{{Theme: "机器人", Type: "market_mapping", Source: "hermes-ai", Snippet: "弱产业链映射", Strength: .98, Freshness: 1}},
		Themes:             []foundation.ThemeOverview{{Name: "机器人概念", TrendScore: 99, RisingNodes: 30, MatchedNodes: 32, LimitUpCount: 8, ActiveDays: 9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Theme.IsHot || analysis.Theme.HotTheme != "" || analysis.Theme.Primary != "科技出版、期刊出版与知识服务" {
		t.Fatalf("market strength alone promoted a model mapping: %+v", analysis.Theme)
	}
}

func TestThemeCatalogOnlyDoesNotCreateHotTheme(t *testing.T) {
	lines := syntheticTrendLines("688297.SH", 80, 10, 0.02, 900_000_000)
	analysis, err := Analyze(Input{Symbol: "688297.SH", Quote: foundation.Quote{Symbol: "688297.SH", Name: "样本股", Price: lines[len(lines)-1].Close}, KLines: lines, Business: "无人机系统", Concepts: []string{"西部大开发", "无人机"}, Themes: []foundation.ThemeOverview{{Name: "西部大开发", TrendScore: 95, RisingNodes: 30, MatchedNodes: 30}}})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Theme.IsHot || analysis.Theme.Primary != "无人机系统" {
		t.Fatalf("catalog-only concept was promoted: %+v", analysis.Theme)
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

func TestAnalyzeIncludesStockAndThemeNews(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 180, 10, 0.08, 1_500_000_000)
	now := time.Now()
	analysis, err := Analyze(Input{
		Symbol:   "600519.SH",
		Quote:    foundation.Quote{Symbol: "600519.SH", Name: "贵州茅台", Price: lines[len(lines)-1].Close},
		KLines:   lines,
		Business: "白酒生产与销售",
		Concepts: []string{"白酒消费"},
		Announcements: []foundation.MarketResearchItem{{
			ID: "a1", Title: "贵州茅台关于实施股份回购的公告", PublishedAt: now.Add(-2 * time.Hour), URL: "https://example.com/a1", Meta: foundation.SourceMeta{Source: "eastmoney:announcement"},
		}},
		News: []foundation.NewsItem{
			{ID: "n1", Title: "贵州茅台渠道改革带动订单增长", Content: "公司订单增长，市场关注后续落地。", PublishedAt: now.Add(-time.Hour), Meta: foundation.SourceMeta{Source: "cls"}},
			{ID: "n2", Title: "白酒消费迎来政策支持", Content: "行业景气预期改善。", PublishedAt: now.Add(-3 * time.Hour), Meta: foundation.SourceMeta{Source: "cls"}},
			{ID: "n3", Title: "无关市场新闻", PublishedAt: now.Add(-4 * time.Hour), Meta: foundation.SourceMeta{Source: "cls"}},
			{ID: "n4", Title: "贵州茅台旧新闻", PublishedAt: now.AddDate(0, 0, -45), Meta: foundation.SourceMeta{Source: "cls"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.StockNews == nil || !analysis.StockNews.Available || analysis.StockNews.ArticleCount != 2 {
		t.Fatalf("stock news analysis missing: %+v", analysis.StockNews)
	}
	if analysis.ThemeNews == nil || !analysis.ThemeNews.Available || analysis.ThemeNews.ArticleCount != 1 {
		t.Fatalf("theme news analysis missing: %+v", analysis.ThemeNews)
	}
	if len(analysis.StockNews.Catalysts) == 0 || analysis.StockNews.AnalysisSource != "local-rules" {
		t.Fatalf("stock news catalysts missing: %+v", analysis.StockNews)
	}
	quality := map[string]string{}
	for _, item := range analysis.DataQuality {
		quality[item.Key] = item.Status
	}
	if quality["stock_news"] != "ready" || quality["theme_news"] != "ready" {
		t.Fatalf("news data quality missing: %+v", quality)
	}
}

func TestAnalyzeEmotionLeaderBuildsDynamicShortTermPlan(t *testing.T) {
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
	if analysis.Profile.PrimaryType != "emotion_leader" || analysis.Fundamental == nil || analysis.Research == nil {
		t.Fatalf("emotion leader should retain global evidence for AI classification: %+v", analysis)
	}
	if analysis.ActionPlan.DecisionMode != "short_term" || analysis.ActionPlan.ShortTerm == nil {
		t.Fatalf("emotion leader should use dynamic short-term plan: %+v", analysis.ActionPlan)
	}
	if analysis.ActionPlan.Entry.PriceText != "" || analysis.ActionPlan.ShortTerm.Auction.Status == "" || len(analysis.ActionPlan.ShortTerm.VetoConditions) == 0 {
		t.Fatalf("short-term plan must use auction conditions instead of static prices: %+v", analysis.ActionPlan)
	}
}

func TestAnalyzeShortTermPlanUsesQuantifiedIndexAndThemePeers(t *testing.T) {
	lines := syntheticTrendLines("003032.SZ", 60, 10, .1, 800_000_000)
	benchmarkLines := syntheticTrendLines("399001.SZ", 60, 10_000, 8, 100_000_000_000)
	tradeDate := time.Date(2026, 8, 17, 0, 0, 0, 0, time.Local)
	analysis, err := Analyze(Input{
		Symbol:          "003032.SZ",
		Quote:           foundation.Quote{Symbol: "003032.SZ", Name: "目标股", Price: lines[len(lines)-1].Close},
		KLines:          lines,
		BenchmarkSymbol: "399001.SZ",
		BenchmarkName:   "深证成指",
		BenchmarkKLines: benchmarkLines,
		CachedThemes:    []foundation.StockThemeAttribution{{Symbol: "003032.SZ", Theme: "机器人概念", Source: "duanxianxia:kaipanla-limit-up", TradeDate: "2026-08-17"}},
		LimitUps: []foundation.LimitUpEvent{
			{Symbol: "003032.SZ", Name: "目标股", Date: tradeDate, Streak: 3, PrimaryTheme: "机器人概念", Amount: 900_000_000},
			{Symbol: "000001.SZ", Name: "同行甲", Date: tradeDate, Streak: 4, PrimaryTheme: "机器人概念", ThemeLeaderRole: "龙一", Amount: 1_200_000_000},
			{Symbol: "000002.SZ", Name: "同行乙", Date: tradeDate, Streak: 2, PrimaryTheme: "机器人概念", ThemeLeaderRole: "龙二", Amount: 800_000_000},
			{Symbol: "000003.SZ", Name: "同行丙", Date: tradeDate, Streak: 1, PrimaryTheme: "机器人概念", Amount: 600_000_000},
		},
		Catalog: []foundation.StockCatalogEntry{
			{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "同行甲", Price: 12, ChangePercent: 9.98, Amount: 1_200_000_000, LimitUpStreak: 4}, Concepts: []string{"机器人概念"}},
			{BoardStock: foundation.BoardStock{Symbol: "000002.SZ", Name: "同行乙", Price: 8, ChangePercent: 5.2, Amount: 800_000_000, LimitUpStreak: 2}, Concepts: []string{"机器人概念"}},
			{BoardStock: foundation.BoardStock{Symbol: "000003.SZ", Name: "同行丙", Price: 6, ChangePercent: 2.1, Amount: 600_000_000, LimitUpStreak: 1}, Concepts: []string{"机器人概念"}},
		},
		Themes: []foundation.ThemeOverview{{Name: "机器人概念", LimitUpCount: 4, BoardCount: 3, MaxStreak: 4, ActiveDays: 5, Leaders: []string{"同行甲 4板", "同行乙 2板", "同行丙 1板"}, TradeDate: "2026-08-17", Source: "duanxianxia"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	playbook := analysis.ActionPlan.ShortTerm
	if playbook == nil {
		t.Fatalf("quantified short-term playbook missing: %+v", analysis.ActionPlan)
	}
	quantitative := playbook.Quantitative
	if quantitative.Benchmark.Name != "深证成指" || quantitative.Stock.AuctionAmountMin <= 0 || quantitative.Stock.AuctionPriceMin <= 0 {
		t.Fatalf("stock/index thresholds missing: %+v", quantitative)
	}
	if quantitative.Theme.Name != "机器人概念" || quantitative.Theme.MinimumPositivePeers != 2 || len(quantitative.Peers) < 3 {
		t.Fatalf("theme peer thresholds missing: %+v", quantitative)
	}
	required := strings.Join(playbook.Auction.Required, "；")
	if !strings.Contains(required, "深证成指") || !strings.Contains(required, "同行甲") || !strings.Contains(required, "竞价成交额") {
		t.Fatalf("auction conditions are not concrete enough: %s", required)
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
		StockNews:  &NewsAnalysis{Available: true, Summary: "本地个股新闻结论", AnalysisSource: "local-rules"},
		ThemeNews:  &NewsAnalysis{Available: true, Summary: "本地题材新闻结论", AnalysisSource: "local-rules"},
	}
	capturedPrompt := ""
	prompter := fakeStockPrompter{content: `{"headline":"趋势仍在但不追高","summary":"中期结构保持向上，当前更适合等待回踩或放量突破确认。","action":"等待回踩确认","best_path":"缩量回踩后重新放量","main_risk":"跌破中期趋势线","stock_news":{"tone":"偏多","summary":"订单与回购构成潜在催化，但需要观察兑现节奏。","catalysts":["订单增长"],"risks":["兑现不及预期"]},"theme_news":{"tone":"中性","summary":"题材有政策催化，持续性仍需板块扩散确认。","catalysts":["政策支持"],"risks":[]}}`, prompt: &capturedPrompt}
	if err := EnrichWithAI(context.Background(), prompter, &analysis, ""); err != nil {
		t.Fatal(err)
	}
	if analysis.Conclusion.Source != "hermes-ai" || analysis.AI.Status != "ready" {
		t.Fatalf("AI synthesis not applied: %+v", analysis)
	}
	if analysis.Trend.Score != 78 || analysis.Profile.PrimaryType != "trend_capacity" {
		t.Fatalf("AI must not rewrite deterministic fields: %+v", analysis)
	}
	if analysis.StockNews.AnalysisSource != "hermes-ai" || analysis.StockNews.Tone != "偏多" || analysis.ThemeNews.AnalysisSource != "hermes-ai" {
		t.Fatalf("AI news synthesis not applied: stock=%+v theme=%+v", analysis.StockNews, analysis.ThemeNews)
	}
	if !strings.Contains(capturedPrompt, `"stock_news"`) || !strings.Contains(capturedPrompt, `"theme_news"`) {
		t.Fatalf("news payload missing from AI prompt: %s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, `"price_context"`) || !strings.Contains(capturedPrompt, `"daily_bars"`) || !strings.Contains(capturedPrompt, `"current_price"`) {
		t.Fatalf("price and daily K-line context missing from AI prompt: %s", capturedPrompt)
	}
}

func TestEnrichWithAIAppliesGlobalNonShortPricePlan(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 180, 10, .08, 1_500_000_000)
	analysis, err := Analyze(Input{Symbol: "600519.SH", KLines: lines})
	if err != nil {
		t.Fatal(err)
	}
	prompter := fakeStockPrompter{content: `{
		"headline":"等待全局条件共振","summary":"趋势结构保持，但价格计划同时考虑题材持续性、基本面质量和资金承接。","action":"等待进入允许介入区后确认","best_path":"基本面预期稳定且回踩承接","main_risk":"题材转弱并跌破逻辑失效位",
		"decision":{"decision_mode":"non_short","decision_label":"趋势与价值定价","decision_confidence":0.86,"horizon":"波段 / 1—3个月","rationale":"结合趋势支撑、基本面预期和题材持续性确定价格边界。","current_action":"等待回踩确认","position_hint":"首次验证不超过三成仓位","non_short_price_plan":{"entry":{"price_low":23,"price_high":24,"reason":"趋势支撑与基本面预期交集形成安全边际。","action":"缩量止跌并重新转强后分批介入。"},"hold":{"price_low":22,"price_high":28,"reason":"逻辑失效位上方且产业预期未下修时继续持有。","action":"趋势延续则持有，放量滞涨时降仓。"},"take_profit":{"price_low":28,"price_high":34,"reason":"前高压力与估值兑现区共同构成分批止盈目标。","action":"第一目标处理本金风险，第二目标不追高。"},"stop_loss":{"price_low":0,"price_high":21,"reason":"跌破结构支撑且题材与资金同步转弱，原逻辑失效。","action":"触发后减仓或止损。"}},"short_term_playbook":{}}
	}`}
	if err := EnrichWithAI(context.Background(), prompter, &analysis, ""); err != nil {
		t.Fatal(err)
	}
	if analysis.ActionPlan.DecisionMode != "non_short" || analysis.ActionPlan.PricingSource != "hermes-ai" {
		t.Fatalf("AI non-short decision not applied: %+v", analysis.ActionPlan)
	}
	if analysis.ActionPlan.Entry.PriceLow != 23 || analysis.ActionPlan.StopLoss.PriceHigh != 21 || analysis.ActionPlan.TakeProfit.PriceLow != 28 {
		t.Fatalf("AI price plan not applied: %+v", analysis.ActionPlan)
	}
	if analysis.RiskControl.EntryReference != 23.5 || analysis.RiskControl.StopPrice != 21 || analysis.RiskControl.TakeProfitFirst != 28 {
		t.Fatalf("risk control not aligned to AI plan: %+v", analysis.RiskControl)
	}
}

func TestValidatedAIPricePlanRejectsTakeProfitBelowCurrentPrice(t *testing.T) {
	input := aiNonShortPricePlan{
		Entry:      aiPriceZone{PriceLow: 80, PriceHigh: 82, Reason: "回踩支撑", Action: "确认后介入"},
		Hold:       aiPriceZone{PriceLow: 82, PriceHigh: 90, Reason: "趋势未破", Action: "继续持有"},
		TakeProfit: aiPriceZone{PriceLow: 88, PriceHigh: 94, Reason: "阶段压力", Action: "分批兑现"},
		StopLoss:   aiPriceZone{PriceHigh: 75, Reason: "跌破支撑", Action: "执行止损"},
	}
	if _, _, _, _, ok := validatedAIPricePlan(input, 100); ok {
		t.Fatal("AI price plan with take-profit below current price must be rejected")
	}
}

func TestLocalPricePlanKeepsTakeProfitAboveCurrentPrice(t *testing.T) {
	trend := TrendAnalysis{LatestClose: 80, ATR14Percent: 4, Support: 76, Resistance: 86}
	risk := RiskControl{EntryReference: 80, StopPrice: 74}
	_, _, takeProfit, _ := buildActionPriceZones(Profile{PrimaryType: "trend_growth"}, trend, ShortTermAnalysis{}, nil, risk, 100)
	if takeProfit.PriceLow <= 100 || takeProfit.PriceHigh <= takeProfit.PriceLow {
		t.Fatalf("local price plan must be above current price: %+v", takeProfit)
	}
}

func TestEnrichWithAIAppliesShortTermAuctionPlaybook(t *testing.T) {
	lines := syntheticTrendLines("003032.SZ", 60, 10, .1, 800_000_000)
	now := time.Now()
	analysis, err := Analyze(Input{
		Symbol: "003032.SZ", KLines: lines,
		LimitUps: []foundation.LimitUpEvent{{Symbol: "003032.SZ", Date: now.AddDate(0, 0, -2), Streak: 2}, {Symbol: "003032.SZ", Date: now.AddDate(0, 0, -1), Streak: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	prompter := fakeStockPrompter{content: `{
		"headline":"次日只看竞价与承接","summary":"该股收益来源依赖题材流动性和辨识度，盘后价格不能代替次日资金确认。","action":"等待9:25竞价确认","best_path":"板块正反馈后首次分歧承接","main_risk":"高位负反馈扩散",
		"decision":{"decision_mode":"short_term","decision_label":"超短次日作战","decision_confidence":0.91,"horizon":"隔日","rationale":"连板身位和题材合力主导，次日竞价与开盘承接比静态技术价更重要。","current_action":"等待竞价与开盘双确认","position_hint":"T+1下只用小仓验证","non_short_price_plan":{},"short_term_playbook":{"positioning":"题材高辨识度核心","sentiment_cycle":"发酵转分歧","expected_pattern":"分歧转一致","overnight_conclusion":"盘后预案成立，但次日仍需板块同步确认。","data_status":"尚无次日竞价数据。","auction":{"label":"9:25竞价确认","status":"待9:25竞价确认","summary":"判断预期差与板块合力。","required":["板块核心正反馈","竞价最后阶段不持续回落"],"avoid":["高位股集体低开"]},"opening":{"label":"9:30—9:35开盘确认","status":"待开盘确认","summary":"观察首次分歧承接。","required":["回踩后主动收回"],"avoid":["放量跌破竞价低点"]},"participation_conditions":["竞价与开盘双确认"],"hold_conditions":["保持板块辨识度"],"exit_conditions":["低于预期且不能修复"],"veto_conditions":["高位股批量负反馈"],"scenarios":[{"name":"符合预期","tone":"neutral","condition":"竞价匹配地位且开盘有承接","action":"等待首次分歧确认"}]}}
	}`}
	if err := EnrichWithAI(context.Background(), prompter, &analysis, ""); err != nil {
		t.Fatal(err)
	}
	if analysis.ActionPlan.DecisionMode != "short_term" || analysis.ActionPlan.ShortTerm == nil || analysis.ActionPlan.ShortTerm.Auction.Status != "待9:25竞价确认" {
		t.Fatalf("AI short-term playbook not applied: %+v", analysis.ActionPlan)
	}
	if analysis.ActionPlan.Entry.PriceText != "" || analysis.ActionPlan.PricingSource != "not-applicable" {
		t.Fatalf("short-term decision must not expose static price plan: %+v", analysis.ActionPlan)
	}
	if analysis.ActionPlan.ShortTerm.Quantitative.Stock.AuctionChangeMax <= 0 || !strings.Contains(analysis.ActionPlan.ShortTerm.Auction.Required[0], "竞价涨幅") {
		t.Fatalf("AI must preserve deterministic quantified conditions: %+v", analysis.ActionPlan.ShortTerm)
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

type fakeStockPrompter struct {
	content string
	prompt  *string
}

func (p fakeStockPrompter) Prompt(_ context.Context, prompt string) (hermes.PromptResult, error) {
	if p.prompt != nil {
		*p.prompt = prompt
	}
	return hermes.PromptResult{Content: p.content}, nil
}
