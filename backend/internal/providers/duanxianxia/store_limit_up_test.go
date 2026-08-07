package duanxianxia

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestStoreRetainsNewestLimitUpSnapshotPerTradingDay(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("CST", 8*60*60)
	dayOne := time.Date(2026, 8, 6, 15, 5, 0, 0, location)
	dayTwo := time.Date(2026, 8, 7, 10, 0, 0, 0, location)
	snapshots := []LimitUpPoolSnapshot{
		{ID: "day-one-old", TradeDate: "2026-08-06", FetchedAt: dayOne, Events: []foundation.LimitUpEvent{{Name: "旧快照"}}},
		{ID: "day-one-new", TradeDate: "2026-08-06", FetchedAt: dayOne.Add(time.Minute), Events: []foundation.LimitUpEvent{{Name: "新快照"}}},
		{ID: "day-two", TradeDate: "2026-08-07", FetchedAt: dayTwo, Events: []foundation.LimitUpEvent{{Name: "当日快照"}}},
	}
	for _, snapshot := range snapshots {
		if err := store.SaveLimitUpSuccess(context.Background(), snapshot); err != nil {
			t.Fatalf("SaveLimitUpSuccess: %v", err)
		}
	}

	recent, err := store.RecentLimitUps(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].TradeDate != "2026-08-07" || recent[1].TradeDate != "2026-08-06" {
		t.Fatalf("unexpected recent snapshots: %+v", recent)
	}
	if len(recent[1].Events) != 1 || recent[1].Events[0].Name != "新快照" {
		t.Fatalf("newest same-day snapshot was not retained: %+v", recent[1])
	}
	var rows int
	if err := store.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM duanxianxia_limit_up_snapshots").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("stored rows=%d err=%v, want one row per trading day", rows, err)
	}
}

func TestStoreRetainsNewestThemeSnapshotPerTradingDay(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("CST", 8*60*60)
	dayOne := time.Date(2026, 8, 6, 15, 5, 0, 0, location)
	dayTwo := time.Date(2026, 8, 7, 10, 0, 0, 0, location)
	for _, snapshot := range []Snapshot{
		{ID: "theme-day-one-old", TradeDate: "2026-08-06", FetchedAt: dayOne, Themes: []Theme{{Name: "旧题材"}}},
		{ID: "theme-day-one-new", TradeDate: "2026-08-06", FetchedAt: dayOne.Add(time.Minute), Themes: []Theme{{Name: "AI应用"}}},
		{ID: "theme-day-two", TradeDate: "2026-08-07", FetchedAt: dayTwo, Themes: []Theme{{Name: "算力"}}},
	} {
		if err := store.SaveSuccess(context.Background(), snapshot); err != nil {
			t.Fatalf("SaveSuccess: %v", err)
		}
	}

	recent, err := store.Recent(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].TradeDate != "2026-08-07" || recent[1].Themes[0].Name != "AI应用" {
		t.Fatalf("unexpected recent theme snapshots: %+v", recent)
	}
	var rows int
	if err := store.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM duanxianxia_snapshots").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("stored rows=%d err=%v, want one theme row per trading day", rows, err)
	}
}

type staticRecentLimitUps struct {
	events []foundation.LimitUpEvent
}

func (p staticRecentLimitUps) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	return cloneLimitUpEvents(p.events), nil
}

func TestLimitUpProviderPrefersRetainedKaipanlaPreviousDay(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("CST", 8*60*60)
	previousDate := time.Date(2026, 8, 6, 0, 0, 0, 0, location)
	currentDate := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	kaipanlaMeta := foundation.SourceMeta{Source: "duanxianxia:kaipanla-limit-up"}
	for _, snapshot := range []LimitUpPoolSnapshot{
		{ID: "previous", TradeDate: "2026-08-06", FetchedAt: previousDate.Add(15 * time.Hour), Events: []foundation.LimitUpEvent{{Symbol: "600001.SH", Name: "昨日开盘啦", Date: previousDate, Streak: 2, Concepts: []string{"算力租赁"}, Meta: kaipanlaMeta}}},
		{ID: "current", TradeDate: "2026-08-07", FetchedAt: currentDate.Add(10 * time.Hour), Events: []foundation.LimitUpEvent{{Symbol: "600002.SH", Name: "今日开盘啦", Date: currentDate, Streak: 3, Concepts: []string{"PCB"}, Meta: kaipanlaMeta}}},
	} {
		if err := store.SaveLimitUpSuccess(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	now := currentDate.Add(11 * time.Hour)
	if allowed, _, err := store.TryBegin(context.Background(), now, 5*time.Minute); err != nil || !allowed {
		t.Fatalf("reserve refresh gate: allowed=%v err=%v", allowed, err)
	}
	service := NewService(nil, store, ServiceConfig{Now: func() time.Time { return now }})
	fallback := staticRecentLimitUps{events: []foundation.LimitUpEvent{
		{Symbol: "600001.SH", Name: "昨日东财", Date: previousDate, Streak: 1, Price: 12.3, Industry: "电子", Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
		{Symbol: "600003.SH", Name: "昨日缺股补位", Date: previousDate, Streak: 1, Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
		{Symbol: "600002.SH", Name: "今日东财", Date: currentDate, Streak: 2, Price: 20.5, Meta: foundation.SourceMeta{Source: "eastmoney:limit-up-pool"}},
	}}
	provider := NewLimitUpProvider(service, fallback)
	events, err := provider.RecentLimitUps(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]foundation.LimitUpEvent{}
	for _, event := range events {
		byKey[limitUpEventKey(event)] = event
	}
	previous := byKey["2026-08-06|600001.SH"]
	if previous.Meta.Source != "duanxianxia:kaipanla-limit-up" || previous.Streak != 2 || previous.Price != 12.3 || len(previous.Concepts) != 1 || previous.Concepts[0] != "算力租赁" {
		t.Fatalf("previous Kaipanla event was not authoritative and hydrated: %+v", previous)
	}
	if byKey["2026-08-06|600003.SH"].Meta.Source != "eastmoney:limit-up-pool" {
		t.Fatalf("missing previous stock was not filled by EastMoney: %+v", byKey["2026-08-06|600003.SH"])
	}
}
