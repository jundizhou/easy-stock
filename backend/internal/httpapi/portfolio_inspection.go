package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"easy-stock/backend/internal/portfolioinspection"
)

func (s *Server) portfolioInspectionCreate(w http.ResponseWriter, r *http.Request) {
	if s.portfolioInspection == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓巡检服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request portfolioinspection.Request
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.SourceOptimizationID != "" {
		if err := s.portfolioOptimization.ValidateApplication(r.Context(), request); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	job, err := s.portfolioInspection.Start(r.Context(), request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, portfolioinspection.ErrJobRunning) {
			status = http.StatusConflict
		} else if strings.Contains(err.Error(), "AI") || strings.Contains(err.Error(), "模型") {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) portfolioInspectionGet(w http.ResponseWriter, r *http.Request) {
	if s.portfolioInspection == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓巡检服务不可用")
		return
	}
	job, err := s.portfolioInspection.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "未找到持仓巡检任务")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func (s *Server) portfolioInspectionList(w http.ResponseWriter, r *http.Request) {
	if s.portfolioInspection == nil {
		writeError(w, http.StatusServiceUnavailable, "持仓巡检服务不可用")
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
	jobs, err := s.portfolioInspection.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": jobs})
}

func (s *Server) portfolioInspectionResume(w http.ResponseWriter, r *http.Request) {
	job, err := s.portfolioInspection.Resume(r.Context(), r.PathValue("id"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, portfolioinspection.ErrJobRunning) {
			status = http.StatusConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}
func (s *Server) portfolioInspectionCancel(w http.ResponseWriter, r *http.Request) {
	if !s.portfolioInspection.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusConflict, "持仓任务已结束")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "持仓分析已停止，共享个股研究可独立完成"})
}
