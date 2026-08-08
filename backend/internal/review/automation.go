package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/hermes"
)

type URLImporter interface {
	ImportURL(ctx context.Context, rawURL string) (Post, error)
}

const WechatArticleListUnavailableMessage = "微信已停用公众号历史文章列表接口，自动订阅暂不可用；请粘贴具体文章链接导入"

type Automation struct {
	store               *Store
	importer            URLImporter
	settings            *appsettings.Store
	httpClient          *http.Client
	prompter            hermes.Prompter
	fallbackWechat      string
	browserStateDir     string
	browserBridgeURL    string
	browserBridgeToken  string
	taogubaBridgeURL    string
	taogubaBridgeToken  string
	browserBridgeClient *http.Client
	mu                  sync.Mutex
	dailySummaryMu      sync.Mutex
	dailySummaryJobMu   sync.Mutex
	dailySummaryRunning bool
}

func NewAutomation(store *Store, importer URLImporter, settings *appsettings.Store, httpClient *http.Client, fallbackWechat string, prompters ...hermes.Prompter) *Automation {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	var prompter hermes.Prompter
	if len(prompters) > 0 {
		prompter = prompters[0]
	}
	bridgeClient := *httpClient
	if bridgeClient.Timeout == 0 || bridgeClient.Timeout < 90*time.Second {
		bridgeClient.Timeout = 90 * time.Second
	}
	return &Automation{
		store:               store,
		importer:            importer,
		settings:            settings,
		httpClient:          httpClient,
		prompter:            prompter,
		fallbackWechat:      strings.TrimRight(strings.TrimSpace(fallbackWechat), "/"),
		browserStateDir:     strings.TrimSpace(os.Getenv("A_STOCK_BROWSER_STATE_DIR")),
		browserBridgeURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("A_STOCK_BROWSER_BRIDGE_URL")), "/"),
		browserBridgeToken:  strings.TrimSpace(os.Getenv("A_STOCK_BROWSER_BRIDGE_TOKEN")),
		taogubaBridgeURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("A_STOCK_TAOGUBA_BROWSER_BRIDGE_URL")), "/"),
		taogubaBridgeToken:  strings.TrimSpace(os.Getenv("A_STOCK_TAOGUBA_BROWSER_BRIDGE_TOKEN")),
		browserBridgeClient: &bridgeClient,
	}
}

func (a *Automation) AddSubscription(ctx context.Context, source, homepageURL, name, configID string) (Subscription, error) {
	source = strings.TrimSpace(strings.ToLower(source))
	homepageURL = strings.TrimSpace(homepageURL)
	name = strings.TrimSpace(name)
	if source != "wechat" && source != "xueqiu" && source != "taoguba" {
		return Subscription{}, errors.New("只支持微信公众号、雪球和淘股吧")
	}
	if homepageURL == "" {
		return Subscription{}, errors.New("主页地址或公众号名称不能为空")
	}
	externalID := ""
	if source == "wechat" {
		if strings.HasPrefix(homepageURL, "Mz") || strings.HasSuffix(homepageURL, "==") {
			externalID = homepageURL
		}
		if strings.HasPrefix(homepageURL, "http") {
			if parsed, err := url.Parse(homepageURL); err == nil {
				externalID = firstNonEmpty(parsed.Query().Get("__biz"), parsed.Query().Get("fakeid"), homepageURL)
			}
		}
		if name == "" {
			name = firstNonEmpty(externalID, homepageURL)
		}
	} else {
		parsed, err := url.Parse(homepageURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return Subscription{}, errors.New("请输入有效的作者主页 URL")
		}
		if source == "xueqiu" && !strings.HasSuffix(strings.ToLower(parsed.Hostname()), "xueqiu.com") {
			return Subscription{}, errors.New("请输入雪球主页 URL")
		}
		if source == "taoguba" && !strings.HasSuffix(strings.ToLower(parsed.Hostname()), "taoguba.com.cn") && !strings.HasSuffix(strings.ToLower(parsed.Hostname()), "tgb.cn") {
			return Subscription{}, errors.New("请输入淘股吧主页 URL")
		}
		externalID = lastPathPart(parsed.Path)
		if name == "" {
			name = externalID
		}
	}
	profile, err := a.profileFor(source, configID)
	if err != nil {
		return Subscription{}, err
	}
	return a.store.UpsertSubscription(ctx, Subscription{Source: source, Name: firstNonEmpty(name, sourceName(source)), HomepageURL: homepageURL, ExternalID: externalID, ConfigID: profile.ID, Enabled: true, LastStatus: "pending"})
}

