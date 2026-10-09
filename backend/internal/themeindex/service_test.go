package themeindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeBoards struct {
	calls atomic.Int32
	names []foundation.Board
	lines []foundation.KLine
	err   error
}

func (f *fakeBoards) ThemeIndexBoards(context.Context) ([]foundation.Board, error) {
	return f.names, nil
}
func (f *fakeBoards) BoardKLine(context.Context, string, int) ([]foundation.KLine, error) {
	f.calls.Add(1)
	return f.lines, f.err
}
func memberMap(count int) foundation.SectorMap {
	stocks := make([]foundation.BoardStock, count)
	for i := range stocks {
		stocks[i] = foundation.BoardStock{Symbol: fmt.Sprintf("%06d.SZ", i+1)}
	}
	return foundation.SectorMap{Groups: []foundation.SectorMapGroup{{Nodes: []foundation.SectorMapNode{{Stocks: stocks}}}}}
}
func TestNativeIndexAvoidsAllConstituentAndStockRequests(t *testing.T) {
	boards := &fakeBoards{names: []foundation.Board{{Code: "BK0475", Name: "银行"}}, lines: []foundation.KLine{testBar("2026-09-29", 1000), testBar("2026-09-30", 1010)}}
	service := New(Config{Now: func() time.Time { return testNow }, Boards: boards,
		Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
			return foundation.ThemeIndexTarget{Name: "银行", Names: []string{"银行"}}, nil
		},
		Members: func(context.Context, string, string) (foundation.SectorMap, error) {
			t.Fatal("native path loaded constituents")
			return foundation.SectorMap{}, nil
		},
		KLines: func(context.Context, string, int) ([]foundation.KLine, error) {
			t.Fatal("native path loaded stock bars")
			return nil, nil
		},
	})
	for range 2 {
		result, err := service.Series(context.Background(), "bank", "", 240)
		if err != nil || result.Method != "provider-index" || result.Lines[1].Close != 1010 {
			t.Fatalf("native: %+v %v", result, err)
		}
	}
	if boards.calls.Load() != 1 {
		t.Fatal("native series was not cached")
	}
}
func TestMatchBoardRejectsFuzzyAndAmbiguousNames(t *testing.T) {
	for _, boards := range [][]foundation.Board{{{Code: "BK0475", Name: "银行"}}, {{Code: "BK0475", Name: "AI"}, {Code: "BK0476", Name: "AI"}}} {
		if _, ok := MatchBoard(foundation.ThemeIndexTarget{Names: []string{"AI"}}, boards); ok {
			t.Fatalf("unsafe index match: %+v", boards)
		}
	}
}
func TestConcurrentSeriesDeduplicatesAndSharesStockCacheAcrossThemes(t *testing.T) {
	var calls, active, maximum atomic.Int32
	service := New(Config{Now: func() time.Time { return testNow }, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "basket"}, nil
	},
		Members: func(context.Context, string, string) (foundation.SectorMap, error) { return memberMap(20), nil },
		KLines: func(ctx context.Context, _ string, _ int) ([]foundation.KLine, error) {
			calls.Add(1)
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			select {
			case <-time.After(time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []foundation.KLine{testBar("2026-09-28", 10), testBar("2026-09-29", 11), testBar("2026-09-30", 12)}, nil
		}})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.Series(context.Background(), "same", "", 240)
			if err != nil || result.Quality.Loaded != 20 {
				t.Errorf("series: %+v %v", result, err)
			}
		}()
	}
	wg.Wait()
	_, err := service.Series(context.Background(), "other", "", 240)
	if err != nil || calls.Load() != 20 || maximum.Load() > 6 {
		t.Fatalf("cache/concurrency: calls=%d max=%d err=%v", calls.Load(), maximum.Load(), err)
	}
}
func TestSamplingIsIndependentOfLeaderOrderAndExpandsForVolatility(t *testing.T) {
	members := memberMap(300)
	first := MemberSymbols("theme", members)
	stocks := members.Groups[0].Nodes[0].Stocks
	for i, j := 0, len(stocks)-1; i < j; i, j = i+1, j-1 {
		stocks[i], stocks[j] = stocks[j], stocks[i]
	}
	second := MemberSymbols("theme", members)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("sampling follows provider ranking")
		}
	}
	var calls atomic.Int32
	service := New(Config{Now: func() time.Time { return testNow }, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "basket"}, nil
	}, Members: func(context.Context, string, string) (foundation.SectorMap, error) { return members, nil },
		KLines: func(_ context.Context, symbol string, _ int) ([]foundation.KLine, error) {
			calls.Add(1)
			delta := .1
			if symbol[5]%2 == 0 {
				delta = -.1
			}
			return []foundation.KLine{testBar("2026-09-28", 100), testBar("2026-09-29", 100*(1+delta)), testBar("2026-09-30", 100)}, nil
		}})
	result, err := service.Series(context.Background(), "theme", "", 240)
	if err != nil || calls.Load() != maxSample || result.Quality.Sampled != maxSample || result.Quality.SamplingErrorPercent <= samplingTolerancePercent {
		t.Fatalf("adaptive sample: calls=%d result=%+v err=%v", calls.Load(), result.Quality, err)
	}
}
func TestInsufficientHistoryDoesNotManufactureIndex(t *testing.T) {
	service := New(Config{Now: func() time.Time { return testNow }, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "basket"}, nil
	}, Members: func(context.Context, string, string) (foundation.SectorMap, error) { return memberMap(10), nil }, KLines: func(context.Context, string, int) ([]foundation.KLine, error) { return nil, errors.New("unavailable") }})
	if _, err := service.Series(context.Background(), "theme", "", 240); err == nil {
		t.Fatal("missing histories produced a successful index")
	}
}

