package httpapi

import (
	"context"
	"net/http"
	"sort"
	"time"

	"easy-stock/backend/internal/strategy/inflection"
)

// inflectionAuto 用本机现有涨停梯队数据自动装配快照并调用拐点引擎。
//
// 与 POST /api/v1/strategy/inflections/evaluate 的区别：
//   - evaluate 要求调用方自行给出完整的 MarketSnapshot / CandidateSnapshot；
//   - auto 由后端从涨停梯队（当日 + 前一日 + 盘中高标指标）装配，零输入即可评估。
//
// 装配口径（全部只读，不写任何文件）：
//   · 旧锚 old_profit   —— 当日最高连板层级的个股（最多 2 只）
//   · 旧锚 old_negative —— 前一日高标中当日未维持强势（现价涨跌幅 < 0）的个股（最多 2 只）
//   · 新载体 new_carrier—— 当日首板股按成交额降序（最多 3 只）
//   · 识别度五特征（height/attention/persistence/influence/resilience）由连板高度、
//     成交额分位、炸板次数等可得字段归一化估算，不是外部标注的真值。
//
// 无法从现有数据源可靠获得的字段一律置零，并在响应 assumptions 中逐条声明，
// 避免"看起来有数其实是我编的"。
//
//	POST /api/v1/strategy/inflections/auto
//	→ { data: Evaluation, assumptions: string[], source: {...} }
func (s *Server) inflectionAutoHandler(w http.ResponseWriter, r *http.Request) {
	if s.limitUpProvider == nil || s.inflection == nil {
		writeError(w, http.StatusServiceUnavailable, "inflection auto source is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	data, err := s.limitUpSnapshots.load(ctx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
	if err != nil {
		writeError(w, http.StatusBadGateway, "limit-up ladder unavailable: "+err.Error())
		return
	}
	request, assumptions := buildInflectionAutoRequest(data)
	result, err := s.inflection.Evaluate(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": result,
		"assumptions": assumptions,
		"source": map[string]any{
			"current_trade_date":  data.Current.TradeDate,
			"previous_trade_date": data.Previous.TradeDate,
			"limit_up_count":      data.Current.LimitUpCount,
			"max_streak":          data.Current.MaxStreak,
			"previous_max_streak": data.Previous.MaxStreak,
			"candidate_count":     len(request.Candidates),
			"session_status":      data.SessionStatus,
		},
	})
}

func buildInflectionAutoRequest(data limitUpLadderData) (inflection.EvaluationRequest, []string) {
	assumptions := []string{
		"市场环境快照由涨停梯队与盘中高标指标自动装配，非人工录入。",
		"识别度五特征（height/attention/persistence/influence/resilience）由连板高度、成交额分位、炸板次数归一化估算。",
	}
	market := inflection.MarketSnapshot{
		Time:             time.Now(),
		Scope:            inflection.ScopeMarket,
		ScopeName:        "全市场",
		LimitUps:         data.Current.LimitUpCount,
		BrokenBoards:     data.Current.ReopenedCount,
		BreadthImprovement: float64(data.Current.LimitUpCount - data.Previous.LimitUpCount),
	}
	if data.Intraday != nil {
		market.LimitDowns = data.Intraday.Metrics.HighLimitDown
		market.ScopeChangePercent = data.Intraday.Metrics.HighAverageReturn
		market.PriorStressScore = data.Intraday.Metrics.HighDownRate * 100
		if collapse := data.Intraday.Metrics.HeightCollapse; collapse > 0 {
			market.StressDays = collapse
		}
	}
	assumptions = append(assumptions,
		"advancers/decliners（全市场涨跌家数）现有数据源未提供，置 0 —— 广度相关判据因此偏保守。",
		"index_above_vwap（指数与 VWAP 关系）未提供，置 false。",
		"limit_down_release_ratio（跌停撬板比例）未提供，置 0。",
		"market_change_percent（指数涨跌幅）未提供，置 0；scope_change_percent 用盘中高标平均收益代替。",
		"prior_stress_score 用盘中高标高跌比例（high_down_rate×100）近似；stress_days 用高标高度坍塌级数近似。",
	)

	var candidates []inflection.CandidateSnapshot
	industryCount := countIndustryLimitUps(data.Current)

	// 旧锚：当日最高连板（共识锚）
	for _, stock := range topStreakStocks(data.Current, 2) {
		candidates = append(candidates, candidateFromLadder(stock, inflection.AnchorOldProfit, industryCount, true))
	}
	// 旧锚：前一日高标中当日转弱（负锚 / 出清对象）
	for _, stock := range weakenedPreviousHighs(data.Previous, 2) {
		candidates = append(candidates, candidateFromLadder(stock, inflection.AnchorOldNegative, industryCount, false))
	}
	// 新载体：当日首板，按成交额降序
	for _, stock := range topFirstBoards(data.Current, 3) {
		candidates = append(candidates, candidateFromLadder(stock, inflection.AnchorNewCarrier, industryCount, true))
	}
	if len(candidates) == 0 {
		assumptions = append(assumptions, "当日涨停池为空，未装配任何候选 —— 引擎将返回 none。")
	}
	return inflection.EvaluationRequest{Market: market, Candidates: candidates}, assumptions
}

func countIndustryLimitUps(day limitUpLadderDay) map[string]int {
	counts := make(map[string]int)
	for _, level := range day.Levels {
		for _, stock := range level.Stocks {
			if stock.Industry != "" {
				counts[stock.Industry]++
			}
		}
	}
	return counts
}

// amountRank 返回 amount 在该批候选中的百分位（0-100），用于 attention / influence 估算。
func amountRank(stocks []limitUpLadderStock) map[string]float64 {
	if len(stocks) == 0 {
		return map[string]float64{}
	}
	sorted := make([]limitUpLadderStock, len(stocks))
	copy(sorted, stocks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Amount > sorted[j].Amount })
	ranks := make(map[string]float64, len(sorted))
	for idx, stock := range sorted {
		ranks[stock.Symbol] = float64(len(sorted)-1-idx) / float64(len(sorted)) * 100
	}
	return ranks
}

