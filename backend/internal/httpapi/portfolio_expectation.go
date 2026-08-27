package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"easy-stock/backend/internal/portfolioinspection"
)

func (s *Server) portfolioExpectationCreate(w http.ResponseWriter, r *http.Request) {
	if s.portfolioExpectation == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓明日预期服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request portfolioinspection.ExpectationRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	job, err := s.portfolioExpectation.Start(r.Context(), request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, portfolioinspection.ErrExpectationRunning) {
			status = http.StatusConflict
		} else if strings.Contains(err.Error(), "AI") || strings.Contains(err.Error(), "模型") {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) portfolioExpectationGet(w http.ResponseWriter, r *http.Request) {
	if s.portfolioExpectation == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓明日预期服务不可用")
		return
	}
	job, err := s.portfolioExpectation.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "未找到持仓明日预期任务")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func (s *Server) portfolioExpectationLatest(w http.ResponseWriter, r *http.Request) {
	if s.portfolioExpectation == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓明日预期服务不可用")
		return
	}
	tradeDate := strings.TrimSpace(r.URL.Query().Get("summary_date"))
	if tradeDate == "" {
		writeError(w, http.StatusBadRequest, "summary_date is required")
		return
	}
	job, err := s.portfolioExpectation.Latest(r.Context(), tradeDate)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}
