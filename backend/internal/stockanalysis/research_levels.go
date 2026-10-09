package stockanalysis

import "time"

type ResearchLevel string

const (
	ResearchLevelQuantitative ResearchLevel = "quantitative"
	ResearchLevelQuick        ResearchLevel = "quick"
	ResearchLevelStandard     ResearchLevel = "standard"
	ResearchLevelDeep         ResearchLevel = "deep"
)

type researchLevelPolicy struct {
	DailyBars             int
	RelativeBars          int
	AnnouncementChars     int
	MaxAnnouncements      int
	MaxCards              int
	MaxEvidenceBytes      int
	TradeMaxCards         int
	TradeEvidenceBytes    int
	MaxLimitations        int
	MaxAnchors            int
	MaxBaselineDimensions int
	Outline               bool
	Supplement            bool
	Repair                bool
	StageTimeout          time.Duration
}

func normalizeResearchLevel(level ResearchLevel) (ResearchLevel, bool) {
	if level == "" {
		return ResearchLevelDeep, true
	}
	switch level {
	case ResearchLevelQuantitative, ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep:
		return level, true
	default:
		return ResearchLevelDeep, false
	}
}

func researchLevelPolicyFor(level ResearchLevel) researchLevelPolicy {
	level, _ = normalizeResearchLevel(level)
	switch level {
	case ResearchLevelQuantitative:
		return researchLevelPolicy{DailyBars: 300, RelativeBars: 20, MaxLimitations: 16, MaxAnchors: 8, MaxBaselineDimensions: 8}
	case ResearchLevelQuick:
		return researchLevelPolicy{DailyBars: 60, RelativeBars: 4, AnnouncementChars: 180, MaxAnnouncements: 4, MaxCards: 6, MaxEvidenceBytes: 4_000, TradeMaxCards: 6, TradeEvidenceBytes: 4_000, MaxLimitations: 4, MaxAnchors: 2, MaxBaselineDimensions: 3, StageTimeout: 3 * time.Minute}
	case ResearchLevelStandard:
		return researchLevelPolicy{DailyBars: 100, RelativeBars: 6, AnnouncementChars: 700, MaxAnnouncements: 8, MaxCards: 12, MaxEvidenceBytes: 12_000, TradeMaxCards: 8, TradeEvidenceBytes: 8_000, MaxLimitations: 8, MaxAnchors: 3, MaxBaselineDimensions: 4, StageTimeout: 3 * time.Minute}
	default:
		return researchLevelPolicy{DailyBars: 300, RelativeBars: 20, AnnouncementChars: 1500, MaxAnnouncements: 12, MaxCards: 16, MaxEvidenceBytes: 24_000, TradeMaxCards: 12, TradeEvidenceBytes: 12_000, MaxLimitations: 16, MaxAnchors: 8, MaxBaselineDimensions: 8, Outline: true, Supplement: true, Repair: true, StageTimeout: 8 * time.Minute}
	}
}

func ResearchStageTimeout(request ResearchRequest) time.Duration {
	return researchLevelPolicyFor(request.AnalysisLevel).StageTimeout
}

func ResearchTotalTimeout(request ResearchRequest) time.Duration {
	return ResearchBudgetFor(request, 0, "").TotalTimeout()
}