func (a *Automation) SyncAll(ctx context.Context) []SyncResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	subs, err := a.store.ListSubscriptions(ctx)
	if err != nil {
		return []SyncResult{{Error: err.Error()}}
	}
	results := []SyncResult{}
	for _, sub := range subs {
		if sub.Enabled {
			if subscriptionHasUnavailableWechatList(sub) {
				results = append(results, SyncResult{SubscriptionID: sub.ID, Error: WechatArticleListUnavailableMessage})
				continue
			}
			results = append(results, a.syncOneUnlocked(ctx, sub))
		}
	}
	return results
}

func (a *Automation) SyncOne(ctx context.Context, id string) SyncResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	sub, err := a.store.GetSubscription(ctx, id)
	if err != nil {
		return SyncResult{SubscriptionID: id, Error: err.Error()}
	}
	return a.syncOneUnlocked(ctx, sub)
}

func (a *Automation) syncOneUnlocked(ctx context.Context, sub Subscription) SyncResult {
	result := SyncResult{SubscriptionID: sub.ID}
	profile, profileErr := a.profileFor(sub.Source, sub.ConfigID)
	if profileErr != nil {
		result.Error = profileErr.Error()
		_ = a.store.SetSubscriptionSync(ctx, sub.ID, "error", result.Error, time.Now().Add(24*time.Hour))
		return result
	}
	if sub.ConfigID != profile.ID {
		sub.ConfigID = profile.ID
		_, _ = a.store.UpsertSubscription(ctx, sub)
	}
	if sub.Source == "xueqiu" || sub.Source == "taoguba" {
		return a.syncBrowserSourceWithHermesUnlocked(ctx, sub, profile)
	}
	links, resolvedName, resolvedID, err := a.discover(ctx, sub, profile)
	if err != nil {
		result.Error = err.Error()
		next := a.nextRun(profile)
		if isWechatArticleListUnavailableText(result.Error) {
			next = time.Now().AddDate(10, 0, 0)
		}
		_ = a.store.SetSubscriptionSync(ctx, sub.ID, "error", result.Error, next)
		return result
	}
	if resolvedName != "" || resolvedID != "" {
		sub.Name = firstNonEmpty(resolvedName, sub.Name)
		sub.ExternalID = firstNonEmpty(resolvedID, sub.ExternalID)
		_, _ = a.store.UpsertSubscription(ctx, sub)
	}
	result.Found = len(links)
	for _, link := range links {
		if _, getErr := a.store.GetPostByURL(ctx, link); getErr == nil {
			continue
		} else if !errors.Is(getErr, sql.ErrNoRows) {
			continue
		}
		post, importErr := a.importForSubscription(ctx, sub, profile, link)
		if importErr != nil {
			continue
		}
		post.AuthorName = firstNonEmpty(resolvedName, sub.Name, post.AuthorName)
		post.AuthorID = stableID(sub.Source + "\n" + firstNonEmpty(resolvedID, sub.ExternalID, sub.HomepageURL))[:24]
		post, importErr = a.store.UpsertPost(ctx, post)
		if importErr != nil {
			continue
		}
		result.Imported++
		if profile.AutoAnalyze {
			if analyzed, analyzeErr := a.AnalyzePost(ctx, post.ID); analyzeErr == nil && !analyzed.AIAnalyzedAt.IsZero() {
				result.Analyzed++
			}
		}
	}
	_ = a.store.SetSubscriptionSync(ctx, sub.ID, "ok", "", a.nextRun(profile))
	return result
}

