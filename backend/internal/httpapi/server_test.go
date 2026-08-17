package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/gorilla/websocket"
)

func TestServerRejectsRealtimeWithoutSymbols(t *testing.T) {
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/realtime", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "symbols") {
		t.Fatalf("body = %q, want symbols error", rec.Body.String())
	}
}

func TestServerListsSources(t *testing.T) {
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "duanxianxia") || !strings.Contains(rec.Body.String(), "eastmoney") || !strings.Contains(rec.Body.String(), "sina") {
		t.Fatalf("body = %q, want source ids", rec.Body.String())
	}
}

func TestServerRejectsUnknownNewsSource(t *testing.T) {
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/news?source=unknown", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "source") {
		t.Fatalf("body = %q, want source error", rec.Body.String())
	}
}

func TestServerReturnsSectorMap(t *testing.T) {
	server := NewServer(Config{SectorMap: fakeSectorMapProvider{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sector-map?theme=semiconductor_materials", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Data foundation.SectorMap `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Data.Theme != "semiconductor_materials" || payload.Data.Name != "半导体材料" {
		t.Fatalf("unexpected sector map: %+v", payload.Data)
	}
	if len(payload.Data.Groups) != 1 || len(payload.Data.Groups[0].Nodes) != 1 {
		t.Fatalf("unexpected groups: %+v", payload.Data.Groups)
	}
}

func TestServerPaginatesAndFiltersThemeScreen(t *testing.T) {
	server := NewServer(Config{SectorMap: fakePagedSectorMapProvider{}})

	pageOne := requestThemeScreen(t, server, "/api/v1/themes/screen?theme=medicine&page=1&page_size=20")
	if pageOne.Pagination.Total != 25 || pageOne.Pagination.TotalPages != 2 || !pageOne.Pagination.HasMore {
		t.Fatalf("unexpected first-page pagination: %+v", pageOne.Pagination)
	}
	pageOneStocks := uniqueThemeScreenStocks(pageOne.Map)
	if len(pageOneStocks) != 20 {
		t.Fatalf("first-page unique stocks = %d, want 20", len(pageOneStocks))
	}
	for _, stock := range pageOneStocks {
		if stock.RankScore <= 0 || stock.RankRole == "" {
			t.Fatalf("missing full-pool rank fields: %+v", stock)
		}
	}
	if pageOne.SnapshotID == "" || pageOne.Map.Groups[0].Nodes[0].CandidateCount != 25 {
		t.Fatalf("missing snapshot metadata or candidate count: %+v", pageOne)
	}

	pageTwo := requestThemeScreen(t, server, "/api/v1/themes/screen?theme=medicine&page=2&page_size=20")
	if len(uniqueThemeScreenStocks(pageTwo.Map)) != 5 || pageTwo.Pagination.HasMore {
		t.Fatalf("unexpected second page: %+v", pageTwo.Pagination)
	}

	search := requestThemeScreen(t, server, "/api/v1/themes/screen?theme=medicine&q=%E8%BF%9C%E7%AB%AF%E7%9B%AE%E6%A0%87&page_size=20")
	searchStocks := uniqueThemeScreenStocks(search.Map)
	if search.Pagination.Total != 1 || searchStocks["600024.SH"].Name != "远端目标" {
		t.Fatalf("full-pool search did not find target: total=%d stocks=%+v", search.Pagination.Total, searchStocks)
	}

	branch := requestThemeScreen(t, server, "/api/v1/themes/screen?theme=medicine&node=branch&page_size=20")
	if branch.Pagination.Total != 5 {
		t.Fatalf("node-filtered total = %d, want 5", branch.Pagination.Total)
	}
	wideLane := requestThemeScreen(t, server, "/api/v1/themes/screen?theme=medicine&lane=20cm&sort=amount&page_size=20")
	if wideLane.Pagination.Total != 12 {
		t.Fatalf("20cm total = %d, want 12", wideLane.Pagination.Total)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/api/v1/themes/screen?theme=medicine&page_size=100", nil)
	badRec := httptest.NewRecorder()
	server.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid page_size status = %d, want %d", badRec.Code, http.StatusBadRequest)
	}
}

func TestSortThemeCandidatesKeepsKaipanlaLeadersFirstByDefault(t *testing.T) {
	items := []themeCandidate{
		{Stock: foundation.BoardStock{Symbol: "600001.SH", Name: "东财高分", RankScore: 100}, RankScore: 100},
		{Stock: foundation.BoardStock{Symbol: "600005.SH", Name: "龙五", RankScore: 64, RankRole: "龙五"}, RankScore: 64},
		{Stock: foundation.BoardStock{Symbol: "600001.SZ", Name: "龙一", RankScore: 96, RankRole: "龙一"}, RankScore: 96},
		{Stock: foundation.BoardStock{Symbol: "600003.SZ", Name: "龙三", RankScore: 80, RankRole: "龙三"}, RankScore: 80},
	}
	sortThemeCandidates(items, "rank_score")
	got := []string{items[0].Stock.Name, items[1].Stock.Name, items[2].Stock.Name, items[3].Stock.Name}
	want := []string{"龙一", "龙三", "龙五", "东财高分"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order=%v want=%v", got, want)
		}
	}
}

func TestServerReturnsLimitUpLadderWithAdvanceStructure(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	previousDate := time.Date(2026, 8, 3, 0, 0, 0, 0, location)
	currentDate := time.Date(2026, 8, 4, 0, 0, 0, 0, location)
	previousMeta := foundation.SourceMeta{Source: "test:limit-up", SourceURL: "https://example.test/20260803", FetchedAt: previousDate}
	currentMeta := foundation.SourceMeta{Source: "test:limit-up", SourceURL: "https://example.test/20260804", FetchedAt: currentDate}
	requestedSymbols := []string{}
	server := NewServer(Config{
		LimitUp: fakeLimitUpLadderProvider{events: []foundation.LimitUpEvent{
			{Symbol: "600001.SH", Name: "晋级股", Date: previousDate, Streak: 1, Industry: "医药", Amount: 100, Meta: previousMeta},
			{Symbol: "600002.SH", Name: "断板股", Date: previousDate, Streak: 2, Industry: "医药", Amount: 90, Meta: previousMeta},
			{Symbol: "600001.SH", Name: "晋级股", Date: currentDate, Streak: 2, Industry: "医药", Amount: 120, FirstLimitTime: "09:31:00", Meta: currentMeta},
			{Symbol: "300001.SZ", Name: "首板股", Date: currentDate, Streak: 1, Industry: "机器人", Amount: 80, OpenCount: 1, Meta: currentMeta},
			{Symbol: "600003.SH", Name: "*ST样本", Date: currentDate, Streak: 3, Industry: "医药", Amount: 70, Meta: currentMeta},
		}},
		Realtime: fakeLadderRealtimeProvider{
			quotes:    []foundation.Quote{{Symbol: "600001.SH", ChangePercent: 3.25}, {Symbol: "600002.SH", ChangePercent: -1.5}},
			requested: &requestedSymbols,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/short-term/limit-up-ladder", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Data limitUpLadderData `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode limit-up ladder: %v", err)
	}
	data := payload.Data
	if data.Current.TradeDate != "2026-08-04" || data.Previous.TradeDate != "2026-08-03" {
		t.Fatalf("unexpected trade dates: current=%s previous=%s", data.Current.TradeDate, data.Previous.TradeDate)
	}
	if data.Current.LimitUpCount != 2 || data.Current.BoardCount != 1 || data.Current.FirstBoardCount != 1 || data.Current.MaxStreak != 2 || data.Current.STCount != 1 {
		t.Fatalf("unexpected current summary: %+v", data.Current)
	}
	if data.Current.ReopenedCount != 1 {
		t.Fatalf("reopened count = %d, want 1", data.Current.ReopenedCount)
	}
	if len(data.Advance) != 2 || data.Advance[0].FromLevel != 1 || data.Advance[0].Success != 1 || data.Advance[0].Rate != 1 {
		t.Fatalf("unexpected advance structure: %+v", data.Advance)
	}
	if len(data.IndustryHeat) == 0 || data.IndustryHeat[0].Name != "医药" || data.IndustryHeat[0].MaxStreak != 2 {
		t.Fatalf("unexpected industry heat: %+v", data.IndustryHeat)
	}
	if data.Meta.SourceURL != currentMeta.SourceURL {
		t.Fatalf("source URL = %q, want current trading-day URL %q", data.Meta.SourceURL, currentMeta.SourceURL)
	}
	if strings.Join(requestedSymbols, ",") != "600001.SH,600002.SH" {
		t.Fatalf("realtime symbols = %v, want previous ladder symbols", requestedSymbols)
	}
	previousChanges := map[string]*float64{}
	for _, level := range data.Previous.Levels {
		for _, stock := range level.Stocks {
			previousChanges[stock.Symbol] = stock.CurrentChangePercent
		}
	}
	if previousChanges["600001.SH"] == nil || *previousChanges["600001.SH"] != 3.25 || previousChanges["600002.SH"] == nil || *previousChanges["600002.SH"] != -1.5 {
		t.Fatalf("unexpected current changes in previous ladder: %+v", previousChanges)
	}
}

func TestEnrichPreviousCurrentChangesIgnoresRealtimeFailure(t *testing.T) {
	previous := limitUpLadderDay{Levels: []limitUpLadderLevel{{Stocks: []limitUpLadderStock{{Symbol: "600001.SH"}}}}}
	enrichPreviousCurrentChanges(context.Background(), &previous, fakeLadderRealtimeProvider{err: fmt.Errorf("quote service unavailable")})
	if previous.Levels[0].Stocks[0].CurrentChangePercent != nil {
		t.Fatalf("current change should remain absent when realtime quotes fail: %+v", previous)
	}
}

func TestBuildLimitUpLadderAttributesAIApplicationAsPrimaryTheme(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tradeDate := time.Date(2026, 8, 4, 0, 0, 0, 0, location)
	meta := foundation.SourceMeta{Source: "test:limit-up", FetchedAt: tradeDate}
	events := []foundation.LimitUpEvent{
		{Symbol: "003032.SZ", Name: "传智教育", Date: tradeDate, Streak: 7, Industry: "教育", Meta: meta},
		{Symbol: "002354.SZ", Name: "天娱数科", Date: tradeDate, Streak: 3, Meta: meta},
		{Symbol: "603106.SH", Name: "恒银科技", Date: tradeDate, Streak: 3, Meta: meta},
		{Symbol: "002421.SZ", Name: "达实智能", Date: tradeDate, Streak: 2, Meta: meta},
		{Symbol: "001229.SZ", Name: "魅视科技", Date: tradeDate, Streak: 2, Meta: meta},
		{Symbol: "601858.SH", Name: "中国科传", Date: tradeDate, Streak: 2, Meta: meta},
	}
	catalog := []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "003032.SZ", Name: "传智教育"}, Industry: "教育", Concepts: []string{"智谱AI概念", "在线教育", "鸿蒙概念", "华为概念", "职业教育"}},
		{BoardStock: foundation.BoardStock{Symbol: "002354.SZ", Name: "天娱数科"}, Concepts: []string{"AI应用", "AI智能体", "在线教育"}},
		{BoardStock: foundation.BoardStock{Symbol: "603106.SH", Name: "恒银科技"}, Concepts: []string{"AI应用"}},
		{BoardStock: foundation.BoardStock{Symbol: "002421.SZ", Name: "达实智能"}, Concepts: []string{"AI应用", "华为概念"}},
		{BoardStock: foundation.BoardStock{Symbol: "001229.SZ", Name: "魅视科技"}, Concepts: []string{"AI应用"}},
		{BoardStock: foundation.BoardStock{Symbol: "601858.SH", Name: "中国科传"}, Concepts: []string{"在线教育"}},
	}
	for index := 0; index < 20; index++ {
		catalog = append(catalog, foundation.StockCatalogEntry{
			BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("600%03d.SH", index), Name: fmt.Sprintf("华为样本%02d", index)},
			Concepts:   []string{"华为概念"},
		})
	}
	data, err := buildLimitUpLadder(events, catalog, tradeDate.Add(16*time.Hour))
	if err != nil {
		t.Fatalf("build limit-up ladder: %v", err)
	}
	var target *limitUpLadderStock
	for levelIndex := range data.Current.Levels {
		for stockIndex := range data.Current.Levels[levelIndex].Stocks {
			stock := &data.Current.Levels[levelIndex].Stocks[stockIndex]
			if stock.Symbol == "003032.SZ" {
				target = stock
			}
		}
	}
	if target == nil {
		t.Fatal("传智教育 not found in ladder")
	}
	if target.PrimaryTheme != "AI应用" {
		t.Fatalf("primary theme = %q, want AI应用; stock=%+v", target.PrimaryTheme, target)
	}
	if len(target.SecondaryThemes) == 0 || target.SecondaryThemes[0] != "教育" {
		t.Fatalf("secondary themes = %+v, want 教育 first", target.SecondaryThemes)
	}
	if target.ThemeConfidence < 0.7 || len(target.ThemeEvidence) < 2 || !strings.Contains(target.ThemeEvidence[1], "剔除本股后") {
		t.Fatalf("unexpected attribution evidence: confidence=%v evidence=%+v", target.ThemeConfidence, target.ThemeEvidence)
	}
	if len(data.ConceptHeat) == 0 || data.ConceptHeat[0].Name != "AI应用" {
		t.Fatalf("concept heat should rank AI应用 first: %+v", data.ConceptHeat)
	}
}

