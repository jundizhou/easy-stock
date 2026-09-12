package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/providers/xueqiu"
)

type xueqiuHotStockView struct {
	Rank       int     `json:"rank"`
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	Percent    float64 `json:"percent"`
	RankChange int     `json:"rank_change"`
}

type xueqiuHotUserView struct {
	ScreenName string `json:"screen_name"`
	Followers  int64  `json:"followers_count"`
	Verified   bool   `json:"verified"`
	Intro      string `json:"description,omitempty"`
}

// xueqiuHotStocks 返回雪球 A 股热榜（社区讨论热度排名，与东财人气榜互证）。
func (s *Server) xueqiuHotStocks(w http.ResponseWriter, r *http.Request) {
	if s.xueqiu == nil {
		writeError(w, http.StatusServiceUnavailable, "雪球数据源不可用")
		return
	}
	size := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 50 {
			writeError(w, http.StatusBadRequest, "size must be between 1 and 50")
			return
		}
		size = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := s.xueqiu.HotStocks(ctx, size)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	views := make([]xueqiuHotStockView, 0, len(list))
	for _, item := range list {
		views = append(views, xueqiuHotStockView{
			Rank: item.Rank, Symbol: item.Symbol, Name: item.Name,
			Price: item.Current, Percent: item.Percent,
			RankChange: item.RankChange,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "meta": map[string]string{"source": "xueqiu:hot"}})
}

// xueqiuHotUsers 返回讨论某只 A 股最热的雪球用户（KOL 视角的社区关注度）。
func (s *Server) xueqiuHotUsers(w http.ResponseWriter, r *http.Request) {
	if s.xueqiu == nil {
		writeError(w, http.StatusServiceUnavailable, "雪球数据源不可用")
		return
	}
	symbolParam := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbolParam == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	normalized, err := foundation.NormalizeSymbol(symbolParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	count := 8
	if raw := strings.TrimSpace(r.URL.Query().Get("count")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 || parsed > 20 {
			writeError(w, http.StatusBadRequest, "count must be between 1 and 20")
			return
		}
		count = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	users, err := s.xueqiu.HotUsers(ctx, normalized.Canonical, count)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	views := make([]xueqiuHotUserView, 0, len(users))
	for _, user := range users {
		views = append(views, xueqiuHotUserView{
			ScreenName: user.ScreenName, Followers: user.Followers,
			Verified: user.Verified, Intro: user.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": views, "meta": map[string]string{"source": "xueqiu:hot-users", "symbol": normalized.Canonical}})
}

// xueqiuNewsItems 把雪球 7×24 快讯映射为项目标准 NewsItem（财联社的备源）。
func xueqiuNewsItems(items []xueqiu.News) []foundation.NewsItem {
	news := make([]foundation.NewsItem, 0, len(items))
	for _, item := range items {
		title := stripXueqiuTags(item.Text)
		if title == "" {
			continue
		}
		view := foundation.NewsItem{
			ID:      item.ID.String(),
			Title:   title,
			Content: title,
			URL:     item.Target,
			Tags:    []string{},
		}
		if item.Mark == 1 {
			view.Tags = append(view.Tags, "important")
		}
		if item.CreatedAt > 0 {
			view.PublishedAt = time.UnixMilli(item.CreatedAt)
		}
		news = append(news, view)
	}
	return news
}

func stripXueqiuTags(text string) string {
	// 雪球快讯正文带少量 HTML 标签（<a>/<b>/<br>），按 dsh-xueqiu 的口径清成纯文本。
	inner := text
	for {
		start := strings.IndexAny(inner, "<")
		if start < 0 {
			break
		}
		end := strings.Index(inner[start:], ">")
		if end < 0 {
			break
		}
		inner = inner[:start] + " " + inner[start+end+1:]
	}
	return strings.TrimSpace(strings.Join(strings.Fields(inner), " "))
}

// xueqiuHotSourceLoader 把雪球热榜适配为 hotstock 的附加源：与同花顺、东财
// 并行加载，按同一套 consensus 规则参与综合排名。
func xueqiuHotSourceLoader(client *xueqiu.Client) func(context.Context, int) foundation.HotStockRankList {
	return func(ctx context.Context, limit int) foundation.HotStockRankList {
		list := foundation.HotStockRankList{Source: "xueqiu", SourceName: "雪球"}
		if client == nil {
			list.Error = "雪球客户端未初始化"
			return list
		}
		hot, err := client.HotStocks(ctx, limit)
		if err != nil {
			list.Error = "雪球热榜暂不可用"
			return list
		}
		list.FetchedAt = time.Now()
		list.Items = make([]foundation.HotStockRankItem, 0, len(hot))
		for _, item := range hot {
			symbol, ok := normalizeXueqiuRankSymbol(item.Symbol)
			if !ok {
				continue
			}
			list.Items = append(list.Items, foundation.HotStockRankItem{
				Symbol: symbol, Name: strings.TrimSpace(item.Name), Rank: item.Rank,
			})
		}
		if len(list.Items) == 0 {
			list.Error = "雪球热榜未返回股票"
		}
		return list
	}
}

// normalizeXueqiuRankSymbol 把雪球代码（SH600519/SZ000636）转成规范形（600519.SH）。
func normalizeXueqiuRankSymbol(raw string) (string, bool) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if len(value) != 8 {
		return "", false
	}
	suffix, code := value[:2], value[2:]
	switch suffix {
	case "SH", "SZ", "BJ":
	default:
		return "", false
	}
	normalized, err := foundation.NormalizeSymbol(code)
	if err != nil {
		return "", false
	}
	return normalized.Canonical, true
}

// ---- 雪球个股讨论 + 情绪归纳 ----

type xueqiuPostView struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	ReplyCount int    `json:"reply_count"`
	ViewCount  int64  `json:"view_count"`
	CreatedAt  string `json:"created_at,omitempty"`
	UserName   string `json:"user_name,omitempty"`
	UserFollow int64  `json:"user_followers,omitempty"`
	URL        string `json:"url,omitempty"`
}

type xueqiuSentiment struct {
	Bullish int    `json:"bullish"`
	Neutral int    `json:"neutral"`
	Bearish int    `json:"bearish"`
	Summary string `json:"summary"`
	Source  string `json:"source"` // hermes-ai / local-rules
}

type xueqiuDiscussionsPayload struct {
	Symbol    string           `json:"symbol"`
	Name      string           `json:"name,omitempty"`
	Posts     []xueqiuPostView `json:"posts"`
	Sentiment xueqiuSentiment  `json:"sentiment"`
}

// xueqiuDiscussions 返回雪球社区关于某只 A 股的最新讨论帖，并附情绪归纳：
// Hermes 可用且样本充足时用 AI 归纳，否则本地关键词规则兜底。
func (s *Server) xueqiuDiscussions(w http.ResponseWriter, r *http.Request) {
	if s.xueqiu == nil {
		writeError(w, http.StatusServiceUnavailable, "雪球数据源不可用")
		return
	}
	symbolParam := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbolParam == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	normalized, err := foundation.NormalizeSymbol(symbolParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	count := 12
	if raw := strings.TrimSpace(r.URL.Query().Get("count")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 5 && parsed <= 30 {
			count = parsed
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	name := normalized.Canonical
	if s.stockDirectory != nil {
		if directory, loadErr := s.stockDirectories.load(ctx, s.stockDirectory); loadErr == nil {
			for _, entry := range directory.Stocks {
				if entry.Symbol == normalized.Canonical && strings.TrimSpace(entry.Name) != "" {
					name = strings.TrimSpace(entry.Name)
					break
				}
			}
		}
	}

	posts, err := s.xueqiu.StockPosts(ctx, name, count)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	views := make([]xueqiuPostView, 0, len(posts))
	for _, post := range posts {
		view := xueqiuPostView{
			ID: post.ID.String(), Text: post.Text,
			ReplyCount: post.ReplyCount, ViewCount: post.ViewCount,
			UserName: post.UserName, UserFollow: post.UserFollow, URL: post.Target,
		}
		if post.CreatedAt > 0 {
			view.CreatedAt = time.UnixMilli(post.CreatedAt).Format(time.RFC3339)
		}
		views = append(views, view)
	}

	sentiment := localXueqiuSentiment(posts)
	if s.hermesGateway != nil && len(posts) >= 5 {
		if status := s.hermesGateway.Status(); status.Available && status.Configured {
			if ai, aiErr := s.xueqiuAISentiment(r.Context(), name, posts); aiErr == nil {
				sentiment = ai
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": xueqiuDiscussionsPayload{Symbol: normalized.Canonical, Name: name, Posts: views, Sentiment: sentiment},
		"meta": map[string]string{"source": "xueqiu:discussions"},
	})
}

// ---- 情绪归纳：本地关键词规则 ----

var xueqiuBullishWords = []string{"看多", "买入", "加仓", "做多", "满仓", "抄底", "目标价", "看涨", "主升", "拿住", "新高", "连板", "龙头", "突破"}
var xueqiuBearishWords = []string{"看空", "卖出", "清仓", "减持", "减仓", "割肉", "止损", "风险", "见顶", "崩", "跌停", "避雷", "套牢", "出货"}

func localXueqiuSentiment(posts []xueqiu.Post) xueqiuSentiment {
	sentiment := xueqiuSentiment{Source: "local-rules"}
	bullHits, bearHits := map[string]int{}, map[string]int{}
	for _, post := range posts {
		text := post.Text
		bullish, bearish := 0, 0
		for _, word := range xueqiuBullishWords {
			if strings.Contains(text, word) {
				bullish++
				bullHits[word]++
			}
		}
		for _, word := range xueqiuBearishWords {
			if strings.Contains(text, word) {
				bearish++
				bearHits[word]++
			}
		}
		switch {
		case bullish > bearish:
			sentiment.Bullish++
		case bearish > bullish:
			sentiment.Bearish++
		default:
			sentiment.Neutral++
		}
	}
	sentiment.Summary = fmt.Sprintf("共 %d 条讨论：看多 %d、看空 %d、中性 %d（本地关键词规则归纳）", len(posts), sentiment.Bullish, sentiment.Bearish, sentiment.Neutral)
	if top := topHits(bullHits); top != "" {
		sentiment.Summary += "；多头高频词：" + top
	}
	if top := topHits(bearHits); top != "" {
		sentiment.Summary += "；空头高频词：" + top
	}
	return sentiment
}

func topHits(hits map[string]int) string {
	best, bestCount := "", 0
	for word, count := range hits {
		if count > bestCount {
			best, bestCount = word, count
		}
	}
	return best
}

// ---- 情绪归纳：Hermes AI（结果缓存 30 分钟）----

type xueqiuSentimentCacheEntry struct {
	at    time.Time
	value xueqiuSentiment
}

var (
	xueqiuSentimentMu    sync.Mutex
	xueqiuSentimentCache = map[string]xueqiuSentimentCacheEntry{}
)

func (s *Server) xueqiuAISentiment(ctx context.Context, name string, posts []xueqiu.Post) (xueqiuSentiment, error) {
	fingerprint := name + "|" + firstNonEmptyString(posts[0].ID.String(), strconv.Itoa(len(posts)))
	xueqiuSentimentMu.Lock()
	if entry, ok := xueqiuSentimentCache[fingerprint]; ok && time.Since(entry.at) < 30*time.Minute {
		xueqiuSentimentMu.Unlock()
		return entry.value, nil
	}
	xueqiuSentimentMu.Unlock()

	var builder strings.Builder
	builder.WriteString("你是 A 股投研助手。以下是雪球社区关于「" + name + "」的最新讨论帖，请判断整体社区情绪，输出严格 JSON（不要 markdown 代码块）：\n")
	builder.WriteString(`{"bullish":看多帖子数,"neutral":中性帖子数,"bearish":看空帖子数,"summary":"60字以内情绪归纳，概括多空理由与主要分歧"}` + "\n")
	builder.WriteString("分类标准：明确表达买入/持有/看涨倾向计入 bullish；卖出/回避/看跌计入 bearish；提问或中性讨论计入 neutral。三个数字之和应等于帖子总数。\n\n")
	for index, post := range posts {
		if index >= 12 {
			break
		}
		text := post.Text
		runes := []rune(text)
		if len(runes) > 120 {
			text = string(runes[:120])
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n", index+1, text))
	}
	response, err := hermes.PromptFullyAuthorized(ctx, s.hermesGateway, builder.String())
	if err != nil {
		return xueqiuSentiment{}, err
	}
	var parsed struct {
		Bullish int    `json:"bullish"`
		Neutral int    `json:"neutral"`
		Bearish int    `json:"bearish"`
		Summary string `json:"summary"`
	}
	content := strings.TrimSpace(response.Content)
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return xueqiuSentiment{}, errors.New("AI 未返回 JSON")
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &parsed); err != nil {
		return xueqiuSentiment{}, err
	}
	if parsed.Bullish+parsed.Neutral+parsed.Bearish <= 0 || strings.TrimSpace(parsed.Summary) == "" {
		return xueqiuSentiment{}, errors.New("AI 归纳缺少有效字段")
	}
	result := xueqiuSentiment{Bullish: parsed.Bullish, Neutral: parsed.Neutral, Bearish: parsed.Bearish, Summary: strings.TrimSpace(parsed.Summary), Source: "hermes-ai"}
	xueqiuSentimentMu.Lock()
	xueqiuSentimentCache[fingerprint] = xueqiuSentimentCacheEntry{at: time.Now(), value: result}
	// 缓存容量兜底：超过 200 条时清掉 30 分钟以前的条目
	if len(xueqiuSentimentCache) > 200 {
		for key, entry := range xueqiuSentimentCache {
			if time.Since(entry.at) > 30*time.Minute {
				delete(xueqiuSentimentCache, key)
			}
		}
	}
	xueqiuSentimentMu.Unlock()
	return result, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