func (a *Automation) syncBrowserSourceWithHermesUnlocked(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile) SyncResult {
	result := SyncResult{SubscriptionID: sub.ID}
	label := browserSourceLabel(sub.Source)
	if a.prompter == nil {
		result.Error = "Hermes AI 采集底座不可用"
		_ = a.store.SetSubscriptionSync(ctx, sub.ID, "error", result.Error, a.nextRun(profile))
		return result
	}
	statePath := a.browserStatePath(profile.ID, sub.Source)
	if !browserStateLoggedIn(statePath, sub.Source) {
		result.Error = label + "登录态不可用，请在设置中打开" + label + "登录窗口，完成登录或安全验证后点击“我已完成登录”"
		_ = a.store.SetSubscriptionSync(ctx, sub.ID, "error", result.Error, a.nextRun(profile))
		return result
	}
	posts, resolvedName, resolvedID, err := a.collectBrowserSourceWithHermes(ctx, sub, profile, statePath)
	if err != nil {
		result.Error = "内置浏览器 / Hermes 采集失败: " + err.Error()
		_ = a.store.SetSubscriptionSync(ctx, sub.ID, "error", result.Error, a.nextRun(profile))
		return result
	}
	result.Found = len(posts)
	resolvedName = firstNonEmpty(resolvedName, sub.Name)
	resolvedID = firstNonEmpty(resolvedID, sub.ExternalID)
	for _, post := range posts {
		if _, getErr := a.store.GetPostByURL(ctx, post.OriginalURL); getErr == nil {
			continue
		} else if !errors.Is(getErr, sql.ErrNoRows) {
			continue
		}
		post.AuthorName = firstNonEmpty(resolvedName, post.AuthorName)
		post.AuthorID = stableID(sub.Source + "\n" + firstNonEmpty(resolvedID, sub.HomepageURL))[:24]
		stored, storeErr := a.store.UpsertPost(ctx, post)
		if storeErr != nil {
			continue
		}
		result.Imported++
		if profile.AutoAnalyze {
			if analyzed, analyzeErr := a.AnalyzePost(ctx, stored.ID); analyzeErr == nil && !analyzed.AIAnalyzedAt.IsZero() {
				result.Analyzed++
			}
		}
	}
	if resolvedName != "" || resolvedID != "" {
		sub.Name = firstNonEmpty(resolvedName, sub.Name)
		sub.ExternalID = firstNonEmpty(resolvedID, sub.ExternalID)
		_, _ = a.store.UpsertSubscription(ctx, sub)
	}
	_ = a.store.SetSubscriptionSync(ctx, sub.ID, "ok", "", a.nextRun(profile))
	return result
}

func (a *Automation) importForSubscription(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile, link string) (Post, error) {
	if sub.Source == "wechat" {
		base, token := strings.TrimRight(profile.BaseURL, "/"), profile.Credential
		if base != "" {
			return a.importWechat(ctx, base, token, link)
		}
	}
	if sub.Source == "taoguba" {
		if importer, ok := a.importer.(interface {
			ImportURLWithHeaders(context.Context, string, http.Header) (Post, error)
		}); ok {
			headers := http.Header{}
			if cookie := strings.TrimSpace(profile.Credential); cookie != "" {
				headers.Set("Cookie", cookie)
			}
			return importer.ImportURLWithHeaders(ctx, link, headers)
		}
	}
	return a.importer.ImportURL(ctx, link)
}

func (a *Automation) discover(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile) ([]string, string, string, error) {
	switch sub.Source {
	case "wechat":
		return a.discoverWechat(ctx, sub, profile)
	case "xueqiu":
		return nil, "", "", errors.New("雪球订阅必须使用内置浏览器登录态采集")
	case "taoguba":
		return a.discoverHTML(ctx, sub, profile)
	default:
		return nil, "", "", errors.New("不支持的数据源")
	}
}

