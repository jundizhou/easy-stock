package monitor

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

// testBars 把收盘价与成交量序列包成日线。日期固定在过去，
// 避免"最后一根是今天会被剔除"的分支干扰断言。
func testBars(closes []float64, volumes []float64) []foundation.KLine {
	day := time.Date(2026, 10, 12, 15, 0, 0, 0, foundation.AStockLocation)
	bars := make([]foundation.KLine, 0, len(closes))
	for index, close := range closes {
		volume := 0.0
		if index < len(volumes) {
			volume = volumes[index]
		}
		bars = append(bars, foundation.KLine{
			Symbol: "600000", Time: day.Add(time.Duration(index) * 24 * time.Hour),
			Open: close, High: close, Low: close, Close: close, Volume: volume,
		})
	}
	return bars
}

func fill(value float64, count int) []float64 {
	out := make([]float64, count)
	for index := range out {
		out[index] = value
	}
	return out
}

func TestComputeSignalsMA5CrossUp(t *testing.T) {
	// 63 根 80 的长平稳后一根暴涨到 200：
	// 昨日 MA5=MA20=80，今日 MA5=104 > MA20=86，构成上穿。
	closes := append(fill(80, 62), 200)
	flags := computeSignals(testBars(closes, nil))
	if !flags[SignalMA5UpMA20] {
		t.Fatalf("MA5 应上穿 MA20，实际 %v", flags)
	}
	if flags[SignalMA5DownMA20] {
		t.Fatalf("下穿信号不应成立：%v", flags)
	}
}

func TestComputeSignalsMA5CrossDown(t *testing.T) {
	// 长平稳后一根暴跌：MA5 下穿 MA20。
	closes := append(fill(80, 62), 20)
	flags := computeSignals(testBars(closes, nil))
	if !flags[SignalMA5DownMA20] {
		t.Fatalf("MA5 应下穿 MA20，实际 %v", flags)
	}
	if flags[SignalMA5UpMA20] {
		t.Fatalf("上穿信号不应成立：%v", flags)
	}
}

func TestComputeSignalsMACDCross(t *testing.T) {
	// 长期横盘让 DIF=DEA=0，随后一根大跌/大涨制造 DIF 与 DEA 的分离。
	down := append(fill(100, 40), 60)
	flags := computeSignals(testBars(down, nil))
	if !flags[SignalMACDDead] {
		t.Fatalf("急跌应产生 MACD 死叉，实际 %v", flags)
	}
	up := append(fill(100, 40), 140)
	flags = computeSignals(testBars(up, nil))
	if !flags[SignalMACDGolden] {
		t.Fatalf("急涨应产生 MACD 金叉，实际 %v", flags)
	}
}

func TestComputeSignalsBollBreak(t *testing.T) {
	// 20 根 100（σ=0，上下轨都是 100），第 21 根 110 突破上轨。
	upper := fill(100, 20)
	upper = append(upper, 110)
	flags := computeSignals(testBars(upper, nil))
	if !flags[SignalBollUpperBreak] {
		t.Fatalf("应突破布林上轨，实际 %v", flags)
	}
	lower := fill(100, 20)
	lower = append(lower, 90)
	flags = computeSignals(testBars(lower, nil))
	if !flags[SignalBollLowerBreak] {
		t.Fatalf("应跌破布林下轨，实际 %v", flags)
	}
}

func TestComputeSignalsVolumeBurstAndShrink(t *testing.T) {
	volumes := append(fill(100, 5), 300)
	flags := computeSignals(testBars(fill(100, 6), volumes))
	if !flags[SignalVolDouble] {
		t.Fatalf("3 倍量应判放量，实际 %v", flags)
	}
	if flags[SignalVolShrink] {
		t.Fatalf("放量时不应同时判缩量：%v", flags)
	}
	volumes = append(fill(100, 5), 40)
	flags = computeSignals(testBars(fill(100, 6), volumes))
	if !flags[SignalVolShrink] {
		t.Fatalf("0.4 倍量应判缩量，实际 %v", flags)
	}
}

func TestComputeSignalsTodayBarExcluded(t *testing.T) {
	// 最后一根是今天的盘中棒：信号计算必须剔除它，
	// 结果与"少给一根"的已收盘序列完全一致。
	// 最后一根的时间戳取"最近已收盘交易日"：与 engine 的剔除判断同口径。
	today, _ := time.ParseInLocation("2006-01-02", foundation.LatestAStockSession(time.Now()), foundation.AStockLocation)
	today = today.Add(15 * time.Hour)
	base := testBars(append(fill(80, 62), 200), nil)
	withToday := append(append([]foundation.KLine{}, base...), foundation.KLine{
		Symbol: "600000", Time: today, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1,
	})
	first := computeSignals(base)
	second := computeSignals(withToday)
	if first[SignalMA5UpMA20] != second[SignalMA5UpMA20] {
		t.Fatalf("今日棒应被剔除：base=%v withToday=%v", first[SignalMA5UpMA20], second[SignalMA5UpMA20])
	}
}

func TestComputeSignalsInsufficientData(t *testing.T) {
	// 数据不足时相应信号缺失而不是误报；map 里不该有半个垃圾字段。
	flags := computeSignals(testBars(fill(100, 30), nil))
	for _, id := range []string{SignalMA5UpMA20, SignalMA5DownMA20, SignalMA20UpMA60, SignalMA20DownMA60, SignalBollUpperBreak} {
		if flags[id] {
			t.Fatalf("%s 在数据不足时不应成立", id)
		}
	}
	if flags := computeSignals(testBars(nil, nil)); flags != nil {
		t.Fatalf("空序列应返回 nil，实际 %v", flags)
	}
}

