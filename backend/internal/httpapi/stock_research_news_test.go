package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

type stockNewsSearchFixture struct {
	query string
	items []foundation.NewsItem
	err   error
}

func (p *stockNewsSearchFixture) SearchStockNews(_ context.Context, symbol, query string, limit int) ([]foundation.NewsItem, error) {
	p.query = query
	return p.items, p.err
}

func TestStockResearchNewsFiltersIdentityDateAndRanksCompanyStories(t *testing.T) {
	cutoff := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	items := []foundation.NewsItem{
		{Title: "全市场排行榜", Content: "688099", PublishedAt: cutoff.Add(-time.Hour)},
		{Title: "晶晨股份端侧AI新品", Content: "研发进展", URL: "https://example.com/ai", PublishedAt: cutoff.Add(-24 * time.Hour)},
		{Title: "晶晨股份端侧AI新品", Content: "重复", PublishedAt: cutoff.Add(-25 * time.Hour)},
		{Title: "其他公司产品", Content: "无关", PublishedAt: cutoff.Add(-time.Hour)},
		{Title: "晶晨股份未来消息", PublishedAt: cutoff.Add(time.Hour)},
		{Title: "晶晨股份旧消息", PublishedAt: cutoff.AddDate(0, 0, -61)},
		{Title: "晶晨股份未注明日期的报道"},
	}
	selected := selectStockResearchNews(items, "688099.SH", "晶晨股份", cutoff, 10)
	if len(selected) != 3 || selected[0].Title != "晶晨股份端侧AI新品" {
		t.Fatalf("unexpected stories: %+v", selected)
	}
	provider := &stockNewsSearchFixture{items: items}
	s := &Server{stockNewsSearch: provider}
	sources, err := s.supplementStockResearch(context.Background(), stockanalysis.ResearchSnapshot{Symbol: "688099.SH", Name: "晶晨股份", CutoffAt: cutoff}, stockanalysis.ResearchQuestion{Tool: "news", Query: "端侧AI 产品"})
	if err != nil || len(sources) != 3 || provider.query != "晶晨股份 端侧AI 产品" || sources[0].ContentStatus != "excerpt" {
		t.Fatalf("supplement: query=%s sources=%+v err=%v", provider.query, sources, err)
	}
	for _, source := range sources {
		if source.Kind != "news" {
			t.Fatal("news upgraded to company fact")
		}
	}
}

func TestStockNewsSupplementPreservesEventTermsAndDates(t *testing.T) {
	provider := &stockNewsSearchFixture{}
	s := &Server{stockNewsSearch: provider}
	for _, tc := range []struct{ query, want string }{
		{"晶晨股份 9月29日 9月30日 上涨 消息", "晶晨股份 9月29日 9月30日 上涨 消息"},
		{"688099.SH 晶晨股份 端侧AI 量产 量产", "晶晨股份 端侧AI 量产"},
		{"晶晨股份，新品、客户导入", "晶晨股份 新品 客户导入"},
	} {
		_, err := s.collectStockResearchNews(context.Background(), "688099.SH", "晶晨股份", tc.query, time.Now())
		if err != nil || provider.query != tc.want {
			t.Fatalf("query %q: got %q, want %q, err=%v", tc.query, provider.query, tc.want, err)
		}
	}
}

func TestStockNewsFailureKeepsQuantitativeSnapshotAndRecordsGap(t *testing.T) {
	provider := &stockNewsSearchFixture{err: errors.New("search offline")}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, LimitUp: stockAnalysisLimitUps{}, StockBusiness: stockAnalysisBusiness{}, ThemeOverview: stockAnalysisThemes{}, MarketOverview: &fakeMarketOverviewProvider{}, News: stockAnalysisNews{}, StockNews: provider, ReviewDBPath: ":memory:"})
	analysis, snapshot, err := server.collectStockResearch(context.Background(), "600519.SH")
	if err != nil || snapshot == nil || analysis.Scorecard.Overall == 0 {
		t.Fatalf("lost snapshot: %v", err)
	}
	if !strings.Contains(strings.Join(snapshot.Limitations, " "), "个股定向新闻检索不可用") {
		t.Fatalf("missing failure reason: %+v", snapshot.Limitations)
	}
}
