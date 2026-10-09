package portfoliooptimization

import (
	"easy-stock/backend/internal/foundation"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// ScreeningPolicy uses percentage points and completed A-share sessions.
// These are transparent candidate filters, never a recommendation to buy.
type ScreeningPolicy struct {
	IndustryFiveDayMin, IndustryFiveDayMax       float64
	IndustryTwentyDayMin, IndustryTwentyDayMax   float64
	IndustryDailyMax, StockReturnMax             float64
	ReturnSessions                               int
	HighGrowthMin, SteadyGrowthMin               float64
	StableRevenueMin, StableProfitMin            float64
	ValuePEMax, ValuePBMax, BankPEMax, BankPBMax float64
}

var CandidatePolicy = ScreeningPolicy{
	IndustryFiveDayMin: .5, IndustryFiveDayMax: 8, IndustryTwentyDayMin: -5, IndustryTwentyDayMax: 15,
	IndustryDailyMax: 4, StockReturnMax: 50, ReturnSessions: 20, HighGrowthMin: 20, SteadyGrowthMin: 5,
	StableRevenueMin: -5, StableProfitMin: -20, ValuePEMax: 25, ValuePBMax: 3, BankPEMax: 12, BankPBMax: 1.5,
}

// Normalize either provider's classification; diversification checks both.
func IndustryGroup(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, "ⅠⅡⅢ ")
	switch name {
	case "国有大型银行", "股份制银行", "城商行", "农商行", "其他银行", "银行业":
		return "银行"
	case "", "未知", "其他", "--", "-":
		return ""
	default:
		return name
	}
}

// CandidateIndustry preserves the verified momentum board and the directory
// classification separately. One board cannot masquerade as many industries.
func CandidateIndustry(c *Candidate, directory string) {
	c.CatalogIndustryGroup = IndustryGroup(directory)
	c.IndustryGroup = ""
	if c.Screening != nil && c.Screening.Industry.Qualified {
		c.IndustryGroup = IndustryGroup(c.Screening.Industry.Name)
	}
	if c.IndustryGroup == "" {
		c.IndustryGroup = c.CatalogIndustryGroup
	}
}

// DiverseCandidates finds up to limit non-overlapping source-board/directory
// pairs. Augmenting paths avoid a greedy choice blocking a valid later pair.
// Input order is ranking order; returned indices retain that order.
func DiverseCandidates(candidates []Candidate, limit int) []int {
	groups, order := map[string][]int{}, []string{}
	for i, c := range candidates {
		if c.Screening == nil || !c.Screening.Qualified || c.IndustryGroup == "" || c.CatalogIndustryGroup == "" {
			continue
		}
		if len(groups[c.IndustryGroup]) == 0 {
			order = append(order, c.IndustryGroup)
		}
		groups[c.IndustryGroup] = append(groups[c.IndustryGroup], i)
	}
	matched := map[string]int{}
	var augment func(string, map[string]bool) bool
	augment = func(group string, visited map[string]bool) bool {
		// Prefer an unused directory before displacing a higher ranked stock.
		for _, i := range groups[group] {
			key := candidates[i].CatalogIndustryGroup
			if _, ok := matched[key]; !ok && !visited[key] {
				matched[key] = i
				return true
			}
		}
		for _, i := range groups[group] {
			key := candidates[i].CatalogIndustryGroup
			if visited[key] {
				continue
			}
			visited[key] = true
			if augment(candidates[matched[key]].IndustryGroup, visited) {
				matched[key] = i
				return true
			}
		}
		return false
	}
	for _, group := range order {
		if len(matched) >= limit {
			break
		}
		augment(group, map[string]bool{})
	}
	out := []int{}
	for _, i := range matched {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// Retain the diverse shortlist first, then the highest ranked reserves.
func QualifiedCandidatePool(candidates []Candidate) []Candidate {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		left, right := a.Screening.Score+float64(a.FitBonus), b.Screening.Score+float64(b.FitBonus)
		if left != right {
			return left > right
		}
		return a.Symbol < b.Symbol
	})
	keep := map[int]bool{}
	for _, i := range DiverseCandidates(candidates, MaxCandidateResearch) {
		keep[i] = true
	}
	for i := range candidates {
		if len(keep) >= MaxCandidates {
			break
		}
		keep[i] = true
	}
	out := []Candidate{}
	for i, c := range candidates {
		if keep[i] {
			out = append(out, c)
		}
	}
	return out
}

