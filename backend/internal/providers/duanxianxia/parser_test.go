package duanxianxia

import "testing"

func TestParseRotationHTML(t *testing.T) {
	fragment := "<tr><td>排名</td><td>2026-08-07</td><td>2026-08-06</td></tr>" +
		"<tr><td>1</td><td code='801807' name='算力'><span>算力</span><span>12000</span></td><td code='801660' name='通信'><span>通信</span><span>9000</span></td></tr>" +
		"<tr><td>2</td><td code='801001' name='芯片'><span>芯片</span><span>8000</span></td><td code='801807' name='算力'><span>算力</span><span>7000</span></td></tr>" +
		"<tr id='long'></tr>"
	tradeDate, themes, err := ParseRotationHTML(fragment)
	if err != nil {
		t.Fatalf("ParseRotationHTML: %v", err)
	}
	if tradeDate != "2026-08-07" || len(themes) != 2 {
		t.Fatalf("unexpected result: date=%s themes=%+v", tradeDate, themes)
	}
	if themes[0].Code != "801807" || themes[0].Name != "算力" || themes[0].Strength != 12000 {
		t.Fatalf("unexpected first theme: %+v", themes[0])
	}
	if len(themes[0].History) != 2 || themes[0].History[1].Rank != 2 {
		t.Fatalf("unexpected rank history: %+v", themes[0].History)
	}
}

func TestParseLeadersHTML(t *testing.T) {
	fragment := "<td>领涨</td><td><div class='kline' code='603629'><span>龙一</span>利通电子</div>" +
		"<div class='kline' code='300308'><span>龙二</span>中际旭创</div></td><td>当日无领涨</td>"
	leaders, noLeaders, err := ParseLeadersHTML(fragment)
	if err != nil {
		t.Fatalf("ParseLeadersHTML: %v", err)
	}
	if noLeaders || len(leaders) != 2 {
		t.Fatalf("unexpected leaders: noLeaders=%v leaders=%+v", noLeaders, leaders)
	}
	if leaders[0].Symbol != "603629.SH" || leaders[0].Name != "利通电子" || leaders[0].Role != "龙一" {
		t.Fatalf("unexpected first leader: %+v", leaders[0])
	}
}

func TestParseLeadersHTMLAllowsNoLeaderDay(t *testing.T) {
	leaders, noLeaders, err := ParseLeadersHTML("<td>领涨</td><td>当日无领涨</td>")
	if err != nil {
		t.Fatalf("ParseLeadersHTML: %v", err)
	}
	if !noLeaders || len(leaders) != 0 {
		t.Fatalf("unexpected result: noLeaders=%v leaders=%+v", noLeaders, leaders)
	}
}
