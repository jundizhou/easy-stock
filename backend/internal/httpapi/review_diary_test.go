package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/review"
)

type fakeReviewImporter struct{}

type fakeReviewSummaryPrompter struct{}

func (fakeReviewSummaryPrompter) Prompt(context.Context, string) (hermes.PromptResult, error) {
	return hermes.PromptResult{Content: `{}`}, nil
}

func (fakeReviewImporter) ImportURL(_ context.Context, rawURL string) (review.Post, error) {
	now := time.Date(2026, 8, 4, 16, 0, 0, 0, time.UTC)
	return review.Post{
		ID: "review-1", Source: "taoguba", ExternalID: "review-1", AuthorID: "author-1",
		AuthorName: "复盘作者", Title: "今日复盘", Digest: "短线情绪修复", ContentText: "短线情绪修复。",
		OriginalURL: rawURL, PublishedAt: now, FetchedAt: now, RelatedStocks: []string{}, RelatedThemes: []string{},
	}, nil
}

func TestReviewDiaryImportAndList(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()
	server := NewServer(Config{ReviewStore: store, ReviewImporter: fakeReviewImporter{}})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/import", strings.NewReader(`{"url":"https://www.tgb.cn/a/1"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/posts?source=taoguba&q=%E6%83%85%E7%BB%AA", nil)
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var payload struct {
		Data  []review.Post `json:"data"`
		Total int           `json:"total"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if payload.Total != 1 || len(payload.Data) != 1 || payload.Data[0].Title != "今日复盘" {
		t.Fatalf("unexpected payload: %+v", payload)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/reviews/posts/review-1", nil)
	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	emptyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/posts", nil)
	emptyRecorder := httptest.NewRecorder()
	server.ServeHTTP(emptyRecorder, emptyRequest)
	if emptyRecorder.Code != http.StatusOK || !strings.Contains(emptyRecorder.Body.String(), `"total":0`) {
		t.Fatalf("empty list status=%d body=%s", emptyRecorder.Code, emptyRecorder.Body.String())
	}
}

func TestReviewDiaryDeleteAuthorAndAllPosts(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	for index := 1; index <= 2; index++ {
		if _, err := store.UpsertPost(context.Background(), review.Post{
			ID: fmt.Sprintf("author-post-%d", index), Source: "official", ExternalID: fmt.Sprintf("external-%d", index), AuthorID: "author-delete",
			AuthorName: "待删除作者", Title: fmt.Sprintf("复盘%d", index), ContentText: "正文", OriginalURL: fmt.Sprintf("https://example.com/%d", index),
			PublishedAt: now, FetchedAt: now, RelatedStocks: []string{}, RelatedThemes: []string{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	server := NewServer(Config{ReviewStore: store})
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/reviews/authors/author-delete?source=official", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"posts_deleted":2`) {
		t.Fatalf("delete author status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	posts, total, err := store.ListPosts(context.Background(), review.Query{Limit: 20})
	if err != nil || total != 0 || len(posts) != 0 {
		t.Fatalf("posts after author deletion total=%d posts=%+v err=%v", total, posts, err)
	}

	missingRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/reviews/authors/author-delete?source=official", nil)
	missingRecorder := httptest.NewRecorder()
	server.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("missing author status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
}

func TestReviewSourcesPutWechatLastAndMarkAutomaticSyncUnavailable(t *testing.T) {
	server := NewServer(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/sources", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("sources status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []review.SourceStatus `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 4 || payload.Data[0].ID != "official" || payload.Data[1].ID != "xueqiu" || payload.Data[2].ID != "taoguba" || payload.Data[3].ID != "wechat" {
		t.Fatalf("source order = %+v", payload.Data)
	}
	wechat := payload.Data[3]
	if wechat.Status != "limited" || !wechat.ImportReady || wechat.SyncReady || !strings.Contains(wechat.Message, "历史文章列表接口") {
		t.Fatalf("wechat status = %+v", wechat)
	}
}

func TestReviewDailySummaryWindowEndpoints(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	automation := review.NewAutomation(store, fakeReviewImporter{}, nil, http.DefaultClient, "", fakeReviewSummaryPrompter{})
	server := NewServer(Config{ReviewStore: store, ReviewImporter: fakeReviewImporter{}, ReviewAutomation: automation})

	windowRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary/window", nil)
	windowRecorder := httptest.NewRecorder()
	server.ServeHTTP(windowRecorder, windowRequest)
	if windowRecorder.Code != http.StatusOK {
		t.Fatalf("window status=%d body=%s", windowRecorder.Code, windowRecorder.Body.String())
	}
	var windowPayload struct {
		Data review.DailySummaryWindow `json:"data"`
	}
	if err := json.NewDecoder(windowRecorder.Body).Decode(&windowPayload); err != nil {
		t.Fatal(err)
	}
	if windowPayload.Data.WindowStart.IsZero() || windowPayload.Data.WindowEnd.IsZero() || !windowPayload.Data.WindowStart.Before(windowPayload.Data.WindowEnd) {
		t.Fatalf("window = %+v", windowPayload.Data)
	}

	start := "2026-08-07T15:00:00+08:00"
	end := "2026-08-10T09:30:00+08:00"
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/daily-summary", strings.NewReader(`{"force":true,"window_start":"`+start+`","window_end":"`+end+`"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	server.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var createPayload struct {
		Data review.DailySummaryJob `json:"data"`
	}
	if err := json.NewDecoder(createRecorder.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Data.Status != "running" || createPayload.Data.WindowStart.IsZero() || createPayload.Data.WindowEnd.IsZero() {
		t.Fatalf("job = %+v", createPayload.Data)
	}

	query := url.Values{"window_start": {start}, "window_end": {end}}.Encode()
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary/status?"+query, nil)
	statusRecorder := httptest.NewRecorder()
	server.ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK || !strings.Contains(statusRecorder.Body.String(), `"window_start"`) {
		t.Fatalf("status=%d body=%s", statusRecorder.Code, statusRecorder.Body.String())
	}

	summaryRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary?"+query, nil)
	summaryRecorder := httptest.NewRecorder()
	server.ServeHTTP(summaryRecorder, summaryRequest)
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", summaryRecorder.Code, summaryRecorder.Body.String())
	}

	badRequest := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/daily-summary/status?window_start="+url.QueryEscape(start), nil)
	badRecorder := httptest.NewRecorder()
	server.ServeHTTP(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d body=%s", badRecorder.Code, badRecorder.Body.String())
	}

	parsedStart, _ := time.Parse(time.RFC3339, start)
	parsedEnd, _ := time.Parse(time.RFC3339, end)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, getErr := automation.GetSummaryJobForWindow(context.Background(), parsedStart, parsedEnd)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if job.Status != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("summary background job did not stop")
}
