package httpapi

import (
	"net/http"
	"strings"
)

// quantProxy 通用透传：将请求转发到 Quant Terminal 的任意 path。
//
//	GET /api/v1/quant/proxy?path=/api/kline&code=000001&kind=day&days=120&adj=qfq
func (s *Server) quantProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing 'path', e.g. path=/api/kline",
		})
		return
	}
	q := r.URL.Query()
	q.Del("path")
	status, body, err := s.quantProvider.Proxy(r.Context(), path, q)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// quantKLine 代理 Quant Terminal 的 /api/kline。
//
//	GET /api/v1/quant/kline?code=000001&kind=day&days=120&adj=qfq
func (s *Server) quantKLine(w http.ResponseWriter, r *http.Request) {
	s.proxyQuantPath(w, r, "/api/kline")
}

// quantSys 代理 /api/sys（含 stockdb 看护报告/重启）。
//
//	GET /api/v1/quant/sys
func (s *Server) quantSys(w http.ResponseWriter, r *http.Request) {
	s.proxyQuantPath(w, r, "/api/sys")
}

// quantHubStatus 代理 /api/hubstatus（终端运行态）。
//
//	GET /api/v1/quant/hub-status
func (s *Server) quantHubStatus(w http.ResponseWriter, r *http.Request) {
	s.proxyQuantPath(w, r, "/api/hubstatus")
}

// proxyQuantPath 将当前请求（含 query）透传到 Quant Terminal 的指定 path。
func (s *Server) proxyQuantPath(w http.ResponseWriter, r *http.Request, path string) {
	status, body, err := s.quantProvider.Proxy(r.Context(), path, r.URL.Query())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
