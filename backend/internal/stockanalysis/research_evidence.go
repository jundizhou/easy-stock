package stockanalysis

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ResearchCompressionVersion = "evidence-pack-v9"

type researchPromptPhase string

const (
	researchPromptOutline   researchPromptPhase = "outline"
	researchPromptSynthesis researchPromptPhase = "synthesis"
)

type researchEvidenceCard struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Title       string               `json:"title"`
	Date        string               `json:"date,omitempty"`
	Provider    string               `json:"provider,omitempty"`
	Text        string               `json:"text"`
	Exact       bool                 `json:"exact"`
	Compression string               `json:"compression"`
	NewsContext *NewsEvidenceContext `json:"news_context,omitempty"`
}

type researchEvidencePack struct {
	Version     string                 `json:"compression_version"`
	Phase       researchPromptPhase    `json:"phase"`
	Symbol      string                 `json:"symbol"`
	Name        string                 `json:"name"`
	CutoffAt    time.Time              `json:"cutoff_at"`
	Evidence    []researchEvidenceCard `json:"evidence"`
	Limitations []string               `json:"limitations"`
	Anchors     []PriceAnchor          `json:"anchors,omitempty"`
	Baseline    *Scorecard             `json:"rule_baseline,omitempty"`
	Stats       researchPromptStats    `json:"stats"`
}

type researchPromptStats struct {
	OriginalSourceCount  int `json:"original_source_count"`
	SelectedSourceCount  int `json:"selected_source_count"`
	OriginalContentBytes int `json:"original_content_bytes"`
	SelectedContentBytes int `json:"selected_content_bytes"`
}

type researchEvidenceCandidate struct {
	source ResearchSource
	card   researchEvidenceCard
	score  int
}

var researchSentencePattern = regexp.MustCompile(`[^。！？!?；;\n]+[。！？!?；;]?`)
var researchNoiseKeys = map[string]bool{
	"meta": true, "source": true, "provider": true, "url": true, "source_url": true,
	"fetched_at": true, "latency_ms": true, "available_fields": true, "next_refresh_at": true,
	"snapshot_id": true, "fallback_reason": true,
}
var researchRiskTerms = []string{"风险", "下滑", "下降", "亏损", "负", "减持", "诉讼", "问询", "否认", "澄清", "不确定", "现金流", "应收", "存货", "商誉", "减值", "流动性"}
var researchFactTerms = []string{"营业总收入", "归母净利润", "扣非", "经营活动", "公告", "报告期", "合作", "订单", "客户", "项目", "投资", "回购", "中标", "产能", "产品", "业务"}
var researchBusinessTerms = []string{"产品", "客户", "订单", "量产", "商业化", "出货", "研发", "收入构成", "供应链", "合作", "产能", "技术"}