func TestMissingHistoryIsNotReportedAsSamplingOrTruncation(t *testing.T) {
	service := New(Config{Now: func() time.Time { return testNow }, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "basket"}, nil
	}, Members: func(context.Context, string, string) (foundation.SectorMap, error) { return memberMap(10), nil },
		KLines: func(_ context.Context, symbol string, _ int) ([]foundation.KLine, error) {
			if symbol == "000009.SZ" || symbol == "000010.SZ" {
				return nil, errors.New("unavailable")
			}
			return []foundation.KLine{testBar("2026-09-28", 100), testBar("2026-09-29", 100+float64(symbol[5])), testBar("2026-09-30", 100)}, nil
		}})
	result, err := service.Series(context.Background(), "theme", "", 240)
	if err != nil || result.Quality.Loaded != 8 || result.Quality.Sampled != 10 || result.Quality.HistoryCoverage != .8 || result.Quality.SamplingEstimateAvailable || result.Quality.SamplingErrorPercent != 0 || result.Quality.SkippedDays != 0 {
		t.Fatalf("missing histories: %+v %v", result.Quality, err)
	}
	warnings := strings.Join(result.Warnings, " ")
	if !strings.Contains(warnings, "部分成分历史缺失") || strings.Contains(warnings, "抽样上限") || strings.Contains(warnings, "截断") {
		t.Fatalf("misleading quality warnings: %s", warnings)
	}
}

func TestIncompleteSampleDoesNotStopAtFalseZeroError(t *testing.T) {
	var calls atomic.Int32
	service := New(Config{Now: func() time.Time { return testNow }, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "basket"}, nil
	}, Members: func(context.Context, string, string) (foundation.SectorMap, error) { return memberMap(300), nil },
		KLines: func(context.Context, string, int) ([]foundation.KLine, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("unavailable")
			}
			return []foundation.KLine{testBar("2026-09-28", 100), testBar("2026-09-29", 100), testBar("2026-09-30", 100)}, nil
		}})
	result, err := service.Series(context.Background(), "theme", "", 240)
	if err != nil || calls.Load() != maxSample || result.Quality.SamplingEstimateAvailable || result.Quality.SamplingErrorPercent != 0 {
		t.Fatalf("incomplete sample: calls=%d result=%+v err=%v", calls.Load(), result.Quality, err)
	}
}

func BenchmarkWarmSeries240Bars(b *testing.B) {
	bars := make([]foundation.KLine, 0, 240)
	for day := testNow; len(bars) < 240; day = day.AddDate(0, 0, -1) {
		if foundation.IsAStockTradingDay(day) {
			bars = append(bars, testBar(day.Format("2006-01-02"), 1000))
		}
	}
	service := New(Config{Now: func() time.Time { return testNow }, Boards: &fakeBoards{lines: bars}, Resolve: func(context.Context, string, string) (foundation.ThemeIndexTarget, error) {
		return foundation.ThemeIndexTarget{Name: "bank", BoardCode: "BK0475"}, nil
	}})
	if _, err := service.Series(context.Background(), "bank", "", 240); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := service.Series(context.Background(), "bank", "", 240); err != nil {
			b.Fatal(err)
		}
	}
}
