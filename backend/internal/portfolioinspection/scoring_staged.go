package portfolioinspection

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const stagedCitationReserveBytes = 4 * 1024

// A report may contain arbitrarily many sources, conditions and narrative
// fields. Read those in bounded per-holding chunks, then give the portfolio
// scorer a bounded view. The original reports remain the authority for every
// citation and action selected by the scorer.
func (s *Service) stageScoringEvidence(input compactScoringInput, runner *compactionRunner, instructions string) (compactScoringInput, error) {
	staged := compactScoringInput{
		InputVersion: input.InputVersion, CardVersion: input.CardVersion, ContractVersion: input.ContractVersion,
		PromptVersion: input.PromptVersion, ScoringVersion: input.ScoringVersion,
		Request: input.Request, Profile: input.Profile, Actions: input.Actions,
		Facts: map[string]compactFact{}, Definitions: map[string]string{}, Evidence: map[string]EvidenceRef{},
		Holdings: make([]PortfolioAssetCard, len(input.Holdings)), StageCoverage: make([]stagedCoverage, len(input.Holdings)),
	}
	isHoldingFact := func(key string) bool {
		for _, card := range input.Holdings {
			if strings.HasPrefix(key, card.Symbol+".") {
				return true
			}
		}
		return false
	}
	for key, fact := range input.Facts {
		if isHoldingFact(key) {
			continue
		}
		staged.Facts[key] = fact
		if definition := input.Definitions[fact.Method]; definition != "" {
			staged.Definitions[fact.Method] = definition
		}
		if definition := input.Definitions[fact.Limitation]; definition != "" {
			staged.Definitions[fact.Limitation] = definition
		}
	}
	for id, ref := range input.Evidence {
		if ref.Fact != "" && !isHoldingFact(ref.Fact) {
			staged.Evidence[id] = ref
		}
	}
	for i, card := range input.Holdings {
		staged.Holdings[i] = PortfolioAssetCard{
			Symbol: card.Symbol, Name: card.Name, Type: card.Type, ReportID: card.ReportID,
			CompletedAt: card.CompletedAt, CutoffAt: card.CutoffAt, ResearchVersion: card.ResearchVersion,
			OriginalHorizon: card.OriginalHorizon, EquityWeight: card.EquityWeight,
			EvidenceLevel: card.EvidenceLevel, DecisionStatus: card.DecisionStatus,
			QuoteStatus: card.QuoteStatus, QuoteMessage: card.QuoteMessage,
		}
		staged.StageCoverage[i] = stagedCoverage{Symbol: card.Symbol, ReportID: card.ReportID}
	}
	baseline, err := renderCompactScoringPrompt(instructions, staged)
	if err != nil {
		return input, err
	}
	limit := (targetScoringPromptBytes-len(baseline)-stagedCitationReserveBytes)/max(1, len(staged.Holdings)) - len(`,"compacted_research":`)
	if limit < compactionMinSummaryBytes {
		return input, fmt.Errorf("组合共享事实与动作目录占用%d字节，无法为逐标的研究保留摘要空间；原研究已保留", len(baseline))
	}
	runner.summaryBytes = min(compactionSummaryBytes, limit)
	summaries := make(map[string]string, len(input.Holdings))
	for i, card := range input.Holdings {
		var sources []map[string]any
		for _, source := range input.Sources {
			if source["report_id"] == card.ReportID {
				sources = append(sources, source)
			}
		}
		facts := map[string]compactFact{}
		definitions := map[string]string{}
		for key, fact := range input.Facts {
			if !strings.HasPrefix(key, card.Symbol+".") {
				continue
			}
			facts[key] = fact
			for _, id := range []string{fact.Method, fact.Limitation} {
				if definition := input.Definitions[id]; definition != "" {
					definitions[id] = definition
				}
			}
		}
		staged.StageCoverage[i].SourcesRead = len(sources)
		staged.StageCoverage[i].FactsRead = len(facts)
		material := map[string]any{"decision_card": card, "sources": sources, "facts": facts, "definitions": definitions}
		completedBefore := runner.completed
		summary, err := runner.summarize(card, material)
		if err != nil {
			return input, err
		}
		staged.StageCoverage[i].ChunksRead = runner.completed - completedBefore
		staged.Holdings[i].CompactedResearch = summary
		summaries[card.ReportID] = summary
	}
	prompt, err := renderCompactScoringPrompt(instructions, staged)
	if err != nil {
		return input, err
	}
	if len(prompt) > targetScoringPromptBytes {
		return input, errors.New("逐标的研究摘要仍超过组合输入预算，原研究已保留")
	}
	// Prefer sources explicitly mentioned by a staged summary, then expose as
	// many other original source selectors as the remaining budget permits.
	// A selector only identifies an original source; it never authorizes one.
	type sourceSelector struct {
		id        string
		ref       EvidenceRef
		mentioned bool
	}
	type sourceKey struct{ reportID, sourceID string }
	readSources := map[sourceKey]bool{}
	for _, source := range input.Sources {
		reportID, reportOK := source["report_id"].(string)
		sourceID, sourceOK := source["id"].(string)
		if reportOK && sourceOK {
			readSources[sourceKey{reportID, sourceID}] = true
		}
	}
	selectors := make([]sourceSelector, 0)
	for id, ref := range input.Evidence {
		if ref.SourceID != "" && readSources[sourceKey{ref.ReportID, ref.SourceID}] {
			selectors = append(selectors, sourceSelector{id, ref, strings.Contains(summaries[ref.ReportID], ref.SourceID)})
		}
	}
	sort.Slice(selectors, func(i, j int) bool {
		if selectors[i].mentioned != selectors[j].mentioned {
			return selectors[i].mentioned
		}
		return selectors[i].id < selectors[j].id
	})
	remaining := targetScoringPromptBytes - len(prompt) - 512 // Coverage counts grow after selectors are added.
	visibleByReport := map[string]int{}
	selected := map[string]bool{}
	addSelector := func(selector sourceSelector) (bool, error) {
		entry, err := json.Marshal(map[string]EvidenceRef{selector.id: selector.ref})
		if err != nil {
			return false, err
		}
		cost := len(entry) - 1 // Replace the closing brace with a comma and entry.
		if cost > remaining {
			return false, nil
		}
		staged.Evidence[selector.id] = selector.ref
		remaining -= cost
		visibleByReport[selector.ref.ReportID]++
		selected[selector.id] = true
		return true, nil
	}
	// Give every researched holding at least one original source selector before
	// filling the remaining space. Otherwise lexicographic IDs could crowd out
	// the last holdings when the catalogue is large.
	for _, card := range input.Holdings {
		for _, selector := range selectors {
			if selector.ref.ReportID != card.ReportID {
				continue
			}
			added, err := addSelector(selector)
			if err != nil {
				return input, err
			}
			if !added {
				return input, errors.New("逐标的研究缺少必要来源引用空间，原研究已保留")
			}
			break
		}
	}
	for _, selector := range selectors {
		if selected[selector.id] {
			continue
		}
		if _, err := addSelector(selector); err != nil {
			return input, err
		}
	}
	for i := range staged.StageCoverage {
		staged.StageCoverage[i].SourceRefs = visibleByReport[staged.StageCoverage[i].ReportID]
	}
	prompt, err = renderCompactScoringPrompt(instructions, staged)
	if err != nil {
		return input, err
	}
	if len(prompt) > targetScoringPromptBytes {
		return input, errors.New("逐标的研究引用目录超过组合输入预算，原研究已保留")
	}
	runner.progress("研究资料已逐标的整理，正在综合评分和评估组合风险")
	if s.logger != nil {
		s.logger.Printf("level=info event=portfolio_scoring_staged_complete feature=portfolio-inspection output_bytes=%d model_calls=%d reused_chunks=%d holdings=%d", len(prompt), runner.calls, runner.reused, len(staged.Holdings))
	}
	return staged, nil
}

func stagedCoverageFromPrompt(prompt string) []stagedCoverage {
	_, payload, ok := strings.Cut(prompt, "[组合证据JSON]\n")
	if !ok {
		return nil
	}
	var input struct {
		StageCoverage []stagedCoverage `json:"stage_coverage"`
	}
	if json.NewDecoder(strings.NewReader(payload)).Decode(&input) != nil {
		return nil
	}
	return input.StageCoverage
}
