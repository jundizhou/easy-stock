package portfolioinspection

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
)

const ETFScoreRuleVersion = "portfolio-etf-risk-v1"

// Product counts are not issuer counts. The old single-stock concentration
// guard remains for stocks and unverified/narrow funds, while a confirmed
// multi-industry broad ETF is evaluated through its actual common exposure.
func concentrationNeedsExplanation(request Request, results []HoldingResult, metrics Metrics, summary *ETFScoreSummary) bool {
	diversified := map[string]bool{}
	if summary != nil {
		for _, item := range summary.Adjustments {
			diversified[item.Symbol] = item.DiversificationBonus == 8
		}
	}
	weights := []int{}
	for _, r := range results {
		if !diversified[r.Holding.Symbol] && r.Holding.Weight > 0 {
			weights = append(weights, r.Holding.Weight)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(weights)))
	maxWeight, topThree := 0, 0
	for i, w := range weights {
		if i == 0 {
			maxWeight = w
		}
		if i < 3 {
			topThree += w
		}
	}
	return EquityPercent(maxWeight, metrics.TotalPositionPercent) > float64(rulesSingleLimit(request)) || EquityPercent(topThree, metrics.TotalPositionPercent) > float64(rulesThreeLimit(request))
}

// These are versioned product heuristics, not measured probabilities of loss.
type ETFScoreAdjustment struct {
	Symbol                string        `json:"symbol"`
	WeightPercent         float64       `json:"weight_percent"`
	DiversificationBonus  int           `json:"diversification_bonus"`
	DiversificationReason string        `json:"diversification_reason"`
	PremiumPercent        *float64      `json:"premium_percent,omitempty"`
	PremiumPenalty        int           `json:"premium_penalty"`
	PremiumEligible       bool          `json:"premium_eligible"`
	PremiumBasis          string        `json:"premium_basis"`
	PremiumReason         string        `json:"premium_reason"`
	ObservedAt            time.Time     `json:"observed_at,omitempty"`
	IOPVAt                time.Time     `json:"iopv_at,omitempty"`
	BonusContribution     float64       `json:"bonus_contribution"`
	PenaltyContribution   float64       `json:"penalty_contribution"`
	ReversionLossPercent  *float64      `json:"reversion_loss_percent,omitempty"`
	EvidenceRefs          []EvidenceRef `json:"evidence_refs"`
}

type ETFScoreSummary struct {
	RuleVersion          string               `json:"rule_version"`
	EvaluatedAt          time.Time            `json:"evaluated_at"`
	BaseRiskScore        int                  `json:"base_risk_score"`
	FinalRiskScore       int                  `json:"final_risk_score"`
	DiversificationBonus float64              `json:"diversification_bonus"`
	PremiumPenalty       float64              `json:"premium_penalty"`
	MinimumRiskLevel     string               `json:"minimum_risk_level,omitempty"`
	ReversionLossPercent *float64             `json:"reversion_loss_percent,omitempty"`
	ReversionCoverage    float64              `json:"reversion_coverage_percent"`
	Adjustments          []ETFScoreAdjustment `json:"adjustments"`
}

func premiumPenalty(p float64) int {
	switch {
	case p >= 10:
		return 35
	case p >= 5:
		return 20
	case p >= 3:
		return 12
	case p >= 1:
		return 6
	case p >= .5:
		return 3
	default:
		return 0
	}
}

