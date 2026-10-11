package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"easy-stock/backend/internal/execution"
)

// execution 相关端点把 internal/execution 网关暴露给前端执行工作区。
//
// 端点清单（与 V0.2_Execution_Integration.md 对齐）：
//   GET  /api/v1/execution/target   —— 生成 core2 可执行目标权重表（预览，只读）
//   GET  /api/v1/execution/status   —— 网关状态（模式/生产开关/急停/适配层/审计）
//   POST /api/v1/execution/intent   —— 受理执行意图（sim 模拟成交 / live 守卫拒绝）
//   POST /api/v1/execution/fill     —— 登记一笔成交（sim 由网关生成；live 由 P2 回灌）
//   POST /api/v1/execution/killswitch/engage    —— 拉闸（运维/前端急停）
//   POST /api/v1/execution/killswitch/disengage —— 解除拉闸
//
// 全部动作均经网关的 PIT 白名单、急停、生产开关、限额护栏；P0+P1 阶段不接真实下单。

// executionTargetHandler 生成 core2 可执行目标权重表（只读预览）。
//
//	GET /api/v1/execution/target?strategy=core2
func (s *Server) executionTargetHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	strategy := strings.TrimSpace(r.URL.Query().Get("strategy"))
	plan, err := s.executionGateway.GenerateTarget(r.Context(), execution.GenerateTargetRequest{Strategy: strategy})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "plan": plan})
}

// executionStatusHandler 返回网关当前状态（只读）。
//
//	GET /api/v1/execution/status
func (s *Server) executionStatusHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	status, err := s.executionGateway.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": status})
}

// executionIntentHandler 受理执行意图。sim 模式返回模拟成交；live 在 P0+P1 阶段守卫拒绝。
//
//	POST /api/v1/execution/intent
func (s *Server) executionIntentHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var intent execution.OrderIntent
	if err := decoder.Decode(&intent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid order intent: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ack, err := s.executionGateway.AcceptIntent(r.Context(), intent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ack": ack})
}

// executionFillHandler 登记一笔成交。
//
//	POST /api/v1/execution/fill
func (s *Server) executionFillHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	var fill execution.Fill
	if err := decoder.Decode(&fill); err != nil {
		writeError(w, http.StatusBadRequest, "invalid fill: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ack, err := s.executionGateway.RecordFill(r.Context(), fill)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ack": ack})
}

// executionKillEngageHandler 拉闸（拒绝一切下单动作）。
//
//	POST /api/v1/execution/killswitch/engage
func (s *Server) executionKillEngageHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	if err := s.executionGateway.EngageKillSwitch(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kill_switch_engaged": true})
}

// executionKillDisengageHandler 解除拉闸。
//
//	POST /api/v1/execution/killswitch/disengage
func (s *Server) executionKillDisengageHandler(w http.ResponseWriter, r *http.Request) {
	if s.executionGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "execution gateway is not initialized")
		return
	}
	if err := s.executionGateway.DisengageKillSwitch(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kill_switch_engaged": false})
}

// productionEnabledFromEnv 解析 A_STOCK_PRODUCTION_ENABLED（真值才视为开启）。
func productionEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("A_STOCK_PRODUCTION_ENABLED"))
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}
