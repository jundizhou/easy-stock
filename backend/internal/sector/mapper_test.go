package sector

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/narrative"
)

func TestMapperBuildsSemiconductorMaterialsMap(t *testing.T) {
	mapper := NewMapper(fakeBoardProvider{
		boards: []foundation.Board{
			{Code: "BK1036", Name: "半导体", ChangePercent: 2.1, MainNetInflow: 1000, Meta: foundation.SourceMeta{Source: "test"}},
			{Code: "BK0891", Name: "光刻胶", ChangePercent: -1.2, MainNetInflow: -200, Meta: foundation.SourceMeta{Source: "test"}},
			{Code: "BK1595", Name: "原料药", ChangePercent: 4.3, MainNetInflow: 300, Meta: foundation.SourceMeta{Source: "test"}},
		},
		stocks: map[string][]foundation.BoardStock{
			"BK1036": {
				{Symbol: "600171.SH", Name: "上海贝岭", Price: 30.8, ChangePercent: 10},
			},
			"BK0891": {
				{Symbol: "300576.SZ", Name: "容大感光", Price: 48.6, ChangePercent: -1.1},
			},
		},
	})

	got, err := mapper.Build(context.Background(), "semiconductor_materials")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if got.Theme != "semiconductor_materials" || got.Name != "半导体材料" {
		t.Fatalf("unexpected theme: %+v", got)
	}
	if len(got.Groups) == 0 || len(got.Groups[0].Nodes) == 0 {
		t.Fatalf("expected groups and nodes: %+v", got.Groups)
	}
	node := findNode(got, "photoresist")
	if node == nil {
		t.Fatalf("expected photoresist node: %+v", got.Groups)
	}
	if node.BoardCode != "BK0891" || node.BoardName != "光刻胶" || node.ChangePercent != -1.2 {
		t.Fatalf("unexpected photoresist node: %+v", node)
	}
	if node.MatchStatus != "matched" || node.BoardSource != "test" || node.StockSource != "eastmoney:board-constituents" {
		t.Fatalf("unexpected node data sources: %+v", node)
	}
	if len(node.Stocks) != 1 || node.Stocks[0].Symbol != "300576.SZ" {
		t.Fatalf("unexpected photoresist stocks: %+v", node.Stocks)
	}
	if got.Meta.Source != "sector-map:eastmoney" || got.Meta.FetchedAt.IsZero() {
		t.Fatalf("missing map meta: %+v", got.Meta)
	}
	for _, group := range got.Groups {
		for _, node := range group.Nodes {
			if node.Stocks == nil {
				t.Fatalf("node stocks must be an empty array, not nil: %+v", node)
			}
		}
	}
}

func TestMapperRejectsUnknownTheme(t *testing.T) {
	mapper := NewMapper(fakeBoardProvider{})
	_, err := mapper.Build(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected unknown theme error")
	}
}

func TestMapperBuildsBatteryTheme(t *testing.T) {
	mapper := NewMapper(fakeBoardProvider{
		boards: []foundation.Board{
			{Code: "BK1033", Name: "电池", ChangePercent: 1.2, MainNetInflow: 1000, Meta: foundation.SourceMeta{Source: "test"}},
			{Code: "BK1303", Name: "锂电池", ChangePercent: -0.8, MainNetInflow: -2000, Meta: foundation.SourceMeta{Source: "test"}},
			{Code: "BK0989", Name: "储能概念", ChangePercent: 2.4, MainNetInflow: 3000, Meta: foundation.SourceMeta{Source: "test"}},
		},
		stocks: map[string][]foundation.BoardStock{
			"BK1033": {{Symbol: "300750.SZ", Name: "宁德时代", Price: 300, ChangePercent: 2.2}},
		},
	})

	got, err := mapper.Build(context.Background(), "battery")
	if err != nil {
		t.Fatalf("Build battery failed: %v", err)
	}
	if got.Theme != "battery" || got.Name != "电池" {
		t.Fatalf("unexpected battery theme: %+v", got)
	}
	if len(got.ThemeTabs) < 12 {
		t.Fatalf("expected all theme tabs, got %+v", got.ThemeTabs)
	}
	node := findNode(got, "battery_core")
	if node == nil || node.BoardCode != "BK1033" {
		t.Fatalf("expected battery core node matched to BK1033: %+v", node)
	}
}

