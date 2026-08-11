package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/review"
)

type fakeReviewImporter struct{}

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
