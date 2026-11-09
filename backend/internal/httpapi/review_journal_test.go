package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easy-stock/backend/internal/review"
)

func journalRequest(server http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func decodeJournal(t *testing.T, response *httptest.ResponseRecorder, status int) review.Journal {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data review.Journal `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}

func TestPersonalReviewJournalPersistsAndEditsWithoutChangingImportedPosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviews.db")
	store, err := review.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{ReviewStore: store})
	body := `{"title":"周五复盘","entry_date":"2026-11-06","content":"## 盘面观察\n\n**主线**与交易反思\n- [ ] 观察成交量\n"}`
	entry := decodeJournal(t, journalRequest(server, http.MethodPost, "/api/v1/reviews/journals", body), http.StatusCreated)
	if entry.ID == "" || entry.Revision != 1 || entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
		t.Fatalf("invalid created entry: %+v", entry)
	}
	if _, count, err := store.ListPosts(context.Background(), review.Query{Limit: 10}); err != nil || count != 0 {
		t.Fatalf("personal entry leaked into imported posts: count=%d err=%v", count, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = review.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server = NewServer(Config{ReviewStore: store})
	endpoint := "/api/v1/reviews/journals/" + entry.ID
	saved := decodeJournal(t, journalRequest(server, http.MethodGet, endpoint, ""), http.StatusOK)
	if saved.Content != entry.Content || !saved.CreatedAt.Equal(entry.CreatedAt) {
		t.Fatalf("reopened entry changed: %+v", saved)
	}
	update := `{"title":"修订后的复盘","entry_date":"2026-11-05","content":"  保留原始空白\n\n- 仓位反思\n","revision":1}`
	edited := decodeJournal(t, journalRequest(server, http.MethodPut, endpoint, update), http.StatusOK)
	if edited.Revision != 2 || edited.EntryDate != "2026-11-05" || edited.Content != "  保留原始空白\n\n- 仓位反思\n" || !edited.CreatedAt.Equal(entry.CreatedAt) {
		t.Fatalf("incorrect update: %+v", edited)
	}
	stale := journalRequest(server, http.MethodPut, endpoint, update)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update overwrote content: %d %s", stale.Code, stale.Body.String())
	}
	latest := decodeJournal(t, journalRequest(server, http.MethodGet, endpoint, ""), http.StatusOK)
	if latest.Content != edited.Content || latest.Revision != 2 {
		t.Fatalf("stale write changed entry: %+v", latest)
	}

	decodeJournal(t, journalRequest(server, http.MethodPost, "/api/v1/reviews/journals", `{"title":"周末观察","entry_date":"2026-11-07","content":"观察清单"}`), http.StatusCreated)
	list := journalRequest(server, http.MethodGet, "/api/v1/reviews/journals?limit=1&offset=1", "")
	var payload struct {
		Data  []review.Journal `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil || list.Code != http.StatusOK || payload.Total != 2 || len(payload.Data) != 1 || payload.Data[0].ID != entry.ID {
		t.Fatalf("incorrect paging: %s, %v", list.Body.String(), err)
	}
	for _, query := range []string{"%E4%BB%93%E4%BD%8D", "2026-11-05"} {
		list = journalRequest(server, http.MethodGet, "/api/v1/reviews/journals?q="+query, "")
		if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil || payload.Total != 1 || payload.Data[0].ID != entry.ID {
			t.Fatalf("search failed: %s", list.Body.String())
		}
	}
}

func TestPersonalReviewJournalRejectsInvalidWrites(t *testing.T) {
	store, err := review.OpenStore(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := NewServer(Config{ReviewStore: store})
	for _, body := range []string{
		`{"title":" ","entry_date":"2026-11-07","content":"正文"}`,
		`{"title":"复盘","entry_date":"2026-02-30","content":"正文"}`,
		`{"title":"复盘","entry_date":"2026-11-07","content":"\n "}`,
		`{"title":"复盘","entry_date":"2026-11-07","content":"正文","unexpected":true}`,
		`{"title":"复盘","entry_date":"2026-11-07","content":"正文"} {}`,
		`{"title":"` + strings.Repeat("长", 201) + `","entry_date":"2026-11-07","content":"正文"}`,
		`{"title":"复盘","entry_date":"2026-11-07","content":"` + strings.Repeat("字", 200001) + `"}`,
	} {
		response := journalRequest(server, http.MethodPost, "/api/v1/reviews/journals", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid write accepted: %d", response.Code)
		}
	}
	response := journalRequest(server, http.MethodPut, "/api/v1/reviews/journals/missing", `{"title":"复盘","entry_date":"2026-11-07","content":"正文","revision":1}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing update status=%d", response.Code)
	}
	_, count, err := store.ListJournals(context.Background(), "", 10, 0)
	if err != nil || count != 0 {
		t.Fatalf("invalid writes persisted: %d %v", count, err)
	}
}
