package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Only the model's input view changes. Original cards and source catalogs are
// stored on the final report and continue to drive author/vote validation.
type compactDailyAuthorView struct {
	Author                string               `json:"author"`
	Source                string               `json:"source"`
	ArticleCount          int                  `json:"article_count"`
	AvailableArticleCount int                  `json:"available_article_count"`
	TimeRange             string               `json:"time_range"`
	Sources               []DailySummarySource `json:"sources"`
	CompactedSummary      string               `json:"compacted_summary,omitempty"`
	Limitations           string               `json:"summary_limitations"`
}

func compactAuthorSkeleton(view DailyAuthorView) compactDailyAuthorView {
	return compactDailyAuthorView{Author: view.Author, Source: view.Source, ArticleCount: view.ArticleCount, AvailableArticleCount: view.AvailableArticleCount, TimeRange: view.TimeRange, Sources: view.Sources, Limitations: reviewViewCompactionNotice + "；摘要未提及不代表风险、分歧或失效条件不存在"}
}

func (a *Automation) prepareDailySummaryPhasePrompts(ctx context.Context, window reviewFreshnessWindow, viewpoints []DailyAuthorView, market DailyUSMarket) (string, string, bool, error) {
	ctx = withReviewSummarySession(ctx, a.store)
	marketPrompt, tomorrowPrompt, err := buildDailySummaryPhasePrompts(window, viewpoints, market)
	if err != nil || max(len(marketPrompt), len(tomorrowPrompt)) <= targetReviewPromptBytes {
		return marketPrompt, tomorrowPrompt, false, err
	}
	input := make([]any, len(viewpoints))
	minimum := make([]any, len(viewpoints))
	type candidate struct{ index, size int }
	candidates := make([]candidate, len(viewpoints))
	for i, view := range viewpoints {
		input[i] = view
		minimum[i] = compactAuthorSkeleton(view)
		data, err := json.Marshal(view)
		if err != nil {
			return "", "", false, err
		}
		candidates[i] = candidate{index: i, size: len(data)}
	}
	minimumMarket, minimumTomorrow, err := buildDailySummaryPhasePromptsWithInput(window, minimum, market)
	if err != nil {
		return "", "", false, err
	}
	// Account for both phase instructions, immutable market data, every source
	// catalog and the JSON property overhead before allocating summary space.
	protectedBytes := max(len(minimumMarket), len(minimumTomorrow))
	limit := (targetReviewPromptBytes-protectedBytes)/max(1, len(viewpoints)) - len(`,"compacted_summary":`)
	if limit < reviewMinSummaryBytes {
		return "", "", false, fmt.Errorf("复盘事实与来源目录占用%d字节，无法在%d字节预算内为%d位作者保留足够摘要空间，原观点卡已保留", protectedBytes, targetReviewPromptBytes, len(viewpoints))
	}
	limit = min(limit, reviewSummaryBytes)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].size > candidates[j].size })
	for _, candidate := range candidates {
		view := viewpoints[candidate.index]
		data, err := json.Marshal(view)
		if err != nil {
			return "", "", false, err
		}
		record := reviewTextRecord{Reference: reviewTextReference{Author: view.Author, Source: view.Source}, Index: 1, Text: string(data)}
		summary, err := compactReviewRecords(ctx, a.prompter, []reviewTextRecord{record}, limit)
		if err != nil {
			return "", "", false, err
		}
		card := compactAuthorSkeleton(view)
		card.CompactedSummary = summary
		input[candidate.index] = card
		marketPrompt, tomorrowPrompt, err = buildDailySummaryPhasePromptsWithInput(window, input, market)
		if err != nil {
			return "", "", false, err
		}
		if max(len(marketPrompt), len(tomorrowPrompt)) <= targetReviewPromptBytes {
			return marketPrompt, tomorrowPrompt, true, nil
		}
	}
	return "", "", false, errors.New("复盘作者观点卡经压缩后仍超过输入预算，原观点卡已保留")
}