func buildResearchEvidencePack(snapshot ResearchSnapshot, request ResearchRequest, phase researchPromptPhase, outline *ResearchOutline) researchEvidencePack {
	level, _ := normalizeResearchLevel(request.AnalysisLevel)
	policy := researchLevelPolicyFor(level)
	queries := []string{snapshot.Name, strings.Split(snapshot.Symbol, ".")[0]}
	if outline != nil {
		for _, question := range outline.Questions {
			queries = append(queries, question.Question, question.Query)
		}
	}
	maxCards, maxChars := 24, 24000
	if phase == researchPromptSynthesis {
		maxCards, maxChars = 32, 46000
	}
	if policy.MaxCards > 0 && policy.MaxEvidenceBytes > 0 && level != ResearchLevelDeep {
		maxCards, maxChars = policy.MaxCards, policy.MaxEvidenceBytes
	}
	priorityIDs := researchRequestedSourceIDs(outline)
	earnings, hasEarnings := LatestResearchEarningsDisclosure(snapshot.Sources, snapshot.CutoffAt)
	candidates := make([]researchEvidenceCandidate, 0, len(snapshot.Sources))
	originalBytes := 0
	for _, source := range snapshot.Sources {
		originalBytes += len([]byte(source.Content))
		source = researchSourceForLevel(source, snapshot, policy)
		sourceQueries := queries
		if outline != nil {
			requestedQueries := []string{}
			for _, question := range outline.Questions {
				requested := question.SourceID == source.ID
				for _, id := range question.EvidenceSourceIDs {
					requested = requested || id == source.ID
				}
				if requested {
					requestedQueries = append(requestedQueries, question.Query, question.Question)
				}
			}
			if len(requestedQueries) > 0 {
				sourceQueries = append(requestedQueries, snapshot.Name)
			}
		}
		// A question about a capital event must not hide the operating facts
		// in a multipurpose investor-relations record.
		if isResearchBusinessDisclosure(source) {
			sourceQueries = append(append([]string{}, researchBusinessTerms...), sourceQueries...)
		}
		card := compressResearchSource(source, sourceQueries, phase, policy)
		if strings.TrimSpace(card.Text) == "" {
			continue
		}
		candidates = append(candidates, researchEvidenceCandidate{source: source, card: card, score: scoreResearchEvidence(source, card.Text, queries)})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if isResearchBusinessDisclosure(candidates[i].source) && isResearchBusinessDisclosure(candidates[j].source) && !candidates[i].source.PublishedAt.Equal(candidates[j].source.PublishedAt) {
			return candidates[i].source.PublishedAt.After(candidates[j].source.PublishedAt)
		}
		return candidates[i].source.ID < candidates[j].source.ID
	})
	collapseDuplicateResearchEvidence(candidates)

	earningsID := ""
	if hasEarnings {
		earningsID = earnings.ID
	}
	selected := selectResearchEvidence(candidates, maxCards, maxChars, policy.MaxAnnouncements, phase, priorityIDs, earningsID)
	selectedBytes := 0
	for _, item := range selected {
		selectedBytes += len([]byte(item.Text))
	}
	pack := researchEvidencePack{
		Version: ResearchCompressionVersion, Phase: phase, Symbol: snapshot.Symbol, Name: snapshot.Name,
		CutoffAt: snapshot.CutoffAt, Evidence: selected, Limitations: uniqueStrings(snapshot.Limitations, policy.MaxLimitations),
		Stats: researchPromptStats{OriginalSourceCount: len(snapshot.Sources), SelectedSourceCount: len(selected), OriginalContentBytes: originalBytes, SelectedContentBytes: selectedBytes},
	}
	if phase == researchPromptSynthesis {
		pack.Anchors = snapshot.Anchors
		baseline := snapshot.Baseline
		baseline.PositiveSignals = uniqueStrings(baseline.PositiveSignals, 5)
		baseline.NegativeSignals = uniqueStrings(baseline.NegativeSignals, 5)
		if len(baseline.Dimensions) > policy.MaxBaselineDimensions {
			baseline.Dimensions = baseline.Dimensions[:policy.MaxBaselineDimensions]
		}
		if len(pack.Anchors) > policy.MaxAnchors {
			pack.Anchors = pack.Anchors[:policy.MaxAnchors]
		}
		pack.Baseline = &baseline
	}
	_ = request
	return pack
}