type IndustrySignal struct {
	Qualified    bool    `json:"qualified"`
	Score        float64 `json:"score"`
	Name         string  `json:"name"`
	FiveDay      float64 `json:"five_day_percent"`
	TwentyDay    float64 `json:"twenty_day_percent"`
	Acceleration float64 `json:"acceleration"`
	Reason       string  `json:"reason"`
	Source       string  `json:"source"`
}
type CandidateScreening struct {
	Qualified            bool                       `json:"qualified"`
	Score                float64                    `json:"score"`
	Industry             IndustrySignal             `json:"industry"`
	CompletedSession     string                     `json:"completed_session"`
	RecentReturn         *float64                   `json:"recent_return_percent,omitempty"`
	ReturnSessions       int                        `json:"return_sessions"`
	GrowthKind           string                     `json:"growth_kind,omitempty"`
	Valuation            *foundation.StockValuation `json:"valuation,omitempty"`
	ValuationReason      string                     `json:"valuation_reason,omitempty"`
	RevenueYoY           *float64                   `json:"revenue_yoy,omitempty"`
	PreviousRevenueYoY   *float64                   `json:"previous_revenue_yoy,omitempty"`
	FinancialReports     []string                   `json:"financial_reports,omitempty"`
	FinancialPublishedAt []time.Time                `json:"financial_published_at,omitempty"`
	FinancialSource      string                     `json:"financial_source,omitempty"`
	DeductedProfitShare  *float64                   `json:"deducted_profit_share,omitempty"`
	ProfitQualityBonus   float64                    `json:"profit_quality_bonus,omitempty"`
	FinancialMethod      string                     `json:"financial_method"`
	PriceSource          string                     `json:"price_source,omitempty"`
	Reasons              []string                   `json:"reasons"`
}

func LatestCompletedSession(now time.Time) string {
	return foundation.LatestCompletedAStockSession(now).Format("2006-01-02")
}
func ScreenIndustry(m foundation.MarketIndustryMomentum) IndustrySignal {
	s := IndustrySignal{Name: m.Name, FiveDay: m.FiveDayChangePercent, TwentyDay: m.TwentyDayChange, Source: m.Meta.Source}
	if !finite(s.FiveDay) {
		s.FiveDay = 0
	}
	if !finite(s.TwentyDay) {
		s.TwentyDay = 0
	}
	p := CandidatePolicy
	// Eastmoney's legacy momentum adapter maps different horizon fields; do not
	// interpret those values as verified 5/20-session returns.
	if m.Meta.Stale || m.Meta.Source == "eastmoney:industry-momentum" ||
		!slices.Contains(m.Meta.AvailableFields, "change_percent") || !slices.Contains(m.Meta.AvailableFields, "five_day_change_percent") || !slices.Contains(m.Meta.AvailableFields, "twenty_day_change_percent") ||
		!finite(m.ChangePercent) || !finite(m.FiveDayChangePercent) || !finite(m.TwentyDayChange) {
		s.Reason = "行业5/20日动量数据未完整核验"
		return s
	}
	if s.FiveDay < p.IndustryFiveDayMin || s.FiveDay > p.IndustryFiveDayMax || s.TwentyDay < p.IndustryTwentyDayMin || s.TwentyDay > p.IndustryTwentyDayMax || m.ChangePercent < 0 || m.ChangePercent > p.IndustryDailyMax {
		s.Reason = "行业未启动、短期动量过热或前期涨幅过高"
		return s
	}
	priorFifteen := ((1+s.TwentyDay/100)/(1+s.FiveDay/100) - 1) * 100
	s.Acceleration = s.FiveDay - priorFifteen/3
	if s.Acceleration <= 0 {
		s.Reason = "近5日动量未较此前15日改善"
		return s
	}
	s.Qualified = true
	s.Score = math.Min(100, math.Max(0, 100-math.Abs(s.FiveDay-3)*5-math.Max(s.TwentyDay, 0)*2+math.Min(s.Acceleration, 5)*2))
	s.Reason = "近5日转强、较前15日改善，20日涨幅仍温和"
	return s
}

