package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/review"
)

func (s *Server) reviewJournals(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	limit, err := positiveQueryInt(r, "limit", 30, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := nonNegativeQueryInt(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, total, err := s.reviewStore.ListJournals(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": entries, "total": total})
}

func (s *Server) reviewJournal(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	entry, err := s.reviewStore.GetJournal(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "复盘日记不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": entry})
}

func (s *Server) reviewJournalSave(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	var input struct {
		Title     string `json:"title"`
		EntryDate string `json:"entry_date"`
		Content   string `json:"content"`
		Revision  int    `json:"revision"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "无效的日记内容: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 200 {
		writeError(w, http.StatusBadRequest, "请输入 1–200 字的日记标题")
		return
	}
	if strings.TrimSpace(input.Content) == "" || utf8.RuneCountInString(input.Content) > 200000 {
		writeError(w, http.StatusBadRequest, "请输入日记正文，最多 20 万字")
		return
	}
	if date, err := time.Parse("2006-01-02", input.EntryDate); err != nil || date.Format("2006-01-02") != input.EntryDate {
		writeError(w, http.StatusBadRequest, "请输入有效的复盘日期")
		return
	}
	entry := review.Journal{ID: r.PathValue("id"), Title: input.Title, EntryDate: input.EntryDate, Content: input.Content, Revision: input.Revision}
	var err error
	status := http.StatusOK
	if r.Method == http.MethodPost {
		entry, err = s.reviewStore.CreateJournal(r.Context(), entry)
		status = http.StatusCreated
	} else {
		if entry.Revision < 1 {
			writeError(w, http.StatusBadRequest, "缺少日记版本，请重新打开后编辑")
			return
		}
		entry, err = s.reviewStore.UpdateJournal(r.Context(), entry)
	}
	if errors.Is(err, review.ErrJournalConflict) {
		writeError(w, http.StatusConflict, "这篇日记已在其他窗口更新，请先复制当前正文，再重新打开日记")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "复盘日记不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status, map[string]any{"data": entry})
}
