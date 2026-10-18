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

// 板块资金流向用东财的 bkzj 榜单与板块分时资金流，两者都是分路径可达的，
// 这里在真实网络下确认榜单排序、多日字段与分时序列都能取到。
func TestLiveEastMoneySectorFlowRankingAndIntraday(t *testing.T) {
	requireLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := eastmoney.NewClient()
	ranks, meta, err := client.SectorFlowRanking(ctx, "concept", 10)
	if err != nil {
		t.Fatalf("SectorFlowRanking failed: %v", err)
	}
	if len(ranks) < 5 || ranks[0].MainNetInflow == 0 {
		t.Fatalf("unexpected ranking payload: %+v", ranks)
	}
	for index := 1; index < len(ranks); index++ {
		if ranks[index].MainNetInflow > ranks[index-1].MainNetInflow {
			t.Fatalf("ranking is not sorted by main net inflow: %+v", ranks)
		}
	}
	if ranks[0].Name == "" || meta.Source != "eastmoney:bkzj" {
		t.Fatalf("missing ranking evidence: %+v / %+v", ranks[0], meta)
	}

	series, seriesMeta, err := client.SectorFlowIntraday(ctx, []string{ranks[0].Code})
	if err != nil {
		t.Fatalf("SectorFlowIntraday failed: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) < 30 {
		t.Fatalf("unexpected intraday series: %+v", series)
	}
	if series[0].TradeDate == "" || seriesMeta.TradeDate != series[0].TradeDate {
		t.Fatalf("missing trade date evidence: %+v / %+v", series[0], seriesMeta)
	}
	t.Logf("ranking top=%s %.2f亿 5日=%.0f 10日=%.0f; intraday %s points=%d latest=%.2f亿",
		ranks[0].Name,
		float64(ranks[0].MainNetInflow)/1e8,
		float64(ranks[0].FiveDayMainNet),
		float64(ranks[0].TenDayMainNet),
		series[0].TradeDate,
		len(series[0].Points),
		float64(series[0].LatestMain)/1e8,
	)
}