// buildResearchTradeEvidencePack keeps the second model call focused on
// conditions and execution. It receives the sources cited by the core
// judgment, the small set of calculation inputs needed for anchors, and only
// a few additional risk disclosures instead of the whole evidence pack.
func buildResearchTradeEvidencePack(snapshot ResearchSnapshot, request ResearchRequest, outline ResearchOutline, core ResearchCoreSynthesis) researchEvidencePack {
	full := buildResearchEvidencePack(snapshot, request, researchPromptSynthesis, &outline)
	level, _ := normalizeResearchLevel(request.AnalysisLevel)
	policy := researchLevelPolicyFor(level)
	keep := map[string]bool{"m-price": true, "m-sector": true, "m-quote": true, "f-financial": true, "f-business": true}
	earnings, hasEarnings := LatestResearchEarningsDisclosure(snapshot.Sources, snapshot.CutoffAt)
	if hasEarnings {
		keep[earnings.ID] = true
		// Even when the old core cited only news, do not let those citations
		// consume the trade-stage budget before the available primary notice.
		sort.SliceStable(full.Evidence, func(i, j int) bool {
			return full.Evidence[i].ID == earnings.ID && full.Evidence[j].ID != earnings.ID
		})
	}
	claims := append(append(append([]ResearchClaim{core.Thesis}, core.Support...), core.Counter...), core.Alternatives...)
	if core.TradingLogic != nil {
		if core.TradingLogic.Business != nil {
			claims = append(claims, *core.TradingLogic.Business)
		}
		claims = append(claims, core.TradingLogic.Catalysts...)
		for _, item := range append(append([]ResearchLogicItem{}, core.TradingLogic.Mainlines...), core.TradingLogic.Secondary...) {
			claims = append(claims, item.Explanation)
			if item.MarketEvidence != nil {
				claims = append(claims, *item.MarketEvidence)
			}
		}
	}
	for _, claim := range claims {
		for _, id := range claim.SourceIDs {
			keep[id] = true
		}
	}
	compact := func(card researchEvidenceCard) researchEvidenceCard {
		card = compactResearchCardForTrade(card)
		if card.Kind == "announcement" {
			if hasEarnings && card.ID == earnings.ID {
				card.Text = ResearchEarningsExcerpt(earnings, min(policy.AnnouncementChars, policy.TradeEvidenceBytes/24))
				return card
			}
			queries := append(append([]string{}, researchRiskTerms...), researchBusinessTerms...)
			card.Text = ResearchSourceExcerpt(card.Text, queries, min(policy.AnnouncementChars, policy.TradeEvidenceBytes/32))
		}
		return card
	}
	selected := make([]researchEvidenceCard, 0, policy.TradeMaxCards)
	selectedIDs := map[string]bool{}
	selectedBytes := 0
	announcementCount := 0
	for _, card := range full.Evidence {
		card = compact(card)
		cardBytes := len([]byte(card.Text))
		if !keep[card.ID] || selectedIDs[card.ID] || len(selected) >= policy.TradeMaxCards || (card.Kind == "announcement" && announcementCount >= policy.MaxAnnouncements) || selectedBytes+cardBytes > policy.TradeEvidenceBytes {
			continue
		}
		selected = append(selected, card)
		selectedIDs[card.ID] = true
		selectedBytes += cardBytes
		if card.Kind == "announcement" {
			announcementCount++
		}
	}
	for _, card := range full.Evidence {
		card = compact(card)
		if len(selected) >= policy.TradeMaxCards || selectedBytes+len(card.Text) > policy.TradeEvidenceBytes || selectedIDs[card.ID] || card.Kind != "announcement" || announcementCount >= policy.MaxAnnouncements || !containsAnyFold(card.Text, researchRiskTerms...) {
			continue
		}
		selected = append(selected, card)
		selectedIDs[card.ID] = true
		selectedBytes += len([]byte(card.Text))
		announcementCount++
	}
	return researchEvidencePack{
		Version: ResearchCompressionVersion, Phase: researchPromptSynthesis, Symbol: full.Symbol, Name: full.Name,
		CutoffAt: full.CutoffAt, Evidence: selected, Limitations: uniqueStrings(full.Limitations, policy.MaxLimitations), Anchors: limitResearchAnchors(full.Anchors, policy.MaxAnchors),
		Baseline: full.Baseline, Stats: researchPromptStats{OriginalSourceCount: full.Stats.OriginalSourceCount, SelectedSourceCount: len(selected), OriginalContentBytes: full.Stats.OriginalContentBytes, SelectedContentBytes: selectedBytes},
	}
}

