package stockanalysis

import (
	"sort"
	"strings"
)

// ResearchQueryTerms makes natural-language questions useful for deterministic
// retrieval. Company names and whole questions alone match too many or no rows.
func ResearchQueryTerms(queries ...string) []string {
	terms := []string{}
	keywords := []string{"受让方", "转让方", "转让比例", "持股比例", "转让价格", "转让目的", "对价", "锁定", "不减持", "关联方", "投资金额", "投产", "产能", "产品", "订单", "客户", "分季度", "单季", "收入构成", "营业收入", "营收", "扣非", "非经常性损益", "半年度报告", "年度报告", "减持", "解除限售", "异常波动", "现金流", "减值", "风险"}
	for _, query := range queries {
		for _, term := range strings.FieldsFunc(query, func(r rune) bool { return strings.ContainsRune(" \t\n，,。；;：:、（）()？?", r) }) {
			if size := len([]rune(term)); size >= 2 && size <= 24 && strings.Trim(term, "0123456789-年月日") != "" {
				terms = append(terms, term)
			}
		}
		for _, term := range keywords {
			if strings.Contains(query, term) {
				terms = append(terms, term)
			}
		}
	}
	return uniqueStrings(terms, 40)
}

// ResearchSourceExcerpt retains exact original fragments, including facts late
// in long notices. Ellipses explicitly separate non-contiguous fragments.
func ResearchSourceExcerpt(content string, queries []string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]rune(content)) <= limit {
		return content
	}
	terms := ResearchQueryTerms(queries...)
	type fragment struct {
		text         string
		index, score int
	}
	ranked := []fragment{}
	seen := map[string]bool{}
	for index, span := range researchSentencePattern.FindAllStringIndex(content, -1) {
		text := strings.TrimSpace(content[span[0]:span[1]])
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		score := scoreResearchSentence(text, nil)
		for _, term := range terms {
			if strings.Contains(text, term) {
				score += 40
			}
		}
		ranked = append(ranked, fragment{text: text, index: index, score: score})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	selected := []fragment{}
	remaining := limit
	for _, item := range ranked {
		separator := 0
		if len(selected) > 0 {
			separator = 3 // newline, ellipsis, newline
		}
		if size := len([]rune(item.text)); size > remaining-separator {
			if len(selected) > 0 {
				continue
			}
			runes := []rune(item.text)
			start := 0
			for _, term := range terms {
				if offset := strings.Index(item.text, term); offset >= 0 {
					start = max(0, len([]rune(item.text[:offset]))-limit/4)
					break
				}
			}
			start = min(start, len(runes)-limit)
			item.text = string(runes[start : start+limit])
		}
		remaining -= len([]rune(item.text)) + separator
		selected = append(selected, item)
		if remaining < 20 {
			break
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	parts := make([]string, 0, len(selected))
	for _, item := range selected {
		parts = append(parts, item.text)
	}
	return strings.Join(parts, "\n…\n")
}

func researchRequestedSourceIDs(outline *ResearchOutline) map[string]bool {
	ids := map[string]bool{}
	if outline != nil {
		for _, question := range outline.Questions {
			if question.SourceID != "" {
				ids[question.SourceID] = true
			}
			for _, id := range question.EvidenceSourceIDs {
				ids[id] = true
			}
		}
	}
	return ids
}

func ResearchSourceHasBody(source ResearchSource) bool {
	return strings.TrimSpace(source.Content) != "" && source.ContentStatus != "title_only" && (source.Kind != "announcement" || strings.TrimSpace(source.Content) != strings.TrimSpace(source.Title))
}

func availableResearchSourceIDs(snapshot ResearchSnapshot, retrieved []ResearchSource) []string {
	known := map[string]ResearchSource{}
	for _, source := range snapshot.Sources {
		known[source.ID] = source
	}
	ids := []string{}
	for _, source := range retrieved {
		if existing, ok := known[source.ID]; ok && ResearchSourceHasBody(existing) {
			ids = append(ids, source.ID)
		}
	}
	return uniqueStrings(ids, 12)
}
