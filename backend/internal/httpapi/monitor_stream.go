package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/monitor"
)

// monitorStreamHandler 通过 SSE 把监控中心的实时价推给前端。
//
// 用法：GET /api/v1/monitor/stream?symbols=600000.SH,000001.SZ
//   - symbols 省略或为 * 时推送全市场快照（仅本地桌面场景使用）。
//   - 否则只推送指定标的，带宽最小。
//
// 价格来源是引擎的实时价总线（交易时段按 5 秒节拍检查共享快照），
// 与现有 HTTP 列表接口共享缓存与请求去重，前端各页签共用一条推送连接。
func (s *Server) monitorStreamHandler(w http.ResponseWriter, r *http.Request) {
	if s.monitor == nil {
		writeError(w, http.StatusServiceUnavailable, "monitor disabled")
		return
	}
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	symSet := map[string]bool{}
	if symbolsParam != "" && symbolsParam != "*" {
		for _, raw := range strings.Split(symbolsParam, ",") {
			if s := strings.TrimSpace(raw); s != "" {
				symSet[s] = true
			}
		}
	}

	_, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sub, unsub := s.monitor.SubscribeLiveTicks()
	defer unsub()

	writeFrame := func(ticks []monitor.LiveTick) bool {
		fetchedAt := ""
		if len(ticks) > 0 {
			fetchedAt = time.UnixMilli(ticks[0].FetchedAt).Format(time.RFC3339)
		}
		payload := map[string]any{
			"fetched_at": fetchedAt,
			"ticks":      ticks,
			"status":     s.monitor.CurrentStatus(),
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		// 慢客户端的网络写入必须有界，避免断线后长期占用订阅。
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		defer controller.SetWriteDeadline(time.Time{})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		return controller.Flush() == nil
	}

	// 首帧：立即把当前已知快照价推给新连上的客户端（含非交易时段的上次快照）。
	if !writeFrame(s.monitor.LiveTicks(symSet)) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-sub:
			if !open || !writeFrame(s.monitor.LiveTicks(symSet)) {
				return
			}
		case <-heartbeat.C:
			// 只更新连接/行情状态，空ticks不伪造新的价格时间。
			if !writeFrame(nil) {
				return
			}
		}
	}
}
