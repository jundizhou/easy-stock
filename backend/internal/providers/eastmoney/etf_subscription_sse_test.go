package eastmoney

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETFSubscriptionSSEPreservesShareUnitsAndDisclosedDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://www.sse.com.cn/" {
			t.Error("missing SSE referer")
		}
		json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{
			{"SEC_CODE": "510300", "STAT_DATE": "2026-11-17", "TOT_VOL": "12000"},
			{"SEC_CODE": "510050", "STAT_DATE": "2026-11-16", "TOT_VOL": "999999"},
			{"SEC_CODE": "588080", "STAT_DATE": "2026-11-17", "TOT_VOL": "0"},
		}})
	}))
	defer server.Close()
	client := NewClient(WithHTTPClient(server.Client()))
	shares, err := client.nationalTeamShareDay(context.Background(), server.URL, "2026-11-17")
	if err != nil || len(shares) != 2 || shares["510300"] != 12000 {
		t.Fatalf("shares=%v err=%v", shares, err)
	}
	if value, ok := shares["588080"]; !ok || value != 0 {
		t.Fatal("actual zero was lost")
	}
}