func TestBuildLimitUpLadderAttributesComputeLeasingBeforeIndustry(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tradeDate := time.Date(2026, 8, 5, 0, 0, 0, 0, location)
	events := []foundation.LimitUpEvent{
		{Symbol: "603629.SH", Name: "利通电子", Date: tradeDate, Streak: 2, Industry: "消费电子"},
		{Symbol: "600001.SH", Name: "云算科技", Date: tradeDate, Streak: 3, Industry: "软件开发"},
		{Symbol: "600002.SH", Name: "数据运营", Date: tradeDate, Streak: 1, Industry: "通信服务"},
	}
	catalog := []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "603629.SH", Name: "利通电子"}, Concepts: []string{"国产芯片", "云计算", "英伟达概念", "算力概念", "人工智能"}},
		{BoardStock: foundation.BoardStock{Symbol: "600001.SH", Name: "云算科技"}, Concepts: []string{"云计算", "数据中心", "算力概念"}},
		{BoardStock: foundation.BoardStock{Symbol: "600002.SH", Name: "数据运营"}, Concepts: []string{"数据中心", "算力概念"}},
	}
	data, err := buildLimitUpLadder(events, catalog, tradeDate.Add(16*time.Hour))
	if err != nil {
		t.Fatalf("build limit-up ladder: %v", err)
	}
	var target *limitUpLadderStock
	for levelIndex := range data.Current.Levels {
		for stockIndex := range data.Current.Levels[levelIndex].Stocks {
			stock := &data.Current.Levels[levelIndex].Stocks[stockIndex]
			if stock.Symbol == "603629.SH" {
				target = stock
			}
		}
	}
	if target == nil || target.PrimaryTheme != "算力租赁" {
		t.Fatalf("利通电子 primary theme = %+v, want 算力租赁", target)
	}
}

