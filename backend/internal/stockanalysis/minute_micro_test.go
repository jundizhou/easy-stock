package stockanalysis

import (
	"fmt"
	"math"
	"testing"
)

// genTradingStamps 生成一个 A 股交易日的240个1分钟左端点时间戳
// （09:30-11:29 + 13:00-14:59），升序。
func genTradingStamps(date string) []string {
	stamps := make([]string, 0, 240)
	appendRange := func(startHour, startMinute, count int) {
		for i := 0; i < count; i++ {
			total := startHour*60 + startMinute + i
			stamps = append(stamps, fmt.Sprintf("%s%02d%02d00", date, total/60, total%60))
		}
	}
	appendRange(9, 30, 120) // 上午 09:30-11:29
	appendRange(13, 0, 120) // 下午 13:00-14:59
	return stamps
}

// genTailRallyDay 合成「上午冲高→午后回落→尾盘30分钟拉升」的已知形态：
//   - 上午 close 从 10.0 线性升至 10.354（i=0..119，+0.003/根）
//   - 午后 close 从 10.35 线性回落至 10.172（i=0..89，-0.002/根）
//   - 末30分钟 close 从 10.172 线性拉升至 10.55（+0.0126/根）
//   - 每根 volume=100_000 股、amount=close*volume
func genTailRallyDay(date string) []MinuteBar {
	stamps := genTradingStamps(date)
	bars := make([]MinuteBar, 0, len(stamps))
	for i, stamp := range stamps {
		var close float64
		switch {
		case i < 120:
			close = 10.0 + float64(i)*0.003
		case i < 210:
			close = 10.35 - float64(i-120)*0.002
		default:
			close = 10.172 + float64(i-209)*0.0126
		}
		bars = append(bars, MinuteBar{
			Stamp:  stamp,
			Open:   close - 0.001,
			Close:  close,
			High:   close + 0.001,
			Low:    close - 0.002,
			Volume: 100_000,
			Amount: close * 100_000,
		})
	}
	return bars
}

func nearlyEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

func TestLatestMinuteDay(t *testing.T) {
	bars := genTailRallyDay("20260929")
	bars = append(bars, genTailRallyDay("20260930")...)
	// 注入零成交占位行与跨日数据
	bars = append(bars, MinuteBar{Stamp: "20260930093000", Open: 10, Close: 10, High: 10, Low: 10})
	got := LatestMinuteDay(bars)
	if len(got) != 240 {
		t.Fatalf("LatestMinuteDay 应只保留最近一个交易日的240根有效K线，实际 %d", len(got))
	}
	for i, bar := range got {
		if len(bar.Stamp) != 14 || bar.Stamp[:8] != "20260930" {
			t.Fatalf("第%d根时间戳异常：%q", i, bar.Stamp)
		}
		if bar.Volume <= 0 {
			t.Fatalf("零成交占位行未被过滤：%q", bar.Stamp)
		}
		if i > 0 && got[i-1].Stamp >= bar.Stamp {
			t.Fatalf("样本未按时间升序：%s >= %s", got[i-1].Stamp, bar.Stamp)
		}
	}
}

