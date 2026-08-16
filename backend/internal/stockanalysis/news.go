package stockanalysis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const recentNewsWindowDays = 30

var positiveNewsKeywords = []string{
	"中标", "签约", "获批", "回购", "增持", "预增", "扭亏", "增长", "突破", "涨价",
	"订单", "扩产", "投产", "合作", "上调", "创新高", "景气", "政策支持", "超预期", "提价",
}

var negativeNewsKeywords = []string{
	"减持", "立案", "调查", "处罚", "亏损", "下滑", "风险", "终止", "解禁", "诉讼",
	"退市", "问询", "警示", "跌停", "下调", "违约", "事故", "澄清", "不及预期", "监管",
}

func analyzeRecentNews(input Input, theme ThemeAnalysis) (NewsAnalysis, NewsAnalysis) {
	stockTerms := uniqueNewsTerms([]string{input.Quote.Name, strings.Split(input.Symbol, ".")[0]}, 4)
	themeTerms := []string{theme.HotTheme, theme.Primary, theme.BusinessTheme, input.Industry}
	themeTerms = append(themeTerms, theme.Concepts...)
	themeTerms = uniqueNewsTerms(themeTerms, 8)

	stockCandidates := make([]foundation.NewsItem, 0, len(input.News)+len(input.Announcements))
	for _, item := range input.Announcements {
		stockCandidates = append(stockCandidates, foundation.NewsItem{
			ID:          item.ID,
			Title:       item.Title,
			Content:     item.Content,
			URL:         item.URL,
			PublishedAt: item.PublishedAt,
			Tags:        uniqueStrings([]string{"公司公告", item.Category}, 3),
			Meta:        item.Meta,
		})
	}
	stockCandidates = append(stockCandidates, filterNewsByTerms(input.News, stockTerms)...)
	stockItems := recentUniqueNews(stockCandidates, recentNewsWindowDays)
	themeItems := recentUniqueNews(filterNewsByTerms(input.News, themeTerms), recentNewsWindowDays)

	return buildNewsAnalysis("个股", stockItems, matchedNewsTerms(stockItems, stockTerms)),
		buildNewsAnalysis("题材", themeItems, matchedNewsTerms(themeItems, themeTerms))
}

func buildNewsAnalysis(scope string, items []foundation.NewsItem, keywords []string) NewsAnalysis {
	analysis := NewsAnalysis{
		WindowDays:     recentNewsWindowDays,
		Tone:           "信息不足",
		Catalysts:      []string{},
		Risks:          []string{},
		Keywords:       keywords,
		Articles:       []foundation.NewsItem{},
		AnalysisSource: "local-rules",
	}
	if len(items) == 0 {
		analysis.Summary = fmt.Sprintf("近%d日暂未检索到可验证的%s新闻，当前结论不对新闻催化作额外加分。", recentNewsWindowDays, scope)
		return analysis
	}

	sources := map[string]bool{}
	positiveCount := 0
	negativeCount := 0
	for _, item := range items {
		source := strings.TrimSpace(item.Meta.Source)
		if source != "" {
			sources[source] = true
		}
		text := strings.ToLower(item.Title + " " + item.Content)
		positive := containsAnyFold(text, positiveNewsKeywords...)
		negative := containsAnyFold(text, negativeNewsKeywords...)
		if positive && !negative {
			positiveCount++
			analysis.Catalysts = append(analysis.Catalysts, truncateText(item.Title, 80))
		}
		if negative {
			negativeCount++
			analysis.Risks = append(analysis.Risks, truncateText(item.Title, 80))
		}
	}
	analysis.Available = true
	analysis.ArticleCount = len(items)
	analysis.SourceCount = len(sources)
	if !items[0].PublishedAt.IsZero() {
		analysis.LatestAt = items[0].PublishedAt.Format(time.RFC3339)
	}
	analysis.Catalysts = uniqueStrings(analysis.Catalysts, 3)
	analysis.Risks = uniqueStrings(analysis.Risks, 3)
	analysis.Articles = append([]foundation.NewsItem(nil), items[:min(len(items), 6)]...)

	difference := positiveCount - negativeCount
	switch {
	case difference >= 2:
		analysis.Tone = "偏多"
	case difference <= -2:
		analysis.Tone = "偏空"
	default:
		analysis.Tone = "中性"
	}
	analysis.Summary = fmt.Sprintf("近%d日检索到%d条%s新闻，来自%d个数据源；关键词识别到%d条潜在催化、%d条风险信号，整体%s。事件是否已被股价反映，仍需结合量价和后续落地验证。", recentNewsWindowDays, len(items), scope, len(sources), positiveCount, negativeCount, analysis.Tone)
	return analysis
}

func filterNewsByTerms(items []foundation.NewsItem, terms []string) []foundation.NewsItem {
	if len(terms) == 0 {
		return nil
	}
	result := make([]foundation.NewsItem, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(item.Title + " " + item.Content + " " + strings.Join(item.Tags, " "))
		for _, term := range terms {
			if strings.Contains(haystack, strings.ToLower(term)) {
				result = append(result, item)
				break
			}
		}
	}
	return result
}

func recentUniqueNews(items []foundation.NewsItem, windowDays int) []foundation.NewsItem {
	cutoff := time.Now().AddDate(0, 0, -windowDays)
	result := make([]foundation.NewsItem, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		if item.Title == "" || (!item.PublishedAt.IsZero() && item.PublishedAt.Before(cutoff)) {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(item.Title), ""))
		if seen[key] {
			continue
		}
		seen[key] = true
		item.Title = truncateText(item.Title, 120)
		item.Content = truncateText(item.Content, 280)
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].PublishedAt.After(result[j].PublishedAt)
	})
	return result
}

func uniqueNewsTerms(values []string, limit int) []string {
	result := make([]string, 0, min(len(values), limit))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		length := len([]rune(value))
		if length < 2 || length > 18 || containsAnyFold(value, "公司主营业务", "所属行业", "暂无", "待确认") {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func matchedNewsTerms(items []foundation.NewsItem, terms []string) []string {
	matched := make([]string, 0, min(len(terms), 5))
	for _, term := range terms {
		for _, item := range items {
			haystack := strings.ToLower(item.Title + " " + item.Content + " " + strings.Join(item.Tags, " "))
			if strings.Contains(haystack, strings.ToLower(term)) {
				matched = append(matched, term)
				break
			}
		}
	}
	return uniqueStrings(matched, 5)
}

func firstNewsSource(items []foundation.NewsItem) string {
	for _, item := range items {
		if source := strings.TrimSpace(item.Meta.Source); source != "" {
			return source
		}
	}
	return "近期新闻"
}

func newsAnalysisDate(analysis *NewsAnalysis) string {
	if analysis == nil {
		return ""
	}
	return analysis.LatestAt
}