func TestBuildLimitUpLadderPrefersKaipanlaPerStockThemes(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tradeDate := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	events := []foundation.LimitUpEvent{
		{Symbol: "600601.SH", Name: "方正科技", Date: tradeDate, Streak: 2, Concepts: []string{"PCB", "AI算力"}, StreakLabel: "2天2板", BoardType: "分歧"},
		{Symbol: "603228.SH", Name: "景旺电子", Date: tradeDate, Streak: 2, Concepts: []string{"AI算力PCB", "光模块"}},
	}
	catalog := []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "600601.SH", Name: "方正科技"}, Concepts: []string{"消费电子", "人工智能", "国产芯片"}},
		{BoardStock: foundation.BoardStock{Symbol: "603228.SH", Name: "景旺电子"}, Concepts: []string{"汽车电子"}},
	}
	data, err := buildLimitUpLadder(events, catalog, tradeDate.Add(11*time.Hour))
	if err != nil {
		t.Fatalf("buildLimitUpLadder: %v", err)
	}
	stock := data.Current.Levels[0].Stocks[0]
	if stock.Symbol != "600601.SH" {
		stock = data.Current.Levels[0].Stocks[1]
	}
	if strings.Join(stock.RawConcepts, ",") != "PCB,AI算力" || stock.StreakLabel != "2天2板" || stock.BoardType != "分歧" {
		t.Fatalf("kaipanla fields were not preferred: %+v", stock)
	}
	if stock.PrimaryTheme != "AI算力" {
		t.Fatalf("primary theme=%q want AI算力; stock=%+v", stock.PrimaryTheme, stock)
	}
}