func TestAnalyzeMinuteMicroTailRally(t *testing.T) {
	bars := genTailRallyDay("20260930")
	m := analyzeMinuteMicro(bars)
	if m == nil {
		t.Fatal("240根有效样本不应返回 nil")
	}
	if m.TradeDate != "2026-09-30" {
		t.Fatalf("TradeDate = %q，期望 2026-09-30", m.TradeDate)
	}
	if m.BarCount != 240 || !m.Available {
		t.Fatalf("BarCount/Available 异常：%d/%v", m.BarCount, m.Available)
	}
	// 末30分钟：10.172 → 10.55，收益约 +3.71%
	if !nearlyEqual(m.Last30Return, 3.71, 0.05) {
		t.Fatalf("Last30Return = %.4f，期望约 3.71", m.Last30Return)
	}
	// 高点出现在尾盘拉升末段（约10.55+0.001）
	if m.HighTime != "14:59" {
		t.Fatalf("HighTime = %q，期望 14:59", m.HighTime)
	}
	// 全日低点在开盘附近（10.0-0.002）
	if m.LowTime != "09:30" {
		t.Fatalf("LowTime = %q，期望 09:30", m.LowTime)
	}
	// 收盘 10.55 显著高于均价，尾盘收益>=0.5 → 状态=尾盘走强
	if m.State != "尾盘走强" {
		t.Fatalf("State = %q，期望 尾盘走强（CloseVsVWAP=%.2f%%）", m.State, m.CloseVsVWAPPercent)
	}
	if m.CloseVsVWAPPercent <= 0 {
		t.Fatalf("收盘应高于VWAP，实际 %+.2f%%", m.CloseVsVWAPPercent)
	}
	// 形态标签：尾盘拉升 + 尾盘企稳
	joined := ""
	for _, tag := range m.Tags {
		joined += tag + "|"
	}
	if !containsSubstring(joined, "尾盘拉升") {
		t.Fatalf("缺少尾盘拉升标签：%v", m.Tags)
	}
	if !containsSubstring(joined, "尾盘企稳") {
		t.Fatalf("缺少尾盘企稳标签：%v", m.Tags)
	}
	if containsSubstring(joined, "冲高回落") {
		t.Fatalf("该形态不应标记冲高回落：%v", m.Tags)
	}
	// 量分布均匀时尾盘量占比约 12.5%
	if !nearlyEqual(m.Last30VolumeShare, 12.5, 0.1) {
		t.Fatalf("Last30VolumeShare = %.2f，期望约 12.5", m.Last30VolumeShare)
	}
	// 证据链非空且类别正确
	evidence := minuteMicroEvidence(m)
	if len(evidence) != 1 || evidence[0].Category != "分钟结构" {
		t.Fatalf("minuteMicroEvidence 输出异常：%+v", evidence)
	}
	if evidence[0].AsOf != "2026-09-30" {
		t.Fatalf("Evidence AsOf = %q", evidence[0].AsOf)
	}
}

func TestAnalyzeMinuteMicroInsufficientSample(t *testing.T) {
	if m := analyzeMinuteMicro(genTailRallyDay("20260930")[:59]); m != nil {
		t.Fatalf("59根样本应返回 nil")
	}
	if got := LatestMinuteDay(nil); got != nil {
		t.Fatalf("空输入应返回 nil")
	}
}

func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestAnalyzeWiresMinuteMicro 验证 Analyze 全流程接线：带分钟K输入时
// Analysis.MinuteMicro 有值、证据链出现「分钟结构」、数据质量标注 ready；
// 不带分钟K时三者均退化为缺失态且不影响主分析。
func TestAnalyzeWiresMinuteMicro(t *testing.T) {
	lines := syntheticTrendLines("000001.SZ", 60, 10, .1, 500_000_000)
	minuteBars := genTailRallyDay("20260930")

	analysis, err := Analyze(Input{Symbol: "000001.SZ", KLines: lines, MinuteBars: minuteBars})
	if err != nil {
		t.Fatalf("Analyze 失败: %v", err)
	}
	if analysis.MinuteMicro == nil || !analysis.MinuteMicro.Available {
		t.Fatal("Analysis.MinuteMicro 应有值且可用")
	}
	if analysis.MinuteMicro.State != "尾盘走强" {
		t.Fatalf("MinuteMicro.State = %q", analysis.MinuteMicro.State)
	}
	foundEvidence := false
	for _, item := range analysis.Evidence {
		if item.Category == "分钟结构" {
			foundEvidence = true
			break
		}
	}
	if !foundEvidence {
		t.Fatal("证据链缺少分钟结构条目")
	}
	foundQuality := false
	for _, item := range analysis.DataQuality {
		if item.Key == "minute_kline" && item.Status == "ready" {
			foundQuality = true
			break
		}
	}
	if !foundQuality {
		t.Fatal("DataQuality 缺少 minute_kline=ready")
	}

	baseline, err := Analyze(Input{Symbol: "000001.SZ", KLines: lines})
	if err != nil {
		t.Fatalf("无分钟K的 Analyze 失败: %v", err)
	}
	if baseline.MinuteMicro != nil {
		t.Fatal("无分钟K输入时 MinuteMicro 应为 nil")
	}
	for _, item := range baseline.Evidence {
		if item.Category == "分钟结构" {
			t.Fatal("无分钟K输入时证据链不应出现分钟结构")
		}
	}
	foundMissing := false
	for _, item := range baseline.DataQuality {
		if item.Key == "minute_kline" && item.Status == "missing" {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatal("无分钟K输入时 DataQuality 应标注 minute_kline=missing")
	}
}
