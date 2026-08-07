package duanxianxia

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestFetchLimitUpPoolDecryptsKaipanlaPayload(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 7, 11, 40, 0, 0, location)
	plain := []byte(`{"list":[["603629","利通电子",10.01,120000000,2,"09:31:02","算力租赁+云计算","6天5板",880000000,9200000000,"回封","4","10:22:08"]]}`)
	encrypted := encryptPoolFixture(t, plain)
	requests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/vendor/stockdata/datasource.json":
			_, _ = w.Write([]byte(`{"data_url":"` + "http://" + r.Host + `"}`))
		case "/vendor/stockdata/ztpool.json":
			w.Header().Set("Last-Modified", "Fri, 07 Aug 2026 03:30:20 GMT")
			_, _ = w.Write([]byte(encrypted))
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	client := NewClient(ClientConfig{BaseURL: remote.URL, Now: func() time.Time { return now }})
	snapshot, err := client.FetchLimitUpPool(context.Background())
	if err != nil {
		t.Fatalf("FetchLimitUpPool: %v", err)
	}
	if requests != 2 || snapshot.TradeDate != "2026-08-07" || len(snapshot.Events) != 1 {
		t.Fatalf("unexpected snapshot: requests=%d snapshot=%+v", requests, snapshot)
	}
	event := snapshot.Events[0]
	if event.Symbol != "603629.SH" || event.Streak != 4 || event.Days != 6 || event.Count != 5 || event.BoardType != "回封" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if len(event.Concepts) != 2 || event.Concepts[0] != "算力租赁" || event.Meta.Source != "duanxianxia:kaipanla-limit-up" {
		t.Fatalf("unexpected concepts/meta: %+v", event)
	}
}

func TestLimitUpProviderPrefersKaipanlaAndFillsFromEastMoney(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	date := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	primary := []foundation.LimitUpEvent{{
		Symbol: "603629.SH", Name: "利通电子", Date: date, Streak: 3,
		Concepts: []string{"算力租赁"}, BoardType: "回封",
		Meta: foundation.SourceMeta{Source: "duanxianxia:kaipanla-limit-up"},
	}}
	fallback := []foundation.LimitUpEvent{
		{Symbol: "603629.SH", Name: "利通电子", Date: date, Streak: 2, Price: 18.8, TurnoverRate: 9.6, Industry: "消费电子", Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
		{Symbol: "600001.SH", Name: "东财补位", Date: date, Streak: 1, Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
	}
	merged := mergeLimitUpEvents(primary, fallback, nil)
	if len(merged) != 2 {
		t.Fatalf("merged=%+v", merged)
	}
	if merged[0].Streak != 3 || merged[0].Price != 18.8 || merged[0].TurnoverRate != 9.6 || merged[0].Industry != "消费电子" || merged[0].Meta.Source != "duanxianxia:kaipanla-limit-up" {
		t.Fatalf("primary event was not preserved and hydrated: %+v", merged[0])
	}
	if len(merged[0].Concepts) != 1 || merged[0].Concepts[0] != "算力租赁" || merged[1].Symbol != "600001.SH" {
		t.Fatalf("unexpected merge order/concepts: %+v", merged)
	}
}

func TestApplyKaipanlaThemeLeadersAttributesMatchingTradingDay(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	previousDate := time.Date(2026, 8, 6, 0, 0, 0, 0, location)
	currentDate := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	events := []foundation.LimitUpEvent{
		{Symbol: "600892.SH", Name: "大晟文化", Date: previousDate, Concepts: []string{"创投", "影视"}},
		{Symbol: "600892.SH", Name: "大晟文化", Date: currentDate, Concepts: []string{"创投"}},
	}
	snapshots := []Snapshot{{
		TradeDate: "2026-08-06",
		Themes: []Theme{{
			Code: "803023", Name: "AI应用", Rank: 3, LeadersLoaded: true,
			Leaders: []Leader{{Rank: 2, Role: "龙二", Symbol: "600892.SH", Name: "大晟文化"}},
		}},
	}}

	result := applyKaipanlaThemeLeaders(events, snapshots)
	previous := result[0]
	if previous.PrimaryTheme != "AI应用" || previous.ThemeSource != kaipanlaThemeLeaderSource || previous.ThemeRank != 3 || previous.ThemeLeaderRole != "龙二" {
		t.Fatalf("Kaipanla theme leader attribution missing: %+v", previous)
	}
	if len(previous.Concepts) == 0 || previous.Concepts[0] != "AI应用" {
		t.Fatalf("authoritative theme was not added to concepts: %+v", previous.Concepts)
	}
	if result[1].PrimaryTheme != "" {
		t.Fatalf("theme attribution leaked across trading days: %+v", result[1])
	}
}

func encryptPoolFixture(t *testing.T, plain []byte) string {
	t.Helper()
	block, err := aes.NewCipher(poolCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	padded := append(append([]byte(nil), plain...), make([]byte, padding)...)
	for index := len(padded) - padding; index < len(padded); index++ {
		padded[index] = byte(padding)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, poolCipherIV).CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext)
}
