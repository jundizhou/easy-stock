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
		Matched     bool                    `json:"matched"`
		MatchedName string                  `json:"matched_name"`
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
		Matched bool   `json:"matched"`
		Stocks  []any  `json:"stocks"`
		Reason  string `json:"reason"`
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

// ---------------------------------------------------------------------------
// 全市场股票目录兜底（L2）：按所属行业/概念反查成分股
// ---------------------------------------------------------------------------

func newsCatalogSample() []foundation.StockCatalogEntry {
	return []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "600760.SH", Name: "中航沈飞", Price: 42.5, ChangePercent: 3.2}, Industry: "航空装备", Concepts: []string{"军工", "大飞机"}},
		{BoardStock: foundation.BoardStock{Symbol: "000768.SZ", Name: "中航西飞", Price: 28.1, ChangePercent: 2.1}, Industry: "航空装备", Concepts: []string{"军工"}},
		{BoardStock: foundation.BoardStock{Symbol: "601398.SH", Name: "工商银行", Price: 6.1, ChangePercent: 0.8}, Industry: "银行"},
		{BoardStock: foundation.BoardStock{Symbol: "688981.SH", Name: "中芯国际", Price: 88.0, ChangePercent: -1.2}, Industry: "半导体", Concepts: []string{"芯片"}},
		{BoardStock: foundation.BoardStock{Symbol: "300750.SZ", Name: "宁德时代", Price: 200.0, ChangePercent: 1.5}, Industry: "电池", Concepts: []string{"固态电池"}},
		{BoardStock: foundation.BoardStock{Symbol: "601858.SH", Name: "中国科传", Price: 25.0, ChangePercent: 1.2}, Industry: "文化传媒"},
	}
}

type fakeStockCatalog struct {
	entries []foundation.StockCatalogEntry
	calls   int
}

func (f *fakeStockCatalog) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	f.calls++
	return f.entries, nil
}

// 行业/概念类资讯题材应能匹配到目录里的行业/概念名（行情侧雷达只有数十个题材，覆盖不到）。
func TestBestBoardMatchBridgesIndustryThemes(t *testing.T) {
	names := catalogBoardNames(newsCatalogSample())
	cases := map[string]string{
		"军工":   "军工",
		"芯片":   "芯片",
		"半导体":  "半导体",
		"银行":   "银行",
		"固态电池": "固态电池",
		"航空":   "航空装备",
	}
	for news, want := range cases {
		got, ok, score := bestBoardMatch(names, news)
		if !ok {
			t.Fatalf("theme %q: expected board %q, got no match", news, want)
		}
		if got != want {
			t.Fatalf("theme %q: expected %q, got %q (score %d)", news, want, got, score)
		}
	}
}

// 同分时按同义词书写顺序取优先项（而非名字长短），避免「地产建筑 → 水泥」这类漂移。
func TestBestBoardMatchPrefersSynonymOrderOnTie(t *testing.T) {
	names := []string{"水泥", "建筑施工", "房地产开发"}
	got, ok, _ := bestBoardMatch(names, "地产建筑")
	if !ok || got != "房地产开发" {
		t.Fatalf("expected 房地产开发 (first synonym), got %q ok=%v", got, ok)
	}
}

// 宽松包含匹配容易把宏观/泛词错误塞给板块，必须拒绝。
func TestBestBoardMatchRejectsLooseAndMacro(t *testing.T) {
	names := catalogBoardNames(newsCatalogSample())
	for _, noise := range []string{"美联储", "制裁", "天气变化", "消费", "消费电子"} {
		if got, ok, score := bestBoardMatch(names, noise); ok {
			t.Fatalf("theme %q should not match, but matched %q (score %d)", noise, got, score)
		}
	}
}

// L1 未命中时应退回全市场股票目录反查成分股，并标记 match_source。
func TestNewsThemeStocksFallsBackToStockCatalog(t *testing.T) {
	s := &Server{
		themeOverview:     fakeNewsThemeProvider{items: newsThemeSampleOverviews()},
		themeStockCatalog: &fakeStockCatalog{entries: newsCatalogSample()},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E5%86%9B%E5%B7%A5", nil)
	s.newsThemeStocks(rec, req)

	var resp struct {
		Matched     bool                    `json:"matched"`
		MatchedName string                  `json:"matched_name"`
		MatchSource string                  `json:"match_source"`
		Stocks      []foundation.BoardStock `json:"stocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Matched || resp.MatchedName != "军工" || resp.MatchSource != "eastmoney-catalog" || len(resp.Stocks) != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Stocks[0].Name != "中航沈飞" {
		t.Fatalf("expected stocks sorted by change desc, got %+v", resp.Stocks)
	}
}

// L1 命中但无龙头（空壳题材）时也应退回目录，拿到真实成分股。
func TestNewsThemeStocksUsesCatalogWhenRadarThemeEmpty(t *testing.T) {
	s := &Server{
		themeOverview:     fakeNewsThemeProvider{items: newsThemeSampleOverviews()},
		themeStockCatalog: &fakeStockCatalog{entries: newsCatalogSample()},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E9%93%B6%E8%A1%8C", nil)
	s.newsThemeStocks(rec, req)

	var resp struct {
		Matched     bool                    `json:"matched"`
		MatchSource string                  `json:"match_source"`
		Stocks      []foundation.BoardStock `json:"stocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Matched || resp.MatchSource != "eastmoney-catalog" || len(resp.Stocks) != 1 {
		t.Fatalf("expected catalog fallback, got %+v", resp)
	}
}

// L1 命中的龙头股缺行情时，应改用目录源（否则面板显示「行情缺」）。
func TestNewsThemeStocksPrefersCatalogWhenRadarLacksQuotes(t *testing.T) {
	s := &Server{
		themeOverview:     fakeNewsThemeProvider{items: newsThemeSampleOverviews()},
		themeStockCatalog: &fakeStockCatalog{entries: newsCatalogSample()},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E4%BC%A0%E5%AA%92%E6%B8%B8%E6%88%8F", nil)
	s.newsThemeStocks(rec, req)

	var resp struct {
		Matched     bool                    `json:"matched"`
		MatchedName string                  `json:"matched_name"`
		MatchSource string                  `json:"match_source"`
		Stocks      []foundation.BoardStock `json:"stocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Matched || resp.MatchSource != "eastmoney-catalog" || resp.MatchedName != "文化传媒" || len(resp.Stocks) != 1 {
		t.Fatalf("expected catalog fallback when radar leaders lack quotes, got %+v", resp)
	}
	if resp.Stocks[0].Price <= 0 {
		t.Fatalf("expected quoted stock from catalog, got %+v", resp.Stocks[0])
	}
}

// 股票目录应按 TTL 缓存，避免每次下钻都全量拉取 5000+ 行。
func TestThemeCatalogCachedAcrossRequests(t *testing.T) {
	catalog := &fakeStockCatalog{entries: newsCatalogSample()}
	s := &Server{
		themeOverview:     fakeNewsThemeProvider{items: newsThemeSampleOverviews()},
		themeStockCatalog: catalog,
	}
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/news/theme/stocks?theme=%E5%8D%8A%E5%AF%BC%E4%BD%93", nil)
		s.newsThemeStocks(rec, req)
	}
	if catalog.calls != 1 {
		t.Fatalf("expected stock catalog fetched once (cached), got %d", catalog.calls)
	}
}
