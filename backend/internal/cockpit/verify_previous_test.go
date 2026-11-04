package cockpit

import (
	"reflect"
	"strings"
	"testing"
)

// 盘中巡查的提示词会带上一次巡查的数值；引用这些数值做环比是正确行为，
// 校验若只看本次事实，会把它们误报为「未核实」。
func TestVerifyNarrativeAcceptsPreviousBriefNumbers(t *testing.T) {
	current := Facts{Intraday: &IntradayView{Status: "高位延续", RiskScore: 15.91}}
	previous := Facts{Intraday: &IntradayView{Status: "高位延续", RiskScore: 13.64, HighWeakRate: 0.1818}}
	narrative := "风险分由 13.64 升至 15.91，高位股走弱率 0.1818（盘中情绪，对比上次巡查）。"

	if got := VerifyNarrative(narrative, current); len(got) != 2 {
		t.Fatalf("只用本次事实校验时应报出环比数值，实际 %v", got)
	}
	if got := VerifyNarrativeAgainst(narrative, current, previous); len(got) != 0 {
		t.Fatalf("纳入上一次巡查的事实时不应报未核实，实际 %v", got)
	}
	// 真正编造的数值仍须被指出。
	if got := VerifyNarrativeAgainst(narrative+" 情绪分 99.99。", current, previous); len(got) != 1 || got[0] != "99.99" {
		t.Fatalf("编造数值必须被标记，实际 %v", got)
	}
}

func TestVerifyNarrativeSeparatesCurrentAndHistoricalTemperature(t *testing.T) {
	current := Facts{Temperature: &Temperature{
		Value: 81.12880391164973, UpPercent: 80.71315996847912,
		MeanChangePercent: 2.174769669970475, MedianChangePercent: 1.6826612622815595,
	}}
	previous := Facts{Temperature: &Temperature{
		Value: 71.65396185034605, UpPercent: 70.47468977742761,
		FetchedAt: "2026-10-28T10:39:50+08:00", Stale: true,
	}}
	cached := *current.Temperature
	cached.Stale = true
	cases := []struct {
		name      string
		narrative string
		current   Facts
		want      []string
	}{
		{
			name:      "reported 11:10 regression",
			narrative: "**数据缺口**：市场温度数据刷新失败，目前沿用 2026-10-28T10:39:50 抓取的缓存快照（71.654 度、上涨占比 70.4747%），涨跌分布结论以该缓存为准。",
			want:      []string{"71.654", "70.4747%"},
		},
		{name: "recovered current readings", current: current, narrative: "本次市场温度 81.1288 度，上涨占比 80.7132%，等权涨跌幅 2.1748%，中位涨跌幅 1.6827%。"},
		{name: "genuine current cache", current: Facts{Temperature: &cached}, narrative: "本次缓存温度 81.1288 度，上涨占比 80.7132%。"},
		{name: "signed statistics", current: Facts{Temperature: &Temperature{MeanChangePercent: -2.1748, MedianChangePercent: 1.6827}}, narrative: "等权涨跌幅 -2.1748%，中位涨跌幅 +1.6827%。"},
		{name: "explicit historical reference", current: current, narrative: "上次温度 71.654 度；本次温度 81.1288 度。"},
		{name: "explicit from-to comparison", current: current, narrative: "温度由 71.654 升至 81.1288（市场温度）。"},
		{name: "comparison field", current: current, narrative: "**较上次变化**：温度 71.654 度，本次 81.1288 度。"},
		{name: "unlabelled old reading", current: current, narrative: "市场温度 71.654 度。", want: []string{"71.654"}},
		{name: "reusing last cache is still a current claim", current: current, narrative: "目前沿用上次缓存，温度 71.654 度。", want: []string{"71.654"}},
		{name: "history marker cannot override current claim", current: current, narrative: "目前沿用上次缓存温度 71.654 度。", want: []string{"71.654"}},
		{name: "gap cannot use a historical qualifier", narrative: "**数据缺口**：上次温度 71.654 度（上次巡查）。", want: []string{"71.654"}},
		{name: "previous number repeated as current", current: current, narrative: "上次温度 71.654 度。当前温度 71.654 度。", want: []string{"71.654"}},
		{name: "current number repeated as historical", current: current, narrative: "当前温度 81.1288 度；上次温度 81.1288 度。", want: []string{"81.1288"}},
		{name: "current transition cannot use historical reading", current: current, narrative: "较上次变化：由 71.654 升至 71.654 度。", want: []string{"71.654"}},
		{name: "missing temperature cannot borrow index values", current: Facts{CNIndexes: []IndexQuote{{Price: 71.654, ChangePercent: 70.4747}}}, narrative: "市场温度 71.654 度，上涨占比 70.4747%。", want: []string{"71.654", "70.4747%"}},
		{name: "temperature fields cannot substitute one another", current: current, narrative: "上涨占比 81.1288%，等权涨跌幅 1.6827%。", want: []string{"81.1288%", "1.6827%"}},
		{name: "missing integer readings", narrative: "市场温度 71 度，上涨占比 70%。", want: []string{"71", "70%"}},
		{name: "genuine zero temperature", current: Facts{Temperature: &Temperature{}}, narrative: "市场温度 0 度，上涨占比 0%。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyNarrativeAgainst(tc.narrative, tc.current, previous); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unverified = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifyNarrativeDoesNotUseErrorStringsAsEvidence(t *testing.T) {
	facts := Facts{
		Sources:     []SourceResult{{Name: "市场温度", Error: "旧缓存 9.99"}},
		Temperature: &Temperature{Value: 81.1288, Warning: "旧缓存 8.88"},
		News:        []NewsLine{{Content: "已提供的新闻正文报价 7.77"}},
	}
	want := []string{"9.99", "8.88"}
	if got := VerifyNarrative("当前读数 9.99、8.88，新闻报价 7.77。", facts); !reflect.DeepEqual(got, want) {
		t.Fatalf("errors must not verify readings; source news remains usable: %v", got)
	}
	if facts.Sources[0].Error == "" || facts.Temperature.Warning == "" {
		t.Fatal("verification must not mutate stored provenance")
	}
}

func TestBriefFlagsHistoricalTemperatureUsedAsCurrentCache(t *testing.T) {
	text := "**数据缺口**：市场温度刷新失败，目前沿用缓存（71.654 度、上涨占比 70.4747%）。"
	narrator := &fakeNarrator{available: true, text: text}
	service, store := newTestService(t, narrator, at(28, 11, 10))
	if err := store.Put(Brief{
		ID: "previous-temperature-test", Phase: PhaseIntraday, SessionDate: "2026-10-28", Status: "ready",
		Facts: Facts{Temperature: &Temperature{Value: 71.65396185034605, UpPercent: 70.47468977742761}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunManual(PhaseIntraday); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	brief := store.List("", 0)[0]
	if brief.Facts.Temperature != nil || !reflect.DeepEqual(brief.UnverifiedNumbers, []string{"71.654", "70.4747%"}) {
		t.Fatalf("missing current temperature must remain missing and old readings must be flagged: %+v", brief)
	}
	if brief.Narrative != text || !strings.Contains(narrator.lastPrompt(), "本次无可用温度读数") {
		t.Fatal("keep generated evidence visible and provide an explicit missing-data constraint")
	}
}