func TestBuildLimitUpLadderKeepsFallbackConceptsForKaipanlaThemeLeader(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	tradeDate := time.Date(2026, 8, 6, 0, 0, 0, 0, location)
	events := []foundation.LimitUpEvent{{
		Symbol: "600892.SH", Name: "大晟文化", Date: tradeDate, Streak: 3,
		Concepts: []string{"AI应用"}, PrimaryTheme: "AI应用",
		ThemeSource: "duanxianxia:kaipanla-theme-leader", ThemeRank: 3, ThemeLeaderRole: "龙二",
		Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"},
	}}
	catalog := []foundation.StockCatalogEntry{{
		BoardStock: foundation.BoardStock{Symbol: "600892.SH", Name: "大晟文化"},
		Concepts:   []string{"创投", "网络游戏", "影视概念"},
	}}

	data, err := buildLimitUpLadder(events, catalog, tradeDate.Add(16*time.Hour))
	if err != nil {
		t.Fatalf("buildLimitUpLadder: %v", err)
	}
	stock := data.Current.Levels[0].Stocks[0]
	if stock.PrimaryTheme != "AI应用" || !containsConceptLabel(stock.RawConcepts, "创投") || !containsConceptLabel(stock.RawConcepts, "影视概念") {
		t.Fatalf("Kaipanla primary and EastMoney fallback concepts were not combined: %+v", stock)
	}
}

func TestAttributeLimitUpThemesKeepsKaipanlaThemeConsistentAcrossConsecutiveDays(t *testing.T) {
	current := limitUpLadderDay{TradeDate: "2026-08-07", Levels: []limitUpLadderLevel{{Level: 4, Stocks: []limitUpLadderStock{{
		Symbol: "600721.SH", Name: "百花医药", Streak: 4,
		RawConcepts: []string{"CRO", "创新药"},
		Source:      "duanxianxia:kaipanla-limit-up",
	}}}}}
	previousStocks := []limitUpLadderStock{{
		Symbol: "600721.SH", Name: "百花医药", Streak: 3,
		RawConcepts: []string{"CRO", "西部大开发", "创新药", "央国企改革"},
		Source:      "eastmoney:limit-up-pool",
	}}
	for index := 0; index < 5; index++ {
		previousStocks = append(previousStocks, limitUpLadderStock{
			Symbol: fmt.Sprintf("6008%02d.SH", index), Name: fmt.Sprintf("西部样本%d", index), Streak: 3,
			RawConcepts: []string{"西部大开发"}, Source: "eastmoney:limit-up-pool",
		})
	}
	previous := limitUpLadderDay{TradeDate: "2026-08-06", Levels: []limitUpLadderLevel{{Level: 3, Stocks: previousStocks}}}

	attributeLimitUpThemes(&current, &previous, nil)
	stock := previous.Levels[0].Stocks[0]
	if stock.PrimaryTheme != "创新药" {
		t.Fatalf("previous primary theme=%q, want current Kaipanla theme 创新药; stock=%+v", stock.PrimaryTheme, stock)
	}
	if len(stock.SecondaryThemes) == 0 || stock.SecondaryThemes[0] != "西部大开发" {
		t.Fatalf("previous contextual theme should remain secondary: %+v", stock.SecondaryThemes)
	}
	if !strings.Contains(stock.ThemeSource, "cross-day") || len(stock.ThemeEvidence) < 2 || !strings.Contains(stock.ThemeEvidence[0], "昨日3板晋级今日4板") {
		t.Fatalf("missing cross-day evidence/source: source=%q evidence=%+v", stock.ThemeSource, stock.ThemeEvidence)
	}
	if current.Levels[0].Stocks[0].ThemeEvidence[0] != "开盘啦逐股题材：CRO、创新药" {
		t.Fatalf("current Kaipanla evidence label is inaccurate: %+v", current.Levels[0].Stocks[0].ThemeEvidence)
	}
}

