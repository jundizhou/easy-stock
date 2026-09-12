package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/dailyanalysis"
)

func (s *Server) dailyAnalysisStart(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request dailyanalysis.Request
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.dailyAnalysis.Start(r.Context(), request, "manual")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, dailyanalysis.ErrJobRunning) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) dailyAnalysisGet(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	job, err := s.dailyAnalysis.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "未找到自选股日报任务")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func (s *Server) dailyAnalysisList(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 30 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 30")
			return
		}
		limit = parsed
	}
	jobs, err := s.dailyAnalysis.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": jobs})
}

func (s *Server) dailyAnalysisConfigGet(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	config, err := s.dailyAnalysis.Config(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": config})
}

func (s *Server) dailyAnalysisConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var config dailyanalysis.Config
	if err := decoder.Decode(&config); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.dailyAnalysis.UpdateConfig(r.Context(), config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (s *Server) dailyAnalysisPush(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	job, err := s.dailyAnalysis.PushJob(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "未找到自选股日报任务")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func (s *Server) dailyAnalysisCorrelations(w http.ResponseWriter, r *http.Request) {
	if s.dailyAnalysis == nil {
		writeError(w, http.StatusServiceUnavailable, "自选股日报服务不可用")
		return
	}
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	days := 60
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 250 {
			writeError(w, http.StatusBadRequest, "days must be between 1 and 250")
			return
		}
		days = parsed
	}
	symbols := strings.Split(symbolsParam, ",")
	trimmed := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if value := strings.TrimSpace(symbol); value != "" {
			trimmed = append(trimmed, value)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	matrix, err := s.dailyAnalysis.Correlations(ctx, trimmed, days)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": matrix})
}
