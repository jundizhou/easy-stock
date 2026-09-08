package stockanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
)

const (
	maxThemeAIAnnouncements = 8
	maxThemeAINews          = 6
	maxThemeAIMarketThemes  = 10
	maxThemeAIResults       = 5
	maxThemeAISnippetRunes  = 320
)

type ThemeEvidencePromptStats struct {
	AnnouncementInput      int
	AnnouncementCandidates int
	NewsInput              int
	NewsCandidates         int
	ThemeInput             int
	ThemeCandidates        int
	PromptBytes            int
	ToolMode               string
}

type ThemeEvidenceAttempt struct {
	Number        int
	DurationMS    int64
	PromptBytes   int
	ResponseBytes int
	Error         string
}

// ThemeEvidencePrompt is an opaque, prefiltered request. HTTP callers can log
// its bounded sizes before the model starts without exposing prompt contents.
type ThemeEvidencePrompt struct {
	prompt  string
	sources map[string]themeAISource
	stats   ThemeEvidencePromptStats
}

func (p ThemeEvidencePrompt) Empty() bool                     { return strings.TrimSpace(p.prompt) == "" }
func (p ThemeEvidencePrompt) Stats() ThemeEvidencePromptStats { return p.stats }

type themeHint struct {
	name  string
	score int
}

type rankedThemeSource struct {
	kind            string
	title           string
	content         string
	category        string
	source          string
	url             string
	publishedAt     time.Time
	score           int
	candidateThemes []string
}

type themeAISource struct {
	ID              string    `json:"source_id"`
	Kind            string    `json:"kind"`
	Date            string    `json:"date,omitempty"`
	Title           string    `json:"title"`
	Category        string    `json:"category,omitempty"`
	Snippet         string    `json:"snippet"`
	CandidateThemes []string  `json:"candidate_themes"`
	Source          string    `json:"-"`
	URL             string    `json:"-"`
	PublishedAt     time.Time `json:"-"`
}

type themeAIMarketTheme struct {
	Name            string  `json:"name"`
	TrendScore      int     `json:"trend_score"`
	Stage           string  `json:"stage,omitempty"`
	ChangePercent   float64 `json:"change_percent"`
	FiveDayStrength int     `json:"five_day_strength"`
	LimitUpCount    int     `json:"limit_up_count"`
}

type aiThemeEvidenceItem struct {
	Theme     string  `json:"theme"`
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Relation  string  `json:"relation"`
	Direction string  `json:"direction"`
	Strength  float64 `json:"strength"`
}

type aiThemeEvidenceResponse struct {
	Items []aiThemeEvidenceItem `json:"items"`
}