func (a *Automation) discoverWechat(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile) ([]string, string, string, error) {
	base, token := strings.TrimRight(profile.BaseURL, "/"), profile.Credential
	if base == "" {
		return nil, "", "", errors.New("请先在设置中配置微信公众号解析服务地址")
	}
	fakeID := sub.ExternalID
	name := sub.Name
	if fakeID == "" || strings.Contains(fakeID, "/") || strings.HasPrefix(fakeID, "http") {
		endpoint := base + "/api/public/searchbiz?query=" + url.QueryEscape(firstNonEmpty(name, sub.HomepageURL))
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				List []struct {
					FakeID   string `json:"fakeid"`
					Nickname string `json:"nickname"`
				} `json:"list"`
			} `json:"data"`
			Error string `json:"error"`
		}
		if err := a.getJSON(ctx, endpoint, token, &response); err != nil {
			return nil, "", "", normalizeWechatArticleListError(err)
		}
		if !response.Success || len(response.Data.List) == 0 {
			return nil, "", "", normalizeWechatArticleListError(errors.New(firstNonEmpty(response.Error, "没有搜索到该公众号")))
		}
		fakeID = response.Data.List[0].FakeID
		name = response.Data.List[0].Nickname
	}
	endpoint := base + "/api/public/articles?fakeid=" + url.QueryEscape(fakeID) + "&begin=0&count=20"
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Articles []struct {
				Link string `json:"link"`
				URL  string `json:"url"`
			} `json:"articles"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := a.getJSON(ctx, endpoint, token, &response); err != nil {
		return nil, "", "", normalizeWechatArticleListError(err)
	}
	if !response.Success {
		return nil, "", "", normalizeWechatArticleListError(errors.New(firstNonEmpty(response.Error, "公众号文章列表获取失败，请检查扫码登录状态")))
	}
	links := []string{}
	for _, item := range response.Data.Articles {
		if link := firstNonEmpty(item.Link, item.URL); link != "" {
			links = append(links, link)
		}
	}
	return uniqueStrings(links), name, fakeID, nil
}

func normalizeWechatArticleListError(err error) error {
	if err != nil && isWechatArticleListUnavailableText(err.Error()) {
		return errors.New(WechatArticleListUnavailableMessage)
	}
	return err
}

func isWechatArticleListUnavailableText(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(message, "ret=200013") ||
		strings.Contains(message, "freq control") ||
		strings.Contains(message, "微信已停用公众号历史文章列表接口")
}

func subscriptionHasUnavailableWechatList(sub Subscription) bool {
	return sub.Source == "wechat" && isWechatArticleListUnavailableText(sub.LastError)
}

type hermesXueqiuArticle struct {
	Title       string `json:"title"`
	OriginalURL string `json:"original_url"`
	ContentText string `json:"content_text"`
	PublishedAt string `json:"published_at"`
}

type hermesXueqiuCollection struct {
	AuthorName string                `json:"author_name"`
	ExternalID string                `json:"external_id"`
	Articles   []hermesXueqiuArticle `json:"articles"`
	Error      string                `json:"error"`
}

func (a *Automation) collectBrowserSourceWithHermes(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile, statePath string) ([]Post, string, string, error) {
	bridgeURL, bridgeToken := a.browserBridgeForSource(sub.Source)
	if bridgeURL != "" {
		collection, err := a.collectFromBrowserBridge(ctx, sub, profile, bridgeURL, bridgeToken)
		if err != nil {
			return nil, "", "", err
		}
		collection = a.normalizeBrowserBridgeCollection(ctx, sub, collection)
		return postsFromBrowserCollection(sub, collection)
	}
	return a.collectWithBrowserState(ctx, sub, statePath)
}

func (a *Automation) browserBridgeForSource(source string) (string, string) {
	if source == "taoguba" {
		return a.taogubaBridgeURL, a.taogubaBridgeToken
	}
	return a.browserBridgeURL, a.browserBridgeToken
}

func (a *Automation) collectFromBrowserBridge(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile, bridgeURL, bridgeToken string) (hermesXueqiuCollection, error) {
	body, err := json.Marshal(map[string]any{
		"profile_id":   profile.ID,
		"homepage_url": sub.HomepageURL,
		"limit":        5,
	})
	if err != nil {
		return hermesXueqiuCollection{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, bridgeURL+"/v1/"+sub.Source+"/collect", bytes.NewReader(body))
	if err != nil {
		return hermesXueqiuCollection{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if bridgeToken != "" {
		request.Header.Set("X-A-Stock-Browser-Token", bridgeToken)
	}
	response, err := a.browserBridgeClient.Do(request)
	if err != nil {
		return hermesXueqiuCollection{}, fmt.Errorf("连接内置浏览器失败: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return hermesXueqiuCollection{}, err
	}
	var payload struct {
		OK    bool                   `json:"ok"`
		Data  hermesXueqiuCollection `json:"data"`
		Error string                 `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return hermesXueqiuCollection{}, fmt.Errorf("内置浏览器返回格式无效: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !payload.OK {
		return hermesXueqiuCollection{}, errors.New(firstNonEmpty(strings.TrimSpace(payload.Error), fmt.Sprintf("内置浏览器返回 HTTP %d", response.StatusCode)))
	}
	if len(payload.Data.Articles) == 0 {
		return hermesXueqiuCollection{}, errors.New("内置浏览器没有读取到" + browserSourceLabel(sub.Source) + "文章")
	}
	return payload.Data, nil
}

