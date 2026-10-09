package eastmoney

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThemeIndexCatalogPagesAcrossIndustriesAndConcepts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("fs") != "m:90+t:2,m:90+t:3" {
			t.Errorf("catalog omitted industries or concepts: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("pn") == "1" {
			fmt.Fprint(w, `{"rc":0,"data":{"total":2,"diff":[{"f12":"BK0475","f14":"银行"}]}}`)
		} else {
			fmt.Fprint(w, `{"rc":0,"data":{"total":2,"diff":[{"f12":"BK1000","f14":"机器人概念"}]}}`)
		}
	}))
	defer server.Close()
	client := NewClient(WithQuoteBaseURL(server.URL))
	boards, err := client.ThemeIndexBoards(context.Background())
	if err != nil || len(boards) != 2 || calls != 2 {
		t.Fatalf("catalog: %+v %v calls=%d", boards, err, calls)
	}
}
func TestBoardIndexKLineUsesBoardNamespaceAndRawIndexPoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("secid") != "90.BK0475" || r.URL.Query().Get("fqt") != "0" {
			t.Errorf("wrong index request: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"rc":0,"data":{"klines":["2026-09-30,1000,1010,1020,990,123,456,3,1,10,0.1"]}}`)
	}))
	defer server.Close()
	client := NewClient(WithBaseURL(server.URL))
	lines, err := client.BoardKLine(context.Background(), "BK0475", 301)
	if err != nil || len(lines) != 1 || lines[0].Close != 1010 || lines[0].Meta.Source != "eastmoney:board-index" {
		t.Fatalf("index: %+v %v", lines, err)
	}
	if _, err = client.BoardKLine(context.Background(), "0.600000", 301); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatal("accepted non-board identifier")
	}
}
