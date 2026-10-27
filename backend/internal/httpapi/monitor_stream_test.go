package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/monitor"
)

func TestMonitorStreamRouteAuthenticatesAndSharesSnapshot(t *testing.T) {
	engine := monitor.NewEngine(monitor.EngineConfig{Snapshot: chainFakeSnapshot{rows: []foundation.MarketSnapshotRow{
		{Symbol: "600000.SH", Price: 12.34, ChangePercent: 1.25},
		{Symbol: "000001.SZ", Price: 10.50, ChangePercent: -0.5},
	}}})
	server := &Server{mux: http.NewServeMux(), monitor: engine, token: "test-stream-token"}
	server.routes()
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	unauthorized, err := http.Get(httpServer.URL + "/api/v1/monitor/stream")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthed stream status=%d", unauthorized.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	readers := make([]*bufio.Scanner, 0, 2)
	for range 2 {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/v1/monitor/stream?symbols=600000.SH&token=test-stream-token", nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("stream not registered/flushed: status=%d headers=%v", response.StatusCode, response.Header)
		}
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 2<<20)
		if ticks := readMonitorStreamFrame(t, scanner); len(ticks) != 0 {
			t.Fatal("an empty cache must not fabricate quotes")
		}
		readers = append(readers, scanner)
	}
	// 一次公共懒加载同时唤醒两个 SSE 客户端；连接本身不调用上游。
	if _, err := engine.Abnormal(ctx, "limit_up", 20); err != nil {
		t.Fatal(err)
	}
	for _, scanner := range readers {
		ticks := readMonitorStreamFrame(t, scanner)
		if len(ticks) != 1 || ticks[0].Symbol != "600000.SH" || ticks[0].Price != 12.34 || ticks[0].FetchedAt <= 0 {
			t.Fatalf("fanout/filter mismatch: %+v", ticks)
		}
	}
}

func readMonitorStreamFrame(t *testing.T, scanner *bufio.Scanner) []monitor.LiveTick {
	t.Helper()
	for scanner.Scan() {
		if !strings.HasPrefix(scanner.Text(), "data: ") {
			continue
		}
		var frame struct {
			Ticks []monitor.LiveTick `json:"ticks"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		return frame.Ticks
	}
	t.Fatalf("stream stopped before data: %v", scanner.Err())
	return nil
}