func (a *Automation) normalizeBrowserBridgeCollection(ctx context.Context, sub Subscription, raw hermesXueqiuCollection) hermesXueqiuCollection {
	data, err := json.Marshal(raw)
	if err != nil || a.prompter == nil {
		return raw
	}
	prompt := "你是" + browserSourceLabel(sub.Source) + "文章整理代理。以下 JSON 来自用户已登录的内置 Electron 浏览器，字段均为实际页面读取结果。请挑选最近最多5篇文章，清理标题，把发布时间尽量转换为RFC3339；不得新增链接、不得编造正文、不得输出Cookie。只返回严格JSON，结构保持不变：" + string(data) + "\n目标主页：" + sub.HomepageURL
	response, err := a.prompter.Prompt(ctx, prompt)
	if err != nil {
		return raw
	}
	normalized, err := parseHermesXueqiuCollection(response.Content)
	if err != nil || strings.TrimSpace(normalized.Error) != "" {
		return raw
	}
	byURL := map[string]hermesXueqiuArticle{}
	for _, article := range raw.Articles {
		byURL[strings.TrimSpace(article.OriginalURL)] = article
	}
	articles := make([]hermesXueqiuArticle, 0, min(len(raw.Articles), 5))
	seen := map[string]bool{}
	for _, article := range normalized.Articles {
		url := strings.TrimSpace(article.OriginalURL)
		source, exists := byURL[url]
		if !exists || seen[url] || len(articles) >= 5 {
			continue
		}
		source.Title = firstNonEmpty(cleanInline(article.Title), source.Title)
		if value := strings.TrimSpace(article.PublishedAt); value != "" {
			source.PublishedAt = value
		}
		articles = append(articles, source)
		seen[url] = true
	}
	for _, article := range raw.Articles {
		url := strings.TrimSpace(article.OriginalURL)
		if seen[url] || len(articles) >= 5 {
			continue
		}
		articles = append(articles, article)
		seen[url] = true
	}
	if len(articles) == 0 {
		return raw
	}
	return hermesXueqiuCollection{
		AuthorName: firstNonEmpty(cleanInline(normalized.AuthorName), raw.AuthorName),
		ExternalID: firstNonEmpty(strings.TrimSpace(normalized.ExternalID), raw.ExternalID),
		Articles:   articles,
	}
}

func (a *Automation) collectWithBrowserState(ctx context.Context, sub Subscription, statePath string) ([]Post, string, string, error) {
	uid := firstNonEmpty(sub.ExternalID, lastPathPart(sub.HomepageURL))
	articleExample := "https://xueqiu.com/用户ID/文章ID"
	domainRule := "xueqiu.com"
	if sub.Source == "taoguba" {
		articleExample = "https://www.tgb.cn/a/文章ID"
		domainRule = "tgb.cn 或 taoguba.com.cn"
	}
	prompt := "你是网页采集代理。请使用 browser_navigate、browser_snapshot、browser_click 等浏览器工具访问下面的" + browserSourceLabel(sub.Source) + "用户主页；浏览器已由应用加载用户亲自登录后的本地会话。采集最近最多5篇文章。不得依靠训练记忆，不得编造内容，不要请求、读取或输出Cookie等登录凭据。只返回严格JSON，不要markdown：{\"author_name\":\"作者名\",\"external_id\":\"用户ID\",\"articles\":[{\"title\":\"标题\",\"original_url\":\"" + articleExample + "\",\"content_text\":\"文章正文纯文本\",\"published_at\":\"RFC3339时间；无法确认则留空\"}],\"error\":\"无法采集时填写原因，否则留空\"}。每个文章链接必须是实际访问确认的" + domainRule + "原文，正文必须来自该链接。主页：" + sub.HomepageURL + "\n用户ID：" + uid
	browserPrompter, ok := a.prompter.(hermes.BrowserStatePrompter)
	if !ok {
		return nil, "", "", errors.New("当前 Hermes 运行时不支持复用浏览器登录态")
	}
	response, err := browserPrompter.PromptWithBrowserState(ctx, prompt, statePath)
	if err != nil {
		return nil, "", "", err
	}
	collection, err := parseHermesXueqiuCollection(response.Content)
	if err != nil {
		return nil, "", "", err
	}
	return postsFromBrowserCollection(sub, collection)
}