// PrepareThemeEvidencePrompt performs all expensive breadth reduction locally:
// source ranking, title deduplication, exact snippet selection and market-theme
// matching. The model receives only the small classification problem.
func PrepareThemeEvidencePrompt(input Input) ThemeEvidencePrompt {
	hints := collectThemeHints(input)
	marketThemes := selectThemeAIMarketThemes(input, hints)
	announcements := selectThemeAIAnnouncements(input, hints, marketThemes)
	news := selectThemeAINews(input, hints, marketThemes)

	prepared := ThemeEvidencePrompt{
		sources: map[string]themeAISource{},
		stats: ThemeEvidencePromptStats{
			AnnouncementInput:      len(input.Announcements),
			AnnouncementCandidates: len(announcements),
			NewsInput:              len(input.News),
			NewsCandidates:         len(news),
			ThemeInput:             len(input.Themes),
			ThemeCandidates:        len(marketThemes),
			ToolMode:               "disabled",
		},
	}
	if len(announcements) == 0 && len(news) == 0 {
		return prepared
	}

	sources := make([]themeAISource, 0, len(announcements)+len(news))
	appendSources := func(prefix string, items []rankedThemeSource) {
		for index, item := range items {
			id := fmt.Sprintf("%s%d", prefix, index+1)
			source := themeAISource{
				ID:              id,
				Kind:            item.kind,
				Date:            formatThemeAIDate(item.publishedAt),
				Title:           strings.TrimSpace(item.title),
				Category:        strings.TrimSpace(item.category),
				Snippet:         selectThemeAISnippet(item.content, item.title, item.candidateThemes),
				CandidateThemes: append([]string(nil), item.candidateThemes...),
				Source:          item.source,
				URL:             item.url,
				PublishedAt:     item.publishedAt,
			}
			prepared.sources[id] = source
			sources = append(sources, source)
		}
	}
	appendSources("a", announcements)
	appendSources("n", news)

	cached := make([]map[string]string, 0, 6)
	for _, item := range input.CachedThemes {
		if item.Symbol != input.Symbol || strings.TrimSpace(item.Theme) == "" {
			continue
		}
		cached = append(cached, map[string]string{"date": item.TradeDate, "theme": item.Theme, "role": item.Role, "source": item.Source})
		if len(cached) >= 6 {
			break
		}
	}
	limitUps := make([]map[string]any, 0, 6)
	for _, item := range input.LimitUps {
		if item.Symbol != input.Symbol {
			continue
		}
		limitUps = append(limitUps, map[string]any{"date": item.Date.Format("2006-01-02"), "theme": item.PrimaryTheme, "concepts": item.Concepts, "streak": item.Streak})
		if len(limitUps) >= 6 {
			break
		}
	}
	priceFeedback := map[string]float64{}
	if lines := normalizeKLines(input.KLines); len(lines) > 0 {
		daily, five := stockRecentReturns(lines)
		priceFeedback = map[string]float64{
			"daily_percent":      round2(daily),
			"five_day_percent":   round2(five),
			"twenty_day_percent": round2(windowReturn(closesOf(lines), min(len(lines), 20))),
		}
	}
	payload := map[string]any{
		"stock": map[string]any{
			"symbol": input.Symbol, "name": input.Quote.Name,
			"business": firstNonEmpty(input.Business, input.Industry),
			"concepts": input.Concepts,
		},
		"price_feedback":      priceFeedback,
		"cached_attributions": cached,
		"recent_limit_ups":    limitUps,
		"market_themes":       marketThemes,
		"sources":             sources,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return prepared
	}
	prepared.prompt = `你是A股题材证据分类器。本地代码已经筛选来源、截取可回查原文并给出候选题材；你只负责判断关系，不搜索、不调用工具、不补充输入之外的事实。
规则：
1. 最多输出5项，按可信度排序；没有可靠关系时返回{"items":[]}。
2. source_id必须逐字使用sources中的编号；theme必须来自该来源的candidate_themes，不得自创新题材。
3. type只能是fact、market_mapping、inference；relation只能是own_business、subsidiary、equity_investment、customer_supplier、disposed_asset、market_mapping、unknown；direction只能是positive、negative、neutral。
4. 区分上市公司、子公司、参股公司、客户和交易标的。出售、处置、退出、注销、否认相关业务或未形成收入不能包装成当前正向事实。
5. 概念目录、市场热度和价格反馈只用于校验，不能单独证明公司业务。不要返回source、title或snippet，它们由本地代码按source_id恢复。
6. 只输出JSON：{"items":[{"theme":"候选题材","source_id":"a1","type":"fact|market_mapping|inference","relation":"own_business|subsidiary|equity_investment|customer_supplier|disposed_asset|market_mapping|unknown","direction":"positive|negative|neutral","strength":0.0}]}

[输入]
` + string(encoded)
	prepared.stats.PromptBytes = len([]byte(prepared.prompt))
	return prepared
}