func compactResearchCardForTrade(card researchEvidenceCard) researchEvidenceCard {
	if card.ID == "f-business" {
		card.Text = ResearchSourceExcerpt(card.Text, researchBusinessTerms, 180)
		return card
	}
	if card.ID != "m-price" && card.ID != "m-relative" && card.ID != "f-financial" && card.ID != "m-sector" {
		return card
	}
	var value any
	if json.Unmarshal([]byte(card.Text), &value) != nil {
		return card
	}
	// Core judgment already carries window statistics. Five recent OHLCV rows
	// suffice for execution context and leave room for the current disclosure;
	// do not drop the entire price card when a notice is present.
	policy := researchLevelPolicy{DailyBars: 5, RelativeBars: 6}
	value = compactResearchJSON(value, 0, card.ID, policy)
	if fields, ok := value.(map[string]any); ok {
		if card.ID == "f-financial" {
			if history, ok := fields["history"].([]any); ok && len(history) > 1 {
				fields["history"] = history[:1]
				fields["history_note"] = "交易条件阶段仅展示最近历史对照，其他期见核心判断与快照"
			}
		}
		if card.ID == "m-sector" {
			delete(fields, "peers")
			fields["displayed_peer_count"] = 0
			limitResearchMarketWindows(fields)
		}
	}
	encoded, err := json.Marshal(value)
	if err == nil {
		card.Text = string(encoded)
	}
	return card
}

// buildResearchCoreEvidencePack limits the first synthesis call to the
// evidence needed to explain the stock. The full snapshot remains persisted;
// the model sees the mandatory facts plus the highest-signal disclosures.
func buildResearchCoreEvidencePack(snapshot ResearchSnapshot, request ResearchRequest, outline ResearchOutline) researchEvidencePack {
	full := buildResearchEvidencePack(snapshot, request, researchPromptSynthesis, &outline)
	level, _ := normalizeResearchLevel(request.AnalysisLevel)
	policy := researchLevelPolicyFor(level)
	selected := make([]researchEvidenceCard, 0, policy.MaxCards)
	selectedIDs := map[string]bool{}
	selectedBytes := 0
	announcementCount := 0
	add := func(card researchEvidenceCard) bool {
		if len(selected) >= policy.MaxCards || selectedIDs[card.ID] || (card.Kind == "announcement" && announcementCount >= policy.MaxAnnouncements) {
			return false
		}
		cardBytes := len([]byte(card.Text))
		if selectedBytes+cardBytes > policy.MaxEvidenceBytes {
			return false
		}
		selected = append(selected, card)
		selectedIDs[card.ID] = true
		selectedBytes += cardBytes
		if card.Kind == "announcement" {
			announcementCount++
		}
		return true
	}
	for _, card := range full.Evidence {
		if isResearchMustKeep(ResearchSource{ID: card.ID, Kind: card.Kind}) {
			add(card)
		}
	}
	if earnings, ok := LatestResearchEarningsDisclosure(snapshot.Sources, snapshot.CutoffAt); ok {
		for _, card := range full.Evidence {
			if card.ID == earnings.ID {
				add(card)
			}
		}
	}
	for _, card := range researchBusinessCards(full.Evidence, snapshot) {
		add(card)
	}
	newsCount := 0
	for _, card := range researchRecentNewsCards(full.Evidence) {
		if newsCount < 2 && add(card) {
			newsCount++
		}
	}
	for _, card := range researchPriorityCards(full.Evidence, outline, snapshot) {
		add(card)
	}
	for _, card := range full.Evidence {
		if card.Kind == "announcement" && containsAnyFold(card.Text, researchRiskTerms...) {
			add(card)
		}
	}
	for _, card := range full.Evidence {
		add(card)
	}
	return researchEvidencePack{
		Version: ResearchCompressionVersion, Phase: researchPromptSynthesis, Symbol: full.Symbol, Name: full.Name,
		CutoffAt: full.CutoffAt, Evidence: selected, Limitations: uniqueStrings(full.Limitations, policy.MaxLimitations), Anchors: limitResearchAnchors(full.Anchors, policy.MaxAnchors),
		Baseline: full.Baseline, Stats: researchPromptStats{OriginalSourceCount: full.Stats.OriginalSourceCount, SelectedSourceCount: len(selected), OriginalContentBytes: full.Stats.OriginalContentBytes, SelectedContentBytes: selectedBytes},
	}
}

