package review

import (
	"context"
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