func TestMapperBuildsThemeOverviewsFromOneBoardSnapshot(t *testing.T) {
	mapper := NewMapper(fakeBoardProvider{
		boards: []foundation.Board{
			{Code: "BK1036", Name: "半导体", ChangePercent: 2, MainNetInflow: 100},
			{Code: "BK0891", Name: "光刻胶", ChangePercent: 4, MainNetInflow: 300},
			{Code: "BK1033", Name: "电池", ChangePercent: -1, MainNetInflow: -200},
		},
	})

	items, meta, err := mapper.Overviews(context.Background())
	if err != nil {
		t.Fatalf("Overviews failed: %v", err)
	}
	if len(items) != len(Themes()) {
		t.Fatalf("overview count = %d, want %d", len(items), len(Themes()))
	}
	semiconductor := findThemeOverview(items, "semiconductor")
	if semiconductor == nil {
		t.Fatal("expected semiconductor overview")
	}
	if semiconductor.MatchedNodes != 1 || semiconductor.ChangePercent != 2 || semiconductor.RisingNodes != 1 {
		t.Fatalf("unexpected semiconductor overview: %+v", semiconductor)
	}
	if semiconductor.TopNode != "半导体" || semiconductor.TopNodeChangePercent != 2 {
		t.Fatalf("unexpected top node: %+v", semiconductor)
	}
	if meta.Source != "theme-overview:eastmoney:stock-selection" || meta.FetchedAt.IsZero() {
		t.Fatalf("unexpected overview meta: %+v", meta)
	}
}

func TestThemeOverviewUsesCatalogChangeWhenBoardSnapshotHasNoChange(t *testing.T) {
	theme := simpleTheme("test", "测试题材", group("core", "核心",
		node("test_node", "测试节点", []string{"测试板块"}),
	))
	boards := []foundation.Board{{
		Code: "BK0001", Name: "测试板块", MainNetInflow: 100,
		Meta: foundation.SourceMeta{Source: "eastmoney:bkzj"},
	}}
	catalog := []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", ChangePercent: 4}, Industry: "测试板块"},
		{BoardStock: foundation.BoardStock{Symbol: "000002.SZ", ChangePercent: 2}, Industry: "测试板块"},
	}
	overview := buildThemeOverview(theme, boards, newCatalogIndex(catalog))
	if overview.ChangePercent != 3 || overview.RisingNodes != 1 || overview.TopNodeChangePercent != 3 {
		t.Fatalf("unexpected catalog overview: %+v", overview)
	}
	if overview.MainNetInflow != 100 {
		t.Fatalf("expected board fund flow to be retained: %+v", overview)
	}
}

func TestMapperUsesStockCatalogWhenBoardStocksFail(t *testing.T) {
	mapper := NewMapper(
		fakeBoardProvider{
			boards: []foundation.Board{
				{Code: "BK0891", Name: "光刻胶", ChangePercent: 0, MainNetInflow: -200, Meta: foundation.SourceMeta{Source: "test"}},
			},
			stockErr: errors.New("constituents unavailable"),
		},
		WithStockCatalogProvider(fakeStockCatalogProvider{entries: []foundation.StockCatalogEntry{{
			BoardStock: foundation.BoardStock{Symbol: "300576.SZ", Name: "容大感光", Price: 48.8, ChangePercent: 1.24},
			Concepts:   []string{"光刻胶"},
		}}}),
	)

	got, err := mapper.Build(context.Background(), "semiconductor_materials")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	node := findNode(got, "photoresist")
	if node == nil {
		t.Fatal("expected photoresist node")
	}
	if len(node.Stocks) == 0 {
		t.Fatalf("expected stock-catalog fallback stocks: %+v", node)
	}
	if node.StockSource != "eastmoney:stock-selection" {
		t.Fatalf("expected stock-catalog source: %+v", node)
	}
	if node.ChangePercent != 1.24 {
		t.Fatalf("expected stock catalog to backfill node change percent: %+v", node)
	}
	if len(node.Warnings) != 0 {
		t.Fatalf("stock-catalog fallback should not add noisy warnings: %+v", node)
	}
	if node.Stocks[0].Symbol != "300576.SZ" || node.Stocks[0].Price != 48.8 {
		t.Fatalf("unexpected stock-catalog fallback stock: %+v", node.Stocks[0])
	}
}