func compressResearchSource(source ResearchSource, queries []string, phase researchPromptPhase, policy researchLevelPolicy) researchEvidenceCard {
	text := compactResearchContent(source, queries, phase, policy)
	date := source.ReportDate
	if date == "" && !source.PublishedAt.IsZero() {
		date = source.PublishedAt.Format("2006-01-02")
	}
	exact := source.Kind != "calculation" && !strings.HasPrefix(source.ID, "f-")
	compression := "原文摘录，省略处用…分隔；引文只能引用连续片段，较完整来源保存在快照"
	if source.Kind == "announcement" && !ResearchSourceHasBody(source) {
		compression = "仅公告标题，未取得正文；不能据此确认交易条款或业务细节"
	}
	if source.Kind == "news" && source.ContentStatus == "excerpt" {
		compression = "新闻检索摘要，非全文；可评估已展示的事实转述，不声称已读完整报道或公告原文"
	}
	if !exact {
		compression = "结构化字段；不用于逐字引文，完整来源保存在快照"
	}
	return researchEvidenceCard{
		ID: source.ID, Kind: source.Kind, Title: truncateExactText(source.Title, 100), Date: date,
		Provider: truncateExactText(source.Provider, 40), Text: text, Exact: exact, Compression: compression,
		NewsContext: ResearchNewsEvidenceContext(source, 0),
	}
}

func collapseDuplicateResearchEvidence(candidates []researchEvidenceCandidate) {
	owners := map[string]string{}
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.source.Kind != "news" && candidate.source.Kind != "opinion" {
			continue
		}
		signature := normalizeResearchText(candidate.card.Text)
		if len([]rune(signature)) < 40 {
			continue
		}
		if owner, ok := owners[signature]; ok {
			candidate.card.Text = fmt.Sprintf("与来源[%s]的证据文本高度重复，未重复展开；需要引用时可使用本来源编号。", owner)
			candidate.card.Compression = "重复来源摘要；完整来源保存在快照"
			candidate.card.Exact = false
			continue
		}
		owners[signature] = candidate.source.ID
	}
}

func compactResearchContent(source ResearchSource, queries []string, phase researchPromptPhase, policy researchLevelPolicy) string {
	if source.Kind == "calculation" || strings.HasPrefix(source.ID, "f-") {
		var value any
		if json.Unmarshal([]byte(source.Content), &value) == nil {
			value = compactResearchJSON(value, 0, source.ID, policy)
			encoded, _ := json.Marshal(value)
			return string(encoded)
		}
	}
	text := strings.TrimSpace(source.Content)
	if text == "" {
		return truncateExactText(source.Title, 160)
	}
	if source.Kind == "announcement" {
		if isResearchEarningsDisclosure(source) {
			return ResearchEarningsExcerpt(source, policy.AnnouncementChars)
		}
		return compactResearchAnnouncement(text, queries, policy.AnnouncementChars)
	}
	limit := 520
	if source.Kind == "announcement" || source.Kind == "disclosure" || source.Kind == "company_profile" {
		limit = 900
	}
	if phase == researchPromptSynthesis && source.Kind == "announcement" {
		limit = policy.AnnouncementChars
	}
	sentences := researchSentencePattern.FindAllString(text, -1)
	if len(sentences) == 0 {
		return truncateExactText(text, limit)
	}
	type scoredSentence struct {
		text  string
		score int
		index int
	}
	ranked := make([]scoredSentence, 0, len(sentences))
	for index, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		ranked = append(ranked, scoredSentence{text: sentence, score: scoreResearchSentence(sentence, queries), index: index})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].index < ranked[j].index
	})
	selected := make([]scoredSentence, 0, len(ranked))
	length := 0
	for _, sentence := range ranked {
		if length+len([]rune(sentence.text)) > limit && len(selected) > 0 {
			continue
		}
		selected = append(selected, sentence)
		length += len([]rune(sentence.text))
		if length >= limit {
			break
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	parts := make([]string, 0, len(selected))
	for _, sentence := range selected {
		parts = append(parts, sentence.text)
	}
	return truncateExactText(strings.Join(parts, ""), limit)
}

func compactResearchAnnouncement(text string, queries []string, limit int) string {
	return ResearchSourceExcerpt(text, queries, limit)
}

func compactResearchJSON(value any, depth int, sourceID string, policy researchLevelPolicy) any {
	if depth > 5 {
		return nil
	}
	switch item := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, child := range item {
			if researchNoiseKeys[key] || child == nil {
				continue
			}
			if sourceID == "m-price" && key == "recent_bars" {
				if values, ok := child.([]any); ok && len(values) > policy.DailyBars {
					child = values[len(values)-policy.DailyBars:]
				}
			}
			if sourceID == "m-relative" && key == "bars" {
				if values, ok := child.([]any); ok && len(values) > policy.RelativeBars {
					child = values[len(values)-policy.RelativeBars:]
				}
			}
			out[key] = compactResearchJSON(child, depth+1, sourceID, policy)
		}
		return out
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			if compacted := compactResearchJSON(child, depth+1, sourceID, policy); compacted != nil {
				out = append(out, compacted)
			}
		}
		return out
	default:
		return value
	}
}

