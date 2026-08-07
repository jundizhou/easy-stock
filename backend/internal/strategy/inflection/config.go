package inflection

type RecognitionWeights struct {
	Height      float64
	Attention   float64
	Persistence float64
	Influence   float64
	Resilience  float64
}

type BehaviorWeights struct {
	Relative    float64
	VWAP        float64
	Board       float64
	Expectation float64
	Liquidity   float64
}

type SignalWeights struct {
	First  float64
	Second float64
	Third  float64
	Fourth float64
}

type Config struct {
	RecognitionWeights RecognitionWeights
	WeaknessWeights    BehaviorWeights
	StrengthWeights    BehaviorWeights
	BigWeights         SignalWeights
	SmallWeights       SignalWeights

	RecognitionThreshold float64
	MinimumAnchorGap     float64
	BigCandidateScore    float64
	BigConfirmedScore    float64
	SmallCandidateScore  float64
	SmallConfirmedScore  float64

	BigStressGate        float64
	BigClearanceGate     float64
	BigEnvironmentGate   float64
	BigNewCarrierGate    float64
	SmallOldWeaknessGate float64
	SmallNewCarrierGate  float64
}

func DefaultConfig() Config {
	return Config{
		RecognitionWeights: RecognitionWeights{
			Height: 0.25, Attention: 0.20, Persistence: 0.20, Influence: 0.20, Resilience: 0.15,
		},
		WeaknessWeights: BehaviorWeights{
			Relative: 0.35, VWAP: 0.20, Board: 0.20, Expectation: 0.15, Liquidity: 0.10,
		},
		StrengthWeights: BehaviorWeights{
			Relative: 0.25, VWAP: 0.15, Board: 0.30, Expectation: 0.15, Liquidity: 0.15,
		},
		BigWeights: SignalWeights{
			First: 0.25, Second: 0.25, Third: 0.20, Fourth: 0.30,
		},
		SmallWeights: SignalWeights{
			First: 0.35, Second: 0.35, Third: 0.15, Fourth: 0.15,
		},
		RecognitionThreshold: 70,
		MinimumAnchorGap:     8,
		BigCandidateScore:    62,
		BigConfirmedScore:    75,
		SmallCandidateScore:  58,
		SmallConfirmedScore:  68,
		BigStressGate:        65,
		BigClearanceGate:     60,
		BigEnvironmentGate:   55,
		BigNewCarrierGate:    68,
		SmallOldWeaknessGate: 62,
		SmallNewCarrierGate:  65,
	}
}
