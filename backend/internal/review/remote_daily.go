package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/runtimelog"
)

const (
	DefaultRemoteDailyBaseURL = "https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/reviews/daily"
	remoteDailySource         = "official"
	remoteDailySchemaVersion  = 1
	remoteDailyMaxBodyBytes   = 2 << 20
)

type RemoteDailyAuthor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Enabled  bool   `json:"enabled"`
}

type RemoteDailyAuthorsManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Authors       []RemoteDailyAuthor `json:"authors"`
}

type RemoteDailyArticle struct {
	SchemaVersion int       `json:"schema_version"`
	TradeDate     string    `json:"trade_date"`
	ID            string    `json:"id"`
	ExternalID    string    `json:"external_id"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	Platform      string    `json:"platform"`
	Title         string    `json:"title"`
	Digest        string    `json:"digest"`
	ContentText   string    `json:"content_text"`
	ContentSHA256 string    `json:"content_sha256"`
	SourceURL     string    `json:"source_url,omitempty"`
	PublishedAt   time.Time `json:"published_at"`
	RelatedStocks []string  `json:"related_stocks"`
	RelatedThemes []string  `json:"related_themes"`
}

type RemoteDailyAuthorSyncStatus struct {
	AuthorID   string `json:"author_id"`
	AuthorName string `json:"author_name"`
	Platform   string `json:"platform"`
	Status     string `json:"status"`
	ArticleID  string `json:"article_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RemoteDailySyncStatus struct {
	TradeDate      string                        `json:"trade_date"`
	Status         string                        `json:"status"`
	Message        string                        `json:"message"`
	TotalAuthors   int                           `json:"total_authors"`
	LocalAuthors   int                           `json:"local_authors"`
	SyncedAuthors  int                           `json:"synced_authors"`
	PendingAuthors int                           `json:"pending_authors"`
	Authors        []RemoteDailyAuthorSyncStatus `json:"authors"`
	LastAttemptAt  time.Time                     `json:"last_attempt_at,omitempty"`
	NextAttemptAt  time.Time                     `json:"next_attempt_at,omitempty"`
	LastError      string                        `json:"last_error,omitempty"`
}

type RemoteDailySyncConfig struct {
	BaseURL  string
	Interval time.Duration
	Client   *http.Client
	Now      func() time.Time
}

type RemoteDailySync struct {
	store    *Store
	baseURL  string
	interval time.Duration
	client   *http.Client
	now      func() time.Time

	mu          sync.RWMutex
	status      RemoteDailySyncStatus
	authorsETag string
	authors     []RemoteDailyAuthor
}

func NewRemoteDailySync(store *Store, cfg RemoteDailySyncConfig) *RemoteDailySync {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultRemoteDailyBaseURL
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &RemoteDailySync{
		store: store, baseURL: baseURL, interval: interval, client: client, now: now,
		status: RemoteDailySyncStatus{Status: "idle", Message: "等待检查远程大V每日复盘", Authors: []RemoteDailyAuthorSyncStatus{}},
	}
}

func (s *RemoteDailySync) Status() RemoteDailySyncStatus {
	if s == nil {
		return RemoteDailySyncStatus{Status: "disabled", Message: "每日复盘远程同步未启用", Authors: []RemoteDailyAuthorSyncStatus{}}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *RemoteDailySync) Run(ctx context.Context, loggers ...*log.Logger) {
	if s == nil || s.store == nil {
		return
	}
	var logger *log.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	run := func() {
		s.syncWithTimeout(ctx)
		if logger == nil {
			return
		}
		status := s.Status()
		level := "info"
		if strings.TrimSpace(status.LastError) != "" || status.Status == "error" {
			level = "warn"
		}
		logger.Printf(
			"level=%s event=scheduler_run feature=reviews task=remote_daily_sync status=%q total=%d synced=%d pending=%d error=%q",
			level,
			status.Status,
			status.TotalAuthors,
			status.SyncedAuthors,
			status.PendingAuthors,
			runtimelog.Redact(status.LastError),
		)
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.shouldPoll(s.now()) {
				run()
			}
		}
	}
}