func TestAttributeLimitUpThemesUsesKaipanlaTrendLeaderAsAuthority(t *testing.T) {
	current := limitUpLadderDay{TradeDate: "2026-08-07", Levels: []limitUpLadderLevel{{Level: 1, Stocks: []limitUpLadderStock{{
		Symbol: "600001.SH", Name: "今日样本", Streak: 1, RawConcepts: []string{"其他题材"}, Source: "eastmoney:limit-up-pool",
	}}}}}
	previousStocks := []limitUpLadderStock{{
		Symbol: "600892.SH", Name: "大晟文化", Streak: 3,
		RawConcepts:  []string{"AI应用", "创投", "影视"},
		PrimaryTheme: "AI应用", ThemeSource: "duanxianxia:kaipanla-theme-leader", ThemeRank: 3, ThemeLeaderRole: "龙二",
		Source: "eastmoney:limit-up-pool",
	}}
	for index := 0; index < 8; index++ {
		previousStocks = append(previousStocks, limitUpLadderStock{
			Symbol: fmt.Sprintf("6007%02d.SH", index), Name: fmt.Sprintf("创投样本%d", index), Streak: 3,
			RawConcepts: []string{"创投"}, Source: "eastmoney:limit-up-pool",
		})
	}
	previous := limitUpLadderDay{TradeDate: "2026-08-06", Levels: []limitUpLadderLevel{{Level: 3, Stocks: previousStocks}}}

	attributeLimitUpThemes(&current, &previous, nil)
	stock := previous.Levels[0].Stocks[0]
	if stock.PrimaryTheme != "AI应用" || stock.ThemeSource != "duanxianxia:kaipanla-theme-leader" {
		t.Fatalf("Kaipanla trend theme was not authoritative: %+v", stock)
	}
	if stock.ThemeConfidence < 0.9 || len(stock.ThemeEvidence) == 0 || !strings.Contains(stock.ThemeEvidence[0], "第3名：AI应用（龙二）") {
		t.Fatalf("missing Kaipanla trend evidence: confidence=%v evidence=%+v", stock.ThemeConfidence, stock.ThemeEvidence)
	}
}

func TestAttributeLimitUpThemesPrioritizesExplicitAIApplicationEvidence(t *testing.T) {
	stocks := []limitUpLadderStock{{
		Symbol: "002721.SZ", Name: "金一文化", Streak: 2,
		RawConcepts: []string{"国产软件", "DeepSeek概念", "阿里概念", "AI智能体", "数字货币"},
		Source:      "eastmoney:limit-up-pool",
	}, {
		Symbol: "002425.SZ", Name: "凯撒文化", Streak: 3,
		RawConcepts: []string{"人工智能", "网络游戏", "影视概念", "创投"},
		Source:      "eastmoney:limit-up-pool",
	}}
	for index := 0; index < 10; index++ {
		stocks = append(stocks, limitUpLadderStock{
			Symbol: fmt.Sprintf("6009%02d.SH", index), Name: fmt.Sprintf("泛概念样本%d", index), Streak: 2,
			RawConcepts: []string{"创投", "阿里概念"}, Source: "eastmoney:limit-up-pool",
		})
	}
	current := limitUpLadderDay{TradeDate: "2026-08-06", Levels: []limitUpLadderLevel{{Level: 2, Stocks: stocks}}}

	attributeLimitUpThemes(&current, nil, nil)
	for _, stock := range current.Levels[0].Stocks[:2] {
		if stock.PrimaryTheme != "AI应用" {
			t.Fatalf("%s primary theme=%q want AI应用; stock=%+v", stock.Name, stock.PrimaryTheme, stock)
		}
	}
}

