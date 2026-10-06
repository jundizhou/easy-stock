package httpapi

import (
	"net/http"
	"strings"
)

// stockDBQuery 透传任意 stockdb KV 查询。
//
//	GET /api/v1/stockdb/query?t=日k:600633:20260625
func (s *Server) stockDBQuery(w http.ResponseWriter, r *http.Request) {
	t := strings.TrimSpace(r.URL.Query().Get("t"))
	if t == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing 't' query param, e.g. t=日k:600633:20260625",
		})
		return
	}
	body, err := s.stockDBProvider.RawQuery(r.Context(), t)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// stockDBKLine 按周期查询单根 K 线。
//
//	GET /api/v1/stockdb/kline?code=600633&period=daily&date=20260625
//	period: daily|weekly|monthly|minute
func (s *Server) stockDBKLine(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := strings.TrimSpace(q.Get("code"))
	date := strings.TrimSpace(q.Get("date"))
	if code == "" || date == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "require 'code' and 'date'"})
		return
	}
	table := periodToStockDBTable(q.Get("period"))
	data, err := s.stockDBProvider.Query(r.Context(), table+":"+code+":"+date)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// stockDBAdjustment 查询复权因子。
//
//	GET /api/v1/stockdb/adjustment?code=600633
func (s *Server) stockDBAdjustment(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "require 'code'"})
		return
	}
	data, err := s.stockDBProvider.Adjustment(r.Context(), code)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// stockDBCodes 返回全市场代码索引。
//
//	GET /api/v1/stockdb/codes
func (s *Server) stockDBCodes(w http.ResponseWriter, r *http.Request) {
	data, err := s.stockDBProvider.Codes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// stockDBStatus 暴露后端视角下的 stockdb 连通状态（探活用）。
//
//	GET /api/v1/stockdb/status
func (s *Server) stockDBStatus(w http.ResponseWriter, r *http.Request) {
	_, err := s.stockDBProvider.Codes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func periodToStockDBTable(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "weekly", "wk", "周":
		return "周k"
	case "monthly", "mo", "月":
		return "月k"
	case "minute", "min", "1min", "分钟":
		return "分钟k"
	default: // daily, day, d, 日
		return "日k"
	}
}
