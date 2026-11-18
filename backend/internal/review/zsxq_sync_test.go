package review

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/appsettings"
)

func newZsxqSyncTest(t *testing.T, run zsxqRunner) (*Automation, *Store, Subscription) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	settings, err := appsettings.Open("")
	if err != nil {
		t.Fatal(err)
	}
	sub, err := store.UpsertSubscription(context.Background(), Subscription{
		Source: ZsxqSource, Name: "复盘星球", HomepageURL: zsxqGroupURL("12345678"),
		ExternalID: "12345678", ConfigID: "zsxq-default", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	collector := NewZsxqCollector("fake-zsxq-cli")
	collector.retryDelays = nil
	collector.run = func(ctx context.Context, path string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "auth status --json" {
			return []byte(`{"ok":true,"data":{"loggedIn":true}}`), nil
		}
		return run(ctx, path, args...)
	}
	automation := NewAutomation(store, nil, settings, nil)
	automation.SetZsxqCollector(collector)
	return automation, store, sub
}

func zsxqSyncTestTopic(id, userID string, at time.Time) zsxqTopic {
	return zsxqTopic{
		TopicID: json.Number(id), Content: "市场复盘内容：关注成交量变化以及主线方向的承接。",
		CreateTime: at.Format(time.RFC3339), Owner: zsxqOwner{UserID: userID, Name: "作者" + userID},
	}
}

func zsxqSyncTestPage(t *testing.T, hasMore bool, cursor string, topics ...zsxqTopic) []byte {
	t.Helper()
	data, err := json.Marshal(zsxqTopicPage{Success: true, HasMore: hasMore, NextEndTime: cursor, TopicsBrief: topics})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func setZsxqSyncTestCheckpoint(t *testing.T, store *Store, id string, at time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE review_subscriptions SET last_sync_at=? WHERE id=?`, at.UTC().Format(time.RFC3339), id); err != nil {
		t.Fatal(err)
	}
}

func TestZsxqSyncRecoversFailedInterval(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	checkpoint := now.Add(-48 * time.Hour)
	offline := true
	page := zsxqSyncTestPage(t, false, "", zsxqSyncTestTopic("1", "111", now.Add(-24*time.Hour)))
	automation, store, sub := newZsxqSyncTest(t, func(context.Context, string, ...string) ([]byte, error) {
		if offline {
			return nil, errors.New("network unavailable")
		}
		return page, nil
	})
	setZsxqSyncTestCheckpoint(t, store, sub.ID, checkpoint)
	if first := automation.SyncOne(ctx, sub.ID); first.Error == "" {
		t.Fatal("expected failed attempt")
	}
	saved, err := store.GetSubscription(ctx, sub.ID)
	if err != nil || saved.LastStatus != "error" || !saved.LastSyncAt.Equal(checkpoint) || saved.NextSyncAt.IsZero() {
		t.Fatalf("failed attempt moved the checkpoint or lost retry state: sub=%+v err=%v", saved, err)
	}
	offline = false
	if result := automation.SyncOne(ctx, sub.ID); result.Error != "" || result.Imported != 1 {
		t.Fatalf("failed interval was not recovered: %+v", result)
	}
	saved, err = store.GetSubscription(ctx, sub.ID)
	if err != nil || saved.LastStatus != "ok" || saved.LastError != "" || !saved.LastSyncAt.After(checkpoint) {
		t.Fatalf("successful attempt did not advance the checkpoint: sub=%+v err=%v", saved, err)
	}
}

func TestZsxqSyncKeepsPartialPostsAndRetriesMissingPages(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	checkpoint := now.Add(-48 * time.Hour)
	firstPage := zsxqSyncTestPage(t, true, "cursor", zsxqSyncTestTopic("1", "111", now.Add(-12*time.Hour)))
	secondPage := zsxqSyncTestPage(t, false, "", zsxqSyncTestTopic("2", "222", now.Add(-24*time.Hour)))
	failSecond := true
	automation, store, sub := newZsxqSyncTest(t, func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "--end-time") {
			if failSecond {
				return nil, errors.New("HTTP 429 Too Many Requests")
			}
			return secondPage, nil
		}
		return firstPage, nil
	})
	setZsxqSyncTestCheckpoint(t, store, sub.ID, checkpoint)
	first := automation.SyncOne(ctx, sub.ID)
	if first.Imported != 1 || !strings.Contains(first.Error, "第 2 页") {
		t.Fatalf("partial sync must retain posts and report failure: %+v", first)
	}
	saved, err := store.GetSubscription(ctx, sub.ID)
	if err != nil || saved.LastStatus != "error" || !saved.LastSyncAt.Equal(checkpoint) {
		t.Fatalf("partial sync advanced checkpoint: sub=%+v err=%v", saved, err)
	}
	failSecond = false
	if result := automation.SyncOne(ctx, sub.ID); result.Error != "" || result.Found != 2 || result.Imported != 1 {
		t.Fatalf("retry did not deduplicate and recover the missing page: %+v", result)
	}
	_, total, err := store.ListPosts(ctx, Query{Source: ZsxqSource, Limit: 20})
	if err != nil || total != 2 {
		t.Fatalf("recovered posts: total=%d err=%v", total, err)
	}
}

func TestZsxqSyncDoesNotRestoreDeletedAuthor(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	posts := []zsxqTopic{zsxqSyncTestTopic("1", "111", now.Add(-time.Hour)), zsxqSyncTestTopic("2", "222", now.Add(-time.Hour))}
	page := zsxqSyncTestPage(t, false, "", posts...)
	automation, store, sub := newZsxqSyncTest(t, func(context.Context, string, ...string) ([]byte, error) { return page, nil })
	if result := automation.SyncOne(ctx, sub.ID); result.Error != "" || result.Imported != 2 {
		t.Fatalf("initial sync: %+v", result)
	}
	if _, err := store.DeleteAuthor(ctx, ZsxqSource, zsxqAuthorID("111", "作者111")); err != nil {
		t.Fatal(err)
	}
	posts = append(posts, zsxqSyncTestTopic("3", "111", now), zsxqSyncTestTopic("4", "222", now))
	page = zsxqSyncTestPage(t, false, "", posts...)
	if result := automation.SyncOne(ctx, sub.ID); result.Error != "" || result.Imported != 1 {
		t.Fatalf("only the remaining author's new post should be imported: %+v", result)
	}
	authors, err := store.ListAuthors(ctx, ZsxqSource)
	if err != nil || len(authors) != 1 || authors[0].ID != zsxqAuthorID("222", "作者222") || authors[0].PostCount != 2 {
		t.Fatalf("deleted author was restored or remaining author was lost: authors=%+v err=%v", authors, err)
	}
	if _, err := store.GetSubscription(ctx, sub.ID); err != nil {
		t.Fatalf("deleting one author must not remove the shared group subscription: %v", err)
	}
}

func TestZsxqSyncDoesNotAdvanceCheckpointOnStorageFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	checkpoint := now.Add(-24 * time.Hour)
	page := zsxqSyncTestPage(t, false, "", zsxqSyncTestTopic("1", "111", now))
	automation, store, sub := newZsxqSyncTest(t, func(context.Context, string, ...string) ([]byte, error) { return page, nil })
	setZsxqSyncTestCheckpoint(t, store, sub.ID, checkpoint)
	if _, err := store.db.Exec(`CREATE TRIGGER fail_review_insert BEFORE INSERT ON review_posts BEGIN SELECT RAISE(FAIL, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	if result := automation.SyncOne(ctx, sub.ID); !strings.Contains(result.Error, "disk full") || result.Imported != 0 {
		t.Fatalf("storage failure was swallowed: %+v", result)
	}
	saved, err := store.GetSubscription(ctx, sub.ID)
	if err != nil || saved.LastStatus != "error" || !saved.LastSyncAt.Equal(checkpoint) {
		t.Fatalf("storage failure advanced checkpoint: sub=%+v err=%v", saved, err)
	}
}
