package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// limitUpEventSummary 是个股级涨停事件的精简投影：前端只需要逐日判定
// 「哪天涨停、几连板」，完整的行情字段留在事件流接口里。
type limitUpEventSummary struct {
	Date          string `json:"date"`
	Streak        int    `json:"streak"`
	FirstLimit    string `json:"first_limit_time,omitempty"`
	OpenCount     int    `json:"open_count,omitempty"`
	ChangePercent float64 `json:"change_percent,omitempty"`
	PrimaryTheme  string `json:"primary_theme,omitempty"`
}

type limitUpEventsPayload struct {
	Events       map[string][]limitUpEventSummary `json:"events"`
	CoveredDates []string                         `json:"covered_dates"`
	LookbackDays int                              `json:"lookback_days"`
	Fallback     bool                             `json:"fallback"`
}

// limitUpEvents 返回指定个股近 N 个交易日的逐日涨停事件序列。
// 数据来自开盘啦涨停池快照（已持久化积累）与东财补位；covered_dates 列出
// 池子实际覆盖的交易日，前端据此区分「精确确认未涨停」与「无数据只能近似」。
func (s *Server) limitUpEvents(w http.ResponseWriter, r *http.Request) {
	if s.limitUpProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "涨停事件数据源不可用")
		return
	}
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(symbols) > 30 {
		writeError(w, http.StatusBadRequest, "batch limit-up events supports at most 30 symbols")
		return
	}
	days := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 || parsed > 30 {
			writeError(w, http.StatusBadRequest, "days must be between 1 and 30")
			return
		}
		days = parsed
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	events, poolErr := s.limitUpProvider.RecentLimitUps(ctx, days)
	if poolErr != nil && len(events) == 0 {
		writeError(w, http.StatusBadGateway, poolErr.Error())
		return
	}

	wanted := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		wanted[symbol] = struct{}{}
	}
	payload := limitUpEventsPayload{
		Events:       make(map[string][]limitUpEventSummary, len(symbols)),
		LookbackDays: days,
	}
	// covered_dates 必须来自全量事件（与查询个股无关）：即使被查个股当天
	// 没有涨停，只要池子覆盖了该交易日，就能精确判定「该日未涨停」。
	coveredSet := map[string]struct{}{}
	for _, event := range events {
		if event.Date.IsZero() {
			continue
		}
		date := event.Date.Format("2006-01-02")
		coveredSet[date] = struct{}{}
		if _, ok := wanted[event.Symbol]; !ok {
			continue
		}
		payload.Events[event.Symbol] = append(payload.Events[event.Symbol], limitUpEventSummary{
			Date:          date,
			Streak:        event.Streak,
			FirstLimit:    event.FirstLimitTime,
			OpenCount:     event.OpenCount,
			ChangePercent: event.ChangePercent,
			PrimaryTheme:  event.PrimaryTheme,
		})
	}
	for date := range coveredSet {
		payload.CoveredDates = append(payload.CoveredDates, date)
	}
	sort.Strings(payload.CoveredDates)
	if poolErr != nil {
		payload.Fallback = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": payload})
}