func ExtractPreparedThemeEvidence(ctx context.Context, prompter hermes.Prompter, prepared ThemeEvidencePrompt) ([]ThemeEvidence, []ThemeEvidenceAttempt, error) {
	if prompter == nil {
		return nil, nil, fmt.Errorf("AI分析底座不可用")
	}
	if prepared.Empty() {
		return nil, nil, nil
	}
	attempts := make([]ThemeEvidenceAttempt, 0, 1)
	decoded, err := promptJSONObjectWithOptions[aiThemeEvidenceResponse](ctx, prompter, prepared.prompt, "题材证据", promptJSONObjectOptions{
		maxAttempts:  1,
		disableTools: true,
		onAttempt: func(item promptJSONAttempt) {
			attempts = append(attempts, ThemeEvidenceAttempt{
				Number: item.number, DurationMS: item.durationMS, PromptBytes: item.promptBytes,
				ResponseBytes: item.responseBytes, Error: item.err,
			})
		},
	})
	if err != nil {
		return nil, attempts, err
	}

	out := make([]ThemeEvidence, 0, min(len(decoded.Items), maxThemeAIResults))
	seen := map[string]bool{}
	for _, item := range decoded.Items {
		source, ok := prepared.sources[strings.TrimSpace(item.SourceID)]
		if !ok {
			continue
		}
		item.Theme = strings.TrimSpace(item.Theme)
		if item.Theme == "" || !themeAINameAllowed(item.Theme, source.CandidateThemes) {
			continue
		}
		key := source.ID + "|" + canonicalTheme(item.Theme)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ThemeEvidence{
			Theme:       canonicalTheme(item.Theme),
			Type:        normalizeThemeEvidenceType(item.Type),
			Relation:    normalizeThemeEvidenceRelation(item.Relation),
			Direction:   normalizeThemeEvidenceDirection(item.Direction),
			Source:      firstNonEmpty(source.Source, "hermes-ai"),
			Title:       source.Title,
			URL:         source.URL,
			PublishedAt: source.PublishedAt,
			Snippet:     source.Snippet,
			Strength:    clamp(item.Strength, .15, .98),
			Freshness:   freshnessForTime(source.PublishedAt),
		})
		if len(out) >= maxThemeAIResults {
			break
		}
	}
	return out, attempts, nil
}

func ExtractThemeEvidence(ctx context.Context, prompter hermes.Prompter, input Input) ([]ThemeEvidence, error) {
	items, _, err := ExtractPreparedThemeEvidence(ctx, prompter, PrepareThemeEvidencePrompt(input))
	return items, err
}

func collectThemeHints(input Input) []themeHint {
	scores := map[string]int{}
	labels := map[string]string{}
	add := func(name string, score int) {
		name = strings.TrimSpace(name)
		canonical := canonicalTheme(name)
		if canonical == "" {
			return
		}
		if score > scores[canonical] {
			scores[canonical] = score
			labels[canonical] = canonical
		}
	}
	for _, item := range input.CachedThemes {
		if item.Symbol == input.Symbol {
			add(item.Theme, 100)
			for _, concept := range item.Concepts {
				add(concept, 85)
			}
		}
	}
	for _, item := range input.LimitUps {
		if item.Symbol == input.Symbol {
			add(item.PrimaryTheme, 95)
			for _, concept := range item.Concepts {
				add(concept, 78)
			}
		}
	}
	for _, concept := range input.Concepts {
		add(concept, 72)
	}
	add(input.Industry, 38)

	corpus := strings.Join([]string{input.Business, input.BusinessDetail, input.Industry, strings.Join(input.Concepts, " ")}, " ")
	for _, item := range input.Announcements {
		corpus += " " + item.Title + " " + item.Content
	}
	for _, item := range input.News {
		if containsAnyFold(item.Title+" "+item.Content, input.Quote.Name, strings.Split(input.Symbol, ".")[0]) {
			corpus += " " + item.Title + " " + item.Content
		}
	}
	for _, group := range themeKeywordGroups {
		if containsAnyFold(corpus, append([]string{group.name}, group.keywords...)...) {
			add(group.name, 82)
		}
	}
	for _, item := range input.Themes {
		name := firstNonEmpty(item.Name, item.Theme)
		if name != "" && themeTextContains(corpus, name) {
			add(name, 76)
		}
	}

	hints := make([]themeHint, 0, len(scores))
	for canonical, score := range scores {
		hints = append(hints, themeHint{name: labels[canonical], score: score})
	}
	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].score != hints[j].score {
			return hints[i].score > hints[j].score
		}
		return hints[i].name < hints[j].name
	})
	return hints
}

