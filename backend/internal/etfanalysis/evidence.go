package etfanalysis

import (
	"math"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type ExecutionAssessment struct {
	SessionStatus             string                  `json:"session_status"`
	ObservationPremiumPercent *float64                `json:"observation_premium_percent"`
	ObservationBasis          string                  `json:"observation_basis,omitempty"`
	Snapshot                  foundation.ETFExecution `json:"snapshot"`
	SpreadPercent             *float64                `json:"spread_percent"`
	PremiumPercent            *float64                `json:"premium_percent"`
	Comparable                bool                    `json:"comparable"`
	Limitations               []string                `json:"limitations"`
	TradingRule               string                  `json:"trading_rule"`
	SubscriptionStatus        string                  `json:"subscription_status"`
}

// Enrich keeps source failures independent and never promotes a quote flag to
// exchange trading permission, a T+0 rule or creation/redemption availability.
func (a *Analysis) Enrich(holdings []foundation.ETFHoldings, notices []foundation.ETFAnnouncement, execution foundation.ETFExecution, now time.Time) {
	previous := map[string]bool{}
	for _, v := range a.evidenceLimitations {
		previous[v] = true
	}
	kept := []string{}
	for _, v := range a.Limitations {
		if !previous[v] {
			kept = append(kept, v)
		}
	}
	a.Limitations = kept
	baseLimitations := len(a.Limitations)
	a.Holdings = []foundation.ETFHoldings{}
	a.Announcements = []foundation.ETFAnnouncement{}
	today := now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, h := range holdings {
		if _, err := time.Parse("2006-01-02", h.ReportDate); err != nil || h.Symbol != a.Profile.Symbol || h.ReportDate > today || len(h.Items) == 0 {
			continue
		}
		a.Holdings = append(a.Holdings, h)
	}
	for _, n := range notices {
		if n.Symbol == a.Profile.Symbol && !n.PublishedAt.IsZero() && !n.PublishedAt.After(now) {
			a.Announcements = append(a.Announcements, n)
		}
	}
	a.Execution = AssessExecution(a.Profile, execution, now)
	a.ExecutionBlockers = []string{}
	if !a.Execution.Comparable {
		a.ExecutionBlockers = append(a.ExecutionBlockers, "实时盘口与参考估值尚不满足可比条件，不能形成当前入场价格计划")
	}
	if len(a.Bars) < 20 {
		a.ExecutionBlockers = append(a.ExecutionBlockers, "完整价格日线不足20日")
	}
	if a.Profile.AssetClass == "money" {
		a.ExecutionBlockers = append(a.ExecutionBlockers, "货币ETF仅研究收益与流动性，不生成价格突破计划")
	}
	if a.Profile.AssetClass == "unknown" {
		a.ExecutionBlockers = append(a.ExecutionBlockers, "底层资产分类未核实")
	}
	// These permissions are never inferred from the retail distributor's 场内交易 label.
	a.ExecutionBlockers = append(a.ExecutionBlockers, "逐产品交易规则及交易所停复牌、申赎限制尚需原始文件核验")
	if len(a.Holdings) == 0 {
		a.Limitations = append(a.Limitations, "未取得带报告期的披露持仓；投资范围不等于实际持仓")
	} else {
		a.Limitations = append(a.Limitations, "持仓仅覆盖来源前十大披露项目，报告期不同不能按实时组合合并；未披露部分不是零暴露")
	}
	if len(a.Announcements) == 0 {
		a.Limitations = append(a.Limitations, "基金公告暂不可用，不能确认最新产品变更")
	} else {
		a.Limitations = append(a.Limitations, "公告覆盖最近10条，优先补取拆分、分红、定期报告与交易变更等最多6篇正文；标题不等于正文，样本未见限制不代表不存在限制")
	}
	a.Limitations = append(a.Limitations, a.Execution.Limitations...)
	a.evidenceLimitations = append([]string(nil), a.Limitations[baseLimitations:]...)
}

// PlanningBlockers uses frozen product and price facts. Live quote freshness and
// trading permissions are execution prerequisites, not reasons to erase a plan.
func (a Analysis) PlanningBlockers() []string {
	blockers := []string{}
	if len(a.Bars) < 20 {
		blockers = append(blockers, "完整价格日线不足20日")
	}
	switch a.Profile.AssetClass {
	case "money":
		blockers = append(blockers, "货币ETF仅研究收益与流动性，不生成价格突破计划")
	case "equity", "bond", "commodity":
	default:
		blockers = append(blockers, "底层资产分类未核实")
	}
	return blockers
}
func AssessExecution(p foundation.ETFProfile, e foundation.ETFExecution, now time.Time) ExecutionAssessment {
	a := ExecutionAssessment{Snapshot: e, Limitations: []string{}, TradingRule: "未取得逐产品规则原文；不推定T+0、涨跌幅上限或最小申赎单位", SubscriptionStatus: "未核实；二级市场报价与一级市场申赎状态独立"}
	valid := func(v *float64) bool { return v != nil && *v > 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
	same := e.Symbol == p.Symbol
	if same && valid(e.Bid) && valid(e.Ask) && *e.Ask >= *e.Bid && valid(e.BidUnits) && valid(e.AskUnits) && !e.QuoteAt.IsZero() && !e.QuoteAt.After(now) {
		a.SpreadPercent = number((*e.Ask - *e.Bid) / ((*e.Bid + *e.Ask) / 2) * 100)
	}
	fresh := func(t time.Time) bool { return !t.IsZero() && !t.After(now) && now.Sub(t) <= 90*time.Second }
	loc := now.In(time.FixedZone("CST", 8*3600))
	minutes := loc.Hour()*60 + loc.Minute()
	session := foundation.IsAStockTradingDay(loc) && ((minutes >= 570 && minutes < 690) || (minutes >= 780 && minutes < 900))
	a.SessionStatus = "outside_session"
	if session {
		a.SessionStatus = "continuous_session"
	}
	// A dated close snapshot remains useful for research, never live permission.
	qtime := e.QuoteAt.In(foundation.AStockLocation)
	qminutes := qtime.Hour()*60 + qtime.Minute()
	comparableValues := same && a.SpreadPercent != nil && valid(e.Price) && valid(e.IOPV) && !e.IOPVAt.IsZero() && !e.IOPVAt.After(now) && e.QuoteAt.Sub(e.IOPVAt).Abs() <= 30*time.Second && e.QuoteStatus == "normal_quote" && !e.Meta.Stale && !e.Meta.CarryForward && !p.CrossBorder && p.AssetClass != "money" && p.AssetClass != "unknown"
	if !session && comparableValues && qminutes >= 900 && qminutes <= 960 && qtime.Format("2006-01-02") == foundation.LatestAStockSession(now) && e.IOPVAt.In(foundation.AStockLocation).Format("2006-01-02") == qtime.Format("2006-01-02") {
		a.ObservationPremiumPercent = number((*e.Price / *e.IOPV - 1) * 100)
		a.ObservationBasis = "最近交易日收盘后同时间窗口的市价/IOPV偏离，仅供盘后研究；不是实时溢价、可成交价格或套利依据"
	}
	// Live execution retains its stricter freshness checks.
	a.Comparable = same && session && fresh(e.QuoteAt) && fresh(e.IOPVAt) && e.QuoteAt.Sub(e.IOPVAt).Abs() <= 30*time.Second && a.SpreadPercent != nil && valid(e.Price) && valid(e.IOPV) && e.QuoteStatus == "normal_quote" && !e.Meta.Stale && !e.Meta.CarryForward && !p.CrossBorder && p.AssetClass != "money" && p.AssetClass != "unknown"
	if a.Comparable {
		a.PremiumPercent = number((*e.Price / *e.IOPV - 1) * 100)
	} else if !session {
		a.Limitations = append(a.Limitations, "当前为非连续交易时段；按最新完整交易日做研究，实时执行条件需下个交易时段复核，不影响已取得的历史与产品事实")
	} else {
		a.Limitations = append(a.Limitations, "实时折溢价需盘口和IOPV均90秒内且时差不超过30秒；当前未满足，已取得的量价与产品事实仍可独立研究")
	}
	if p.CrossBorder {
		a.Limitations = append(a.Limitations, "跨境IOPV可能沿用海外收盘与估算汇率，未核实底层估值时点，不计算实时折溢价")
	}
	a.Limitations = append(a.Limitations, "盘口价差按盘口时间展示；正常报价标记不证明交易所可成交、T+0或申赎开放")
	return a
}

// Portfolio summary includes only dated fund facts and keeps stock financials out.
func (a Analysis) PortfolioFacts() map[string]any {
	return map[string]any{"instrument_type": "etf", "asset_class": a.Profile.AssetClass, "cross_border": a.Profile.CrossBorder, "tracking_target": a.Profile.TrackingTarget, "direction": a.PortfolioDirection(), "premium_announcements": a.PortfolioPremiumAnnouncements(), "holdings_summary": a.holdingsSummary(), "execution": map[string]any{"comparable": a.Execution.Comparable, "quote_at": a.Execution.Snapshot.QuoteAt, "iopv_at": a.Execution.Snapshot.IOPVAt, "spread_percent": a.Execution.SpreadPercent, "premium_percent": a.Execution.PremiumPercent, "observation_premium_percent": a.Execution.ObservationPremiumPercent, "observation_basis": a.Execution.ObservationBasis}, "execution_blockers": a.ExecutionBlockers, "limitations": a.Limitations}
}

// Titles remain labelled as titles. An announcement is evidence for a dated
// qualitative assessment, never a manufactured current premium percentage.
func (a Analysis) PortfolioPremiumAnnouncements() []map[string]any {
	result := []map[string]any{}
	for _, notice := range a.Announcements {
		if notice.Symbol != a.Profile.Symbol || notice.PublishedAt.IsZero() || notice.Meta.Stale || notice.Meta.CarryForward || !strings.Contains(notice.Title+notice.Content, "溢价") {
			continue
		}
		text := []rune(notice.Content)
		index := strings.Index(string(text), "溢价")
		start := 0
		if index >= 0 {
			start = max(0, len([]rune(string(text)[:index]))-100)
		}
		end := min(len(text), start+600)
		result = append(result, map[string]any{"title": notice.Title, "published_at": notice.PublishedAt, "url": notice.URL, "excerpt": string(text[start:end]), "content_status": notice.ContentStatus})
		if len(result) == 4 {
			break
		}
	}
	return result
}

// Keep the frozen direction evidence, without bars or unbounded news bodies.
// Copies prevent portfolio prompts from modifying the canonical ETF report.
func (a Analysis) PortfolioDirection() *DirectionAnalysis {
	if a.Direction == nil {
		return nil
	}
	d := *a.Direction
	if d.Technical != nil {
		t := *d.Technical
		t.Bars = nil
		d.Technical = &t
	}
	d.News = append([]DirectionNews(nil), d.News[:min(len(d.News), 6)]...)
	bounded := func(s string, n int) string {
		r := []rune(s)
		if len(r) > n {
			return string(r[:n]) + "…"
		}
		return s
	}
	for i := range d.News {
		d.News[i].Item.Title = bounded(d.News[i].Item.Title, 160)
		d.News[i].Item.Content = bounded(d.News[i].Item.Content, 400)
	}
	return &d
}

func (a Analysis) holdingsSummary() []any {
	summaries := []any{}
	for _, h := range a.Holdings {
		summaries = append(summaries, map[string]any{"asset_type": h.AssetType, "report_date": h.ReportDate, "disclosed_weight_percent": h.DisclosedWeightPercent, "scope": h.Scope})
	}
	return summaries
}
