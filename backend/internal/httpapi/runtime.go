package httpapi

import (
	"net/http"
	"os"
	"strings"
)

// runtimeSidecars 暴露本机 sidecar 服务地址，供前端 iframe 直挂与探活。
//
//	GET /api/v1/runtime/sidecars
//	-> { "quant_url": "http://127.0.0.1:8766", "stockdb_url": "http://127.0.0.1:7899" }
//
// 地址与 providers 相同来源：环境变量 A_STOCK_QUANT_URL / A_STOCK_STOCKDB_URL，
// 未注入时回退本地默认端口（Electron 编排器 P1 已保证缺省注入）。
func (s *Server) runtimeSidecars(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"quant_url":   sidecarBaseURL("A_STOCK_QUANT_URL", "http://127.0.0.1:8766"),
		"stockdb_url": sidecarBaseURL("A_STOCK_STOCKDB_URL", "http://127.0.0.1:7899"),
	})
}

func sidecarBaseURL(envKey, fallback string) string {
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}
