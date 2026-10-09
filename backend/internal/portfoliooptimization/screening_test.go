package portfoliooptimization

import (
	"easy-stock/backend/internal/foundation"
	"math"
	"strings"
	"testing"
	"time"
)

func screeningFixture() (time.Time, IndustrySignal, []foundation.KLine, []foundation.StockFundamentals) {
	z := time.FixedZone("Asia/Shanghai", 8*3600)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, z)
	d, _ := time.ParseInLocation("2006-01-02", LatestCompletedSession(now), z)
	bars := make([]foundation.KLine, 21)
	for i := 20; i >= 0; i-- {
		bars[i] = foundation.KLine{Time: d, Close: 100 + float64(i)*.2, Meta: foundation.SourceMeta{Source: "test-daily"}}
		d = d.AddDate(0, 0, -1)
		for !foundation.IsAStockTradingDay(d) {
			d = d.AddDate(0, 0, -1)
		}
	}
	f := []foundation.StockFundamentals{}
	for _, date := range []string{"2026-06-30", "2026-03-31"} {
		published, _ := time.Parse("2006-01-02", date)
		f = append(f, foundation.StockFundamentals{Symbol: "600001.SH", ReportDate: date, PublishedAt: published.AddDate(0, 1, 0), Revenue: 1000, RevenueYearOverYear: 10, NetProfit: 100, DeductedNetProfit: 80, DeductedNetProfitAvailable: true, DeductedNetProfitReportDate: date, ROE: 5, OperatingCashFlowPerShare: 1, Meta: foundation.SourceMeta{Source: "test-financial"}})
	}
	signal := ScreenIndustry(foundation.MarketIndustryMomentum{Name: "测试行业", ChangePercent: 1, FiveDayChangePercent: 3, TwentyDayChange: 4, Meta: foundation.SourceMeta{Source: "tencent:industry-rank", AvailableFields: []string{"change_percent", "five_day_change_percent", "twenty_day_change_percent"}}})
	return now, signal, bars, f
}

func TestProfitQualityRanksOnlyQualifiedCandidates(t *testing.T) {
	now, signal, bars, fs := screeningFixture()
	a := ScreenCandidate("600001.SH", "测试", signal, bars, fs, now)
	fs[0].DeductedNetProfit = 95
	b := ScreenCandidate("600001.SH", "测试", signal, bars, fs, now)
	if !a.Qualified || !b.Qualified || b.Score <= a.Score || b.DeductedProfitShare == nil || *b.DeductedProfitShare != .95 {
		t.Fatal(a, b)
	}
	blocked := ScreenCandidate("600001.SH", "*ST测试", signal, bars, fs, now)
	if blocked.Qualified || blocked.ProfitQualityBonus != 0 {
		t.Fatal("quality ranking bypassed exclusion", blocked)
	}
}
func TestIndustryEarlyTrendRejectsOverheatedAndUnknownHorizons(t *testing.T) {
	_, signal, _, _ := screeningFixture()
	if !signal.Qualified {
		t.Fatal(signal)
	}
	for _, tc := range []struct {
		five, twenty, daily float64
		valid               bool
	}{{3, 4, 1, true}, {8, 15, 4, true}, {8.1, 4, 1, false}, {3, 15.1, 1, false}, {3, 4, 4.1, false}, {.4, 4, 1, false}, {1, 14, 1, false}, {3, 4, -1, false}, {math.NaN(), 4, 1, false}} {
		m := foundation.MarketIndustryMomentum{FiveDayChangePercent: tc.five, TwentyDayChange: tc.twenty, ChangePercent: tc.daily, Meta: foundation.SourceMeta{AvailableFields: []string{"change_percent", "five_day_change_percent", "twenty_day_change_percent"}}}
		if got := ScreenIndustry(m); got.Qualified != tc.valid {
			t.Fatalf("%+v -> %+v", tc, got)
		}
	}
	if ScreenIndustry(foundation.MarketIndustryMomentum{FiveDayChangePercent: 3, TwentyDayChange: 4}).Qualified {
		t.Fatal("unknown horizons accepted")
	}
}
func TestCodeScreeningFinancialGrowthAndRiskBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(*string, *[]foundation.KLine, *[]foundation.StockFundamentals)
	}{
		{"steady growth", "", func(*string, *[]foundation.KLine, *[]foundation.StockFundamentals) {}},
		{"high growth", "", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].RevenueYearOverYear = 25
		}},
		{"ST", "ST", func(n *string, _ *[]foundation.KLine, _ *[]foundation.StockFundamentals) { *n = "*ST测试" }},
		{"limit up yesterday", "涨停或跌停", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) {
			(*b)[19].Close = 100
			(*b)[20].Close = 110
		}},
		{"limit down yesterday", "涨停或跌停", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) {
			(*b)[19].Close = 100
			(*b)[20].Close = 90
		}},
		{"return exactly fifty", "", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) {
			(*b)[0].Close = (*b)[20].Close / 1.5
		}},
		{"return over fifty", "超过50", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) {
			(*b)[0].Close = (*b)[20].Close / 1.5001
		}},
		{"missing prices", "价格样本不足", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) { *b = (*b)[1:] }},
		{"stale last bar", "收盘行情缺失", func(_ *string, b *[]foundation.KLine, _ *[]foundation.StockFundamentals) { (*b)[20].Meta.Stale = true }},
		{"missing financials", "两期", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) { *f = (*f)[:1] }},
		{"unknown publication", "两期", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].PublishedAt = time.Time{}
		}},
		{"future publication", "两期", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].PublishedAt = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		}},
		{"no recurring profit", "财务健康", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].DeductedNetProfitAvailable = false
		}},
		{"negative cash flow", "财务健康", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].OperatingCashFlowPerShare = -1
		}},
		{"profit loss", "财务健康", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) { (*f)[0].NetProfit = -1 }},
		{"weak growth", "增长标准", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].RevenueYearOverYear = 4.9
		}},
		{"nonfinite growth", "增长标准", func(_ *string, _ *[]foundation.KLine, f *[]foundation.StockFundamentals) {
			(*f)[0].RevenueYearOverYear = math.NaN()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, signal, bars, fs := screeningFixture()
			name := "测试股"
			tc.mutate(&name, &bars, &fs)
			got := ScreenCandidate("600001.SH", name, signal, bars, fs, now)
			if tc.reason == "" {
				if !got.Qualified || got.Score <= 0 {
					t.Fatal(got)
				}
			} else if got.Qualified || !strings.Contains(strings.Join(got.Reasons, "；"), tc.reason) {
				t.Fatal(got)
			}
		})
	}
}
func TestCompletedSessionDuringHolidayAndBeforeClose(t *testing.T) {
	z := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, tc := range []struct {
		now  time.Time
		want string
	}{{time.Date(2026, 10, 5, 12, 0, 0, 0, z), "2026-09-30"}, {time.Date(2026, 10, 8, 10, 0, 0, 0, z), "2026-09-30"}, {time.Date(2026, 10, 8, 15, 1, 0, 0, z), "2026-10-08"}} {
		if got := LatestCompletedSession(tc.now); got != tc.want {
			t.Fatalf("%v: %s", tc.now, got)
		}
	}
}

func TestIndustryGroupMergesBankSubsectors(t *testing.T) {
	for input, want := range map[string]string{"银行": "银行", "国有大型银行Ⅱ": "银行", "城商行Ⅱ": "银行", "农商行Ⅱ": "银行", "机械设备": "机械设备", "": "", "未知": ""} {
		if got := IndustryGroup(input); got != want {
			t.Fatalf("%q group=%q want=%q", input, got, want)
		}
	}
}
