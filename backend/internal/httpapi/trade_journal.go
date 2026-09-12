package httpapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/tradejournal"
)

func (s *Server) tradeJournalAnalyze(w http.ResponseWriter, r *http.Request) {
	if s.tradeJournalStore == nil {
		writeError(w, http.StatusServiceUnavailable, "交易复盘服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request tradejournal.AnalyzeRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := tradejournal.Analyze(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := tradejournal.HistoryEntry{
		ID: newJournalID(), Filename: result.Filename, Imported: result.Imported, Trades: len(result.Trades),
		WinRate: result.Statistics.WinRate, TotalProfit: result.Statistics.TotalNetProfit,
		MaxDrawdownPct: result.Statistics.MaxDrawdownPct, BiasCount: len(result.Biases),
		AnalyzedAt: time.Now().UTC(), Result: &result,
	}
	if _, err := s.tradeJournalStore.Save(r.Context(), entry); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result, "entry_id": entry.ID})
}

func (s *Server) tradeJournalList(w http.ResponseWriter, r *http.Request) {
	if s.tradeJournalStore == nil {
		writeError(w, http.StatusServiceUnavailable, "交易复盘服务不可用")
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
	entries, err := s.tradeJournalStore.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": entries})
}

func (s *Server) tradeJournalGet(w http.ResponseWriter, r *http.Request) {
	if s.tradeJournalStore == nil {
		writeError(w, http.StatusServiceUnavailable, "交易复盘服务不可用")
		return
	}
	entry, err := s.tradeJournalStore.Get(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "未找到复盘记录")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": entry})
}

func (s *Server) tradeJournalDelete(w http.ResponseWriter, r *http.Request) {
	if s.tradeJournalStore == nil {
		writeError(w, http.StatusServiceUnavailable, "交易复盘服务不可用")
		return
	}
	if err := s.tradeJournalStore.Delete(r.Context(), strings.TrimSpace(r.PathValue("id"))); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": "deleted"})
}

func newJournalID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return "journal-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