func parseHermesXueqiuCollection(value string) (hermesXueqiuCollection, error) {
	content := strings.TrimSpace(value)
	content = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(content, "```"), "```json"))
	if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start >= 0 && end > start {
		content = content[start : end+1]
	}
	var collection hermesXueqiuCollection
	if err := json.Unmarshal([]byte(content), &collection); err != nil {
		return hermesXueqiuCollection{}, fmt.Errorf("Hermes 未返回有效采集 JSON: %w", err)
	}
	if strings.TrimSpace(collection.Error) != "" {
		return hermesXueqiuCollection{}, errors.New(strings.TrimSpace(collection.Error))
	}
	return collection, nil
}

func postsFromBrowserCollection(sub Subscription, collection hermesXueqiuCollection) ([]Post, string, string, error) {
	uid := firstNonEmpty(sub.ExternalID, lastPathPart(sub.HomepageURL))
	posts := make([]Post, 0, min(len(collection.Articles), 5))
	seen := map[string]bool{}
	for _, article := range collection.Articles {
		if len(posts) >= 5 {
			break
		}
		articleURL := strings.TrimSpace(article.OriginalURL)
		parsed, source, parseErr := classifyURL(articleURL)
		if parseErr != nil || source != sub.Source || parsed.Scheme != "https" || seen[parsed.String()] {
			continue
		}
		title := cleanInline(article.Title)
		body := strings.TrimSpace(article.ContentText)
		if title == "" || body == "" {
			continue
		}
		if len([]rune(body)) > 120000 {
			body = string([]rune(body)[:120000])
		}
		publishedAt := time.Now()
		if value := strings.TrimSpace(article.PublishedAt); value != "" {
			if parsedTime, timeErr := time.Parse(time.RFC3339, value); timeErr == nil {
				publishedAt = parsedTime
			} else if parsedTime, timeErr := time.ParseInLocation("2006-01-02 15:04", value, time.Local); timeErr == nil {
				publishedAt = parsedTime
			} else if parsedTime, timeErr := time.ParseInLocation("2006-01-02", value, time.Local); timeErr == nil {
				publishedAt = parsedTime
			}
		}
		post := newPost(sub.Source, parsed.String(), firstNonEmpty(collection.AuthorName, sub.Name), title, body, "", publishedAt)
		post.Digest = truncateRunes(body, 180)
		posts = append(posts, post)
		seen[parsed.String()] = true
	}
	if len(posts) == 0 {
		return nil, "", "", errors.New("Hermes 没有返回可验证的" + browserSourceLabel(sub.Source) + "文章")
	}
	return posts, cleanInline(collection.AuthorName), firstNonEmpty(strings.TrimSpace(collection.ExternalID), uid), nil
}

func (a *Automation) BrowserAuthReady(profileID string, sources ...string) bool {
	source := "xueqiu"
	if len(sources) > 0 && sources[0] == "taoguba" {
		source = "taoguba"
	}
	return browserStateLoggedIn(a.browserStatePath(profileID, source), source)
}

func browserSourceLabel(source string) string {
	if source == "taoguba" {
		return "淘股吧"
	}
	return "雪球"
}

func (a *Automation) browserStatePath(profileID string, sources ...string) string {
	if a.browserStateDir == "" || strings.TrimSpace(profileID) == "" {
		return ""
	}
	source := "xueqiu"
	if len(sources) > 0 && sources[0] == "taoguba" {
		source = "taoguba"
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(profileID))))[:32]
	return filepath.Join(a.browserStateDir, source, hash+".json")
}

