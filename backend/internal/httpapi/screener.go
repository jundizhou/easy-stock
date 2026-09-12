package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/screener"
)

// screenerStrategies 返回内置策略目录，供前端勾选界面渲染。
func (s *Server) screenerStrategies(w http.ResponseWriter, _ *http.Request) {
	if s.screenerService == nil {
		writeError(w, http.StatusServiceUnavailable, "策略选股服务不可用")
		return
	}
	strategies := screener.Strategies()
	writeJSON(w, http.StatusOK, map[string]any{"data": strategies, "count": len(strategies)})
}

// screenerRunRequest 是一次选股运行的请求体。
type screenerRunRequest struct {
	StrategyIDs []string `json:"strategy_ids"`
	Options     struct {
		ExcludeST          bool    `json:"exclude_st"`
		ExcludeNew         bool    `json:"exclude_new"`
		MinAmountYi        float64 `json:"min_amount_yi"`
		KlineUniverseLimit int     `json:"kline_universe_limit"`
	} `json:"options"`
}

// screenerRun 执行选股。冷启动含全市场快照与最多数百只K线计算，预算 150 秒。
func (s *Server) screenerRun(w http.ResponseWriter, r *http.Request) {
	if s.screenerService == nil {
		writeError(w, http.StatusServiceUnavailable, "策略选股服务不可用")
		return
	}
	var request screenerRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// 运行参数直接作为缓存键的一部分：相同条件 90 秒内复用结果。
	cacheKey, err := json.Marshal(request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cached, ok := s.marketSnapshots.fresh("screener:" + string(cacheKey)); ok {
		if result, typeOK := cached.value.(screener.Result); typeOK {
			writeJSON(w, http.StatusOK, map[string]any{"data": result, "meta": cached.meta})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	result, err := s.screenerService.Run(ctx, screener.Request{
		StrategyIDs: request.StrategyIDs,
		Options: screener.Options{
			ExcludeST:          request.Options.ExcludeST,
			ExcludeNew:         request.Options.ExcludeNew,
			MinAmountYi:        request.Options.MinAmountYi,
			KlineUniverseLimit: request.Options.KlineUniverseLimit,
		},
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		if strings.Contains(err.Error(), "至少选择") || strings.Contains(err.Error(), "未知策略") {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	meta := foundation.SourceMeta{Source: "screener", FetchedAt: time.Now()}
	s.marketSnapshots.store("screener:"+string(cacheKey), result, meta)
	writeJSON(w, http.StatusOK, map[string]any{"data": result, "meta": meta})
}
