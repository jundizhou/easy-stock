package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientBoardsParsesEastMoneyBoardList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/clist/get" {
			t.Fatalf("path = %s, want /api/qt/clist/get", r.URL.Path)
		}
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"total": 2,
				"diff": [
					{"f12":"BK1036","f14":"半导体","f3":2.15,"f20":1000000000,"f21":800000000,"f62":12345678},
					{"f12":"BK0891","f14":"光刻胶","f3":-1.20,"f20":2000000000,"f21":1600000000,"f62":-2345678}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	boards, err := client.Boards(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("Boards failed: %v", err)
	}
	if len(boards) != 2 {
		t.Fatalf("len = %d, want 2", len(boards))
	}
	if boards[0].Code != "BK1036" || boards[0].Name != "半导体" || boards[0].ChangePercent != 2.15 || boards[0].MainNetInflow != 12345678 {
		t.Fatalf("unexpected board: %+v", boards[0])
	}
	if boards[0].Meta.Source != "eastmoney" || boards[0].Meta.SourceURL == "" {
		t.Fatalf("missing source evidence: %+v", boards[0].Meta)
	}
}

func TestClientBoardsFallsBackToDatacenterFundFlow(t *testing.T) {
	quoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary upstream failure", http.StatusBadGateway)
	}))
	defer quoteServer.Close()
	requestedCodes := map[string]bool{}
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dataapi/bkzj/getbkzj" {
			t.Fatalf("path = %s, want /dataapi/bkzj/getbkzj", r.URL.Path)
		}
		requestedCodes[r.URL.Query().Get("code")] = true
		_, _ = w.Write([]byte(`{
			"rc": 0,
			"data": {
				"total": 2,
				"diff": [
					{"f12": "BK0891", "f14": "光刻胶", "f62": -2000000},
					{"f12": "BK1137", "f14": "存储芯片", "f62": -3000000}
				]
			}
		}`))
	}))
	defer dataServer.Close()

	client := NewClient(WithQuoteBaseURL(quoteServer.URL), WithDataBaseURL(dataServer.URL))
	boards, err := client.Boards(context.Background(), "光刻胶", 10)
	if err != nil {
		t.Fatalf("Boards failed: %v", err)
	}
	if len(boards) != 1 || boards[0].Code != "BK0891" || boards[0].MainNetInflow != -2000000 {
		t.Fatalf("unexpected fallback boards: %+v", boards)
	}
	if boards[0].Meta.Source != "eastmoney:bkzj" {
		t.Fatalf("unexpected fallback source: %+v", boards[0].Meta)
	}
	if !requestedCodes["m:90+t:2+f:!50"] || !requestedCodes["m:90+t:3+f:!50"] {
		t.Fatalf("expected fallback to request industry and concept boards: %+v", requestedCodes)
	}
}

func TestClientBoardStocksParsesEastMoneyConstituents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/qt/clist/get" {
			t.Fatalf("path = %s, want /api/qt/clist/get", r.URL.Path)
		}
		if got := r.URL.Query().Get("fs"); got != "b:BK1036" {
			t.Fatalf("fs = %s, want b:BK1036", got)
		}
		w.Write([]byte(`{
			"rc": 0,
			"data": {
				"total": 1,
				"diff": [
					{"f12":"600171","f14":"上海贝岭","f2":30.8,"f3":10.0,"f4":2.8,"f5":405530,"f6":1209720129,"f20":21834837732,"f21":21834837732,"f62":498298566}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithQuoteBaseURL(server.URL))
	stocks, err := client.BoardStocks(context.Background(), "BK1036", 5)
	if err != nil {
		t.Fatalf("BoardStocks failed: %v", err)
	}
	if len(stocks) != 1 {
		t.Fatalf("len = %d, want 1", len(stocks))
	}
	if stocks[0].Symbol != "600171.SH" || stocks[0].Name != "上海贝岭" || stocks[0].Price != 30.8 || stocks[0].ChangePercent != 10 {
		t.Fatalf("unexpected stock: %+v", stocks[0])
	}
	if stocks[0].Meta.Source != "eastmoney" || stocks[0].Meta.SourceURL == "" {
		t.Fatalf("missing source evidence: %+v", stocks[0].Meta)
	}
}
