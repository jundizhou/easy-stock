package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type remoteRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn remoteRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestRemoteDailySyncTracksEachAuthorIndependentlyAndFindsLateArticles(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 8, 11, 16, 20, 0, 0, location)
	var mu sync.Mutex
	authors := []RemoteDailyAuthor{
		{ID: "wechat-author-a", Name: "作者甲", Platform: "wechat", Enabled: true},
		{ID: "taoguba-author-b", Name: "作者乙", Platform: "taoguba", Enabled: true},
	}
	available := map[string]bool{"wechat-author-a": true}
	requests := map[string]int{}
	client := &http.Client{Transport: remoteRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests[r.URL.Path]++
		currentAuthors := append([]RemoteDailyAuthor(nil), authors...)
		isAvailable := available[pathAuthorID(r.URL.Path)]
		mu.Unlock()
		if r.URL.Path == "/authors.json" {
			return remoteJSONResponse(r, http.StatusOK, RemoteDailyAuthorsManifest{SchemaVersion: 1, UpdatedAt: now, Authors: currentAuthors}), nil
		}
		if !isAvailable {
			return remoteTextResponse(r, http.StatusNotFound, "not found"), nil
		}
		authorID := pathAuthorID(r.URL.Path)
		var author RemoteDailyAuthor
		for _, item := range currentAuthors {
			if item.ID == authorID {
				author = item
			}
		}
		return remoteJSONResponse(r, http.StatusOK, testRemoteArticle(now, author)), nil
	})}
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{BaseURL: "https://remote.test", Client: client, Now: func() time.Time { return now }})

	status, err := syncer.SyncToday(context.Background())
	if err != nil || status.Status != "partial" || status.SyncedAuthors != 1 || status.PendingAuthors != 1 {
		t.Fatalf("first status=%+v err=%v", status, err)
	}
	mu.Lock()
	available["taoguba-author-b"] = true
	mu.Unlock()
	status, err = syncer.SyncToday(context.Background())
	if err != nil || status.Status != "synced" || status.LocalAuthors != 1 || status.SyncedAuthors != 1 {
		t.Fatalf("second status=%+v err=%v", status, err)
	}

	mu.Lock()
	authors = append(authors, RemoteDailyAuthor{ID: "xueqiu-author-c", Name: "作者丙", Platform: "xueqiu", Enabled: true})
	available["xueqiu-author-c"] = true
	mu.Unlock()
	status, err = syncer.SyncToday(context.Background())
	if err != nil || status.Status != "synced" || status.LocalAuthors != 2 || status.SyncedAuthors != 1 || status.TotalAuthors != 3 {
		t.Fatalf("late author status=%+v err=%v", status, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/wechat-author-a/2026-08-11.json"] != 1 || requests["/taoguba-author-b/2026-08-11.json"] != 2 || requests["/xueqiu-author-c/2026-08-11.json"] != 1 {
		t.Fatalf("article requests=%v", requests)
	}
}

