package tradejournal

import (
	"strings"
	"testing"
)

const sampleCSV = `成交日期,证券代码,证券名称,操作,成交价格,成交数量,成交金额,手续费
2026-08-01,600519,贵州茅台,证券买入,1500.00,100,150000.00,15.00
2026-08-04,600519,贵州茅台,证券卖出,1560.00,100,156000.00,18.00
2026-08-05,000001,平安银行,证券买入,10.50,2000,21000.00,5.00
2026-08-20,000001,平安银行,证券卖出,10.10,2000,20200.00,5.00
2026-08-22,300750,宁德时代,证券买入,210.00,200,42000.00,8.00
2026-08-23,300750,宁德时代,证券卖出,228.00,200,45600.00,10.00
2026-09-01,601318,中国平安,证券买入,52.00,1000,52000.00,5.00
`

func TestAnalyzeFullPipeline(t *testing.T) {
	result, err := Analyze(AnalyzeRequest{CSVContent: sampleCSV, Filename: "test.csv"})
	if err != nil {
		t.Fatalf("analyze failed: %v", err)
	}
	if result.Imported != 7 {
		t.Fatalf("imported should be 7, got %d", result.Imported)
	}
	if len(result.Trades) != 3 {
		t.Fatalf("should pair 3 round trips, got %d", len(result.Trades))
	}
	if len(result.OpenPositions) != 1 || result.OpenPositions[0].Symbol != "601318" {
		t.Fatalf("should leave 601318 open, got %+v", result.OpenPositions)
	}
	stats := result.Statistics
	if stats.WinCount != 2 || stats.LossCount != 1 {
		t.Fatalf("win/loss mismatch: %+v", stats)
	}
	if mathAbs(stats.WinRate-66.67) > 0.1 {
		t.Fatalf("win rate should be ~66.67, got %.2f", stats.WinRate)
	}
	if stats.TotalNetProfit <= 0 {
		t.Fatalf("total profit should be positive, got %.2f", stats.TotalNetProfit)
	}
	if len(stats.ProfitCurve) != 3 {
		t.Fatalf("profit curve should have 3 points")
	}
}

func TestFIFOPartialMatching(t *testing.T) {
	csv := `日期,代码,名称,操作,价格,数量
2026-08-01,600519,茅台,买入,100.00,300
2026-08-02,600519,茅台,卖出,110.00,100
2026-08-03,600519,茅台,卖出,120.00,200
`
	result, err := Analyze(AnalyzeRequest{CSVContent: csv})
	if err != nil {
		t.Fatalf("analyze failed: %v", err)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("expected 2 round trips, got %d", len(result.Trades))
	}
	if result.Trades[0].Volume != 100 || result.Trades[1].Volume != 200 {
		t.Fatalf("FIFO volumes wrong: %+v", result.Trades)
	}
	if len(result.OpenPositions) != 0 {
		t.Fatalf("no open position should remain, got %+v", result.OpenPositions)
	}
}

func TestBehaviorBiases(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("日期,代码,名称,操作,价格,数量\n")
	day := 1
	date := func(offset int) string {
		months := []string{"01", "02", "03", "04", "05", "06", "07", "08"}
		month := months[(day+offset)/28%8]
		d := (day+offset)%28 + 1
		return "2026-" + month + "-" + twoDigits(d)
	}
	// 模式：盈利单持1天就跑，亏损单持20天（处置效应）+ 高频 + 追涨亏损。
	for i := 0; i < 8; i++ {
		builder.WriteString(date(0) + ",600519,茅台,买入,100.00,100\n")
		builder.WriteString(date(1) + ",600519,茅台,卖出,103.00,100\n")
		day += 2
		builder.WriteString(date(0) + ",000001,平安,买入,10.00,1000\n")
		builder.WriteString(date(20) + ",000001,平安,卖出,9.00,1000\n")
		day += 21
		builder.WriteString(date(0) + ",300750,宁德,买入,200.00,50\n")
		builder.WriteString(date(1) + ",300750,宁德,卖出,196.00,50\n")
		day += 2
	}
	result, err := Analyze(AnalyzeRequest{CSVContent: builder.String()})
	if err != nil {
		t.Fatalf("analyze failed: %v", err)
	}
	found := map[string]bool{}
	for _, bias := range result.Biases {
		found[bias.ID] = true
	}
	if !found["disposition_effect"] {
		t.Fatalf("disposition effect should be detected, biases: %+v", result.Biases)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	if _, err := Analyze(AnalyzeRequest{CSVContent: "foo,bar\n1,2"}); err == nil {
		t.Fatalf("invalid csv should error")
	}
	if _, err := Analyze(AnalyzeRequest{CSVContent: "日期,代码,操作,价格,数量\n2026-08-01,600519,银行转入,100.00,0\n"}); err == nil {
		t.Fatalf("no-trade csv should error")
	}
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
