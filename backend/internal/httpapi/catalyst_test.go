package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/catalyst"
	"easy-stock/backend/internal/foundation"
)

// fakeNewsProvider 返回固定电报流，用来驱动催化筛选端点。
type fakeNewsProvider struct {
	items []foundation.NewsItem
	err   error
}

func (f fakeNewsProvider) LatestNews(context.Context, int) ([]foundation.NewsItem, error) {
	return f.items, f.err
}

// fakeCatalystSelector 模拟模型精筛，记录收到的候选以便断言。
type fakeCatalystSelector struct {
	items    []catalyst.Item
	ok       bool
	err      error
	received []catalyst.Candidate
}

func (f *fakeCatalystSelector) Select(_ context.Context, candidates []catalyst.Candidate) ([]catalyst.Item, bool, error) {
	f.received = candidates
	return f.items, f.ok, f.err
}

func catalystNewsItem(id, title, content string) foundation.NewsItem {
	return foundation.NewsItem{
		ID:          id,
		Title:       title,
		Content:     content,
		PublishedAt: time.Date(2026, 9, 10, 10, 30, 0, 0, time.Local),
		Meta:        foundation.SourceMeta{Source: "cls", SourceURL: "https://www.cls.cn/"},
	}
}

func decodeCatalystResponse(t *testing.T, rec *httptest.ResponseRecorder) ([]catalyst.Item, catalyst.Meta) {
	t.Helper()
	var payload struct {
		Data []catalyst.Item `json:"data"`
		Meta catalyst.Meta   `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return payload.Data, payload.Meta
}

func TestMarketCatalystsReturnsSelectedItems(t *testing.T) {
	item := catalystNewsItem("1", "工信部批准首款国产车规级芯片量产", "已通过审批并开始投产")
	selector := &fakeCatalystSelector{
		ok: true,
		items: []catalyst.Item{{
			NewsItem: item,
			Impact:   "bullish",
			Strength: 85,
			Sectors:  []string{"半导体"},
			Why:      "国产替代进入量产阶段",
			Horizon:  "short",
		}},
	}
	server := NewServer(Config{News: fakeNewsProvider{items: []foundation.NewsItem{item}}})
	server.catalystSelector = selector

	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/catalysts", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	items, meta := decodeCatalystResponse(t, rec)
	if len(items) != 1 || items[0].Title != item.Title {
		t.Fatalf("items = %+v", items)
	}
	if !meta.ModelUsed {
		t.Fatal("应标记为已模型精筛")
	}
	if meta.Scanned != 1 || meta.Candidates != 1 {
		t.Fatalf("meta 计数不对：%+v", meta)
	}
	if meta.UpdatedAt == "" {
		t.Fatal("应带 updated_at")
	}
}

// 这是本功能的核心诉求：没有合格消息时必须返回空数组，而不是把噪音凑满。
func TestMarketCatalystsReturnsEmptyWhenModelFindsNothing(t *testing.T) {
	items := []foundation.NewsItem{
		catalystNewsItem("1", "工信部批准首款国产车规级芯片量产", "已通过审批并开始投产"),
		catalystNewsItem("2", "国家能源局印发新型储能建设方案", ""),
	}
	selector := &fakeCatalystSelector{ok: true, items: []catalyst.Item{}}
	server := NewServer(Config{News: fakeNewsProvider{items: items}})
	server.catalystSelector = selector

	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/catalysts", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeCatalystResponse
	got, meta := payload(t, rec)
	if len(got) != 0 {
		t.Fatalf("模型判定无催化时应返回空数组，实际 %+v", got)
	}
	if !meta.ModelUsed {
		t.Fatal("空结果同样应标记为已模型精筛（区别于降级）")
	}
	if meta.Candidates != 2 {
		t.Fatalf("candidates = %d, want 2", meta.Candidates)
	}
}

func TestMarketCatalystsFallsBackToRulesWithoutModel(t *testing.T) {
	items := []foundation.NewsItem{
		catalystNewsItem("1", "三大指数集体高开，沪指涨0.3%", "截至发稿，板块涨幅居前"),
		catalystNewsItem("2", "工信部批准首款国产车规级芯片量产", "已通过审批并开始投产"),
	}
	// 不设置 catalystSelector，模拟未配置模型。
	server := NewServer(Config{News: fakeNewsProvider{items: items}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/catalysts", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	got, meta := decodeCatalystResponse(t, rec)
	if meta.ModelUsed {
		t.Fatal("无模型时不应标记为已精筛")
	}
	if meta.Note == "" {
		t.Fatal("降级时应给出说明，便于界面解释")
	}
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("降级路径应只留规则高分项，实际 %+v", got)
	}
	if meta.Filtered != 1 {
		t.Fatalf("filtered = %d, want 1（播报噪音被淘汰）", meta.Filtered)
	}
}

func TestMarketCatalystsReportsSourceFailure(t *testing.T) {
	server := NewServer(Config{News: fakeNewsProvider{err: context.DeadlineExceeded}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/catalysts", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestMarketCatalystsHandlesEmptyNewsFeed(t *testing.T) {
	server := NewServer(Config{News: fakeNewsProvider{items: nil}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/market/catalysts", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	got, meta := decodeCatalystResponse(t, rec)
	if len(got) != 0 {
		t.Fatalf("空电报流应返回空数组，实际 %+v", got)
	}
	if meta.Scanned != 0 {
		t.Fatalf("scanned = %d, want 0", meta.Scanned)
	}
}
