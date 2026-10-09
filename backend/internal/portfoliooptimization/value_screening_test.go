package portfoliooptimization

import (
	"easy-stock/backend/internal/foundation"
	"math"
	"testing"
	"time"
)

func valueFixture() (*foundation.StockValuation, []foundation.StockFundamentals) {
	_, _, bars, fs := screeningFixture()
	pe, pb := 20.0, 2.0
	v := &foundation.StockValuation{Symbol: "600001.SH", PETTM: &pe, PB: &pb, TradeTime: bars[len(bars)-1].Time.Add(15 * time.Hour), Meta: foundation.SourceMeta{Source: "test-valuation"}}
	for i := range fs {
		fs[i].RevenueYearOverYear = 0
		fs[i].NetProfitYearOverYear = -5
		fs[i].DeductedNetProfitYearOverYear = -8
		fs[i].Meta.AvailableFields = []string{"revenue_yoy", "net_profit_yoy", "deducted_net_profit_yoy"}
	}
	return v, fs
}

func TestValueAdmissionDoesNotRequireHighGrowth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*foundation.StockValuation, []foundation.StockFundamentals)
		pass   bool
	}{
		{"zero revenue mild profit decline", nil, true},
		{"boundary", func(v *foundation.StockValuation, f []foundation.StockFundamentals) {
			*v.PETTM = 25
			*v.PB = 3
			for i := range f {
				f[i].RevenueYearOverYear = -5
				f[i].NetProfitYearOverYear = -20
				f[i].DeductedNetProfitYearOverYear = -20
			}
		}, true},
		{"too expensive PE", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { *v.PETTM = 25.01 }, false},
		{"too expensive PB", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { *v.PB = 3.01 }, false},
		{"missing PE", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { v.PETTM = nil }, false},
		{"negative PE", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { *v.PETTM = -2 }, false},
		{"infinite PE", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { *v.PETTM = math.Inf(1) }, false},
		{"stale valuation", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { v.Meta.Stale = true }, false},
		{"old session", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) {
			v.TradeTime = v.TradeTime.AddDate(0, 0, -1)
		}, false},
		{"future session", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) {
			v.TradeTime = v.TradeTime.AddDate(0, 0, 15)
		}, false},
		{"wrong stock", func(v *foundation.StockValuation, _ []foundation.StockFundamentals) { v.Symbol = "000001.SZ" }, false},
		{"missing zero is not flat growth", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) { f[0].Meta.AvailableFields = nil }, false},
		{"previous deteriorated", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) {
			f[1].DeductedNetProfitYearOverYear = -21
		}, false},
		{"revenue deteriorated", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) { f[0].RevenueYearOverYear = -5.1 }, false},
		{"one off earnings", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) { f[0].DeductedNetProfit = 49 }, false},
		{"loss", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) { f[0].NetProfit = -10 }, false},
		{"cashflow unhealthy", func(_ *foundation.StockValuation, f []foundation.StockFundamentals) {
			f[0].OperatingCashFlowPerShare = -1
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, industry, bars, _ := screeningFixture()
			v, fs := valueFixture()
			if tc.mutate != nil {
				tc.mutate(v, fs)
			}
			got := ScreenCandidate("600001.SH", "测试股", industry, bars, fs, now, v)
			if got.Qualified != tc.pass || (tc.pass && got.GrowthKind != "reasonable_valuation") {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestValueRanksAlongsideGrowthWithoutBypassingRiskFilters(t *testing.T) {
	now, industry, bars, growth := screeningFixture()
	v, fs := valueFixture()
	value := ScreenCandidate(v.Symbol, "测试", industry, bars, fs, now, v)
	steady := ScreenCandidate(v.Symbol, "测试", industry, bars, growth, now)
	if value.Score != steady.Score {
		t.Fatal("value structurally penalized", value.Score, steady.Score)
	}
	*v.PETTM = 1
	*v.PB = .1
	cheap := ScreenCandidate(v.Symbol, "测试", industry, bars, fs, now, v)
	if value.Score != cheap.Score {
		t.Fatal("lower PE was automatically rewarded")
	}
	for _, name := range []string{"*ST测试", "退市测试"} {
		if ScreenCandidate(v.Symbol, name, industry, bars, fs, now, v).Qualified {
			t.Fatal("risk exclusion bypassed")
		}
	}
	industry.Name = "城商行Ⅱ"
	*v.PETTM = 13
	if ScreenCandidate(v.Symbol, "测试", industry, bars, fs, now, v).Qualified {
		t.Fatal("bank used general PE ceiling")
	}
	*v.PETTM = 12
	*v.PB = 1.5
	if !ScreenCandidate(v.Symbol, "测试", industry, bars, fs, now, v).Qualified {
		t.Fatal("bank boundary rejected")
	}
	// Growth admission is independent of the optional valuation source.
	*v.PETTM = 80
	*v.PB = 10
	if !ScreenCandidate(v.Symbol, "测试", industry, bars, growth, now, v).Qualified {
		t.Fatal("growth became dependent on value ceiling")
	}
}