func TestMapperAddsRecentLimitUpLeadersToHealthcareCandidates(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	mapper := NewMapper(
		fakeBoardProvider{
			boards: []foundation.Board{
				{Code: "BK1216", Name: "医药生物", Meta: foundation.SourceMeta{Source: "test"}},
				{Code: "BK0727", Name: "医疗服务", Meta: foundation.SourceMeta{Source: "test"}},
			},
			stockErr: errors.New("constituents unavailable"),
		},
		WithQuoteProvider(fakeHealthcareQuoteProvider{}),
		WithStockCatalogProvider(fakeStockCatalogProvider{entries: healthcareCatalogEntries()}),
		WithLimitUpProvider(fakeLimitUpProvider{events: []foundation.LimitUpEvent{
			{Symbol: "600664.SH", Name: "哈药股份", Date: time.Date(2026, 7, 16, 0, 0, 0, 0, location), Industry: "化学制药", Streak: 5, Days: 5, Count: 5, Amount: 4_500_000_000, FloatMarketCap: 13_000_000_000, FirstLimitTime: "09:25:02"},
			{Symbol: "301520.SZ", Name: "万邦医药", Date: time.Date(2026, 7, 15, 0, 0, 0, 0, location), Industry: "医疗服务", Streak: 1, Days: 1, Count: 1, Amount: 1_100_000_000, FloatMarketCap: 4_000_000_000, FirstLimitTime: "15:00:00"},
		}}),
	)

	got, err := mapper.Build(context.Background(), "healthcare")
	if err != nil {
		t.Fatalf("Build healthcare failed: %v", err)
	}
	medicine := findNode(got, "medicine")
	medicalService := findNode(got, "medical_service")
	hayao := findStock(medicine, "600664.SH")
	wanbang := findStock(medicalService, "301520.SZ")
	if hayao == nil || hayao.LimitUpStreak != 5 || hayao.LimitRegime != "10cm" {
		t.Fatalf("expected 10cm leader candidate 哈药股份: %+v", hayao)
	}
	if wanbang == nil || wanbang.LimitUpStreak != 1 || wanbang.LimitRegime != "20cm" {
		t.Fatalf("expected 20cm leader candidate 万邦医药: %+v", wanbang)
	}
	if hayao.Price != 5.33 || wanbang.Price != 66.62 {
		t.Fatalf("expected realtime prices for limit-up candidates: hayao=%+v wanbang=%+v", hayao, wanbang)
	}
}

func TestMapperBuildsHealthcareCandidatesFromIndustryAndConceptCatalog(t *testing.T) {
	mapper := NewMapper(
		fakeBoardProvider{boards: []foundation.Board{}},
		WithStockCatalogProvider(fakeStockCatalogProvider{entries: healthcareCatalogEntries()}),
	)

	got, err := mapper.Build(context.Background(), "healthcare")
	if err != nil {
		t.Fatalf("Build healthcare failed: %v", err)
	}
	medicine := findNode(got, "medicine")
	medicalService := findNode(got, "medical_service")
	if findStock(medicine, "600664.SH") == nil {
		t.Fatalf("expected 哈药股份 from 化学制药/创新药 catalog membership: %+v", medicine)
	}
	if findStock(medicine, "301520.SZ") == nil || findStock(medicalService, "301520.SZ") == nil {
		t.Fatalf("expected 万邦医药 from 生物医药/创新药/CRO catalog membership: medicine=%+v medicalService=%+v", medicine, medicalService)
	}
	if medicine.StockSource != "eastmoney:stock-selection" || medicalService.StockSource != "eastmoney:stock-selection" {
		t.Fatalf("expected dynamic EastMoney catalog sources: medicine=%+v medicalService=%+v", medicine, medicalService)
	}
}

