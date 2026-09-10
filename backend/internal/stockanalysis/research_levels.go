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
	TotalTimeout          time.Duration
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
		return researchLevelPolicy{DailyBars: 300, RelativeBars: 20, MaxLimitations: 16, MaxAnchors: 8, MaxBaselineDimensions: 8, StageTimeout: 0, TotalTimeout: 2 * time.Minute}
	case ResearchLevelQuick:
		return researchLevelPolicy{DailyBars: 60, RelativeBars: 4, AnnouncementChars: 30, MaxAnnouncements: 4, MaxCards: 6, MaxEvidenceBytes: 4_000, TradeMaxCards: 6, TradeEvidenceBytes: 4_000, MaxLimitations: 4, MaxAnchors: 2, MaxBaselineDimensions: 3, StageTimeout: 3 * time.Minute, TotalTimeout: 3 * time.Minute}
	case ResearchLevelStandard:
		return researchLevelPolicy{DailyBars: 100, RelativeBars: 6, AnnouncementChars: 50, MaxAnnouncements: 8, MaxCards: 10, MaxEvidenceBytes: 8_000, TradeMaxCards: 8, TradeEvidenceBytes: 8_000, MaxLimitations: 8, MaxAnchors: 3, MaxBaselineDimensions: 4, StageTimeout: 3 * time.Minute, TotalTimeout: 6 * time.Minute}
	default:
		return researchLevelPolicy{DailyBars: 300, RelativeBars: 20, AnnouncementChars: 100, MaxAnnouncements: 12, MaxCards: 12, MaxEvidenceBytes: 16_000, TradeMaxCards: 12, TradeEvidenceBytes: 12_000, MaxLimitations: 16, MaxAnchors: 8, MaxBaselineDimensions: 8, Outline: true, Supplement: true, Repair: true, StageTimeout: 6 * time.Minute, TotalTimeout: 18 * time.Minute}
	}
}

func ResearchStageTimeout(request ResearchRequest) time.Duration {
	return researchLevelPolicyFor(request.AnalysisLevel).StageTimeout
}

func ResearchTotalTimeout(request ResearchRequest) time.Duration {
	return researchLevelPolicyFor(request.AnalysisLevel).TotalTimeout
}