func selectResearchEvidence(candidates []researchEvidenceCandidate, maxCards, maxChars, maxAnnouncements int, phase researchPromptPhase, priorityIDs map[string]bool, earningsID string) []researchEvidenceCard {
	selected := make([]researchEvidenceCard, 0, maxCards)
	used := map[string]bool{}
	chars := 0
	announcements := 0
	add := func(candidate researchEvidenceCandidate) bool {
		if len(selected) >= maxCards || used[candidate.source.ID] {
			return false
		}
		if candidate.source.Kind == "announcement" && announcements >= maxAnnouncements {
			return false
		}
		// The policy and reported statistics both bound body bytes. Titles and
		// other metadata remain independently bounded by card count/length.
		cost := len([]byte(candidate.card.Text))
		if chars+cost > maxChars {
			return false
		}
		selected = append(selected, candidate.card)
		used[candidate.source.ID] = true
		chars += cost
		if candidate.source.Kind == "announcement" {
			announcements++
		}
		return true
	}
	for _, candidate := range candidates {
		if isResearchMustKeep(candidate.source) {
			add(candidate)
		}
	}
	// Read the newest earnings notice before an older IR transcript or a
	// secondary news summary can consume the disclosure budget.
	for _, candidate := range candidates {
		if candidate.source.ID == earningsID {
			add(candidate)
		}
	}
	// Reserve coverage for operating disclosures before the planner's
	// hypothesis-specific sources, including when there is no outline yet.
	businessCount := 0
	for _, candidate := range candidates {
		if isResearchBusinessDisclosure(candidate.source) && businessCount < 1 && add(candidate) {
			businessCount++
		}
	}
	news := []researchEvidenceCandidate{}
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.source.Provider, "eastmoney:stock-news-search:") {
			news = append(news, candidate)
		}
	}
	sort.SliceStable(news, func(i, j int) bool { return news[i].source.PublishedAt.After(news[j].source.PublishedAt) })
	newsCount := 0
	for _, candidate := range news {
		if newsCount < 2 && add(candidate) {
			newsCount++
		}
	}
	for _, candidate := range candidates {
		if priorityIDs[candidate.source.ID] {
			add(candidate)
		}
	}
	if phase == researchPromptSynthesis {
		for _, candidate := range candidates {
			if containsAnyFold(candidate.card.Text, researchRiskTerms...) {
				add(candidate)
			}
		}
	}
	for _, candidate := range candidates {
		add(candidate)
	}
	return selected
}

func isResearchMustKeep(source ResearchSource) bool {
	if source.ID == "f-financial" || source.ID == "f-business" || source.ID == "m-price" || source.ID == "m-sector" || source.ID == "m-quote" {
		return true
	}
	return source.Kind == "disclosure" || source.Kind == "company_profile"
}

func limitResearchAnchors(anchors []PriceAnchor, limit int) []PriceAnchor {
	if limit <= 0 || len(anchors) <= limit {
		return anchors
	}
	return anchors[:limit]
}

func scoreResearchEvidence(source ResearchSource, text string, queries []string) int {
	score := map[string]int{"disclosure": 100, "company_profile": 96, "calculation": 90, "announcement": 82, "opinion": 48, "news": 38, "methodology": 10}[source.Kind]
	if source.ID == "f-financial" || source.ID == "f-business" {
		score += 25
	}
	if !source.PublishedAt.IsZero() {
		score += 6
	}
	if isResearchBusinessDisclosure(source) {
		score += 40
		if containsAnyFold(source.Title, "投资者关系活动记录", "调研记录", "调研纪要") {
			score += 60
		}
	}
	if containsAnyFold(text, researchRiskTerms...) {
		score += 14
	}
	if containsAnyFold(text, researchFactTerms...) {
		score += 10
	}
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if len([]rune(query)) >= 2 && strings.Contains(text, query) {
			score += 18
		}
	}
	return score
}

