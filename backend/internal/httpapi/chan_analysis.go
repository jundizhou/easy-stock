package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/chananalysis"
	"easy-stock/backend/internal/foundation"
)

// chanAnalysisRequest 是个股缠论分析的查询参数，同时用于 GET 查询串与 POST JSON。
type chanAnalysisRequest struct {
	Symbol string `json:"symbol"`
	// Period 是后端 K 线周期：day / week / month / 60min / 30min / 15min / 5min。
	Period string `json:"period"`
	// Freq 是 czsc 频率名（日线/周线/月线），留空时由脚本按 Period 推导。
	Freq string `json:"freq"`
	// Limit 是拉取的 K 线根数，默认 800，上限 2000。
	Limit int `json:"limit"`
	// Signals 覆盖默认信号；留空使用脚本内置的四个默认信号。
	Signals []chananalysis.SignalConfig `json:"signals"`
	// Backtest 指定要回测的信号名；留空表示不回测。
	Backtest string `json:"backtest"`
	// BacktestParams 是回测信号的参数。
	BacktestParams map[string]any `json:"backtest_params"`
	// Chart 为 true 时生成离线缠论 HTML 图。
	Chart bool `json:"chart"`
	// ChartDir 是 HTML 图输出目录；留空时图内联进响应（体积大，不建议）。
	ChartDir string `json:"chart_dir"`
}

var (
	errChanLimitInvalid = errors.New("limit 必须是整数")
	errChanChartInvalid = errors.New("chart 必须是布尔值")
)

func (s *Server) chanAnalysis(w http.ResponseWriter, r *http.Request) {
	request, err := parseChanAnalysisRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := foundation.NormalizeSymbol(request.Symbol)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.chanAnalysisService == nil || !s.chanAnalysisService.Available() {
		writeError(w, http.StatusServiceUnavailable, s.chanAnalysisUnavailableReason())
		return
	}

	// 缠论计算 + 可选回测需逐根重算 CZSC，整体预算放宽到 90s。
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	result, err := s.chanAnalysisService.Query(ctx, chananalysis.Request{
		Symbol:         normalized.Canonical,
		Period:         request.Period,
		Frequency:      request.Freq,
		Limit:          request.Limit,
		Signals:        request.Signals,
		BacktestSignal: request.Backtest,
		BacktestParams: request.BacktestParams,
		WithChart:      request.Chart,
		ChartOutDir:    request.ChartDir,
	})
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		case strings.Contains(err.Error(), "股票代码"):
			status = http.StatusBadRequest
		case strings.Contains(err.Error(), "图表输出目录"):
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

// chanSignalCatalog 返回 czsc 全量信号目录，供前端信号配置界面使用。
func (s *Server) chanSignalCatalog(w http.ResponseWriter, r *http.Request) {
	if s.chanAnalysisService == nil || !s.chanAnalysisService.Available() {
		writeError(w, http.StatusServiceUnavailable, s.chanAnalysisUnavailableReason())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	catalog, err := s.chanAnalysisService.Catalog(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(catalog),
		"signals": catalog,
	})
}

// chanStatus 报告缠论分析服务的可用性，便于前端决定是否展示入口。
func (s *Server) chanStatus(w http.ResponseWriter, r *http.Request) {
	available := s.chanAnalysisService != nil && s.chanAnalysisService.Available()
	payload := map[string]any{"available": available}
	if s.chanAnalysisService != nil {
		payload["script"] = s.chanAnalysisService.ScriptPath()
		if !available {
			payload["reason"] = s.chanAnalysisService.UnavailableReason()
		}
	} else {
		payload["reason"] = "缠论分析服务未初始化"
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) chanAnalysisUnavailableReason() string {
	if s.chanAnalysisService == nil {
		return "缠论分析服务未初始化"
	}
	return s.chanAnalysisService.UnavailableReason()
}

// parseChanAnalysisRequest 同时支持 GET 查询串与 POST JSON body。
func parseChanAnalysisRequest(w http.ResponseWriter, r *http.Request) (chanAnalysisRequest, error) {
	if r.Method == http.MethodPost {
		request := chanAnalysisRequest{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return chanAnalysisRequest{}, errors.New("invalid JSON body")
		}
		return request, nil
	}
	query := r.URL.Query()
	request := chanAnalysisRequest{
		Symbol:   strings.TrimSpace(query.Get("symbol")),
		Period:   strings.TrimSpace(query.Get("period")),
		Freq:     strings.TrimSpace(query.Get("freq")),
		Backtest: strings.TrimSpace(query.Get("backtest")),
		ChartDir: strings.TrimSpace(query.Get("chart_dir")),
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return chanAnalysisRequest{}, errChanLimitInvalid
		}
		request.Limit = parsed
	}
	if value := strings.TrimSpace(query.Get("chart")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return chanAnalysisRequest{}, errChanChartInvalid
		}
		request.Chart = parsed
	}
	return request, nil
}
