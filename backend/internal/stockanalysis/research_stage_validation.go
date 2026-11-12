package stockanalysis

import (
	"fmt"
	"strings"
)

// Stage validation checks the model boundary without changing its judgment.
// Evidence grading, optional trading logic and execution checks remain in the
// final validator; insufficient evidence may still form a valid research result.
func validateResearchStage(value any, snapshot ResearchSnapshot, label string) error {
	sources := make(map[string]ResearchSource, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		sources[source.ID] = source
	}
	aliases := sourceIDRepairAliases(snapshot.Anchors, sources)
	resolveIDs := func(ids []string) ([]string, error) {
		// Validate a repaired copy; the final validator persists the same
		// correction and its notes on the saved report.
		resolved := append([]string(nil), ids...)
		for i, id := range resolved {
			if repaired, ok := repairSourceID(id, sources, aliases); ok {
				resolved[i] = repaired
			}
		}
		for _, id := range resolved {
			if _, ok := sources[id]; !ok {
				return nil, fmt.Errorf("引用了未知来源%s", id)
			}
		}
		return resolved, nil
	}
	validateClaim := func(claim ResearchClaim) error {
		if strings.TrimSpace(claim.Text) == "" {
			return fmt.Errorf("claim缺少text")
		}
		if len(claim.SourceIDs) == 0 {
			return fmt.Errorf("claim缺少来源：%s", truncateExactText(claim.Text, 100))
		}
		ids, err := resolveIDs(claim.SourceIDs)
		if err != nil {
			return err
		}
		if claim.Quote != "" {
			for _, id := range ids {
				if strings.Contains(sources[id].Content, claim.Quote) {
					return nil
				}
			}
			return fmt.Errorf("引文未匹配原文：%s", truncateExactText(claim.Quote, 100))
		}
		return nil
	}
	validateCore := func(core ResearchCoreSynthesis) error {
		if snapshot.InstrumentType == "etf" {
			if err := validateETFJudgments(core.ETFJudgments, snapshot); err != nil {
				return err
			}
		}
		if strings.TrimSpace(core.Headline) == "" {
			return fmt.Errorf("缺少headline")
		}
		if err := validateClaim(core.Thesis); err != nil {
			return fmt.Errorf("thesis无效：%w", err)
		}
		for _, group := range [][]ResearchClaim{core.Support, core.Counter, core.Alternatives} {
			for _, claim := range group {
				if err := validateClaim(claim); err != nil {
					return err
				}
			}
		}
		switch core.EvidenceLevel {
		case "sufficient", "limited", "insufficient":
		default:
			return fmt.Errorf("缺少有效evidence_level")
		}
		return nil
	}
	validateTrade := func(trade ResearchTradeConditions) error {
		switch trade.Decision.Status {
		case "observe", "conditional", "no_plan":
		default:
			return fmt.Errorf("缺少有效decision.status")
		}
		if strings.TrimSpace(trade.Decision.NewPosition) == "" || strings.TrimSpace(trade.Decision.ExistingPosition) == "" || strings.TrimSpace(trade.Decision.Reason) == "" {
			return fmt.Errorf("decision必须分别说明新仓、已有仓位和计划依据")
		}
		for _, condition := range trade.Conditions {
			if _, err := resolveIDs(condition.SourceIDs); err != nil {
				return err
			}
		}
		return nil
	}
	switch stage := value.(type) {
	case *ResearchCoreSynthesis:
		return validateCore(*stage)
	case *ResearchTradeConditions:
		return validateTrade(*stage)
	case *ResearchSynthesis:
		if err := validateCore(ResearchCoreSynthesis{ETFJudgments: stage.ETFJudgments, Headline: stage.Headline, Thesis: stage.Thesis, Support: stage.Support, Counter: stage.Counter, Alternatives: stage.Alternatives, EvidenceLevel: stage.EvidenceLevel}); err != nil {
			return err
		}
		if label != "快速研判" {
			return validateTrade(ResearchTradeConditions{Conditions: stage.Conditions, Decision: stage.Decision})
		}
	}
	return nil
}
