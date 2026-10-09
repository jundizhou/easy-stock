package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLatestNewsAnnouncements(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"list":[` +
			`{"art_code":"ABC123","codes":"SH600519","title":"贵州茅台关于召开股东大会的公告","notice_date":"2026-10-09 18:00:00","ann_type":2},` +
			`{"art_code":"DEF456","codes":"SZ000001","title":"平安银行第三季度报告","notice_date":"2026-10-09 17:30:00","ann_type":2}` +
			`]}}`))
	}))
	defer srv.Close()

	c := NewClient(WithAnnouncementBaseURL(srv.URL))
	items, err := c.LatestNews(context.Background(), 10)
	if err != nil {
		t.Fatalf("LatestNews error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	first := items[0]
	if first.ID != "em-ABC123" {
		t.Errorf("id = %q", first.ID)
	}
	if first.Title == "" {
		t.Errorf("title empty")
	}
	if first.URL != "https://data.eastmoney.com/notices/detail/600519/ABC123.html" {
		t.Errorf("url = %q", first.URL)
	}
	if len(first.Tags) != 1 || first.Tags[0] != "600519" {
		t.Errorf("tags = %v", first.Tags)
	}
	if first.Meta.Source != "eastmoney" {
		t.Errorf("meta source = %q", first.Meta.Source)
	}
	want, _ := time.ParseInLocation("2006-01-02 15:04:05", "2026-10-09 18:00:00", time.Local)
	if !first.PublishedAt.Equal(want) {
		t.Errorf("published_at = %v, want %v", first.PublishedAt, want)
	}
}
