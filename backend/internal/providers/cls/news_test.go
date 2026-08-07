package cls

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLatestNewsParsesTelegraphResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"errno": 0,
			"data": {
				"roll_data": [
					{
						"id": 123,
						"title": "测试快讯",
						"content": "A股市场午后走强",
						"ctime": 1781265600,
						"level": "B",
						"shareurl": "https://www.cls.cn/telegraph/123",
						"subjects": [{"subject_name": "A股"}]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	got, err := client.LatestNews(context.Background(), 10)
	if err != nil {
		t.Fatalf("LatestNews returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(news) = %d, want 1", len(got))
	}
	if got[0].Title != "测试快讯" || got[0].Content != "A股市场午后走强" {
		t.Fatalf("unexpected news item: %+v", got[0])
	}
	if got[0].Meta.Source != "cls" || got[0].Meta.SourceURL == "" {
		t.Fatalf("unexpected meta: %+v", got[0].Meta)
	}
}