func (s *RemoteDailySync) SyncToday(ctx context.Context) (RemoteDailySyncStatus, error) {
	if s == nil || s.store == nil {
		return RemoteDailySyncStatus{Status: "disabled", Message: "每日复盘远程同步未启用"}, errors.New("remote daily review sync is disabled")
	}
	now := s.now()
	tradeDate := now.In(shanghaiLocation()).Format("2006-01-02")
	s.setStatus(RemoteDailySyncStatus{
		TradeDate: tradeDate, Status: "checking", Message: "正在刷新远程大V清单并检查缺失文章",
		Authors: []RemoteDailyAuthorSyncStatus{}, LastAttemptAt: now,
	})
	authors, err := s.fetchAuthors(ctx)
	if err != nil {
		return s.updateFailure(tradeDate, now, fmt.Errorf("读取远程大V清单: %w", err))
	}
	configuredAuthors := 0
	enabledAuthors := make([]RemoteDailyAuthor, 0, len(authors))
	for _, author := range authors {
		if !author.Enabled {
			continue
		}
		configuredAuthors++
		deleted, checkErr := s.store.IsAuthorDeleted(ctx, remoteDailySource, author.ID)
		if checkErr != nil {
			return s.updateFailure(tradeDate, now, fmt.Errorf("检查已删除作者%s: %w", author.Name, checkErr))
		}
		if !deleted {
			enabledAuthors = append(enabledAuthors, author)
		}
	}
	if len(enabledAuthors) == 0 {
		if configuredAuthors == 0 {
			status := RemoteDailySyncStatus{
				TradeDate: tradeDate, Status: "not_found", Message: "远程尚未配置启用的大V",
				Authors: []RemoteDailyAuthorSyncStatus{}, LastAttemptAt: now, NextAttemptAt: now.Add(s.interval),
			}
			s.setStatus(status)
			return status, nil
		}
		status := RemoteDailySyncStatus{
			TradeDate: tradeDate, Status: "synced", Message: "远程作者均已从本机关注列表删除；仍会定时刷新清单以发现新增作者",
			Authors: []RemoteDailyAuthorSyncStatus{}, LastAttemptAt: now, NextAttemptAt: now.Add(s.interval),
		}
		s.setStatus(status)
		return status, nil
	}

	status := RemoteDailySyncStatus{
		TradeDate: tradeDate, Status: "partial", TotalAuthors: len(enabledAuthors),
		Authors:       make([]RemoteDailyAuthorSyncStatus, 0, len(enabledAuthors)),
		LastAttemptAt: now, NextAttemptAt: now.Add(s.interval),
	}
	errorsFound := []string{}
	for _, author := range enabledAuthors {
		result := RemoteDailyAuthorSyncStatus{AuthorID: author.ID, AuthorName: author.Name, Platform: author.Platform}
		externalID := remoteDailyExternalID(tradeDate, author.ID)
		exists, checkErr := s.store.HasPostBySourceExternalID(ctx, remoteDailySource, externalID)
		if checkErr != nil {
			result.Status = "error"
			result.Error = checkErr.Error()
			status.PendingAuthors++
			errorsFound = append(errorsFound, author.Name+": "+checkErr.Error())
			status.Authors = append(status.Authors, result)
			continue
		}
		if exists {
			result.Status = "already_local"
			status.LocalAuthors++
			status.Authors = append(status.Authors, result)
			continue
		}
		post, fetchStatus, fetchErr := s.fetchArticle(ctx, tradeDate, author, now)
		result.Status = fetchStatus
		if fetchErr != nil {
			result.Error = fetchErr.Error()
			errorsFound = append(errorsFound, author.Name+": "+fetchErr.Error())
			status.PendingAuthors++
		} else if fetchStatus == "synced" {
			result.ArticleID = post.ID
			status.SyncedAuthors++
		} else {
			status.PendingAuthors++
		}
		status.Authors = append(status.Authors, result)
	}

	available := status.LocalAuthors + status.SyncedAuthors
	switch {
	case status.PendingAuthors == 0:
		status.Status = "synced"
		status.Message = fmt.Sprintf("%d 位远程大V的当日文章均已在本地；仍会定时刷新作者清单以发现晚新增作者", available)
	case available > 0:
		status.Status = "partial"
		status.Message = fmt.Sprintf("已具备 %d/%d 位大V的当日文章，缺失作者将在30分钟后继续检查", available, status.TotalAuthors)
	default:
		status.Status = "not_found"
		status.Message = "远程大V的当日文章尚未发布"
	}
	if len(errorsFound) > 0 {
		status.LastError = strings.Join(errorsFound, "；")
	}
	s.setStatus(status)
	return status, nil
}

func (s *RemoteDailySync) fetchAuthors(ctx context.Context) ([]RemoteDailyAuthor, error) {
	s.mu.RLock()
	etag := s.authorsETag
	cached := append([]RemoteDailyAuthor(nil), s.authors...)
	s.mu.RUnlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/authors.json", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-stock-daily-review/2")
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return cached, nil
	}
	if response.StatusCode == http.StatusNotFound {
		return []RemoteDailyAuthor{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("作者清单返回 HTTP %d", response.StatusCode)
	}
	body, err := readRemoteDailyBody(response.Body)
	if err != nil {
		return nil, err
	}
	var manifest RemoteDailyAuthorsManifest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析作者清单: %w", err)
	}
	if manifest.SchemaVersion != remoteDailySchemaVersion {
		return nil, fmt.Errorf("不支持的作者清单协议版本 %d", manifest.SchemaVersion)
	}
	seen := map[string]bool{}
	authors := make([]RemoteDailyAuthor, 0, len(manifest.Authors))
	for _, author := range manifest.Authors {
		author.ID = strings.TrimSpace(author.ID)
		author.Name = strings.TrimSpace(author.Name)
		author.Platform = strings.TrimSpace(author.Platform)
		if author.ID == "" || author.Name == "" || seen[author.ID] {
			continue
		}
		seen[author.ID] = true
		authors = append(authors, author)
	}
	s.mu.Lock()
	s.authorsETag = strings.TrimSpace(response.Header.Get("ETag"))
	s.authors = append([]RemoteDailyAuthor(nil), authors...)
	s.mu.Unlock()
	return authors, nil
}