func etfDiversification(a etfanalysis.Analysis, asOf time.Time) (int, string) {
	if a.Profile.Symbol == "" || a.Profile.Meta.Stale || a.Profile.Meta.CarryForward || a.Profile.Meta.FetchedAt.After(asOf) {
		return 0, "产品身份或档案时效不足，分散增益未确认"
	}
	if a.Profile.AssetClass != "equity" {
		return 0, "未取得多发行人或多资产的可核实分散依据；ETF身份本身不加分"
	}
	c := etfanalysis.ClassifyDirection(a.Profile)
	if c.Status != "disclosed_target" {
		return 0, "实际跟踪对象未确认，名称或可投资范围不证明分散优势"
	}
	count, largest := 0, 0.0
	latest := ""
	for _, h := range a.Holdings {
		if h.Symbol == a.Profile.Symbol && h.AssetType == "equity" && h.ReportDate <= asOf.In(foundation.AStockLocation).Format("2006-01-02") && h.ReportDate > latest {
			if _, err := time.Parse("2006-01-02", h.ReportDate); err == nil {
				latest = h.ReportDate
			}
		}
	}
	seen := map[string]bool{}
	for _, h := range a.Holdings {
		if h.Symbol != a.Profile.Symbol || h.AssetType != "equity" || h.ReportDate != latest || h.Meta.Stale || h.Meta.CarryForward || h.Meta.FetchedAt.After(asOf) {
			continue
		}
		for _, item := range h.Items {
			if item.Symbol == "" || seen[item.Symbol] || item.WeightPercent <= 0 || item.WeightPercent > 100 || math.IsNaN(item.WeightPercent) || math.IsInf(item.WeightPercent, 0) {
				continue
			}
			seen[item.Symbol] = true
			count++
			largest = math.Max(largest, item.WeightPercent)
		}
	}
	if largest > 20 {
		return 0, "最新披露中单一证券权重超过20%，不确认本项分散增益"
	}
	broad, concentrated := false, false
	for _, tag := range c.Tags {
		broad = broad || tag.Key == "broad_index"
		concentrated = concentrated || oneOf(tag.Key, "growth_tech", "sector_theme", "dividend", "factor_style", "low_volatility")
	}
	if broad && !concentrated {
		return 8, "档案确认多行业宽基跟踪对象，分散个别公司风险；不消除市场系统风险"
	}
	if broad || count >= 5 {
		return 4, "已确认多证券暴露，适度分散个别公司风险；行业、科技或风格共振仍需评价"
	}
	return 0, "未取得足够的多证券分散依据，保留未知部分"
}

// Reassess the original paired snapshot. Never divide a newly refreshed quote
// by an old IOPV, nor trust a persisted 'comparable' flag at a later time.
func etfScoreAdjustment(r HoldingResult, total int, asOf time.Time) ETFScoreAdjustment {
	a := r.Analysis.ETF
	bonus, reason := etfDiversification(*a, asOf)
	weight := 0.0
	if total > 0 {
		weight = float64(r.Holding.Weight) * 100 / float64(total)
	}
	item := ETFScoreAdjustment{Symbol: r.Holding.Symbol, WeightPercent: weight, DiversificationBonus: bonus, DiversificationReason: reason, PremiumBasis: "unverified", PremiumReason: "溢价风险未核验；缺失不是零溢价", EvidenceRefs: []EvidenceRef{{Fact: r.Holding.Symbol + ".etf.diversification"}}}
	item.ObservedAt, item.IOPVAt = a.Execution.Snapshot.QuoteAt, a.Execution.Snapshot.IOPVAt
	assessment := etfanalysis.AssessExecution(a.Profile, a.Execution.Snapshot, asOf)
	if assessment.Comparable && assessment.PremiumPercent != nil {
		item.PremiumEligible, item.PremiumBasis, item.PremiumPercent = true, "live_iopv", assessment.PremiumPercent
		item.PremiumReason = "本次评估时点满足市价与IOPV实时可比条件"
	} else if assessment.ObservationPremiumPercent != nil {
		item.PremiumEligible, item.PremiumBasis, item.PremiumPercent = true, "dated_observation", assessment.ObservationPremiumPercent
		item.PremiumReason = assessment.ObservationBasis
	} else if a.Profile.CrossBorder {
		item.PremiumReason = "跨境参考价值的底层时点及汇率未核实，不能按错时百分比扣分"
	} else if a.Profile.AssetClass == "money" {
		item.PremiumReason = "货币ETF估值与收益分配口径不适用本项溢价分档"
	} else if !item.ObservedAt.IsZero() {
		item.PremiumReason = "原市价与IOPV在本次评估时点不满足可比条件，不能沿用旧溢价或拼接新报价"
	}
	if item.PremiumEligible {
		// Normalize only floating-point noise at threshold boundaries.
		p := round(*item.PremiumPercent, 8)
		item.PremiumPercent = &p
		item.PremiumPenalty = premiumPenalty(p)
		loss := 0.0
		if p > 0 {
			loss = 100 * p / (100 + p)
		}
		item.ReversionLossPercent = &loss
		item.EvidenceRefs = append(item.EvidenceRefs, EvidenceRef{Fact: r.Holding.Symbol + ".etf.premium_assessment"})
	}
	item.BonusContribution = item.WeightPercent / 100 * float64(item.DiversificationBonus)
	item.PenaltyContribution = item.WeightPercent / 100 * float64(item.PremiumPenalty)
	return item
}

