package inflection

import "testing"

func TestEngineConfirmsBigInflectionWhenOldAnchorClearsAndNewCarrierLeads(t *testing.T) {
	engine := NewEngine(DefaultConfig())
	result, err := engine.Evaluate(EvaluationRequest{
		Market: MarketSnapshot{
			Scope: ScopeMarket, MarketChangePercent: 1.6, ScopeChangePercent: 2.3,
			Advancers: 3800, Decliners: 1100, LimitUps: 45, LimitDowns: 55, BrokenBoards: 35,
			PreviousLimitPremium: -4.2, StressDays: 3, BreadthImprovement: 38,
			PriorStressScore: 88, IndexAboveVWAP: true, LimitDownReleaseRatio: 0.75,
		},
		Candidates: []CandidateSnapshot{
			{
				Symbol: "001309.SZ", Name: "旧负反馈核心", Kind: AnchorOldNegative, Scope: ScopeMarket,
				Recognition:   RecognitionFeatures{Height: 95, Attention: 95, Persistence: 90, Influence: 95, Resilience: 65},
				ChangePercent: -5, ScopeChangePercent: 2.3, MarketChangePercent: 1.6,
				VWAPDistancePercent: -3, ExpectationGap: -6, AmountRatio: 1.4,
				ConsecutiveLimitDown: 3, Drawdown3DPercent: -24, LimitDownOpened: true,
			},
			{
				Symbol: "002384.SZ", Name: "新趋势核心", Kind: AnchorNewCarrier, Scope: ScopeSector,
				Recognition:   RecognitionFeatures{Height: 88, Attention: 92, Persistence: 82, Influence: 90, Resilience: 92},
				ChangePercent: 9.9, ScopeChangePercent: 3, MarketChangePercent: 1.6,
				VWAPDistancePercent: 3.5, AmountRatio: 1.8, SectorFollowers: 6, LimitUp: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Big.Status != StatusConfirmed || result.PrimarySignal != InflectionBig {
		t.Fatalf("big = %+v, primary = %s", result.Big, result.PrimarySignal)
	}
	if result.Big.OldAnchorSymbol != "001309.SZ" || result.Big.NewCarrierSymbol != "002384.SZ" {
		t.Fatalf("unexpected anchors: %+v", result.Big)
	}
}

func TestEngineConfirmsHighLowSmallInflection(t *testing.T) {
	engine := NewEngine(DefaultConfig())
	result, err := engine.Evaluate(EvaluationRequest{
		Market: MarketSnapshot{
			Scope: ScopeSector, MarketChangePercent: 0.8, ScopeChangePercent: 1.2,
			Advancers: 2800, Decliners: 2000, LimitUps: 36, LimitDowns: 12, BrokenBoards: 15,
			PreviousLimitPremium: -0.8, StressDays: 1, BreadthImprovement: 12, IndexAboveVWAP: true,
		},
		Candidates: []CandidateSnapshot{
			{
				Symbol: "603137.SH", Name: "旧情绪核心", Kind: AnchorOldProfit, Scope: ScopeSector,
				Recognition:   RecognitionFeatures{Height: 96, Attention: 90, Persistence: 92, Influence: 90, Resilience: 70},
				ChangePercent: -5.5, ScopeChangePercent: 1.2, MarketChangePercent: 0.8,
				VWAPDistancePercent: -3.2, ExpectationGap: -6, AmountRatio: 1.5, BoardBroken: true,
			},
			{
				Symbol: "000676.SZ", Name: "低位承接物", Kind: AnchorNewCarrier, Scope: ScopeSector,
				Recognition:   RecognitionFeatures{Height: 78, Attention: 82, Persistence: 72, Influence: 75, Resilience: 80},
				ChangePercent: 9.9, ScopeChangePercent: 1.2, MarketChangePercent: 0.8,
				VWAPDistancePercent: 3, AmountRatio: 1.6, SectorFollowers: 3, LimitUp: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Small.Status != StatusConfirmed || result.Small.Setup != SmallSetupHighLowSwitch {
		t.Fatalf("small = %+v", result.Small)
	}
	if result.PrimarySignal != InflectionSmall {
		t.Fatalf("primary = %s, want small", result.PrimarySignal)
	}
}

func TestEngineTreatsPassiveWeaknessAsIndividualReversalInsteadOfOldAnchorFailure(t *testing.T) {
	engine := NewEngine(DefaultConfig())
	result, err := engine.Evaluate(EvaluationRequest{
		Market: MarketSnapshot{
			Scope: ScopeStock, MarketChangePercent: -1.5, ScopeChangePercent: -1.5,
			Advancers: 900, Decliners: 3900, LimitUps: 18, LimitDowns: 35, BrokenBoards: 24,
			PreviousLimitPremium: -2.5, StressDays: 2, BreadthImprovement: 8,
		},
		Candidates: []CandidateSnapshot{
			{
				Symbol: "603137.SH", Name: "逆势修复核心", Kind: AnchorNewCarrier, Scope: ScopeStock,
				Recognition:   RecognitionFeatures{Height: 90, Attention: 88, Persistence: 82, Influence: 86, Resilience: 96},
				ChangePercent: 9.9, ScopeChangePercent: -1.5, MarketChangePercent: -1.5,
				VWAPDistancePercent: 4, AmountRatio: 1.4, SectorFollowers: 1, LimitUp: true,
				PreviousPassiveWeak: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Small.Status != StatusConfirmed || result.Small.Setup != SmallSetupIndividualReversal {
		t.Fatalf("small = %+v", result.Small)
	}
}

func TestEngineDoesNotTreatMarketDraggedOldAnchorAsActiveWeakness(t *testing.T) {
	engine := NewEngine(DefaultConfig())
	result, err := engine.Evaluate(EvaluationRequest{
		Market: MarketSnapshot{
			Scope: ScopeSector, MarketChangePercent: -4, ScopeChangePercent: -4.2,
			Advancers: 500, Decliners: 4300, LimitUps: 8, LimitDowns: 70, BrokenBoards: 20,
			PreviousLimitPremium: -4, StressDays: 3,
		},
		Candidates: []CandidateSnapshot{
			{
				Symbol: "603137.SH", Name: "被动炸板核心", Kind: AnchorOldProfit, Scope: ScopeSector,
				Recognition:   RecognitionFeatures{Height: 95, Attention: 90, Persistence: 88, Influence: 90, Resilience: 92},
				ChangePercent: -3, ScopeChangePercent: -4.2, MarketChangePercent: -4,
				VWAPDistancePercent: 0.3, AmountRatio: 1.1, BoardBroken: true,
			},
			{
				Symbol: "000001.SZ", Name: "随机低位首板", Kind: AnchorNewCarrier, Scope: ScopeSector,
				Recognition:   RecognitionFeatures{Height: 55, Attention: 50, Persistence: 45, Influence: 40, Resilience: 55},
				ChangePercent: 9.9, ScopeChangePercent: -4.2, MarketChangePercent: -4,
				VWAPDistancePercent: 2, AmountRatio: 0.8, SectorFollowers: 0, LimitUp: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Small.Status == StatusConfirmed {
		t.Fatalf("small should not confirm from passive weakness: %+v", result.Small)
	}
}

func TestEngineMarksAnchorAmbiguousWhenTopCandidatesAreTooClose(t *testing.T) {
	engine := NewEngine(DefaultConfig())
	result, err := engine.Evaluate(EvaluationRequest{
		Candidates: []CandidateSnapshot{
			{Symbol: "A", Name: "候选一", Kind: AnchorOldProfit, Recognition: RecognitionFeatures{Height: 80, Attention: 80, Persistence: 80, Influence: 80, Resilience: 80}},
			{Symbol: "B", Name: "候选二", Kind: AnchorOldProfit, Recognition: RecognitionFeatures{Height: 78, Attention: 78, Persistence: 78, Influence: 78, Resilience: 78}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	var oldProfit AnchorSelection
	for _, anchor := range result.Anchors {
		if anchor.Kind == AnchorOldProfit {
			oldProfit = anchor
		}
	}
	if oldProfit.Clear || oldProfit.ScoreGap >= DefaultConfig().MinimumAnchorGap {
		t.Fatalf("anchor should be ambiguous: %+v", oldProfit)
	}
}
