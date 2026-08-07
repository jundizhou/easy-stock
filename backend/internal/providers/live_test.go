package providers_test

import (
	"context"
	"os"
	"testing"
	"time"

	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
)

func requireLive(t *testing.T) {
	t.Helper()
	if os.Getenv("A_STOCK_LIVE_TEST") != "1" {
		t.Skip("set A_STOCK_LIVE_TEST=1 to run live data-source tests")
	}
}

func TestLiveSinaRealtimeReturnsQuote(t *testing.T) {
	requireLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	quotes, err := sina.NewClient().Realtime(ctx, []string{"000001.SZ"})
	if err != nil {
		t.Fatalf("Sina realtime failed: %v", err)
	}
	if len(quotes) == 0 || quotes[0].Price <= 0 {
		t.Fatalf("Sina realtime returned no usable quote: %+v", quotes)
	}
}

func TestLiveSinaKLineReturnsBars(t *testing.T) {
	requireLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lines, err := sina.NewClient().KLine(ctx, "000001.SZ", "day", 5)
	if err != nil {
		t.Fatalf("Sina kline failed: %v", err)
	}
	if len(lines) == 0 || lines[len(lines)-1].Close <= 0 {
		t.Fatalf("Sina kline returned no usable bars: %+v", lines)
	}
}

func TestLiveEastMoneyKLineReportsHealthSignal(t *testing.T) {
	requireLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lines, err := eastmoney.NewClient().KLine(ctx, "000001.SZ", "day", 2)
	if err != nil {
		t.Logf("EastMoney kline unavailable in this network, fallback source should be used by API: %v", err)
		return
	}
	if len(lines) == 0 || lines[len(lines)-1].Close <= 0 {
		t.Fatalf("EastMoney kline returned no usable bars: %+v", lines)
	}
}

func TestLiveCLSNewsReturnsItems(t *testing.T) {
	requireLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := cls.NewClient().LatestNews(ctx, 5)
	if err != nil {
		t.Fatalf("CLS news failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("CLS news returned no items")
	}
}