func TestServerReturnsThemeOverviews(t *testing.T) {
	server := NewServer(Config{ThemeOverview: fakeThemeOverviewProvider{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "semiconductor_materials") || !strings.Contains(rec.Body.String(), "theme-overview:test") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestServerUsesKaipanlaSnapshotForOverviewAndThemeScreen(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	tradeDate := time.Now().In(location).Format("2006-01-02")
	requestCount := 0
	var remote *httptest.Server
	remote = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		switch r.URL.Path {
		case "/api/getPlateRotatData":
			html := "<tr><td>排名</td><td>" + tradeDate + "</td></tr>" +
				"<tr><td>1</td><td code='801660' name='通信'><span>通信</span><span>9836</span></td></tr>" +
				"<tr><td>2</td><td code='801001' name='芯片'><span>芯片</span><span>9558</span></td></tr>" +
				"<tr><td>3</td><td code='803023' name='AI应用'><span>AI应用</span><span>5292</span></td></tr>" +
				"<tr id='long'></tr>"
			_ = json.NewEncoder(w).Encode(map[string]string{"first": "801660", "html": html})
		case "/api/getLongByPlate":
			code := r.Form.Get("platecode")
			stockCode := map[string]string{"801660": "002792", "801001": "603629", "803023": "300308"}[code]
			name := map[string]string{"801660": "通宇通讯", "801001": "利通电子", "803023": "中际旭创"}[code]
			html := "<td>领涨</td><td><div class='kline' code='" + stockCode + "'><span>龙一</span>" + name + "</div></td>"
			_ = json.NewEncoder(w).Encode(map[string]string{"html": html})
		case "/vendor/stockdata/datasource.json":
			_ = json.NewEncoder(w).Encode(map[string]string{"data_url": remote.URL})
		case "/vendor/stockdata/ztpool.json":
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			_ = json.NewEncoder(w).Encode(map[string]any{"list": [][]any{{"002792", "通宇通讯", 10.0, 100000000, 0, "09:25:00", "通信+卫星通信", "2天2板", 300000000, 5000000000, "一字", 2, "09:25:00"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	server := NewServer(Config{
		Realtime:           fakeThemeRealtimeProvider{},
		ThemeRadarDBPath:   filepath.Join(t.TempDir(), "theme-radar.db"),
		DuanxianxiaBaseURL: remote.URL,
		ThemeRadarFallback: fakeServerRadarFallback{},
	})
	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil)
	overviewRecorder := httptest.NewRecorder()
	server.ServeHTTP(overviewRecorder, overviewRequest)
	if overviewRecorder.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", overviewRecorder.Code, overviewRecorder.Body.String())
	}
	var overviewPayload struct {
		Data []foundation.ThemeOverview
		Meta foundation.SourceMeta
	}
	if err := json.NewDecoder(overviewRecorder.Body).Decode(&overviewPayload); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if len(overviewPayload.Data) != 16 || overviewPayload.Data[0].Name != "通信" || overviewPayload.Data[2].Name != "AI应用" || overviewPayload.Data[3].Source != "local-fallback" || overviewPayload.Meta.Source != "duanxianxia:kaipanla" {
		t.Fatalf("unexpected overview payload: %+v meta=%+v", overviewPayload.Data, overviewPayload.Meta)
	}

	screenPath := fmt.Sprintf(
		"/api/v1/themes/screen?theme=%s&snapshot_id=%s&page=1&page_size=5",
		overviewPayload.Data[0].Theme,
		overviewPayload.Data[0].SnapshotID,
	)
	screen := requestThemeScreen(t, server, screenPath)
	stocks := uniqueThemeScreenStocks(screen.Map)
	if _, exists := stocks["002792.SZ"]; !exists {
		t.Fatalf("kaipanla leader missing from screen: %+v", stocks)
	}
	if _, exists := stocks["600050.SH"]; !exists {
		t.Fatalf("mapped eastmoney fallback stock missing from screen: %+v", stocks)
	}
	if requestCount != 6 {
		t.Fatalf("remote request count=%d, want one fixed refresh batch of 6", requestCount)
	}
}

func TestServerReturnsBatchKLines(t *testing.T) {
	server := NewServer(Config{KLinePrimary: fakeKLineProvider{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline/batch?symbols=000001.SZ,600000.SH&period=day&limit=20", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Data map[string][]foundation.KLine `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Data) != 2 || payload.Data["000001.SZ"][0].Close != 10 || payload.Data["600000.SH"][0].Close != 10 {
		t.Fatalf("unexpected batch payload: %+v", payload.Data)
	}
}

func TestNormalizeKLinePeriodKeepsOnlyLatestDayForOneMinuteChart(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	lines := []foundation.KLine{
		{Time: time.Date(2026, 8, 14, 14, 50, 0, 0, location), Close: 67.10},
		{Time: time.Date(2026, 8, 14, 15, 0, 0, 0, location), Close: 67.36},
		{Time: time.Date(2026, 8, 17, 9, 30, 0, 0, location), Close: 80.83},
		{Time: time.Date(2026, 8, 17, 15, 0, 0, 0, location), Close: 80.83},
	}

	intraday := normalizeKLinePeriod(lines, "1")
	if len(intraday) != 2 {
		t.Fatalf("intraday len=%d, want 2: %+v", len(intraday), intraday)
	}
	for _, line := range intraday {
		if got := line.Time.In(location).Format("2006-01-02"); got != "2026-08-17" {
			t.Fatalf("intraday date=%s, want 2026-08-17", got)
		}
		if line.PreviousClose != 67.36 {
			t.Fatalf("previous close=%v, want 67.36", line.PreviousClose)
		}
	}

	fiveDay := normalizeKLinePeriod(lines, "5")
	if len(fiveDay) != len(lines) {
		t.Fatalf("five-day len=%d, want %d", len(fiveDay), len(lines))
	}
}

func TestServerEvaluatesInflectionSnapshot(t *testing.T) {
	server := NewServer(nil)
	body := `{
		"market": {
			"scope": "sector",
			"market_change_percent": 0.8,
			"scope_change_percent": 1.2,
			"advancers": 2800,
			"decliners": 2000,
			"limit_ups": 36,
			"limit_downs": 12,
			"broken_boards": 15,
			"previous_limit_up_premium": -0.8,
			"stress_days": 1,
			"breadth_improvement": 12,
			"index_above_vwap": true
		},
		"candidates": [
			{
				"symbol": "603137.SH",
				"name": "旧情绪核心",
				"kind": "old_profit",
				"scope": "sector",
				"recognition": {"height":96,"attention":90,"persistence":92,"influence":90,"resilience":70},
				"change_percent": -5.5,
				"scope_change_percent": 1.2,
				"market_change_percent": 0.8,
				"vwap_distance_percent": -3.2,
				"expectation_gap_percent": -6,
				"amount_ratio": 1.5,
				"board_broken": true
			},
			{
				"symbol": "000676.SZ",
				"name": "低位承接物",
				"kind": "new_carrier",
				"scope": "sector",
				"recognition": {"height":78,"attention":82,"persistence":72,"influence":75,"resilience":80},
				"change_percent": 9.9,
				"scope_change_percent": 1.2,
				"market_change_percent": 0.8,
				"vwap_distance_percent": 3,
				"amount_ratio": 1.6,
				"sector_followers": 3,
				"limit_up": true
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategy/inflections/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Data struct {
			PrimarySignal string `json:"primary_signal"`
			Small         struct {
				Status string `json:"status"`
				Setup  string `json:"setup"`
			} `json:"small"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Data.PrimarySignal != "small" || payload.Data.Small.Status != "confirmed" || payload.Data.Small.Setup != "high_low_switch" {
		t.Fatalf("unexpected inflection payload: %+v", payload.Data)
	}
}

func TestServerRejectsUnknownInflectionFields(t *testing.T) {
	server := NewServer(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/strategy/inflections/evaluate", strings.NewReader(`{"market":{},"candidates":[],"unknown":true}`))
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown") {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestServerRequiresConfiguredTokenForPrivateRoutes(t *testing.T) {
	server := NewServer(Config{Token: "secret"})

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRec := httptest.NewRecorder()
	server.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthRec.Code, http.StatusOK)
	}

	unauthorizedReq := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	unauthorizedRec := httptest.NewRecorder()
	server.ServeHTTP(unauthorizedRec, unauthorizedReq)
	if unauthorizedRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorizedRec.Code, http.StatusUnauthorized)
	}

	headerReq := httptest.NewRequest(http.MethodGet, "/api/v1/sources", nil)
	headerReq.Header.Set("Authorization", "Bearer secret")
	headerRec := httptest.NewRecorder()
	server.ServeHTTP(headerRec, headerReq)
	if headerRec.Code != http.StatusOK {
		t.Fatalf("header authorized status = %d, want %d", headerRec.Code, http.StatusOK)
	}

	queryReq := httptest.NewRequest(http.MethodGet, "/api/v1/sources?token=secret", nil)
	queryRec := httptest.NewRecorder()
	server.ServeHTTP(queryRec, queryReq)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("query authorized status = %d, want %d", queryRec.Code, http.StatusOK)
	}
}

func TestWebSocketStreamSendsRealtimeQuoteSnapshot(t *testing.T) {
	server := NewServer(Config{
		Token:    "secret",
		Realtime: fakeRealtimeProvider{},
	})
	ts := httptest.NewServer(server)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/stream?symbols=000001.SZ&interval_ms=10&token=secret"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var message struct {
		Type   string             `json:"type"`
		Quotes []foundation.Quote `json:"quotes"`
	}
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if message.Type != "quotes" {
		t.Fatalf("message type = %q, want quotes", message.Type)
	}
	if len(message.Quotes) != 1 || message.Quotes[0].Symbol != "000001.SZ" || message.Quotes[0].Price <= 0 {
		encoded, _ := json.Marshal(message)
		t.Fatalf("unexpected quote snapshot: %s", encoded)
	}
}

type fakeRealtimeProvider struct{}

func (fakeRealtimeProvider) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	return []foundation.Quote{
		{
			Symbol: "000001.SZ",
			Name:   "平安银行",
			Price:  11.06,
			Meta: foundation.SourceMeta{
				Source:    "test",
				FetchedAt: time.Now(),
			},
		},
	}, nil
}

type fakeThemeRealtimeProvider struct{}

func (fakeThemeRealtimeProvider) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	quotes := make([]foundation.Quote, 0, len(symbols))
	for index, symbol := range symbols {
		quotes = append(quotes, foundation.Quote{
			Symbol:        symbol,
			Name:          symbol,
			Price:         10 + float64(index),
			ChangePercent: float64(index + 1),
			Meta:          foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
		})
	}
	return quotes, nil
}

type fakeServerRadarFallback struct{}

func (fakeServerRadarFallback) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	items := make([]foundation.ThemeOverview, 0, 13)
	for index := 1; index <= 13; index++ {
		items = append(items, foundation.ThemeOverview{
			Theme:      fmt.Sprintf("local-%02d", index),
			Name:       fmt.Sprintf("本地补充%02d", index),
			TrendScore: 80 - index,
		})
	}
	return items, foundation.SourceMeta{Source: "local-trend", FetchedAt: time.Now()}, nil
}