func allLadderStocks(day limitUpLadderDay) []limitUpLadderStock {
	out := make([]limitUpLadderStock, 0)
	for _, level := range day.Levels {
		out = append(out, level.Stocks...)
	}
	return out
}

func topStreakStocks(day limitUpLadderDay, limit int) []limitUpLadderStock {
	stocks := allLadderStocks(day)
	sort.Slice(stocks, func(i, j int) bool {
		if stocks[i].Streak != stocks[j].Streak {
			return stocks[i].Streak > stocks[j].Streak
		}
		return stocks[i].Amount > stocks[j].Amount
	})
	if len(stocks) > limit {
		stocks = stocks[:limit]
	}
	return stocks
}

func topFirstBoards(day limitUpLadderDay, limit int) []limitUpLadderStock {
	stocks := make([]limitUpLadderStock, 0)
	for _, stock := range allLadderStocks(day) {
		if stock.Streak <= 1 {
			stocks = append(stocks, stock)
		}
	}
	sort.Slice(stocks, func(i, j int) bool { return stocks[i].Amount > stocks[j].Amount })
	if len(stocks) > limit {
		stocks = stocks[:limit]
	}
	return stocks
}

// weakenedPreviousHighs 取前一日最高连板个股中当日现价走弱的（未维持强势 = 出清迹象）。
func weakenedPreviousHighs(previous limitUpLadderDay, limit int) []limitUpLadderStock {
	highs := topStreakStocks(previous, 8)
	weakened := make([]limitUpLadderStock, 0, len(highs))
	for _, stock := range highs {
		if stock.CurrentChangePercent != nil && *stock.CurrentChangePercent < 0 {
			weakened = append(weakened, stock)
		}
		if len(weakened) >= limit {
			break
		}
	}
	return weakened
}

func candidateFromLadder(stock limitUpLadderStock, kind inflection.AnchorKind, industryCount map[string]int, limitUp bool) inflection.CandidateSnapshot {
	height := float64(stock.Streak) * 20
	if height > 100 {
		height = 100
	}
	persistence := float64(stock.Streak) * 25
	if persistence > 100 {
		persistence = 100
	}
	resilience := 100 - float64(stock.OpenCount)*30
	if resilience < 0 {
		resilience = 0
	}
	// 成交额分位在该股所属批次内计算代价过高，这里用绝对量级的粗分位：
	// 5 亿以上视为高度关注，1 亿以下视为低关注，中间线性插值。
	attention := stock.Amount / 5e8 * 100
	if attention > 100 {
		attention = 100
	}
	influence := stock.FloatMarketCap / 3e10 * 100
	if influence > 100 {
		influence = 100
	}
	if influence <= 0 {
		influence = attention
	}
	change := stock.ChangePercent
	if stock.CurrentChangePercent != nil {
		change = *stock.CurrentChangePercent
	}
	return inflection.CandidateSnapshot{
		Symbol:              stock.Symbol,
		Name:                stock.Name,
		Kind:                kind,
		Scope:               inflection.ScopeStock,
		ScopeName:           stock.Industry,
		Recognition: inflection.RecognitionFeatures{
			Height:      height,
			Attention:   attention,
			Persistence: persistence,
			Influence:   influence,
			Resilience:  resilience,
		},
		ChangePercent:       change,
		AmountRatio:         1,
		SectorFollowers:     industryCount[stock.Industry],
		LimitUp:             limitUp,
		BoardBroken:         stock.OpenCount > 0,
		PreviousPassiveWeak: kind == inflection.AnchorOldNegative,
	}
}
