package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/sector"
	"easy-stock/backend/internal/themeindex"
)

func TestThemeIndexAPIValidatesBeforeFetchingAndHandlesExpiredSnapshot(t *testing.T) {
	calls := 0
	service := themeindex.New(themeindex.Config{Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		calls++
		return foundation.ThemeIndexTarget{}, sector.ErrSnapshotExpired
	}})
	s := NewServer(Config{ThemeIndex: service})
	defer s.Close()
	for _, path := range []string{"/api/v1/themes/index", "/api/v1/themes/index?theme=test&limit=301", "/api/v1/themes/index?theme=test&limit=bad"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("validation: %d %s", w.Code, w.Body.String())
		}
	}
	if calls != 0 {
		t.Fatal("invalid input started expensive fetches")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/themes/index?theme=test&snapshot_id=expired", nil))
	if w.Code != 410 {
		t.Fatalf("expired: %d %s", w.Code, w.Body.String())
	}
}
func TestThemeIndexAPIReturnsSeriesAndRequestedLimit(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	service := themeindex.New(themeindex.Config{Now: func() time.Time { return time.Date(2026, 10, 6, 13, 0, 0, 0, loc) },
		Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
			return foundation.ThemeIndexTarget{Name: "测试题材"}, nil
		},
		Members: func(context.Context, string, string) (foundation.SectorMap, error) {
			return foundation.SectorMap{Groups: []foundation.SectorMapGroup{{Nodes: []foundation.SectorMapNode{{Stocks: []foundation.BoardStock{{Symbol: "600000.SH"}, {Symbol: "000001.SZ"}}}}}}}, nil
		},
		KLines: func(context.Context, string, int) ([]foundation.KLine, error) {
			var lines []foundation.KLine
			for _, day := range []int{28, 29, 30} {
				price := float64(day)
				lines = append(lines, foundation.KLine{Time: time.Date(2026, 9, day, 0, 0, 0, 0, loc), Open: price, Close: price, High: price, Low: price})
			}
			return lines, nil
		}})
	s := NewServer(Config{ThemeIndex: service})
	defer s.Close()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/themes/index?theme=test&limit=1", nil))
	var response struct {
		Data foundation.ThemeIndexSeries `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(response.Data.Lines) != 1 || response.Data.Method != "equal-weight" || response.Data.Meta.TradeDate != "2026-09-30" {
		t.Fatalf("API: %d %s", w.Code, w.Body.String())
	}
}

type failingIndexHistory struct{ calls int }

func (f *failingIndexHistory) AdjustedKLine(context.Context, string, int) ([]foundation.KLine, error) {
	f.calls++
	return nil, errors.New("offline")
}

type fallbackIndexHistory struct{ calls int }

func (f *fallbackIndexHistory) AdjustedKLine(context.Context, string, int) ([]foundation.KLine, error) {
	f.calls++
	return []foundation.KLine{{Close: 10}, {Close: 11}}, nil
}
func TestThemeIndexHistoryCircuitAvoidsRepeatedPrimaryTimeouts(t *testing.T) {
	primary := &failingIndexHistory{}
	fallback := &fallbackIndexHistory{}
	history := &themeIndexHistory{primary: primary, fallback: fallback}
	for range 10 {
		if _, err := history.load(context.Background(), "600000.SH", 301); err != nil {
			t.Fatal(err)
		}
	}
	if primary.calls != 3 || fallback.calls != 10 {
		t.Fatalf("primary failure multiplied across basket: primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}
