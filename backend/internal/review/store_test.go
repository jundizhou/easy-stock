package review

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsAndFiltersReviewPosts(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 4, 9, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	post := Post{
		ID: "post-1", Source: "taoguba", ExternalID: "external-1", AuthorID: "author-1",
		AuthorName: "复盘作者", Title: "盘后复盘", Digest: "机器人方向走强", ContentText: "机器人方向走强，关注分歧承接。",
		OriginalURL: "https://www.tgb.cn/a/1", PublishedAt: now, FetchedAt: now,
		RelatedStocks: []string{"000001.SZ"}, RelatedThemes: []string{"机器人"},
	}
	if _, err := store.UpsertPost(context.Background(), post); err != nil {
		t.Fatalf("UpsertPost failed: %v", err)
	}

	posts, total, err := store.ListPosts(context.Background(), Query{Source: "taoguba", Keyword: "机器人", Limit: 20})
	if err != nil {
		t.Fatalf("ListPosts failed: %v", err)
	}
	if total != 1 || len(posts) != 1 || posts[0].Title != post.Title {
		t.Fatalf("unexpected posts: total=%d posts=%+v", total, posts)
	}
	if len(posts[0].RelatedStocks) != 1 || posts[0].RelatedThemes[0] != "机器人" {
		t.Fatalf("tags were not persisted: %+v", posts[0])
	}

	authors, err := store.ListAuthors(context.Background(), "taoguba")
	if err != nil || len(authors) != 1 || authors[0].PostCount != 1 {
		t.Fatalf("unexpected authors: err=%v authors=%+v", err, authors)
	}
}

func TestStoreDeletePostRemovesOnlyArticle(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 7, 16, 0, 0, 0, time.UTC)
	post := Post{
		ID: "post-delete", Source: "xueqiu", ExternalID: "external-delete", AuthorID: "author-delete",
		AuthorName: "复盘作者", Title: "待删除复盘", ContentText: "本地正文", OriginalURL: "https://xueqiu.com/1/2",
		PublishedAt: now, FetchedAt: now, RelatedStocks: []string{}, RelatedThemes: []string{},
	}
	if _, err := store.UpsertPost(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	summary := DailySummary{TradeDate: "2026-08-07", GeneratedAt: now, ArticleCount: 1, AuthorCount: 1, Sources: []DailySummarySource{{PostID: post.ID, URL: post.OriginalURL}}}
	if _, err := store.SaveDailySummary(context.Background(), summary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDailySummaryJob(context.Background(), DailySummaryJob{TradeDate: summary.TradeDate, Status: "succeeded", Stage: "completed"}); err != nil {
		t.Fatal(err)
	}

	if err := store.DeletePost(context.Background(), post.ID); err != nil {
		t.Fatalf("DeletePost err=%v", err)
	}
	if _, err := store.GetPost(context.Background(), post.ID); err != sql.ErrNoRows {
		t.Fatalf("deleted post err=%v", err)
	}
	if _, err := store.GetDailySummary(context.Background(), summary.TradeDate); err != nil {
		t.Fatalf("cached summary should be retained: %v", err)
	}
	if _, err := store.GetDailySummaryJob(context.Background(), summary.TradeDate); err != nil {
		t.Fatalf("cached summary job should be retained: %v", err)
	}
}

func TestStorePersistsDailySummaryJobWindow(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	start := time.Date(2026, 8, 7, 15, 0, 0, 0, location)
	end := time.Date(2026, 8, 10, 9, 30, 0, 0, location)
	saved, err := store.SaveDailySummaryJob(context.Background(), DailySummaryJob{
		TradeDate:     "2026-08-07",
		WindowStart:   start,
		WindowEnd:     end,
		FreshnessRule: "自定义文章时间窗口",
		Status:        "running",
		Stage:         "preparing",
		Message:       "处理中",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.WindowStart.Equal(start) || !saved.WindowEnd.Equal(end) || saved.FreshnessRule != "自定义文章时间窗口" {
		t.Fatalf("saved job = %+v", saved)
	}
}