func TestSignalRuleNormalize(t *testing.T) {
	// 合法信号规则：布尔条件、value 归一。
	rule, err := NormalizeRule(Rule{
		Name: "金叉提醒", Type: RuleTypeSignal, Scope: ScopeWatchlist,
		Conditions: []Condition{{Field: SignalMACDGolden, Op: OpGTE, Value: 5}},
		Logic:      "and", CooldownSeconds: 300,
	})
	if err != nil {
		t.Fatalf("合法 signal 规则被拒绝：%v", err)
	}
	if rule.Conditions[0].Value != booleanFieldValue || rule.Conditions[0].Op != OpGTE {
		t.Fatalf("布尔条件未归一：%+v", rule.Conditions[0])
	}
	// value=0 是"要求信号不成立"，归一后应保持 0。
	ruleZero, err := NormalizeRule(Rule{
		Name: "无金叉", Type: RuleTypeSignal, Scope: ScopeWatchlist,
		Conditions: []Condition{{Field: SignalMACDGolden, Op: OpGTE, Value: 0}},
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if ruleZero.Conditions[0].Value != 0 {
		t.Fatalf("value=0 应保持原样：%+v", ruleZero.Conditions[0])
	}
	// 全市场作用域被接受：语义是"全市场里谁今天出现了这个信号"。
	// 扫描规模靠 Filters 收窄，符号清单为空由 engine 展开。
	ruleAll, err := NormalizeRule(Rule{
		Type: RuleTypeSignal, Scope: ScopeAll,
		Conditions: []Condition{{Field: SignalMACDGolden, Op: OpGTE, Value: 1}},
	})
	if err != nil {
		t.Fatalf("signal + scope=all 应被接受：%v", err)
	}
	if ruleAll.Scope != ScopeAll || len(ruleAll.Symbols) != 0 {
		t.Fatalf("全市场信号规则不应保留显式标的：%+v", ruleAll)
	}
	// 未知信号字段被拒绝。
	if _, err := NormalizeRule(Rule{
		Type: RuleTypeSignal, Scope: ScopeWatchlist,
		Conditions: []Condition{{Field: "not_a_signal", Op: OpGTE, Value: 1}},
	}); err == nil {
		t.Fatalf("未知信号字段应被拒绝")
	}
}

func TestEvaluateSignalRule(t *testing.T) {
	rule, err := NormalizeRule(Rule{
		Name: "金叉", Type: RuleTypeSignal, Scope: ScopeWatchlist,
		Conditions: []Condition{
			{Field: SignalMACDGolden, Op: OpGTE, Value: 1},
			{Field: SignalVolDouble, Op: OpGTE, Value: 1},
		},
		Logic: "and", CooldownSeconds: 300,
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	// 只有一个信号成立：AND 语义不触发。
	empty := EvaluateRule(rule, Observation{HaveSignals: true, Signals: map[string]bool{SignalMACDGolden: true}})
	if len(empty) != 0 {
		t.Fatalf("AND 下单信号成立不应触发：%v", empty)
	}
	// 两个都成立：触发，消息是中文信号名。
	signals := EvaluateRule(rule, Observation{HaveSignals: true, Signals: map[string]bool{
		SignalMACDGolden: true, SignalVolDouble: true,
	}})
	if len(signals) != 2 {
		t.Fatalf("应有两个成立信号，实际 %d", len(signals))
	}
	if signals[0].Message != signalLabel(SignalMACDGolden) && signals[1].Message != signalLabel(SignalMACDGolden) {
		t.Fatalf("消息应带中文信号名：%+v", signals)
	}
	// 没算过信号（HaveSignals=false）：保持沉默。
	missing := EvaluateRule(rule, Observation{})
	if len(missing) != 0 {
		t.Fatalf("HaveSignals=false 应沉默：%v", missing)
	}
}

func TestSignalKindAndLabel(t *testing.T) {
	if kind := signalKind(RuleTypeSignal, SignalMACDGolden); kind != "signal_macd_golden" {
		t.Fatalf("signalKind = %q", kind)
	}
	if label := signalLabel(SignalVolDouble); label == "" || label == SignalVolDouble {
		t.Fatalf("信号应有中文标签：%q", label)
	}
}

type fakeKline struct{}

func (fakeKline) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	// 62 根 80 + 一根 200：MA5 上穿 MA20 必然成立。
	return testBars(append(fill(80, 62), 200), nil), nil
}

func TestMergeSignalsComputesOnDemand(t *testing.T) {
	engine := &Engine{
		kline:   fakeKline{},
		klines:  map[string]klineEntry{},
		signals: map[string]signalResult{},
		now:     func() time.Time { return time.Now() },
	}
	observation := Observation{}
	engine.mergeSignals(context.Background(), "600000", &observation)
	if !observation.HaveSignals {
		t.Fatalf("现算路径应给出 HaveSignals=true")
	}
	if !observation.Signals[SignalMA5UpMA20] {
		t.Fatalf("现算应识别 MA5 上穿：%v", observation.Signals)
	}
	// 缓存路径：第二次调用直接读缓存，结果一致。
	second := Observation{}
	engine.mergeSignals(context.Background(), "600000", &second)
	if !second.HaveSignals || !second.Signals[SignalMA5UpMA20] {
		t.Fatalf("缓存路径失效：%v", second.Signals)
	}
}