func isResearchBusinessDisclosure(source ResearchSource) bool {
	if source.Kind != "announcement" || !ResearchSourceHasBody(source) {
		return false
	}
	if containsAnyFold(source.Title, "关于参加", "召开", "会议通知", "提示性公告") {
		return false
	}
	if containsAnyFold(source.Title, "投资者关系", "调研", "业绩说明", "经营情况", "业务进展", "产品进展") {
		return true
	}
	matches := 0
	for _, term := range researchBusinessTerms {
		if strings.Contains(source.Content, term) {
			matches++
		}
	}
	return matches >= 4
}

func researchBusinessCards(cards []researchEvidenceCard, snapshot ResearchSnapshot) []researchEvidenceCard {
	sources := make(map[string]ResearchSource, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		sources[source.ID] = source
	}
	result := []researchEvidenceCard{}
	for _, card := range cards {
		if isResearchBusinessDisclosure(sources[card.ID]) {
			result = append(result, card)
			if len(result) == 1 {
				break
			}
		}
	}
	return result
}

func researchRecentNewsCards(cards []researchEvidenceCard) []researchEvidenceCard {
	news := []researchEvidenceCard{}
	for _, card := range cards {
		if strings.HasPrefix(card.Provider, "eastmoney:stock-news-search:") {
			news = append(news, card)
		}
	}
	sort.SliceStable(news, func(i, j int) bool { return news[i].Date > news[j].Date })
	return news
}

func researchPriorityCards(cards []researchEvidenceCard, outline ResearchOutline, snapshot ResearchSnapshot) []researchEvidenceCard {
	ids := researchRequestedSourceIDs(&outline)
	priority := []researchEvidenceCard{}
	scores := map[string]int{}
	for _, card := range cards {
		if !ids[card.ID] {
			continue
		}
		priority = append(priority, card)
		for _, q := range outline.Questions {
			if q.SourceID == card.ID {
				scores[card.ID] += 1000
			}
			for _, term := range ResearchQueryTerms(q.Query) {
				if term == snapshot.Name || term == snapshot.Symbol || term == strings.Split(snapshot.Symbol, ".")[0] {
					continue
				}
				switch term {
				case "公告", "进展", "发行", "上市", "公司", "股份", "消息", "最新", "上涨", "下跌":
					continue
				}
				if strings.Contains(card.Text, term) {
					scores[card.ID] += 20
				}
			}
		}
	}
	sort.SliceStable(priority, func(i, j int) bool {
		if scores[priority[i].ID] != scores[priority[j].ID] {
			return scores[priority[i].ID] > scores[priority[j].ID]
		}
		return priority[i].Date > priority[j].Date
	})
	return priority
}

func scoreResearchSentence(sentence string, queries []string) int {
	score := 0
	if containsAnyFold(sentence, researchRiskTerms...) {
		score += 12
	}
	if containsAnyFold(sentence, researchFactTerms...) {
		score += 8
	}
	if strings.ContainsAny(sentence, "0123456789%亿元万元") {
		score += 8
	}
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if len([]rune(query)) >= 2 && strings.Contains(sentence, query) {
			score += 14
		}
	}
	return score
}

func normalizeResearchText(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.Join(strings.Fields(value), " ")
	value = strings.ReplaceAll(value, " ;", "；")
	value = strings.ReplaceAll(value, " .", "。")
	return strings.TrimSpace(value)
}

func researchCompressionSummary(pack researchEvidencePack) string {
	return fmt.Sprintf("压缩版本%s：来源%d→%d，正文%d→%d字节", pack.Version, pack.Stats.OriginalSourceCount, pack.Stats.SelectedSourceCount, pack.Stats.OriginalContentBytes, pack.Stats.SelectedContentBytes)
}