func ScreenCandidate(symbol, name string, industry IndustrySignal, bars []foundation.KLine, financials []foundation.StockFundamentals, now time.Time, valuations ...*foundation.StockValuation) CandidateScreening {
	s := CandidateScreening{Industry: industry, CompletedSession: LatestCompletedSession(now), ReturnSessions: CandidatePolicy.ReturnSessions,
		FinancialMethod: "增长或稳健经营+合理估值双通道；营收/扣非非主营分项，估值区间仅供初筛", Reasons: []string{}}
	if len(valuations) > 0 && valuations[0].Current(symbol, now) {
		s.Valuation = valuations[0]
	}
	valueOK, valueReason := reasonableValuation(industry.Name, s.Valuation)
	s.ValuationReason = valueReason
	if !industry.Qualified {
		s.Reasons = append(s.Reasons, industry.Reason)
	}
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "ST") || strings.Contains(upper, "退") {
		s.Reasons = append(s.Reasons, "ST或退市风险股票已排除")
	}
	lines := []foundation.KLine{}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, b := range bars {
		day := b.Time.In(zone).Format("2006-01-02")
		if day <= s.CompletedSession && foundation.IsAStockTradingDay(b.Time.In(zone)) && finitePositive(b.Close) && !b.Meta.Stale {
			lines = append(lines, b)
		}
	}
	slices.SortFunc(lines, func(a, b foundation.KLine) int { return a.Time.Compare(b.Time) })
	lines = slices.CompactFunc(lines, func(a, b foundation.KLine) bool {
		return a.Time.In(zone).Format("2006-01-02") == b.Time.In(zone).Format("2006-01-02")
	})
	if len(lines) < CandidatePolicy.ReturnSessions+1 || lines[len(lines)-1].Time.In(zone).Format("2006-01-02") != s.CompletedSession {
		s.Reasons = append(s.Reasons, "最近20个交易日价格样本不足或最新收盘行情缺失")
	} else {
		last, prev := lines[len(lines)-1], lines[len(lines)-2]
		expected := last.Time.In(zone)
		for i := len(lines) - 1; i >= len(lines)-1-CandidatePolicy.ReturnSessions; i-- {
			if lines[i].Time.In(zone).Format("2006-01-02") != expected.Format("2006-01-02") {
				s.Reasons = append(s.Reasons, "近20个交易日价格样本不连续，不能按更早样本替代")
				break
			}
			expected = expected.AddDate(0, 0, -1)
			for !foundation.IsAStockTradingDay(expected) {
				expected = expected.AddDate(0, 0, -1)
			}
		}
		s.PriceSource = last.Meta.Source
		ret := (last.Close/lines[len(lines)-1-CandidatePolicy.ReturnSessions].Close - 1) * 100
		if finite(ret) {
			s.RecentReturn = &ret
		} else {
			s.Reasons = append(s.Reasons, "价格收益计算无效")
		}
		if ret > CandidatePolicy.StockReturnMax+1e-8 {
			s.Reasons = append(s.Reasons, "近20个交易日涨幅超过50%")
		}
		limit := 10.0
		if strings.HasPrefix(symbol, "30") || strings.HasPrefix(symbol, "68") {
			limit = 20
		}
		if strings.HasSuffix(symbol, ".BJ") {
			limit = 30
		}
		up := math.Round(prev.Close*(1+limit/100)*100) / 100
		down := math.Round(prev.Close*(1-limit/100)*100) / 100
		if last.Close >= up-.00001 || last.Close <= down+.00001 {
			s.Reasons = append(s.Reasons, "上一已收盘交易日涨停或跌停")
		}
	}
	fs := []foundation.StockFundamentals{}
	for _, f := range financials {
		date := strings.Split(f.ReportDate, " ")[0]
		if date == "" || f.PublishedAt.IsZero() || f.PublishedAt.After(now) || f.Meta.Stale || (f.Symbol != "" && f.Symbol != symbol) {
			continue
		}
		f.ReportDate = date
		fs = append(fs, f)
	}
	slices.SortFunc(fs, func(a, b foundation.StockFundamentals) int { return strings.Compare(b.ReportDate, a.ReportDate) })
	fs = slices.CompactFunc(fs, func(a, b foundation.StockFundamentals) bool { return a.ReportDate == b.ReportDate })
	if len(fs) < 2 {
		s.Reasons = append(s.Reasons, "两期可核验财务披露不足，不能确认持续经营状况")
	} else {
		a, b := fs[0], fs[1]
		s.FinancialReports = []string{a.ReportDate, b.ReportDate}
		s.FinancialPublishedAt = []time.Time{a.PublishedAt, b.PublishedAt}
		s.FinancialSource = a.Meta.Source
		if finite(a.RevenueYearOverYear) {
			s.RevenueYoY = &a.RevenueYearOverYear
		}
		if finite(b.RevenueYearOverYear) {
			s.PreviousRevenueYoY = &b.RevenueYearOverYear
		}
		reportDay, err := time.ParseInLocation("2006-01-02", a.ReportDate, zone)
		if err != nil || reportDay.After(now) || now.Sub(reportDay) > 270*24*time.Hour {
			s.Reasons = append(s.Reasons, "最近财务报告过旧或报告期异常")
		}
		previousDay, previousErr := time.ParseInLocation("2006-01-02", b.ReportDate, zone)
		if previousErr != nil || reportDay.Sub(previousDay) > 100*24*time.Hour || !reportDay.After(previousDay) {
			s.Reasons = append(s.Reasons, "财务报告期不连续，不能确认连续增长")
		}
		if !financialHealthy(a) || !financialProfitable(b) {
			s.Reasons = append(s.Reasons, "盈利、扣非盈利或经营现金流未通过财务健康检查")
		}
		switch {
		case finite(a.RevenueYearOverYear) && finite(b.RevenueYearOverYear) && a.RevenueYearOverYear >= CandidatePolicy.HighGrowthMin && b.RevenueYearOverYear > 0:
			s.GrowthKind = "high_growth"
		case finite(a.RevenueYearOverYear) && finite(b.RevenueYearOverYear) && a.RevenueYearOverYear >= CandidatePolicy.SteadyGrowthMin && b.RevenueYearOverYear >= CandidatePolicy.SteadyGrowthMin:
			s.GrowthKind = "steady_growth"
		case valueOK && stableValueBusiness(a) && stableValueBusiness(b):
			s.GrowthKind = "reasonable_valuation"
		default:
			s.Reasons = append(s.Reasons, "未达增长标准，且稳健经营+合理估值通道未通过："+valueReason)
			if valueOK {
				s.Reasons = append(s.Reasons, "两期营收同比须≥-5%、归母及扣非利润同比≥-20%、扣非/归母≥50%，缺测不按零增长")
			}
		}
	}
	s.Qualified = len(s.Reasons) == 0
	if s.Qualified {
		// Equal admission credit for growth and value. Growth has only a small,
		// capped ranking advantage; ever-lower PE does not earn more points.
		s.Score = industry.Score*.6 + 20
		if s.GrowthKind == "high_growth" {
			s.Score += 2
		}
		// Rank only already-qualified companies. This ratio is a same-period
		// profit-composition hint, not a cross-industry profitability comparison.
		share := fs[0].DeductedNetProfit / fs[0].NetProfit
		if finitePositive(share) {
			s.DeductedProfitShare = &share
			s.ProfitQualityBonus = math.Min(share, 1) * 8
			s.Score += s.ProfitQualityBonus
		}
		path := map[string]string{"high_growth": "营收高增", "steady_growth": "营收稳增", "reasonable_valuation": "稳健经营+合理估值"}[s.GrowthKind]
		s.Reasons = append(s.Reasons, fmt.Sprintf("代码筛选通过（%s）：%s；近20日涨幅%.1f%%；营收同比%.1f%%/%.1f%%", path, industry.Reason, *s.RecentReturn, *s.RevenueYoY, *s.PreviousRevenueYoY))
	}
	return s
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func financialProfitable(f foundation.StockFundamentals) bool {
	return finitePositive(f.Revenue) && finitePositive(f.NetProfit) && f.DeductedNetProfitAvailable && finitePositive(f.DeductedNetProfit)
}
func financialHealthy(f foundation.StockFundamentals) bool {
	return financialProfitable(f) && finitePositive(f.ROE) && finitePositive(f.OperatingCashFlowPerShare)
}