func etfScoreSummary(results []HoldingResult, total int, asOf time.Time) *ETFScoreSummary {
	var summary *ETFScoreSummary
	highWeight, extremeWeight, loss := 0.0, 0.0, 0.0
	for _, r := range results {
		if !isETF(r.Analysis) || !validHoldingResearch(r) || r.Holding.Weight <= 0 || total <= 0 {
			continue
		}
		if summary == nil {
			summary = &ETFScoreSummary{RuleVersion: ETFScoreRuleVersion, EvaluatedAt: asOf, Adjustments: []ETFScoreAdjustment{}}
		}
		item := etfScoreAdjustment(r, total, asOf)
		summary.Adjustments = append(summary.Adjustments, item)
		summary.DiversificationBonus += item.BonusContribution
		summary.PremiumPenalty += item.PenaltyContribution
		if item.PremiumEligible {
			summary.ReversionCoverage += item.WeightPercent
			loss += item.WeightPercent / 100 * *item.ReversionLossPercent
			if *item.PremiumPercent >= 5 {
				highWeight += item.WeightPercent
			}
			if *item.PremiumPercent >= 10 {
				extremeWeight += item.WeightPercent
			}
		}
	}
	if summary != nil {
		if summary.ReversionCoverage > 0 {
			summary.ReversionLossPercent = &loss
		}
		if extremeWeight >= 50-1e-9 {
			summary.MinimumRiskLevel = "高"
		} else if highWeight >= 30-1e-9 {
			summary.MinimumRiskLevel = "中"
		}
	}
	return summary
}

func applyETFScoring(report *AIReport, dims []ScoreDimension, summary *ETFScoreSummary) {
	report.ETFScoring = summary
	if summary == nil {
		return
	}
	for i := range dims {
		if dims[i].Key != "risk_capacity" {
			continue
		}
		summary.BaseRiskScore = *dims[i].Score
		final := int(math.Round(math.Max(0, math.Min(100, float64(summary.BaseRiskScore)+summary.DiversificationBonus-summary.PremiumPenalty))))
		summary.FinalRiskScore = final
		dims[i].Score = &final
	}
	rank := map[string]int{"低": 0, "中": 1, "高": 2, "极高": 3}
	if summary.MinimumRiskLevel != "" && rank[report.RiskLevel] < rank[summary.MinimumRiskLevel] {
		report.RiskLevel = summary.MinimumRiskLevel
		report.RiskReason += "；主要仓位的有效高溢价触发风险等级下限"
	}
	for _, item := range summary.Adjustments {
		if !item.PremiumEligible {
			report.DataLimitations = append(report.DataLimitations, item.Symbol+"："+item.PremiumReason)
			report.NextChecklist = append(report.NextChecklist, item.Symbol+"：重新核验同时间市价与参考估值的溢价风险")
			if report.ConfidenceLevel == "高" {
				report.ConfidenceLevel = "中"
				report.ConfidenceReason += "；部分ETF溢价尚未核验"
			}
		} else if item.PremiumPercent != nil && *item.PremiumPercent >= 3 {
			risk := fmt.Sprintf("%s参考溢价%.2f%%，占证券持仓%.2f%%，存在溢价回归风险（观察时间%s）", item.Symbol, *item.PremiumPercent, item.WeightPercent, item.ObservedAt.In(foundation.AStockLocation).Format("01-02 15:04"))
			// Keep deterministic warnings visible even if the model filled its list.
			report.PrimaryRisks = append([]string{risk}, report.PrimaryRisks...)
		}
	}
}

func reservedETFAdjustment(a ScoreAdjustment, summary *ETFScoreSummary) bool {
	text := strings.ToLower(a.RiskID + " " + a.Reason)
	if strings.Contains(text, "etf_diversification") || strings.Contains(text, "etf_premium") || strings.Contains(text, "个别公司风险分散") || strings.Contains(text, "个别证券风险分散") || strings.Contains(text, "etf分散") || strings.Contains(text, "etf 分散") {
		return true
	}
	if strings.Contains(text, "溢价") && summary != nil {
		for _, item := range summary.Adjustments {
			if item.PremiumEligible && item.PremiumPenalty > 0 && (strings.Contains(text, strings.ToLower(item.Symbol)) || strings.Contains(text, strings.Split(item.Symbol, ".")[0]) || !strings.Contains(text, "公告") && (strings.Contains(text, "etf") || strings.Contains(text, "基金") || strings.Contains(text, "溢价回归"))) {
				return true
			}
		}
	}
	return false
}
