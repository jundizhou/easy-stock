package duanxianxia

import (
	"encoding/json"
	"testing"
)

func TestParseRotation(t *testing.T) {
	const payload = `{
		"first":"801807","dates":["2026-08-07","2026-08-06"],
		"rows":[
			{"rank":2,"cells":[{"code":"801001","name":"芯片","strong":"-35"},{"code":"801807","name":"算力","strong":"7000"}]},
			{"rank":1,"cells":[{"code":"801807","name":"算力","strong":"12000"},{"code":"801660","name":"通信","strong":9000}]}
		]
	}`
	var rotation rotationResponse
	if err := json.Unmarshal([]byte(payload), &rotation); err != nil {
		t.Fatal(err)
	}
	tradeDate, themes, err := rotation.themes()
	if err != nil {
		t.Fatal(err)
	}
	if tradeDate != "2026-08-07" || len(themes) != 2 || themes[1].Strength != -35 {
		t.Fatalf("unexpected result: date=%s themes=%+v", tradeDate, themes)
	}
	if themes[0].Code != "801807" || themes[0].Name != "算力" || themes[0].Rank != 1 || themes[0].Strength != 12000 {
		t.Fatalf("unexpected first theme: %+v", themes[0])
	}
	history := themes[0].History
	if len(history) != 2 || history[0].TradeDate != tradeDate || history[1] != (RankPoint{TradeDate: "2026-08-06", Rank: 2, Strength: 7000}) {
		t.Fatalf("unexpected rank history: %+v", history)
	}
}

func TestParseLeadersForRotationDate(t *testing.T) {
	const payload = `{"result":"success","data":[
		{"date":"2026-08-06","stocks":[]},
		{"date":"2026-08-07","stocks":[{"code":"603629","name":"利通电子","long":"龙一"},{"code":"000001","name":"平安银行","long":"龙二"}]},
		{"date":"2026-08-05"}
	]}`
	var response leadersResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		t.Fatal(err)
	}
	leaders, err := response.leadersOn("2026-08-07")
	if err != nil || len(leaders) != 2 {
		t.Fatalf("leaders=%+v err=%v", leaders, err)
	}
	if leaders[0] != (Leader{Rank: 1, Role: "龙一", Symbol: "603629.SH", Name: "利通电子"}) ||
		leaders[1] != (Leader{Rank: 2, Role: "龙二", Symbol: "000001.SZ", Name: "平安银行"}) {
		t.Fatalf("unexpected leaders: %+v", leaders)
	}
	if leaders, err := response.leadersOn("2026-08-06"); err != nil || len(leaders) != 0 {
		t.Fatalf("explicit no-leader day: leaders=%+v err=%v", leaders, err)
	}
	for _, date := range []string{"2026-08-05", "2026-08-04"} {
		if _, err := response.leadersOn(date); err == nil {
			t.Errorf("missing data for %s was accepted as a no-leader day", date)
		}
	}
}