func TestMapperHydratesCatalogStocksForNodesWithoutBoardMatch(t *testing.T) {
	mapper := NewMapper(
		fakeBoardProvider{boards: []foundation.Board{}},
		WithStockCatalogProvider(fakeStockCatalogProvider{entries: []foundation.StockCatalogEntry{{
			BoardStock: foundation.BoardStock{Symbol: "300576.SZ", Name: "容大感光", Price: 48.8, ChangePercent: 1.24},
			Concepts:   []string{"光刻胶"},
		}}}),
	)

	got, err := mapper.Build(context.Background(), "semiconductor_materials")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	node := findNode(got, "photoresist")
	if node == nil {
		t.Fatal("expected photoresist node")
	}
	if node.MatchStatus != "matched" || node.StockSource != "eastmoney:stock-selection" {
		t.Fatalf("expected catalog-matched node: %+v", node)
	}
	if len(node.Stocks) == 0 || node.ChangePercent != 1.24 {
		t.Fatalf("expected stock catalog to hydrate node: %+v", node)
	}
}

func TestMapperPrefersSpecificBoardKeywordOverBroadSubstring(t *testing.T) {
	mapper := NewMapper(fakeBoardProvider{
		boards: []foundation.Board{
			{Code: "BK1615", Name: "铜", Meta: foundation.SourceMeta{Source: "test"}},
			{Code: "BK0877", Name: "PCB", Meta: foundation.SourceMeta{Source: "test"}},
		},
	})

	got, err := mapper.Build(context.Background(), "semiconductor_materials")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	node := findNode(got, "pcb_ccl")
	if node == nil {
		t.Fatal("expected pcb_ccl node")
	}
	if node.BoardCode != "BK0877" || node.BoardName != "PCB" {
		t.Fatalf("expected PCB board, got %+v", node)
	}
}

type fakeBoardProvider struct {
	boards   []foundation.Board
	stocks   map[string][]foundation.BoardStock
	stockErr error
}

func (f fakeBoardProvider) Boards(ctx context.Context, keyword string, limit int) ([]foundation.Board, error) {
	return f.boards, nil
}

func (f fakeBoardProvider) BoardStocks(ctx context.Context, boardCode string, limit int) ([]foundation.BoardStock, error) {
	if f.stockErr != nil {
		return nil, f.stockErr
	}
	if f.stocks == nil {
		return nil, nil
	}
	return f.stocks[boardCode], nil
}

type fakeStockCatalogProvider struct {
	entries []foundation.StockCatalogEntry
	err     error
}

func (f fakeStockCatalogProvider) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	return f.entries, f.err
}

func healthcareCatalogEntries() []foundation.StockCatalogEntry {
	meta := foundation.SourceMeta{Source: "eastmoney:stock-selection", FetchedAt: time.Now()}
	return []foundation.StockCatalogEntry{
		{
			BoardStock: foundation.BoardStock{Symbol: "600664.SH", Name: "哈药股份", Price: 5.33, ChangePercent: 7.89, Amount: 4_500_000_000, Meta: meta},
			Industry:   "化学制药",
			Concepts:   []string{"创新药", "中药概念"},
		},
		{
			BoardStock: foundation.BoardStock{Symbol: "301520.SZ", Name: "万邦医药", Price: 66.62, ChangePercent: -0.05, Amount: 1_100_000_000, Meta: meta},
			Industry:   "生物医药",
			Concepts:   []string{"CRO", "创新医疗服务", "创新药"},
		},
	}
}

type fakeHealthcareQuoteProvider struct{}

func (fakeHealthcareQuoteProvider) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	quotes := make([]foundation.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		switch symbol {
		case "600664.SH":
			quotes = append(quotes, foundation.Quote{Symbol: symbol, Name: "哈药股份", Price: 5.33, ChangePercent: 7.89, Meta: foundation.SourceMeta{Source: "test"}})
		case "301520.SZ":
			quotes = append(quotes, foundation.Quote{Symbol: symbol, Name: "万邦医药", Price: 66.62, ChangePercent: -0.05, Meta: foundation.SourceMeta{Source: "test"}})
		default:
			quotes = append(quotes, foundation.Quote{Symbol: symbol, Name: symbol, Price: 10, Meta: foundation.SourceMeta{Source: "test"}})
		}
	}
	return quotes, nil
}

type fakeLimitUpProvider struct {
	events []foundation.LimitUpEvent
}

func (f fakeLimitUpProvider) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	return f.events, nil
}

func findNode(m foundation.SectorMap, id string) *foundation.SectorMapNode {
	for _, group := range m.Groups {
		for i := range group.Nodes {
			if group.Nodes[i].ID == id {
				return &group.Nodes[i]
			}
		}
	}
	return nil
}

