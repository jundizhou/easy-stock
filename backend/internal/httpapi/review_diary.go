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

	"easy-stock/backend/internal/review"
)

func (s *Server) reviewSources(w http.ResponseWriter, r *http.Request) {
	settings := s.settingsStore.Snapshot()
	ready := map[string]bool{"taoguba": false}
	for _, profile := range settings.ReviewAutomation.Profiles {
		if !profile.Enabled {
			continue
		}
		switch profile.Source {
		case "xueqiu", "taoguba":
			ready[profile.Source] = s.reviewAutomation != nil && s.reviewAutomation.BrowserAuthReady(profile.ID, profile.Source)
		default:
			ready[profile.Source] = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": []review.SourceStatus{
		{ID: "official", Name: "每日复盘", Status: "configured", Message: "收盘后每30分钟刷新远程大V清单；每位作者本地已有当日文章后不再重复下载", ImportReady: false, SyncReady: s.remoteDailySync != nil},
		{ID: "xueqiu", Name: "雪球", Status: map[bool]string{true: "configured", false: "limited"}[ready["xueqiu"]], Message: map[bool]string{true: "内置浏览器登录态有效，Hermes 可复用会话采集", false: "请在设置中打开雪球登录窗口并完成登录验证"}[ready["xueqiu"]], ImportReady: true, SyncReady: ready["xueqiu"]},
		{ID: "taoguba", Name: "淘股吧", Status: map[bool]string{true: "configured", false: "limited"}[ready["taoguba"]], Message: map[bool]string{true: "内置浏览器登录态有效，Hermes 可复用会话采集", false: "请在设置中打开淘股吧登录窗口并完成登录验证"}[ready["taoguba"]], ImportReady: true, SyncReady: ready["taoguba"]},
		{ID: "wechat", Name: "微信公众号", Status: "limited", Message: "支持已知文章链接导入；微信已停用历史文章列表接口，自动订阅暂不可用", ImportReady: true, SyncReady: false},
	}})
}

