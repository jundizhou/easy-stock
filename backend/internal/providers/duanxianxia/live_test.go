package duanxianxia

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveFetchKaipanlaSnapshot(t *testing.T) {
	if os.Getenv("A_STOCK_LIVE_TEST") != "1" {
		t.Skip("set A_STOCK_LIVE_TEST=1 to run live data-source tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot, err := NewClient(ClientConfig{}).Fetch(ctx, 3)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snapshot.TradeDate == "" || len(snapshot.Themes) < 3 {
		t.Fatalf("unusable snapshot: %+v", snapshot)
	}
	for _, theme := range snapshot.Themes[:3] {
		if theme.Code == "" || theme.Name == "" || (!theme.NoLeaders && len(theme.Leaders) == 0) {
			t.Fatalf("unusable top theme: %+v", theme)
		}
	}
}

func TestLiveFetchKaipanlaLimitUpPool(t *testing.T) {
	if os.Getenv("A_STOCK_LIVE_TEST") != "1" {
		t.Skip("set A_STOCK_LIVE_TEST=1 to run live data-source tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot, err := NewClient(ClientConfig{}).FetchLimitUpPool(ctx)
	if err != nil {
		t.Fatalf("FetchLimitUpPool: %v", err)
	}
	if snapshot.TradeDate == "" || len(snapshot.Events) == 0 {
		t.Fatalf("unusable limit-up pool snapshot: %+v", snapshot)
	}
	for _, event := range snapshot.Events {
		if event.Symbol == "" || event.Name == "" || event.Streak <= 0 || len(event.Concepts) == 0 {
			t.Fatalf("unusable limit-up event: %+v", event)
		}
	}
}
