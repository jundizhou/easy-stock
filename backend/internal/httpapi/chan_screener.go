package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/chanscreener"
	"easy-stock/backend/internal/foundation"
)

// chanScreenRequest 是缠论选股的请求体（POST JSON）。
type chanScreenRequest struct {
	// Symbols 是规范化的股票代码列表，上限 200。
	Symbols []string `json:"symbols"`
	// Names 提供代码到名称的映射，仅供结果展示。
	Names map[string]string `json:"names"`
	// Period 是K线周期：day/week/month/60min/30min/15min/5min。
	Period string `json:"period"`
	// Limit 是单只拉取的K线根数，默认 500。
	Limit int `json:"limit"`
	// Filters 是选股条件：side/bs_types/zs_state/bi_direction/bsp_recent_bars/min_score/require_sure。
	Filters map[string]any `json:"filters"`
	// Conf 覆盖 chan.py 的 CChanConfig。
	Conf map[string]any `json:"conf"`
	// Workers 是脚本并发取数线程数，默认 8。
	Workers int    `json:"workers"`
	Autype  string `json:"autype"`
}

// chanPyAnalyzeRequest 是 chan.py 单股分析的请求体。
type chanPyAnalyzeRequest struct {
	Symbol string         `json:"symbol"`
	Period string         `json:"period"`
	Limit  int            `json:"limit"`
	Conf   map[string]any `json:"conf"`
	Autype string         `json:"autype"`
}

func (s *Server) chanScreenerScreen(w http.ResponseWriter, r *http.Request) {
	request, err := parseChanScreenRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	symbols := make([]string, 0, len(request.Symbols))
	seen := map[string]bool{}
	for _, raw := range request.Symbols {
		normalized, normErr := foundation.NormalizeSymbol(strings.TrimSpace(raw))
		if normErr != nil {
			writeError(w, http.StatusBadRequest, normErr.Error())
			return
		}
		if seen[normalized.Canonical] {
			continue
		}
		seen[normalized.Canonical] = true
		symbols = append(symbols, normalized.Canonical)
	}
	request.Symbols = symbols
	if len(request.Symbols) == 0 {
		writeError(w, http.StatusBadRequest, "股票池为空")
		return
	}
	if s.chanScreenerService == nil || !s.chanScreenerService.Available() {
		writeError(w, http.StatusServiceUnavailable, s.chanScreenerUnavailableReason())
		return
	}

	// 批量选股包含逐只取数与缠论计算，整体预算放宽到 150s。
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	result, err := s.chanScreenerService.Screen(ctx, chanscreener.ScreenRequest{
		Symbols: request.Symbols,
		Names:   request.Names,
		Period:  request.Period,
		Limit:   request.Limit,
		Filters: request.Filters,
		Conf:    request.Conf,
		Workers: request.Workers,
		Autype:  request.Autype,
	})
	if err != nil {
		writeError(w, screenerErrorStatus(ctx, err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) chanScreenerAnalyze(w http.ResponseWriter, r *http.Request) {
	request, err := parseChanPyAnalyzeRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := foundation.NormalizeSymbol(request.Symbol)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.chanScreenerService == nil || !s.chanScreenerService.Available() {
		writeError(w, http.StatusServiceUnavailable, s.chanScreenerUnavailableReason())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := s.chanScreenerService.Analyze(ctx, chanscreener.AnalyzeRequest{
		Symbol: normalized.Canonical,
		Period: request.Period,
		Limit:  request.Limit,
		Conf:   request.Conf,
		Autype: request.Autype,
	})
	if err != nil {
		writeError(w, screenerErrorStatus(ctx, err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

// chanScreenerStatus 报告 chan.py 引擎可用性，供前端决定是否展示入口。
func (s *Server) chanScreenerStatus(w http.ResponseWriter, _ *http.Request) {
	available := s.chanScreenerService != nil && s.chanScreenerService.Available()
	payload := map[string]any{"available": available}
	if s.chanScreenerService != nil {
		payload["script"] = s.chanScreenerService.ScriptPath()
		payload["engine"] = "chan.py"
		if !available {
			payload["reason"] = s.chanScreenerService.UnavailableReason()
		}
	} else {
		payload["reason"] = "缠论选股服务未初始化"
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) chanScreenerUnavailableReason() string {
	if s.chanScreenerService == nil {
		return "缠论选股服务未初始化"
	}
	return s.chanScreenerService.UnavailableReason()
}

// screenerErrorStatus 把服务错误映射为 HTTP 状态码。
func screenerErrorStatus(ctx context.Context, err error) int {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "股票代码"), strings.Contains(message, "股票池"),
		strings.Contains(message, "单次最多"), strings.Contains(message, "缺少"):
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func parseChanScreenRequest(w http.ResponseWriter, r *http.Request) (chanScreenRequest, error) {
	if r.Method == http.MethodPost {
		request := chanScreenRequest{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return chanScreenRequest{}, errors.New("invalid JSON body")
		}
		return request, nil
	}
	query := r.URL.Query()
	request := chanScreenRequest{
		Period: strings.TrimSpace(query.Get("period")),
		Autype: strings.TrimSpace(query.Get("autype")),
	}
	if raw := strings.TrimSpace(query.Get("symbols")); raw != "" {
		request.Symbols = strings.Split(raw, ",")
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return chanScreenRequest{}, errors.New("limit 必须是整数")
		}
		request.Limit = parsed
	}
	if value := strings.TrimSpace(query.Get("workers")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return chanScreenRequest{}, errors.New("workers 必须是整数")
		}
		request.Workers = parsed
	}
	// GET 形态把常用条件直接平铺在查询串上，便于快速调试。
	filters := map[string]any{}
	if value := strings.TrimSpace(query.Get("side")); value != "" {
		filters["side"] = value
	}
	if value := strings.TrimSpace(query.Get("bs_types")); value != "" {
		filters["bs_types"] = strings.Split(value, ",")
	}
	if value := strings.TrimSpace(query.Get("zs_state")); value != "" {
		filters["zs_state"] = value
	}
	if value := strings.TrimSpace(query.Get("bi_direction")); value != "" {
		filters["bi_direction"] = value
	}
	if value := strings.TrimSpace(query.Get("bsp_recent_bars")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			filters["bsp_recent_bars"] = parsed
		}
	}
	if value := strings.TrimSpace(query.Get("min_score")); value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			filters["min_score"] = parsed
		}
	}
	if len(filters) > 0 {
		request.Filters = filters
	}
	return request, nil
}

func parseChanPyAnalyzeRequest(w http.ResponseWriter, r *http.Request) (chanPyAnalyzeRequest, error) {
	if r.Method == http.MethodPost {
		request := chanPyAnalyzeRequest{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return chanPyAnalyzeRequest{}, errors.New("invalid JSON body")
		}
		return request, nil
	}
	query := r.URL.Query()
	request := chanPyAnalyzeRequest{
		Symbol: strings.TrimSpace(query.Get("symbol")),
		Period: strings.TrimSpace(query.Get("period")),
		Autype: strings.TrimSpace(query.Get("autype")),
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return chanPyAnalyzeRequest{}, errors.New("limit 必须是整数")
		}
		request.Limit = parsed
	}
	return request, nil
}
