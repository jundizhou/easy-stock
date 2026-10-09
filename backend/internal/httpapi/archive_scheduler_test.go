package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestInTradingSession(t *testing.T) {
	// 2026-10-09 是周五
	mk := func(h, m int) time.Time {
		return time.Date(2026, 10, 9, h, m, 0, 0, time.Local)
	}
	cases := []struct {
		t    time.Time
		want bool
	}{
		{mk(9, 0), false},  // 开盘前
		{mk(9, 30), true},  // 早盘
		{mk(11, 30), true}, // 午盘收前
		{mk(12, 0), false}, // 午休
		{mk(13, 30), true}, // 午盘
		{mk(15, 0), true},  // 收盘
		{mk(16, 0), false}, // 收盘后
	}
	for _, c := range cases {
		if got := inTradingSession(c.t); got != c.want {
			t.Errorf("inTradingSession(%s) = %t, want %t", c.t.Format("15:04"), got, c.want)
		}
	}
	// 周六
	sat := time.Date(2026, 10, 10, 10, 0, 0, 0, time.Local)
	if inTradingSession(sat) {
		t.Errorf("saturday should not be a trading session")
	}
}

func TestParseArchiveSymbols(t *testing.T) {
	if got := ParseArchiveSymbols(""); len(got) != len(DefaultArchiveSymbols) {
		t.Errorf("empty should fall back to defaults, got %d", len(got))
	}
	got := ParseArchiveSymbols("600519, 000001.SZ, sh600036")
	want := []string{"600519.SH", "000001.SZ", "600036.SH"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, got[i], want[i])
		}
	}
}

type stubRealtime struct {
	quotes []foundation.Quote
}

func (s stubRealtime) Realtime(_ context.Context, _ []string) ([]foundation.Quote, error) {
	return s.quotes, nil
}

type stubNews struct {
	items []foundation.NewsItem
}

func (s stubNews) LatestNews(_ context.Context, _ int) ([]foundation.NewsItem, error) {
	return s.items, nil
}

func TestArchiveCollectOnceWritesFiles(t *testing.T) {
	root := t.TempDir()
	srv := NewServer(Config{
		ReviewDBPath:    ":memory:",
		PortfolioDBPath: ":memory:",
		SettingsPath:    "",
		ArchiveDir:      root,
		Realtime: stubRealtime{quotes: []foundation.Quote{
			{Symbol: "600519.SH", Price: 1270, Meta: foundation.SourceMeta{Source: "sina"}},
		}},
		News: stubNews{items: []foundation.NewsItem{
			{ID: "n1", Title: "测试资讯", Meta: foundation.SourceMeta{Source: "cls"}},
		}},
		ArchiveCollector: ArchiveCollectorConfig{
			Interval:    time.Minute,
			Symbols:     []string{"600519.SH"},
			NewsLimit:   5,
			TradingOnly: false, // 强制采集，忽略时段
		},
	})
	if srv.realtimeArchiver == nil || srv.newsArchiver == nil {
		t.Fatalf("archivers should be initialized")
	}
	srv.collectOnce(context.Background(), srv.archiveCollector)
	st := srv.realtimeArchiver.Report()
	if st.Lines < 1 {
		t.Errorf("expected realtime written, got %+v", st)
	}
	if srv.newsArchiver.Report().Lines < 1 {
		t.Errorf("expected news written")
	}
}

func TestArchiveCollectNowEndpoint(t *testing.T) {
	root := t.TempDir()
	srv := NewServer(Config{
		ReviewDBPath:    ":memory:",
		PortfolioDBPath: ":memory:",
		SettingsPath:    "",
		ArchiveDir:      root,
		Realtime: stubRealtime{quotes: []foundation.Quote{
			{Symbol: "000001.SZ", Price: 11.6, Meta: foundation.SourceMeta{Source: "sina"}},
		}},
		News:             stubNews{items: []foundation.NewsItem{{ID: "n2", Title: "资讯2"}}},
		ArchiveCollector: ArchiveCollectorConfig{Symbols: []string{"000001.SZ"}, NewsLimit: 5, TradingOnly: true},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/archive/collect", nil)
	rec := httptest.NewRecorder()
	srv.archiveCollectNow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"ok\":true") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}
