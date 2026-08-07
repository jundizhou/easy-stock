package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientLimitUpPoolParsesLeadershipFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getTopicZTPool" {
			t.Fatalf("path = %s, want /getTopicZTPool", r.URL.Path)
		}
		if got := r.URL.Query().Get("date"); got != "20260716" {
			t.Fatalf("date = %s, want 20260716", got)
		}
		_, _ = w.Write([]byte(`{
			"rc": 0,
			"data": {"pool": [{
				"c":"600664","n":"哈药股份","p":4940,"zdp":10.02,
				"amount":4522777492,"ltsz":12000000000,"hs":8.5,"lbc":5,
				"fbt":92502,"lbt":94507,"zbc":1,"hybk":"化学制药",
				"zttj":{"days":5,"ct":5}
			}]}
		}`))
	}))
	defer server.Close()

	client := NewClient(WithTopicBaseURL(server.URL))
	date := time.Date(2026, 7, 16, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	events, err := client.LimitUpPool(context.Background(), date)
	if err != nil {
		t.Fatalf("LimitUpPool failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	event := events[0]
	if event.Symbol != "600664.SH" || event.Streak != 5 || event.Industry != "化学制药" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.FirstLimitTime != "09:25:02" || event.LastLimitTime != "09:45:07" {
		t.Fatalf("unexpected limit times: %+v", event)
	}
	if event.Price != 4.94 || event.Days != 5 || event.Count != 5 {
		t.Fatalf("unexpected limit metadata: %+v", event)
	}
}

func TestClientBrokenAndLimitDownPoolsUseTheirOwnEndpointsAndSorts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getTopicZBPool":
			if got := r.URL.Query().Get("sort"); got != "fbt:asc" {
				t.Fatalf("broken-board sort = %s, want fbt:asc", got)
			}
		case "/getTopicDTPool":
			if got := r.URL.Query().Get("sort"); got != "fund:asc" {
				t.Fatalf("limit-down sort = %s, want fund:asc", got)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"rc":0,"data":{"pool":[{"c":"600363","n":"联创光电","p":22180,"zdp":-9.98,"amount":80786214,"hybk":"消费电子"}]}}`))
	}))
	defer server.Close()

	client := NewClient(WithTopicBaseURL(server.URL))
	date := time.Date(2026, 8, 6, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	broken, err := client.BrokenLimitUpPool(context.Background(), date)
	if err != nil || len(broken) != 1 {
		t.Fatalf("BrokenLimitUpPool = %+v, err=%v", broken, err)
	}
	down, err := client.LimitDownPool(context.Background(), date)
	if err != nil || len(down) != 1 {
		t.Fatalf("LimitDownPool = %+v, err=%v", down, err)
	}
	if down[0].Symbol != "600363.SH" || down[0].Price != 22.18 || down[0].Industry != "消费电子" {
		t.Fatalf("unexpected limit-down event: %+v", down[0])
	}
}
