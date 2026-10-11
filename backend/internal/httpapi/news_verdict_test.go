package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func newsThemeSampleOverviews() []foundation.ThemeOverview {
	return []foundation.ThemeOverview{
		{
			Theme: "fusion:culture", Name: "文化传媒",
			Leaders:      []string{"新华传媒", "掌阅科技"},
			LeaderStocks: []foundation.BoardStock{{Symbol: "600825.SH", Name: "新华传媒", LimitUpStreak: 2}},
		},
		{
			Theme: "industry:battery", Name: "锂电池",
			LeaderStocks: []foundation.BoardStock{{Symbol: "300530.SZ", Name: "领湃科技"}},
		},
		{
			Theme: "kpl:ai", Name: "AI应用",
			LeaderStocks: []foundation.BoardStock{{Symbol: "300418.SZ", Name: "昆仑万维"}},
		},
		{Theme: "kpl:bank", Name: "银行"},
		{Theme: "industry:mat", Name: "半导体材料"},
	}
}

// 资讯题材（分类器口径）应能通过同义词桥命中行情侧题材（本轮修复的核心缺陷）。
func TestBestThemeMatchBridgesNewsTermsToMarketThemes(t *testing.T) {
	overviews := newsThemeSampleOverviews()
	cases := map[string]string{
		"传媒游戏":  "文化传媒",
		"传媒":    "文化传媒",
		"新能源汽车": "锂电池",
		"锂电":    "锂电池",
		"AI算力":  "AI应用",
		"人工智能":  "AI应用",
		"金融":    "银行",
		"半导体":   "半导体材料",
		"半导体设备": "半导体材料",
	}
	for news, want := range cases {
		got, ok, score := bestThemeMatch(overviews, news)
		if !ok {
			t.Fatalf("theme %q: expected match %q, got no match", news, want)
		}
		if got.Name != want {
			t.Fatalf("theme %q: expected %q, got %q (score %d)", news, want, got.Name, score)
		}
	}
}

// 宏观/无对应板块的词不应被硬塞给某个题材。
func TestBestThemeMatchRejectsMacroNoise(t *testing.T) {
	overviews := newsThemeSampleOverviews()
	for _, noise := range []string{"美联储", "制裁", "天气变化", "俄乌冲突快报", "上市"} {
		if got, ok, _ := bestThemeMatch(overviews, noise); ok {
			t.Fatalf("theme %q should not match, but matched %q", noise, got.Name)
		}
	}
}

// 同分时优先返回有龙头股数据的题材（避免命中空壳题材）。
func TestBestThemeMatchPrefersThemeWithLeadersOnTie(t *testing.T) {
	overviews := []foundation.ThemeOverview{
		{Theme: "kpl:bank", Name: "银行"},
		{Theme: "kpl:broker", Name: "证券", LeaderStocks: []foundation.BoardStock{{Symbol: "600030.SH", Name: "中信证券"}}},
	}
	got, ok, _ := bestThemeMatch(overviews, "金融")
	if !ok || got.Name != "证券" {
		t.Fatalf("expected tie to prefer 证券 (has leaders), got %q ok=%v", got.Name, ok)
	}
}

func TestThemeMatchHintsListsMarketThemes(t *testing.T) {
	overviews := newsThemeSampleOverviews()
	hints := themeMatchHints(overviews, "美联储", 5)
	if len(hints) != 5 || hints[0] != "文化传媒" {
		t.Fatalf("expected 5 market theme hints starting with 文化传媒, got %v", hints)
	}
	if hints := themeMatchHints(overviews, "美联储", 0); hints != nil {
		t.Fatalf("n<=0 should return nil, got %v", hints)
	}
}

type fakeNewsThemeProvider struct {
	items []foundation.ThemeOverview
}

func (f fakeNewsThemeProvider) Overviews(context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	return f.items, foundation.SourceMeta{Source: "theme-overview:test", FetchedAt: time.Now()}, nil
}

func TestNewsThemeStocksReturnsMatchedLeaders(t *testing.T) {
	s := &Server{themeOverview: fakeNewsThemeProvider{items: newsThemeSampleOverviews()}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E4%BC%A0%E5%AA%92%E6%B8%B8%E6%88%8F", nil)
	s.newsThemeStocks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Matched     bool                  `json:"matched"`
		MatchedName string                `json:"matched_name"`
		Stocks      []foundation.BoardStock `json:"stocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Matched || resp.MatchedName != "文化传媒" || len(resp.Stocks) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// 命中题材但无龙头数据时，必须回带 reason（前端据此渲染说明，而非静默空白）。
func TestNewsThemeStocksMatchedButEmptyCarriesReason(t *testing.T) {
	s := &Server{themeOverview: fakeNewsThemeProvider{items: newsThemeSampleOverviews()}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E9%93%B6%E8%A1%8C", nil)
	s.newsThemeStocks(rec, req)

	var resp struct {
		Matched bool     `json:"matched"`
		Stocks  []any    `json:"stocks"`
		Reason  string   `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Matched || len(resp.Stocks) != 0 || resp.Reason == "" {
		t.Fatalf("expected matched+empty+reason, got %+v", resp)
	}
}

// 未命中时回带 hints（行情侧题材参考），供前端提示「行情侧有哪些题材可看」。
func TestNewsThemeStocksUnmatchedCarriesHints(t *testing.T) {
	s := &Server{themeOverview: fakeNewsThemeProvider{items: newsThemeSampleOverviews()}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E7%BE%8E%E8%81%94%E5%82%A8", nil)
	s.newsThemeStocks(rec, req)

	var resp struct {
		Matched bool     `json:"matched"`
		Hints   []string `json:"hints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Matched || len(resp.Hints) == 0 {
		t.Fatalf("expected unmatched with hints, got %+v", resp)
	}
}
