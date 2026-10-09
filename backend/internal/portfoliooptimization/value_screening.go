package portfoliooptimization

import (
	"fmt"
	"slices"

	"easy-stock/backend/internal/foundation"
)

// Absolute multiples are transparent admission ceilings, not a fair-value
// estimate or an industry percentile. AI must still compare business/risk.
func reasonableValuation(industry string, v *foundation.StockValuation) (bool, string) {
	peMax, pbMax := CandidatePolicy.ValuePEMax, CandidatePolicy.ValuePBMax
	if IndustryGroup(industry) == "银行" {
		peMax, pbMax = CandidatePolicy.BankPEMax, CandidatePolicy.BankPBMax
	}
	if v == nil || !foundation.PositiveMultiple(v.PETTM) || !foundation.PositiveMultiple(v.PB) {
		return false, "最新有效交易日PE(TTM)/PB缺失或无效，估值未知"
	}
	ok := *v.PETTM <= peMax && *v.PB <= pbMax
	return ok, fmt.Sprintf("PE(TTM) %.2f / PB %.2f，初筛上限 %.0f / %.1f；非公允价值或历史分位", *v.PETTM, *v.PB, peMax, pbMax)
}

func stableValueBusiness(f foundation.StockFundamentals) bool {
	// Serialized absent financial fields historically became zero. Accept a
	// real zero only when the source explicitly reports the field as present.
	known := func(field string, v float64) bool {
		return finite(v) && (v != 0 || slices.Contains(f.Meta.AvailableFields, field))
	}
	return financialProfitable(f) &&
		known("revenue_yoy", f.RevenueYearOverYear) && f.RevenueYearOverYear >= CandidatePolicy.StableRevenueMin &&
		known("net_profit_yoy", f.NetProfitYearOverYear) && f.NetProfitYearOverYear >= CandidatePolicy.StableProfitMin &&
		known("deducted_net_profit_yoy", f.DeductedNetProfitYearOverYear) && f.DeductedNetProfitYearOverYear >= CandidatePolicy.StableProfitMin &&
		f.DeductedNetProfit/f.NetProfit >= .5
}
