package marketemotion

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsSnapshotsAndSyncState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market-emotion.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	first := Snapshot{TradeDate: "2026-08-05", EmotionScore: 55, Phase: "混沌/过渡", UpdatedAt: time.Now()}
	second := Snapshot{TradeDate: "2026-08-06", EmotionScore: 68, Phase: "发酵/主升", UpdatedAt: time.Now()}
	if err := store.Upsert(ctx, first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := store.Upsert(ctx, second); err != nil {
		t.Fatalf("save second: %v", err)
	}
	items, err := store.List(ctx, 30)
	if err != nil || len(items) != 2 {
		t.Fatalf("List = %+v, err=%v", items, err)
	}
	if items[0].TradeDate != "2026-08-05" || items[1].EmotionScore != 68 {
		t.Fatalf("unexpected ordering or payload: %+v", items)
	}

	state := SyncState{LastAttemptDate: "2026-08-06", LastSuccessDate: "2026-08-06", UpdatedAt: time.Now()}
	if err := store.SaveSyncState(ctx, state); err != nil {
		t.Fatalf("SaveSyncState: %v", err)
	}
	got, err := store.SyncState(ctx)
	if err != nil || got.LastSuccessDate != "2026-08-06" {
		t.Fatalf("SyncState = %+v, err=%v", got, err)
	}
}