func TestRemoteDailySyncTreatsMissingAuthorManifestAsPending(t *testing.T) {
	now := time.Date(2026, 8, 11, 15, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	client := &http.Client{Transport: remoteRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return remoteTextResponse(request, http.StatusNotFound, "not found"), nil
	})}
	store, _ := OpenStore(":memory:")
	defer store.Close()
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{BaseURL: "https://remote.test", Client: client, Now: func() time.Time { return now }})
	status, err := syncer.SyncToday(context.Background())
	if err != nil || status.Status != "not_found" || status.TotalAuthors != 0 || status.NextAttemptAt.Sub(now) != 30*time.Minute {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestRemoteDailySyncDoesNotRestoreDeletedAuthor(t *testing.T) {
	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	author := RemoteDailyAuthor{ID: "deleted-author", Name: "已删除作者", Platform: "wechat", Enabled: true}
	articleRequests := 0
	client := &http.Client{Transport: remoteRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/authors.json" {
			return remoteJSONResponse(request, http.StatusOK, RemoteDailyAuthorsManifest{SchemaVersion: 1, Authors: []RemoteDailyAuthor{author}}), nil
		}
		articleRequests++
		return remoteJSONResponse(request, http.StatusOK, testRemoteArticle(now, author)), nil
	})}
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	article := testRemoteArticle(now, author)
	if _, err := store.UpsertPost(context.Background(), Post{
		ID: article.ID, Source: remoteDailySource, ExternalID: article.ExternalID, AuthorID: article.AuthorID, AuthorName: article.AuthorName,
		Title: article.Title, ContentText: article.ContentText, OriginalURL: article.SourceURL, PublishedAt: article.PublishedAt, FetchedAt: now,
		RelatedStocks: []string{}, RelatedThemes: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAuthor(context.Background(), remoteDailySource, author.ID); err != nil {
		t.Fatal(err)
	}

	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{BaseURL: "https://remote.test", Client: client, Now: func() time.Time { return now }})
	status, err := syncer.SyncToday(context.Background())
	if err != nil || status.Status != "synced" || status.TotalAuthors != 0 || articleRequests != 0 {
		t.Fatalf("status=%+v articleRequests=%d err=%v", status, articleRequests, err)
	}
	posts, total, err := store.ListPosts(context.Background(), Query{Limit: 20})
	if err != nil || total != 0 || len(posts) != 0 {
		t.Fatalf("deleted author was restored: total=%d posts=%+v err=%v", total, posts, err)
	}
}

func TestRemoteDailySyncRejectsInvalidAuthorArticleChecksum(t *testing.T) {
	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	author := RemoteDailyAuthor{ID: "wechat-author", Name: "作者", Platform: "wechat", Enabled: true}
	article := testRemoteArticle(now, author)
	article.ContentSHA256 = "bad"
	client := &http.Client{Transport: remoteRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/authors.json" {
			return remoteJSONResponse(r, http.StatusOK, RemoteDailyAuthorsManifest{SchemaVersion: 1, Authors: []RemoteDailyAuthor{author}}), nil
		}
		return remoteJSONResponse(r, http.StatusOK, article), nil
	})}
	store, _ := OpenStore(":memory:")
	defer store.Close()
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{BaseURL: "https://remote.test", Client: client, Now: func() time.Time { return now }})
	status, err := syncer.SyncToday(context.Background())
	if err != nil || status.Status != "not_found" || status.PendingAuthors != 1 || status.Authors[0].Status != "error" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestRemoteDailySyncOnlyPollsAfterClose(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	store, _ := OpenStore(":memory:")
	defer store.Close()
	syncer := NewRemoteDailySync(store, RemoteDailySyncConfig{Interval: 30 * time.Minute})
	syncer.setStatus(RemoteDailySyncStatus{TradeDate: "2026-08-11", Status: "synced", LastAttemptAt: time.Date(2026, 8, 11, 14, 0, 0, 0, location)})
	if syncer.shouldPoll(time.Date(2026, 8, 11, 14, 59, 0, 0, location)) {
		t.Fatal("should not poll before close")
	}
	if !syncer.shouldPoll(time.Date(2026, 8, 11, 15, 0, 0, 0, location)) {
		t.Fatal("should refresh author manifest after close even when current authors are local")
	}
}

func testRemoteArticle(now time.Time, author RemoteDailyAuthor) RemoteDailyArticle {
	content := author.Name + "的今日复盘正文。"
	digest := sha256.Sum256([]byte(content))
	return RemoteDailyArticle{
		SchemaVersion: 1, TradeDate: "2026-08-11", ID: "official-20260811-" + author.ID,
		ExternalID: remoteDailyExternalID("2026-08-11", author.ID), AuthorID: author.ID,
		AuthorName: author.Name, Platform: author.Platform, Title: author.Name + "复盘",
		ContentText: content, ContentSHA256: hex.EncodeToString(digest[:]), PublishedAt: now,
		RelatedStocks: []string{}, RelatedThemes: []string{},
	}
}

func pathAuthorID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func remoteJSONResponse(request *http.Request, status int, value any) *http.Response {
	content, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(content))), Request: request}
}

func remoteTextResponse(request *http.Request, status int, value string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(value)), Request: request}
}