func selectThemeAIMarketThemes(input Input, hints []themeHint) []themeAIMarketTheme {
	type ranked struct {
		item  foundation.ThemeOverview
		name  string
		score int
	}
	items := make([]ranked, 0, len(input.Themes))
	for _, item := range input.Themes {
		name := strings.TrimSpace(firstNonEmpty(item.Name, item.Theme))
		if name == "" {
			continue
		}
		relevance := 0
		for _, hint := range hints {
			if themeMatches(hint.name, item) || canonicalTheme(hint.name) == canonicalTheme(name) {
				relevance = max(relevance, hint.score+100)
			}
		}
		if relevance == 0 {
			continue
		}
		strength := item.TrendScore + item.FiveDayStrengthScore/2 + item.DailyStrengthScore/3 + min(item.LimitUpCount*4, 24)
		items = append(items, ranked{item: item, name: name, score: relevance + strength})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].name < items[j].name
	})
	out := make([]themeAIMarketTheme, 0, min(len(items), maxThemeAIMarketThemes))
	seen := map[string]bool{}
	for _, ranked := range items {
		canonical := canonicalTheme(ranked.name)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, themeAIMarketTheme{
			Name: canonical, TrendScore: ranked.item.TrendScore, Stage: ranked.item.TrendStage,
			ChangePercent: round2(ranked.item.ChangePercent), FiveDayStrength: ranked.item.FiveDayStrengthScore,
			LimitUpCount: ranked.item.LimitUpCount,
		})
		if len(out) >= maxThemeAIMarketThemes {
			break
		}
	}
	return out
}

func selectThemeAIAnnouncements(input Input, hints []themeHint, marketThemes []themeAIMarketTheme) []rankedThemeSource {
	items := make([]rankedThemeSource, 0, len(input.Announcements))
	for _, item := range input.Announcements {
		text := strings.TrimSpace(item.Title + " " + item.Category + " " + item.Content)
		if text == "" {
			continue
		}
		candidates := matchingThemeCandidates(text, hints, marketThemes)
		if len(candidates) == 0 {
			continue
		}
		score := 80 + themeSourceFreshnessScore(item.PublishedAt) + len(candidates)*24
		if containsAnyFold(text, themeCompanyFactKeywords...) {
			score += 18
		}
		negativeKeywords := append(append([]string(nil), themeNegativeFactKeywords...), "出售", "转让", "处置", "退出投资", "注销")
		if containsAnyFold(text, negativeKeywords...) {
			score += 20
		}
		if containsAnyFold(item.Category+" "+item.Title, "重大事项", "投资", "收购", "合作", "项目", "订单", "中标", "经营") {
			score += 12
		}
		items = append(items, rankedThemeSource{
			kind: "announcement", title: item.Title, content: item.Content, category: item.Category,
			source: firstNonEmpty(item.Meta.Source, "eastmoney:announcement"), url: item.URL,
			publishedAt: item.PublishedAt, score: score, candidateThemes: candidates,
		})
	}
	return limitAndDeduplicateThemeSources(items, maxThemeAIAnnouncements)
}

func selectThemeAINews(input Input, hints []themeHint, marketThemes []themeAIMarketTheme) []rankedThemeSource {
	items := make([]rankedThemeSource, 0, len(input.News))
	stockCode := strings.Split(input.Symbol, ".")[0]
	for _, item := range input.News {
		text := strings.TrimSpace(item.Title + " " + item.Content + " " + strings.Join(item.Tags, " "))
		if text == "" || !containsAnyFold(text, input.Quote.Name, stockCode) {
			continue
		}
		candidates := matchingThemeCandidates(text, hints, marketThemes)
		if len(candidates) == 0 {
			continue
		}
		score := 95 + themeSourceFreshnessScore(item.PublishedAt) + len(candidates)*24
		if containsAnyFold(text, themeCompanyFactKeywords...) {
			score += 12
		}
		items = append(items, rankedThemeSource{
			kind: "news", title: item.Title, content: item.Content,
			source: firstNonEmpty(item.Meta.Source, "market-news"), url: item.URL,
			publishedAt: item.PublishedAt, score: score, candidateThemes: candidates,
		})
	}
	return limitAndDeduplicateThemeSources(items, maxThemeAINews)
}