func (s *RemoteDailySync) fetchArticle(ctx context.Context, tradeDate string, author RemoteDailyAuthor, now time.Time) (Post, string, error) {
	objectURL := s.baseURL + "/" + url.PathEscape(author.ID) + "/" + tradeDate + ".json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, objectURL, nil)
	if err != nil {
		return Post{}, "error", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "easy-stock-daily-review/2")
	response, err := s.client.Do(request)
	if err != nil {
		return Post{}, "error", fmt.Errorf("请求文章: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Post{}, "not_found", nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Post{}, "error", fmt.Errorf("文章返回 HTTP %d", response.StatusCode)
	}
	body, err := readRemoteDailyBody(response.Body)
	if err != nil {
		return Post{}, "error", err
	}
	var article RemoteDailyArticle
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&article); err != nil {
		return Post{}, "error", fmt.Errorf("解析文章: %w", err)
	}
	if err := validateRemoteDailyArticle(article, tradeDate, author); err != nil {
		return Post{}, "error", err
	}
	originalURL := strings.TrimSpace(article.SourceURL)
	if parsed, parseErr := url.ParseRequestURI(originalURL); originalURL == "" || parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		originalURL = objectURL
	}
	post, err := s.store.UpsertPost(ctx, Post{
		ID: article.ID, Source: remoteDailySource, ExternalID: article.ExternalID,
		AuthorID: article.AuthorID, AuthorName: article.AuthorName, Title: article.Title,
		Digest: article.Digest, ContentText: article.ContentText, OriginalURL: originalURL,
		PublishedAt: article.PublishedAt, FetchedAt: now,
		RelatedStocks: nonNilStrings(article.RelatedStocks), RelatedThemes: nonNilStrings(article.RelatedThemes),
		AIKeyPoints: []string{},
	})
	if err != nil {
		return Post{}, "error", fmt.Errorf("保存文章: %w", err)
	}
	return post, "synced", nil
}

func (s *RemoteDailySync) shouldPoll(now time.Time) bool {
	local := now.In(shanghaiLocation())
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday || local.Hour() < 15 {
		return false
	}
	status := s.Status()
	tradeDate := local.Format("2006-01-02")
	return status.TradeDate != tradeDate || status.LastAttemptAt.IsZero() || now.Sub(status.LastAttemptAt) >= s.interval
}

func (s *RemoteDailySync) syncWithTimeout(ctx context.Context) {
	attemptCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, _ = s.SyncToday(attemptCtx)
}

func (s *RemoteDailySync) updateFailure(tradeDate string, now time.Time, cause error) (RemoteDailySyncStatus, error) {
	status := RemoteDailySyncStatus{
		TradeDate: tradeDate, Status: "error", Message: "每日复盘同步失败",
		Authors: []RemoteDailyAuthorSyncStatus{}, LastAttemptAt: now, NextAttemptAt: now.Add(s.interval), LastError: cause.Error(),
	}
	s.setStatus(status)
	return status, cause
}

func (s *RemoteDailySync) setStatus(status RemoteDailySyncStatus) {
	if status.Authors == nil {
		status.Authors = []RemoteDailyAuthorSyncStatus{}
	}
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func validateRemoteDailyArticle(article RemoteDailyArticle, tradeDate string, author RemoteDailyAuthor) error {
	if article.SchemaVersion != remoteDailySchemaVersion {
		return fmt.Errorf("不支持的每日复盘协议版本 %d", article.SchemaVersion)
	}
	if article.TradeDate != tradeDate || article.AuthorID != author.ID || article.ExternalID != remoteDailyExternalID(tradeDate, author.ID) {
		return errors.New("每日复盘的日期或作者标识不匹配")
	}
	if article.ID == "" || strings.TrimSpace(article.AuthorName) == "" || strings.TrimSpace(article.Title) == "" || strings.TrimSpace(article.ContentText) == "" || article.PublishedAt.IsZero() {
		return errors.New("每日复盘缺少文章、作者、标题、正文或发布时间")
	}
	digest := sha256.Sum256([]byte(article.ContentText))
	if !strings.EqualFold(strings.TrimSpace(article.ContentSHA256), hex.EncodeToString(digest[:])) {
		return errors.New("每日复盘正文校验失败")
	}
	return nil
}

func remoteDailyExternalID(tradeDate, authorID string) string {
	return "daily:" + tradeDate + ":" + authorID
}

func readRemoteDailyBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, remoteDailyMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > remoteDailyMaxBodyBytes {
		return nil, errors.New("远程每日复盘文件超过 2MB 限制")
	}
	return body, nil
}