func (fakeServerRadarFallback) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	return foundation.SectorMap{
		Theme: themeID,
		Name:  "通信技术",
		Tabs:  []string{"通信技术"},
		Groups: []foundation.SectorMapGroup{{
			ID: "communication", Name: "通信技术", Nodes: []foundation.SectorMapNode{{
				ID: "communication_core", Name: "通信技术", MatchStatus: "matched",
				StockSource: "eastmoney:stock-selection",
				Stocks:      []foundation.BoardStock{{Symbol: "600050.SH", Name: "中国联通", Price: 10}},
			}},
		}},
		Meta: foundation.SourceMeta{Source: "sector-map:eastmoney", FetchedAt: time.Now()},
	}, nil
}

type fakeLadderRealtimeProvider struct {
	quotes    []foundation.Quote
	err       error
	requested *[]string
}

func (provider fakeLadderRealtimeProvider) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if provider.requested != nil {
		*provider.requested = append((*provider.requested)[:0], symbols...)
	}
	return provider.quotes, provider.err
}

type fakeKLineProvider struct{}

func (fakeKLineProvider) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	return []foundation.KLine{{Symbol: symbol, Time: time.Now(), Open: 9, High: 11, Low: 8, Close: 10, Amount: 1000}}, nil
}

type fakeSectorMapProvider struct{}

