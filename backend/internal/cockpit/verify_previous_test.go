package cockpit

import "testing"

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
