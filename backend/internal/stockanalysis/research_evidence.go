package stockanalysis

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ResearchCompressionVersion = "evidence-pack-v1"

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
	candidates := make([]researchEvidenceCandidate, 0, len(snapshot.Sources))
	originalBytes := 0
	for _, source := range snapshot.Sources {
		originalBytes += len([]byte(source.Content))
		card := compressResearchSource(source, queries, phase)
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

	selected := selectResearchEvidence(candidates, maxCards, maxChars, phase)
	selectedBytes := 0
	for _, item := range selected {
		selectedBytes += len([]byte(item.Text))
	}
	pack := researchEvidencePack{
		Version: ResearchCompressionVersion, Phase: phase, Symbol: snapshot.Symbol, Name: snapshot.Name,
		CutoffAt: snapshot.CutoffAt, Evidence: selected, Limitations: uniqueStrings(snapshot.Limitations, 16),
		Stats: researchPromptStats{OriginalSourceCount: len(snapshot.Sources), SelectedSourceCount: len(selected), OriginalContentBytes: originalBytes, SelectedContentBytes: selectedBytes},
	}
	if phase == researchPromptSynthesis {
		pack.Anchors = snapshot.Anchors
		baseline := snapshot.Baseline
		baseline.PositiveSignals = uniqueStrings(baseline.PositiveSignals, 5)
		baseline.NegativeSignals = uniqueStrings(baseline.NegativeSignals, 5)
		if len(baseline.Dimensions) > 8 {
			baseline.Dimensions = baseline.Dimensions[:8]
		}
		pack.Baseline = &baseline
	}
	_ = request
	return pack
}

func compressResearchSource(source ResearchSource, queries []string, phase researchPromptPhase) researchEvidenceCard {
	text := compactResearchContent(source, queries, phase)
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

func compactResearchContent(source ResearchSource, queries []string, phase researchPromptPhase) string {
	if source.Kind == "calculation" || strings.HasPrefix(source.ID, "f-") {
		var value any
		if json.Unmarshal([]byte(source.Content), &value) == nil {
			value = compactResearchJSON(value, 0, source.ID)
			encoded, _ := json.Marshal(value)
			return string(encoded)
		}
	}
	text := normalizeResearchText(source.Content)
	if text == "" {
		return truncateExactText(source.Title, 160)
	}
	limit := 520
	if source.Kind == "announcement" || source.Kind == "disclosure" || source.Kind == "company_profile" {
		limit = 900
	}
	if phase == researchPromptSynthesis && source.Kind == "announcement" {
		limit = 1100
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

func compactResearchJSON(value any, depth int, sourceID string) any {
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
				if values, ok := child.([]any); ok && len(values) > 8 {
					child = values[len(values)-8:]
				}
			}
			if sourceID == "m-relative" && key == "bars" {
				if values, ok := child.([]any); ok && len(values) > 6 {
					child = values[len(values)-6:]
				}
			}
			out[key] = compactResearchJSON(child, depth+1, sourceID)
		}
		return out
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			if compacted := compactResearchJSON(child, depth+1, sourceID); compacted != nil {
				out = append(out, compacted)
			}
		}
		return out
	default:
		return value
	}
}

func selectResearchEvidence(candidates []researchEvidenceCandidate, maxCards, maxChars int, phase researchPromptPhase) []researchEvidenceCard {
	selected := make([]researchEvidenceCard, 0, maxCards)
	used := map[string]bool{}
	chars := 0
	add := func(candidate researchEvidenceCandidate) bool {
		if len(selected) >= maxCards || used[candidate.source.ID] {
			return false
		}
		cost := len([]byte(candidate.card.Text)) + len([]byte(candidate.card.Title)) + 80
		if chars+cost > maxChars && len(selected) > 0 {
			return false
		}
		selected = append(selected, candidate.card)
		used[candidate.source.ID] = true
		chars += cost
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
