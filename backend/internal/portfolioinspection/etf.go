package portfolioinspection

import (
	"math"
	"time"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/stockanalysis"
)

func isETF(a *stockanalysis.Analysis) bool {
	return a != nil && a.InstrumentType == "etf" && a.ETF != nil
}
func analysisChart(a *stockanalysis.Analysis) []stockanalysis.TrendPoint {
	if a == nil {
		return nil
	}
	if !isETF(a) {
		return a.Chart
	}
	if a.ETF.Profile.AssetClass == "money" {
		return nil
	}
	points := []stockanalysis.TrendPoint{}
	bars := a.ETF.TrendBars
	if len(bars) == 0 {
		bars = a.ETF.Bars
	}
	for _, b := range etfanalysis.ContinuousHistory(bars) {
		points = append(points, stockanalysis.TrendPoint{Date: b.Time.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02"), Close: b.Close})
	}
	return points
}
func etfVolatility(a *stockanalysis.Analysis) float64 {
	points := analysisChart(a)
	if len(points) < 21 {
		return 0
	}
	points = points[max(0, len(points)-61):]
	values := []float64{}
	for i := 1; i < len(points); i++ {
		if points[i-1].Close > 0 && points[i].Close > 0 {
			values = append(values, (points[i].Close/points[i-1].Close-1)*100)
		}
	}
	if len(values) < 20 {
		return 0
	}
	mean := average(values)
	variance := 0.
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	return math.Sqrt(variance / float64(len(values)-1))
}

// Disclosed positions are lower-bound exposure, not a complete look-through.
// Identifiers must include a known market to avoid merging HK/US/domestic codes.
func etfPortfolioFacts(results []HoldingResult, metrics Metrics) map[string]Fact {
	return etfPortfolioFactsAt(results, metrics, time.Now().UTC())
}

func etfPortfolioFactsAt(results []HoldingResult, metrics Metrics, evaluatedAt time.Time) map[string]Fact {
	facts := map[string]Fact{}
	exposure := map[string]float64{}
	contributors := map[string]int{}
	for _, r := range results {
		if r.Status != "succeeded" || r.Analysis == nil {
			continue
		}
		a := r.Analysis
		prefix := r.Holding.Symbol + "."
		kind := "stock"
		if isETF(a) {
			kind = "etf"
		}
		facts[prefix+"instrument_type"] = Fact{Value: kind, Available: true, Method: "报告确认的标的类型"}
		if !isETF(a) {
			exposure[r.Holding.Symbol] += EquityPercent(r.Holding.Weight, metrics.TotalPositionPercent)
			contributors[r.Holding.Symbol]++
			continue
		}
		etf := a.ETF
		asOf := r.ResearchCutoffAt
		facts[prefix+"etf.asset_class"] = Fact{Value: etf.Profile.AssetClass, Available: etf.Profile.AssetClass != "" && etf.Profile.AssetClass != "unknown", AsOf: asOf, Method: "基金档案资产类别"}
		facts[prefix+"etf.cross_border"] = Fact{Value: etf.Profile.CrossBorder, Available: true, AsOf: asOf, Method: "基金档案跨境属性"}
		facts[prefix+"etf.tracking_target"] = Fact{Value: etf.Profile.TrackingTarget, Available: etf.Profile.TrackingTarget != "", AsOf: asOf, Method: "基金档案跟踪标的；同指数不同基金不代表分散底层风险"}
		facts[prefix+"etf.execution_blockers"] = Fact{Value: etf.ExecutionBlockers, Available: true, AsOf: asOf, Method: "原研究执行限制；刷新价格不会更新执行许可"}
		facts[prefix+"etf.holdings"] = Fact{Value: etf.Holdings, Available: len(etf.Holdings) > 0, AsOf: asOf, Method: "按原报告期披露的部分持仓", Limitation: "不等于当前实际持仓；不同报告期不可视作同时持有；未披露不等于零"}
		direction := etf.PortfolioDirection()
		facts[prefix+"etf.direction.classification"] = Fact{Available: direction != nil, AsOf: asOf, Method: "原ETF方向分类及披露依据", Limitation: "名称初步分类不等于已确认实际暴露"}
		facts[prefix+"etf.direction.technical"] = Fact{Available: direction != nil && direction.Technical != nil, AsOf: asOf, Method: "原研究对象、关系、日期、单位及技术指标", Limitation: "跟踪指数、相关行业参照与ETF市价代理分别解释；指数点位不作为ETF交易价格"}
		facts[prefix+"etf.direction.news_coverage"] = Fact{Available: direction != nil, AsOf: asOf, Method: "方向消息检索窗口与覆盖", Limitation: "未检出不等于不存在事件，检索失败不证明消息面恶化"}
		if direction != nil {
			f := facts[prefix+"etf.direction.classification"]
			f.Value = direction.Classification
			facts[prefix+"etf.direction.classification"] = f
			f = facts[prefix+"etf.direction.technical"]
			if direction.Technical != nil {
				f.Value = direction.Technical
			}
			facts[prefix+"etf.direction.technical"] = f
			f = facts[prefix+"etf.direction.news_coverage"]
			f.Value = direction.NewsCoverage
			facts[prefix+"etf.direction.news_coverage"] = f
		}
		adjustment := etfScoreAdjustment(r, metrics.TotalPositionPercent, evaluatedAt)
		facts[prefix+"etf.diversification"] = Fact{Value: map[string]any{"bonus": adjustment.DiversificationBonus, "reason": adjustment.DiversificationReason}, Available: true, AsOf: asOf, Method: "ETF分散个别证券风险的专项增益资格", Limitation: "0表示无已确认增益；不表示高风险或零风险。组合只由后端按仓位应用一次"}
		facts[prefix+"etf.premium_assessment"] = Fact{Value: map[string]any{"premium_percent": adjustment.PremiumPercent, "eligible": adjustment.PremiumEligible, "penalty": adjustment.PremiumPenalty, "basis": adjustment.PremiumBasis, "quote_at": adjustment.ObservedAt, "iopv_at": adjustment.IOPVAt, "evaluated_at": evaluatedAt}, Available: adjustment.PremiumEligible, AsOf: adjustment.ObservedAt, Method: "本次评估重新检查原同时间市价与IOPV的可比性", Limitation: adjustment.PremiumReason}
		facts[prefix+"etf.premium_scoring_eligible"] = Fact{Value: adjustment.PremiumEligible, Available: true, AsOf: evaluatedAt, Method: "溢价专项扣分资格", Limitation: adjustment.PremiumReason}
		facts[prefix+"etf.spread_percent"] = Fact{Value: etf.Execution.SpreadPercent, Available: etf.Execution.SpreadPercent != nil, AsOf: etf.Execution.Snapshot.QuoteAt, Method: "原盘口时点买卖价差%", Limitation: "原时点观察，不因刷新报价而变成实时；与溢价来自同一交易摩擦时不重复扣分"}
		announcements := []map[string]any{}
		for _, notice := range etf.PortfolioPremiumAnnouncements() {
			publishedAt := notice["published_at"].(time.Time)
			if !publishedAt.After(evaluatedAt) && (asOf.IsZero() || !publishedAt.After(asOf)) {
				announcements = append(announcements, notice)
			}
		}
		facts[prefix+"etf.premium_announcements"] = Fact{Value: announcements, Available: len(announcements) > 0, AsOf: asOf, Method: "原研究中有日期的基金溢价公告摘录", Limitation: "公告按发布日期和本次观察区间解释；标题不等于正文，历史警示不证明当前溢价数值；已有数值扣分时不重复扣分"}
		seen := map[string]bool{}
		for _, h := range etf.Holdings {
			for _, v := range h.Items {
				if v.Symbol == "" || seen[v.Symbol] {
					continue
				}
				seen[v.Symbol] = true
				exposure[v.Symbol] += EquityPercent(r.Holding.Weight, metrics.TotalPositionPercent) * v.WeightPercent / 100
				contributors[v.Symbol]++
			}
		}
	}
	for symbol, weight := range exposure {
		if contributors[symbol] < 2 {
			continue
		}
		facts["disclosed_overlap."+symbol] = Fact{Value: round(weight, 4), Available: true, Method: "直接持仓与ETF披露持仓穿透后占全部持仓的已知比例%", Limitation: "不同报告期的部分持仓静态估算，不是实时完整暴露；未披露部分未知"}
	}
	return facts
}

const ETFPortfolioPolicy = `
ETF使用instrument_type和etf事实识别；股票、ETF均为组合持仓，equity_*历史字段现表示全部证券持仓归一化，现金单列，货币ETF仍是基金不是现金。ETF不套用公司ROE、营收、扣非利润或个股总分。股票型看底层指数/行业，债券看久期信用，商品看现货/期货，货币看收益分配与流动性；没有证据明确未知。ETF逻辑维度评价底层技术面、消息面及产品适配，风险维度评价价差/跟踪/退出限制；不以未知数据当作零风险。相同指数ETF要合并看共同驱动，disclosed_overlap仅为不同报告期部分持仓的静态重叠，不能宣称完整实时风险分散。跨境关注汇率和时差。原ETF execution_blockers不得由组合研究绕过。
ETF分散个别证券风险的增益及有资格的数值溢价扣分由后端专项计算，四维score仅给基础分，不得提前加入ETF分散奖励或有资格的溢价扣分，不返回etf_scoring。禁用etf_diversification/etf_premium调整编号，同一优势或风险不跨维重复计分。高溢价可能超过分散增益，不能因ETF身份、底层上涨或浮盈忽略。premium_scoring_eligible=false不是零溢价，不拼接新报价与旧IOPV；相关官方高溢价公告可作为非定量已证实风险，但已有数值专项扣分时仅解释不再扣分。不同产品仓位不等于相同底层集中；宽基产品内部个别证券分散只由专项加分评价，结构维度看持仓间重复暴露。方向事实缺失只说明旧报告未采集，不能用产品档案制造技术或新闻支持。
`

func ETFPolicyFor(results []HoldingResult) string {
	for _, r := range results {
		if isETF(r.Analysis) {
			return ETFPortfolioPolicy
		}
	}
	return ""
}