func (s *Server) reviewRemoteDailyStatus(w http.ResponseWriter, _ *http.Request) {
	if s.remoteDailySync == nil {
		writeError(w, http.StatusServiceUnavailable, "每日复盘远程同步未启用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.remoteDailySync.Status()})
}

func (s *Server) reviewRemoteDailySync(w http.ResponseWriter, r *http.Request) {
	if s.remoteDailySync == nil {
		writeError(w, http.StatusServiceUnavailable, "每日复盘远程同步未启用")
		return
	}
	ctx, cancel := contextWithTimeout(r, 25*time.Second)
	defer cancel()
	status, err := s.remoteDailySync.SyncToday(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}

func (s *Server) reviewAuthors(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	authors, err := s.reviewStore.ListAuthors(r.Context(), strings.TrimSpace(r.URL.Query().Get("source")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": authors})
}

func (s *Server) reviewPosts(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	limit, err := positiveQueryInt(r, "limit", 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := nonNegativeQueryInt(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	posts, total, err := s.reviewStore.ListPosts(r.Context(), review.Query{
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		AuthorID: strings.TrimSpace(r.URL.Query().Get("author_id")),
		Keyword:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": posts, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) reviewPost(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	post, err := s.reviewStore.GetPost(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "复盘文章不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": post})
}

func (s *Server) reviewPostDelete(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘日记存储不可用")
		return
	}
	err := s.reviewStore.DeletePost(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "复盘文章不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"deleted": true}})
}

func (s *Server) reviewImport(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil || s.reviewImporter == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘文章导入服务不可用")
		return
	}
	var request struct {
		URL string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的导入请求: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := contextWithTimeout(r, 35*time.Second)
	defer cancel()
	post, err := s.reviewImporter.ImportURL(ctx, request.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	post, err = s.reviewStore.UpsertPost(ctx, post)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": post})
}

func (s *Server) reviewSubscriptions(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘订阅存储不可用")
		return
	}
	items, err := s.reviewStore.ListSubscriptions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) reviewSubscriptionCreate(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "自动订阅服务不可用")
		return
	}
	var request struct {
		Source      string `json:"source"`
		HomepageURL string `json:"homepage_url"`
		Name        string `json:"name"`
		ConfigID    string `json:"config_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的订阅请求: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sub, err := s.reviewAutomation.AddSubscription(r.Context(), request.Source, request.HomepageURL, request.Name, request.ConfigID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": sub})
}

func (s *Server) reviewSubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if s.reviewStore == nil {
		writeError(w, http.StatusServiceUnavailable, "复盘订阅存储不可用")
		return
	}
	if err := s.reviewStore.DeleteSubscription(r.Context(), strings.TrimSpace(r.PathValue("id"))); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reviewSyncAll(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "自动同步服务不可用")
		return
	}
	ctx, cancel := contextWithTimeout(r, 10*time.Minute)
	defer cancel()
	results := s.reviewAutomation.SyncAll(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"data": results})
}

func (s *Server) reviewSyncOne(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "自动同步服务不可用")
		return
	}
	ctx, cancel := contextWithTimeout(r, 3*time.Minute)
	defer cancel()
	result := s.reviewAutomation.SyncOne(ctx, strings.TrimSpace(r.PathValue("id")))
	status := http.StatusOK
	if result.Error != "" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, map[string]any{"data": result})
}

func (s *Server) reviewAnalyzePost(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "AI分析服务不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.modelResponseTimeout())
	defer cancel()
	post, err := s.reviewAutomation.AnalyzePost(ctx, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": post})
}

func (s *Server) reviewDailySummaryGet(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "AI总结服务不可用")
		return
	}
	start, end, err := reviewSummaryWindowFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var summary *review.DailySummary
	if start.IsZero() {
		summary, err = s.reviewAutomation.GetTodaySummary(r.Context())
	} else {
		summary, err = s.reviewAutomation.GetSummaryForWindow(r.Context(), start, end)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary})
}

func (s *Server) reviewDailySummaryWindow(w http.ResponseWriter, _ *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "AI总结服务不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.reviewAutomation.DefaultSummaryWindow()})
}

func (s *Server) reviewDailySummaryCreate(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "AI总结服务不可用")
		return
	}
	var request struct {
		Force       bool      `json:"force"`
		WindowStart time.Time `json:"window_start"`
		WindowEnd   time.Time `json:"window_end"`
	}
	if r.ContentLength != 0 {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "无效的总结请求: "+err.Error())
			return
		}
		if err := ensureJSONEOF(decoder); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ctx, cancel := contextWithTimeout(r, 10*time.Second)
	defer cancel()
	var job review.DailySummaryJob
	var err error
	if request.WindowStart.IsZero() && request.WindowEnd.IsZero() {
		job, err = s.reviewAutomation.StartTodaySummary(ctx, request.Force)
	} else {
		job, err = s.reviewAutomation.StartTodaySummaryWithWindow(ctx, request.Force, request.WindowStart, request.WindowEnd)
	}
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "请先在设置中配置模型") || isReviewWindowValidationError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) reviewDailySummaryStatus(w http.ResponseWriter, r *http.Request) {
	if s.reviewAutomation == nil {
		writeError(w, http.StatusServiceUnavailable, "AI总结服务不可用")
		return
	}
	start, end, err := reviewSummaryWindowFromQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var job review.DailySummaryJob
	if start.IsZero() {
		job, err = s.reviewAutomation.GetTodaySummaryJob(r.Context())
	} else {
		job, err = s.reviewAutomation.GetSummaryJobForWindow(r.Context(), start, end)
	}
	if err != nil {
		status := http.StatusInternalServerError
		if isReviewWindowValidationError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": job})
}

func reviewSummaryWindowFromQuery(r *http.Request) (time.Time, time.Time, error) {
	startValue := strings.TrimSpace(r.URL.Query().Get("window_start"))
	endValue := strings.TrimSpace(r.URL.Query().Get("window_end"))
	if startValue == "" && endValue == "" {
		return time.Time{}, time.Time{}, nil
	}
	if startValue == "" || endValue == "" {
		return time.Time{}, time.Time{}, errors.New("window_start 和 window_end 必须同时提供")
	}
	start, err := time.Parse(time.RFC3339, startValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("window_start 必须是 RFC3339 时间")
	}
	end, err := time.Parse(time.RFC3339, endValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("window_end 必须是 RFC3339 时间")
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, errors.New("开始时间必须早于结束时间")
	}
	if end.Sub(start) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("自定义时间窗口不能超过90天")
	}
	return start, end, nil
}

func isReviewWindowValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "时间窗口") || strings.Contains(message, "开始时间") || strings.Contains(message, "window_start") || strings.Contains(message, "window_end")
}

func positiveQueryInt(r *http.Request, name string, fallback, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, &queryValueError{name: name, message: "必须是 1 到 " + strconv.Itoa(maximum) + " 的整数"}
	}
	return value, nil
}

func nonNegativeQueryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, &queryValueError{name: name, message: "必须是非负整数"}
	}
	return value, nil
}

type queryValueError struct {
	name    string
	message string
}

func (e *queryValueError) Error() string { return e.name + " " + e.message }

func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}
