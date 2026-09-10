package stockanalysis

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ResearchCompressionVersion = "evidence-pack-v2"

type researchPromptPhase string

const (
	researchPromptOutline   researchPromptPhase = "outline"
	researchPromptSynthesis researchPromptPhase = "synthesis"
)

type researchEvidenceCard struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Date        string `json:"date,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Text        string `json:"text"`
	Exact       bool   `json:"exact"`
	Compression string `json:"compression"`
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
	"snapshot_id": true, "fallback_reason": true, "carry_forward": true,
}
var researchRiskTerms = []string{"风险", "下滑", "下降", "亏损", "负", "减持", "诉讼", "问询", "否认", "澄清", "不确定", "现金流", "应收", "存货", "商誉", "减值", "流动性"}
var researchFactTerms = []string{"营业总收入", "归母净利润", "扣非", "经营活动", "公告", "报告期", "合作", "订单", "客户", "项目", "投资", "回购", "中标", "产能", "产品", "业务"}

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
	candidates := make([]researchEvidenceCandidate, 0, len(snapshot.Sources))
	originalBytes := 0
	for _, source := range snapshot.Sources {
		originalBytes += len([]byte(source.Content))
		source = researchSourceForLevel(source, snapshot, policy)
		card := compressResearchSource(source, queries, phase, policy)
		if strings.TrimSpace(card.Text) == "" {
			continue
		}
		candidates = append(candidates, researchEvidenceCandidate{source: source, card: card, score: scoreResearchEvidence(source, card.Text, queries)})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].source.ID < candidates[j].source.ID
	})
	collapseDuplicateResearchEvidence(candidates)

	selected := selectResearchEvidence(candidates, maxCards, maxChars, policy.MaxAnnouncements, phase)
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
	keep := map[string]bool{"m-price": true, "m-quote": true, "f-financial": true, "f-business": true}
	for _, claim := range append(append(append([]ResearchClaim{core.Thesis}, core.Support...), core.Counter...), core.Alternatives...) {
		for _, id := range claim.SourceIDs {
			keep[id] = true
		}
	}
	selected := make([]researchEvidenceCard, 0, policy.TradeMaxCards)
	selectedIDs := map[string]bool{}
	selectedBytes := 0
	announcementCount := 0
	for _, card := range full.Evidence {
		card = compactResearchCardForTrade(card)
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
		card = compactResearchCardForTrade(card)
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
	if card.ID != "m-price" && card.ID != "m-relative" {
		return card
	}
	var value any
	if json.Unmarshal([]byte(card.Text), &value) != nil {
		return card
	}
	policy := researchLevelPolicy{DailyBars: 20, RelativeBars: 6}
	value = compactResearchJSON(value, 0, card.ID, policy)
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
	add := func(card researchEvidenceCard) {
		if len(selected) >= policy.MaxCards || selectedIDs[card.ID] || (card.Kind == "announcement" && announcementCount >= policy.MaxAnnouncements) {
			return
		}
		cardBytes := len([]byte(card.Text))
		if selectedBytes+cardBytes > policy.MaxEvidenceBytes {
			return
		}
		selected = append(selected, card)
		selectedIDs[card.ID] = true
		selectedBytes += cardBytes
		if card.Kind == "announcement" {
			announcementCount++
		}
	}
	for _, card := range full.Evidence {
		if card.ID == "m-price" || card.ID == "m-quote" || card.ID == "f-financial" || card.ID == "f-business" || card.Kind == "disclosure" || card.Kind == "company_profile" {
			add(card)
		}
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
	compression := "原文连续片段；完整来源保存在快照"
	if !exact {
		compression = "结构化字段；不用于逐字引文，完整来源保存在快照"
	}
	return researchEvidenceCard{
		ID: source.ID, Kind: source.Kind, Title: truncateExactText(source.Title, 100), Date: date,
		Provider: truncateExactText(source.Provider, 40), Text: text, Exact: exact, Compression: compression,
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
	text := normalizeResearchText(source.Content)
	if text == "" {
		return truncateExactText(source.Title, 160)
	}
	if source.Kind == "announcement" {
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
	if limit <= 0 {
		limit = 100
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
	selected := make([]scoredSentence, 0, 2)
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

func selectResearchEvidence(candidates []researchEvidenceCandidate, maxCards, maxChars, maxAnnouncements int, phase researchPromptPhase) []researchEvidenceCard {
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
		cost := len([]byte(candidate.card.Text)) + len([]byte(candidate.card.Title)) + 80
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
	if source.ID == "f-financial" || source.ID == "f-business" || source.ID == "m-price" || source.ID == "m-quote" {
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