func findStock(node *foundation.SectorMapNode, symbol string) *foundation.BoardStock {
	if node == nil {
		return nil
	}
	for i := range node.Stocks {
		if node.Stocks[i].Symbol == symbol {
			return &node.Stocks[i]
		}
	}
	return nil
}

func findThemeOverview(items []foundation.ThemeOverview, id string) *foundation.ThemeOverview {
	for i := range items {
		if items[i].Theme == id {
			return &items[i]
		}
	}
	return nil
}

func TestThemeDefinitionsHaveStableIDs(t *testing.T) {
	if len(Themes()) < 16 {
		t.Fatalf("expected expanded theme catalog, got %d themes", len(Themes()))
	}
	for _, theme := range Themes() {
		if theme.ID == "" || theme.Name == "" {
			t.Fatalf("theme missing identity: %+v", theme)
		}
		for _, group := range theme.Groups {
			if group.ID == "" || group.Name == "" {
				t.Fatalf("group missing identity: %+v", group)
			}
			for _, node := range group.Nodes {
				if node.ID == "" || node.Name == "" {
					t.Fatalf("node missing identity: %+v", node)
				}
				if len(node.BoardKeywords) == 0 && node.BoardCode == "" {
					t.Fatalf("node has no board matching rule: %+v", node)
				}
			}
		}
	}
}

func TestMapperBuildsDynamicTradingNarrativeFromConcepts(t *testing.T) {
	tradeDate := time.Date(2026, 8, 5, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	mapper := NewMapper(
		fakeBoardProvider{},
		WithStockCatalogProvider(fakeStockCatalogProvider{entries: []foundation.StockCatalogEntry{{
			BoardStock: foundation.BoardStock{Symbol: "603629.SH", Name: "利通电子", ChangePercent: 8.76},
			Industry:   "电子元件",
			Concepts:   []string{"国产芯片", "云计算", "英伟达概念", "算力概念", "人工智能"},
		}}}),
		WithLimitUpProvider(fakeLimitUpProvider{events: []foundation.LimitUpEvent{{
			Symbol: "603629.SH", Name: "利通电子", Date: tradeDate, Streak: 2, Industry: "消费电子",
		}}}),
	)

	got, err := mapper.Build(context.Background(), narrative.ThemeID("算力租赁"))
	if err != nil {
		t.Fatalf("Build trend narrative failed: %v", err)
	}
	if got.Name != "算力租赁" || got.Theme != narrative.ThemeID("算力租赁") {
		t.Fatalf("unexpected dynamic theme: %+v", got)
	}
	node := findNode(got, "narrative_core")
	stock := findStock(node, "603629.SH")
	if stock == nil || stock.LimitUpStreak != 2 {
		t.Fatalf("利通电子 should be attributed to compute leasing: node=%+v", node)
	}
}

func TestTrendOverviewsUseNarrativesInsteadOfIndustry(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	current := time.Date(2026, 8, 6, 0, 0, 0, 0, location)
	previous := current.AddDate(0, 0, -1)
	events := []foundation.LimitUpEvent{
		{Symbol: "603629.SH", Name: "利通电子", Date: previous, Streak: 2, Industry: "消费电子"},
		{Symbol: "600001.SH", Name: "云算科技", Date: current, Streak: 3, Industry: "软件开发"},
	}
	catalog := []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "603629.SH", Name: "利通电子", ChangePercent: 8.76}, Industry: "电子元件", Concepts: []string{"云计算", "算力概念"}},
		{BoardStock: foundation.BoardStock{Symbol: "600001.SH", Name: "云算科技", ChangePercent: 10}, Industry: "软件开发", Concepts: []string{"数据中心", "算力概念"}},
	}
	items := buildTrendOverviews(events, catalog)
	computeLeasing := findThemeOverview(items, narrative.ThemeID("算力租赁"))
	if computeLeasing == nil || computeLeasing.Name != "算力租赁" || computeLeasing.MaxStreak != 3 {
		t.Fatalf("unexpected trend overview: %+v", items)
	}
	for _, item := range items {
		if item.Name == "消费电子" {
			t.Fatalf("industry must not become a trend narrative: %+v", items)
		}
	}
}

var _ = time.Now