func browserStateLoggedIn(statePath string, sources ...string) bool {
	if statePath == "" {
		return false
	}
	data, err := os.ReadFile(statePath)
	if err != nil || len(data) > 4<<20 {
		return false
	}
	var state struct {
		Cookies []struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Domain  string  `json:"domain"`
			Expires float64 `json:"expires"`
		} `json:"cookies"`
		Origins []struct {
			LocalStorage []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"localStorage"`
		} `json:"origins"`
	}
	if json.Unmarshal(data, &state) != nil {
		return false
	}
	source := "xueqiu"
	if len(sources) > 0 && sources[0] == "taoguba" {
		source = "taoguba"
	}
	if source == "taoguba" {
		for _, origin := range state.Origins {
			for _, item := range origin.LocalStorage {
				if (item.Name == "__easy_stock_taoguba_login_verified" || item.Name == "__a_stock_ai_taoguba_login_verified") && item.Value == "1" {
					return true
				}
			}
		}
	}
	now := float64(time.Now().Unix())
	values := map[string]string{}
	for _, cookie := range state.Cookies {
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
		isXueqiu := domain == "xueqiu.com" || strings.HasSuffix(domain, ".xueqiu.com")
		isTaoguba := domain == "tgb.cn" || strings.HasSuffix(domain, ".tgb.cn") || domain == "taoguba.com.cn" || strings.HasSuffix(domain, ".taoguba.com.cn")
		if (source == "xueqiu" && !isXueqiu) || (source == "taoguba" && !isTaoguba) {
			continue
		}
		if cookie.Expires > 0 && cookie.Expires <= now {
			continue
		}
		values[cookie.Name] = cookie.Value
	}
	if source == "xueqiu" {
		return values["xq_is_login"] == "1" || (values["u"] != "" && values["xq_id_token"] != "")
	}
	for name, value := range values {
		lower := strings.ToLower(strings.TrimSpace(name))
		if value != "" && (lower == "user" || lower == "userid" || lower == "user_id" || lower == "uid" || strings.Contains(lower, "login") || strings.Contains(lower, "auth") || strings.Contains(lower, "token")) {
			return true
		}
	}
	return false
}

func (a *Automation) discoverHTML(ctx context.Context, sub Subscription, profile appsettings.ReviewSourceProfile) ([]string, string, string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, sub.HomepageURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/124 Safari/537.36")
	if profile.Credential != "" {
		req.Header.Set("Cookie", profile.Credential)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", fmt.Errorf("作者主页返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArticleBytes))
	if err != nil {
		return nil, "", "", err
	}
	document := string(body)
	base, _ := url.Parse(sub.HomepageURL)
	links := []string{}
	for _, match := range regexp.MustCompile(`(?is)href=["']([^"']+)["']`).FindAllStringSubmatch(document, -1) {
		target, err := url.Parse(htmlUnescape(match[1]))
		if err != nil {
			continue
		}
		absolute := base.ResolveReference(target)
		host := strings.ToLower(absolute.Hostname())
		if (strings.HasSuffix(host, "taoguba.com.cn") || strings.HasSuffix(host, "tgb.cn")) && (strings.Contains(absolute.Path, "/a/") || strings.Contains(absolute.Path, "/Article/") || strings.Contains(absolute.Path, "/article/")) {
			links = append(links, absolute.String())
		}
	}
	name := cleanInline(firstNonEmpty(metaValue(document, "property", "og:title"), matchText(titlePattern, document), sub.Name))
	return uniqueStrings(links), name, sub.ExternalID, nil
}

func (a *Automation) AnalyzePost(ctx context.Context, id string) (Post, error) {
	post, err := a.store.GetPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	if a.prompter == nil {
		return a.store.SaveAnalysis(ctx, id, "", nil, "", "Hermes AI 分析底座不可用")
	}
	analysis, err := analyzeWithHermes(ctx, a.prompter, post)
	if err != nil {
		_, _ = a.store.SaveAnalysis(ctx, id, "", nil, "", err.Error())
		return Post{}, err
	}
	return a.store.SaveAnalysis(ctx, id, analysis.Summary, analysis.KeyPoints, analysis.Outlook, "")
}

func (a *Automation) nextRun(profile appsettings.ReviewSourceProfile) time.Time {
	hour := profile.SyncHour
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (a *Automation) profileFor(source, configID string) (appsettings.ReviewSourceProfile, error) {
	if a.settings != nil {
		profiles := a.settings.Snapshot().ReviewAutomation.Profiles
		for _, profile := range profiles {
			if profile.Source == source && profile.Enabled && profile.ID == configID {
				return a.withRuntimeProfile(profile), nil
			}
		}
		for _, profile := range profiles {
			if profile.Source == source && profile.Enabled {
				return a.withRuntimeProfile(profile), nil
			}
		}
	}
	if source == "wechat" && a.fallbackWechat != "" {
		return appsettings.ReviewSourceProfile{ID: "wechat-env", Source: "wechat", Name: "环境变量配置", BaseURL: a.fallbackWechat, SyncHour: 7, AutoAnalyze: true, Enabled: true}, nil
	}
	return appsettings.ReviewSourceProfile{}, fmt.Errorf("请先在设置中添加并启用%s采集配置", sourceName(source))
}

func (a *Automation) withRuntimeProfile(profile appsettings.ReviewSourceProfile) appsettings.ReviewSourceProfile {
	if profile.Source == "wechat" && a.fallbackWechat != "" {
		profile.BaseURL = a.fallbackWechat
		profile.Credential = ""
	}
	return profile
}
func (a *Automation) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			a.syncDue(syncCtx)
			cancel()
		}
	}
}

func (a *Automation) syncDue(ctx context.Context) []SyncResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	subs, err := a.store.ListSubscriptions(ctx)
	if err != nil {
		return []SyncResult{{Error: err.Error()}}
	}
	now := time.Now()
	results := []SyncResult{}
	for _, sub := range subs {
		if !sub.Enabled || subscriptionHasUnavailableWechatList(sub) || sub.NextSyncAt.After(now) {
			continue
		}
		results = append(results, a.syncOneUnlocked(ctx, sub))
	}
	return results
}

func (a *Automation) due(ctx context.Context) bool {
	subs, err := a.store.ListSubscriptions(ctx)
	if err != nil {
		return false
	}
	now := time.Now()
	for _, sub := range subs {
		if sub.Enabled && !subscriptionHasUnavailableWechatList(sub) && (sub.NextSyncAt.IsZero() || !sub.NextSyncAt.After(now)) {
			return true
		}
	}
	return false
}

func (a *Automation) getJSON(ctx context.Context, endpoint, token string, target any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var payload struct {
			Error string `json:"error"`
			Data  struct {
				Error string `json:"error"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &payload) == nil {
			if message := firstNonEmpty(payload.Error, payload.Data.Error); message != "" {
				return errors.New(message)
			}
		}
		return fmt.Errorf("内容服务返回 HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxArticleBytes)).Decode(target)
}
func (a *Automation) importWechat(ctx context.Context, base, token, link string) (Post, error) {
	body, _ := json.Marshal(map[string]string{"url": link})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/article", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return Post{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Title        string `json:"title"`
			PlainContent string `json:"plain_content"`
			Author       string `json:"author"`
			PublishTime  int64  `json:"publish_time"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxArticleBytes)).Decode(&payload); err != nil {
		return Post{}, err
	}
	if !payload.Success {
		return Post{}, errors.New(firstNonEmpty(payload.Error, "微信文章解析失败"))
	}
	published := time.Now()
	if payload.Data.PublishTime > 0 {
		published = time.Unix(payload.Data.PublishTime, 0)
	}
	return newPost("wechat", link, payload.Data.Author, payload.Data.Title, payload.Data.PlainContent, "", published), nil
}
func lastPathPart(path string) string {
	parts := strings.FieldsFunc(strings.Trim(path, "/"), func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
func htmlUnescape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "&amp;", "&"), "&#39;", "'")
}

type llmAnalysis struct {
	Summary   string   `json:"summary"`
	KeyPoints []string `json:"key_points"`
	Outlook   string   `json:"outlook"`
}

func analyzeWithHermes(ctx context.Context, prompter hermes.Prompter, post Post) (llmAnalysis, error) {
	prompt := "你是谨慎的A股复盘研究助手，只提炼原作者观点，不编造投资建议。请分析下面的A股复盘文章。只返回严格JSON：{\"summary\":\"200字内摘要\",\"key_points\":[\"要点\"],\"outlook\":\"作者对下一交易日或后市的预期；没有则写未明确\"}。不要添加markdown。\n标题：" + post.Title + "\n作者：" + post.AuthorName + "\n正文：" + truncateRunes(post.ContentText, 12000)
	response, err := prompter.Prompt(ctx, prompt)
	if err != nil {
		return llmAnalysis{}, fmt.Errorf("Hermes AI 提炼失败: %w", err)
	}
	content := strings.TrimSpace(response.Content)
	content = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(content, "```"), "```json"))
	if start, end := strings.Index(content, "{"), strings.LastIndex(content, "}"); start >= 0 && end > start {
		content = content[start : end+1]
	}
	var result llmAnalysis
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &result); err != nil {
		return llmAnalysis{}, fmt.Errorf("Hermes 未返回有效 JSON: %w", err)
	}
	result.KeyPoints = nonNilStrings(result.KeyPoints)
	return result, nil
}