func (fakeSectorMapProvider) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	return foundation.SectorMap{
		Theme: themeID,
		Name:  "半导体材料",
		Tabs:  []string{"半导体", "半导体材料"},
		Groups: []foundation.SectorMapGroup{
			{
				ID:   "materials_core",
				Name: "半导体材料",
				Nodes: []foundation.SectorMapNode{
					{ID: "photoresist", Name: "光刻胶", BoardCode: "BK0891", BoardName: "光刻胶"},
				},
			},
		},
		Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
	}, nil
}

type fakePagedSectorMapProvider struct{}

func (fakePagedSectorMapProvider) Build(ctx context.Context, themeID string) (foundation.SectorMap, error) {
	stocks := make([]foundation.BoardStock, 0, 25)
	for index := 0; index < 25; index++ {
		prefix := "600"
		exchange := "SH"
		if index%2 == 1 {
			prefix = "300"
			exchange = "SZ"
		}
		name := fmt.Sprintf("候选%02d", index)
		if index == 24 {
			name = "远端目标"
		}
		stocks = append(stocks, foundation.BoardStock{
			Symbol:         fmt.Sprintf("%s%03d.%s", prefix, index, exchange),
			Name:           name,
			Price:          20,
			ChangePercent:  float64(24-index) / 3,
			Amount:         float64(25-index) * 100_000_000,
			LimitUpStreak:  max(0, 3-index/8),
			LimitUpDays:    max(0, 3-index/8),
			LimitUpCount:   max(0, 3-index/8),
			TotalMarketCap: 5_000_000_000,
			FloatMarketCap: 3_000_000_000,
			Meta:           foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
		})
	}
	return foundation.SectorMap{
		Theme: themeID,
		Name:  "医药",
		Tabs:  []string{"医药"},
		Groups: []foundation.SectorMapGroup{{
			ID:   "medicine",
			Name: "医药",
			Nodes: []foundation.SectorMapNode{
				{ID: "all_medicine", Name: "医药全景", ChangePercent: 2, MainNetInflow: 1, Stocks: stocks, MatchStatus: "matched"},
				{ID: "branch", Name: "医药分支", ChangePercent: 3, MainNetInflow: 1, Stocks: stocks[:5], MatchStatus: "matched"},
			},
		}},
		Meta: foundation.SourceMeta{Source: "test", FetchedAt: time.Now()},
	}, nil
}

type fakeLimitUpLadderProvider struct {
	events []foundation.LimitUpEvent
}

func (f fakeLimitUpLadderProvider) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	return append([]foundation.LimitUpEvent(nil), f.events...), nil
}

func requestThemeScreen(t *testing.T, server *Server, path string) themeScreenData {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Data themeScreenData `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode theme screen: %v", err)
	}
	return payload.Data
}

func uniqueThemeScreenStocks(sectorMap foundation.SectorMap) map[string]foundation.BoardStock {
	stocks := map[string]foundation.BoardStock{}
	for _, group := range sectorMap.Groups {
		for _, node := range group.Nodes {
			for _, stock := range node.Stocks {
				stocks[stock.Symbol] = stock
			}
		}
	}
	return stocks
}

type fakeThemeOverviewProvider struct{}

func (fakeThemeOverviewProvider) Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	return []foundation.ThemeOverview{{Theme: "semiconductor_materials", Name: "半导体材料", ChangePercent: 2.4}}, foundation.SourceMeta{
		Source: "theme-overview:test", FetchedAt: time.Now(),
	}, nil
}