func matchingThemeCandidates(text string, hints []themeHint, marketThemes []themeAIMarketTheme) []string {
	result := make([]string, 0, 4)
	for _, hint := range hints {
		if themeTextContains(text, hint.name) {
			result = append(result, canonicalTheme(hint.name))
		}
		if len(result) >= 4 {
			return uniqueStrings(result, 4)
		}
	}
	for _, item := range marketThemes {
		if themeTextContains(text, item.Name) {
			result = append(result, canonicalTheme(item.Name))
		}
		if len(result) >= 4 {
			return uniqueStrings(result, 4)
		}
	}
	// Keep a small local candidate set for sources whose wording describes a
	// relationship but uses a product or subsidiary name instead of the theme.
	relationKeywords := append(append([]string(nil), themeCompanyFactKeywords...), themeNegativeFactKeywords...)
	if len(result) == 0 && containsAnyFold(text, relationKeywords...) {
		for _, hint := range hints {
			result = append(result, canonicalTheme(hint.name))
			if len(result) >= 3 {
				break
			}
		}
	}
	return uniqueStrings(result, 4)
}

func limitAndDeduplicateThemeSources(items []rankedThemeSource, limit int) []rankedThemeSource {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].publishedAt.After(items[j].publishedAt)
	})
	out := make([]rankedThemeSource, 0, min(len(items), limit))
	titles := make([]string, 0, limit)
	for _, item := range items {
		normalized := normalizeEvidenceText(item.title)
		duplicate := false
		for _, existing := range titles {
			if normalized == existing || (min(len([]rune(normalized)), len([]rune(existing))) >= 8 && (strings.Contains(normalized, existing) || strings.Contains(existing, normalized))) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		titles = append(titles, normalized)
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func selectThemeAISnippet(content, title string, candidates []string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return truncateExactText(title, maxThemeAISnippetRunes)
	}
	keywords := make([]string, 0, len(candidates)*2)
	for _, candidate := range candidates {
		keywords = append(keywords, candidate)
		keywords = append(keywords, themeAliases(candidate)...)
	}
	var factClause string
	for _, clause := range strings.FieldsFunc(content, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '\n', '\r':
			return true
		default:
			return false
		}
	}) {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		if len(keywords) > 0 && containsAnyFold(clause, keywords...) {
			return truncateExactText(clause, maxThemeAISnippetRunes)
		}
		relationKeywords := append(append([]string(nil), themeCompanyFactKeywords...), themeNegativeFactKeywords...)
		if factClause == "" && containsAnyFold(clause, relationKeywords...) {
			factClause = clause
		}
	}
	if factClause != "" {
		return truncateExactText(factClause, maxThemeAISnippetRunes)
	}
	return truncateExactText(content, maxThemeAISnippetRunes)
}

func themeAINameAllowed(name string, candidates []string) bool {
	canonical := canonicalTheme(name)
	for _, candidate := range candidates {
		if canonical == canonicalTheme(candidate) || themeTextContains(canonical, candidate) || themeTextContains(candidate, canonical) {
			return true
		}
	}
	return false
}

func themeTextContains(text, theme string) bool {
	values := append([]string{theme}, themeAliases(theme)...)
	return containsAnyFold(text, values...)
}

func themeSourceFreshnessScore(publishedAt time.Time) int {
	if publishedAt.IsZero() {
		return 0
	}
	days := time.Since(publishedAt).Hours() / 24
	switch {
	case days <= 7:
		return 24
	case days <= 30:
		return 18
	case days <= 90:
		return 12
	case days <= 180:
		return 6
	default:
		return 0
	}
}

func formatThemeAIDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func truncateExactText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
