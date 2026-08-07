package inflection

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type Engine struct {
	config Config
}

func NewEngine(config Config) *Engine {
	return &Engine{config: config}
}

func (e *Engine) Evaluate(request EvaluationRequest) (Evaluation, error) {
	if err := validateRequest(request); err != nil {
		return Evaluation{}, err
	}

	marketStress, stressFactors := e.scoreMarketStress(request.Market)
	environmentTurn, environmentFactors := e.scoreEnvironmentTurn(request.Market)
	candidates := make([]CandidateEvaluation, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		candidates = append(candidates, e.evaluateCandidate(candidate))
	}

	anchors := []AnchorSelection{
		e.selectAnchor(AnchorOldProfit, candidates),
		e.selectAnchor(AnchorOldNegative, candidates),
		e.selectAnchor(AnchorNewCarrier, candidates),
	}
	anchorByKind := make(map[AnchorKind]AnchorSelection, len(anchors))
	for _, anchor := range anchors {
		anchorByKind[anchor.Kind] = anchor
	}

	big := e.evaluateBig(
		request.Market.Scope,
		marketStress,
		environmentTurn,
		stressFactors,
		environmentFactors,
		anchorByKind,
	)
	small := e.evaluateSmall(request.Market.Scope, marketStress, environmentTurn, anchorByKind)

	primary := InflectionNone
	if big.Status == StatusConfirmed {
		primary = InflectionBig
	} else if small.Status == StatusConfirmed {
		primary = InflectionSmall
	} else if big.Status == StatusCandidate {
		primary = InflectionBig
	} else if small.Status == StatusCandidate {
		primary = InflectionSmall
	}

	warnings := make([]string, 0, 4)
	if anchorByKind[AnchorNewCarrier].Selected == nil {
		warnings = append(warnings, "尚未提供新承接物，无法确认拐点")
	} else if !anchorByKind[AnchorNewCarrier].Clear {
		warnings = append(warnings, "新承接物辨识度不足或龙一不清晰")
	}
	if anchorByKind[AnchorOldProfit].Selected == nil && anchorByKind[AnchorOldNegative].Selected == nil {
		warnings = append(warnings, "尚未提供旧赚钱效应锚或旧负反馈锚")
	}
	warnings = append(warnings, "V1为快照规则引擎，信号仍需通过历史回放、T+1和真实成交约束验证")

	evaluatedAt := request.Market.Time
	if evaluatedAt.IsZero() {
		evaluatedAt = time.Now()
	}
	return Evaluation{
		EvaluatedAt:          evaluatedAt,
		MarketStressScore:    round1(marketStress),
		EnvironmentTurnScore: round1(environmentTurn),
		Candidates:           candidates,
		Anchors:              anchors,
		Big:                  big,
		Small:                small,
		PrimarySignal:        primary,
		Warnings:             warnings,
	}, nil
}

func validateRequest(request EvaluationRequest) error {
	seen := map[string]AnchorKind{}
	for index, candidate := range request.Candidates {
		if candidate.Symbol == "" {
			return fmt.Errorf("candidates[%d].symbol is required", index)
		}
		switch candidate.Kind {
		case AnchorOldProfit, AnchorOldNegative, AnchorNewCarrier:
		default:
			return fmt.Errorf("candidates[%d].kind %q is invalid", index, candidate.Kind)
		}
		key := string(candidate.Kind) + ":" + candidate.Symbol
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate candidate %s for kind %s", candidate.Symbol, candidate.Kind)
		}
		seen[key] = candidate.Kind
	}
	return nil
}

func (e *Engine) evaluateCandidate(candidate CandidateSnapshot) CandidateEvaluation {
	recognition := weighted([]weightedValue{
		{candidate.Recognition.Height, e.config.RecognitionWeights.Height},
		{candidate.Recognition.Attention, e.config.RecognitionWeights.Attention},
		{candidate.Recognition.Persistence, e.config.RecognitionWeights.Persistence},
		{candidate.Recognition.Influence, e.config.RecognitionWeights.Influence},
		{candidate.Recognition.Resilience, e.config.RecognitionWeights.Resilience},
	})
	relativeReturn := candidate.ChangePercent - 0.5*candidate.ScopeChangePercent - 0.5*candidate.MarketChangePercent

	weakRelative := negativeScale(relativeReturn, 4)
	weakVWAP := negativeScale(candidate.VWAPDistancePercent, 3)
	weakBoard := 0.0
	if candidate.BoardBroken {
		weakBoard = 75
	}
	if candidate.LimitDown {
		weakBoard = 100
	}
	weakExpectation := negativeScale(candidate.ExpectationGap, 5)
	weakLiquidity := 0.0
	if relativeReturn < 0 {
		weakLiquidity = clamp((candidate.AmountRatio-0.8)/0.7*100, 0, 100)
	}
	activeWeakness := weighted([]weightedValue{
		{weakRelative, e.config.WeaknessWeights.Relative},
		{weakVWAP, e.config.WeaknessWeights.VWAP},
		{weakBoard, e.config.WeaknessWeights.Board},
		{weakExpectation, e.config.WeaknessWeights.Expectation},
		{weakLiquidity, e.config.WeaknessWeights.Liquidity},
	})

	strongRelative := positiveScale(relativeReturn, 5)
	strongVWAP := positiveScale(candidate.VWAPDistancePercent, 3)
	strongBoard := clamp(positiveScale(candidate.ChangePercent, 9.5), 0, 70)
	if candidate.LimitUp {
		strongBoard = 100
		if candidate.BoardBroken {
			strongBoard = 65
		}
	}
	followers := clamp(float64(candidate.SectorFollowers)/5*100, 0, 100)
	liquidity := clamp(candidate.AmountRatio/1.5*100, 0, 100)
	activeStrength := weighted([]weightedValue{
		{strongRelative, e.config.StrengthWeights.Relative},
		{strongVWAP, e.config.StrengthWeights.VWAP},
		{strongBoard, e.config.StrengthWeights.Board},
		{followers, e.config.StrengthWeights.Expectation},
		{liquidity, e.config.StrengthWeights.Liquidity},
	})

	clearance := weighted([]weightedValue{
		{clamp(float64(candidate.ConsecutiveLimitDown)/3*100, 0, 100), 0.20},
		{negativeScale(candidate.Drawdown3DPercent, 20), 0.20},
		{boolScore(candidate.LimitDownOpened), 0.25},
		{clamp(candidate.AmountRatio/1.2*100, 0, 100), 0.15},
		{activeWeakness, 0.20},
	})

	evidence := []string{
		fmt.Sprintf("辨识度 %.1f", recognition),
		fmt.Sprintf("相对环境收益 %.2f%%", relativeReturn),
	}
	if candidate.PreviousPassiveWeak {
		evidence = append(evidence, "上一状态为被动走弱，当前可评估个股自身修复拐点")
	}
	if candidate.LimitDownOpened {
		evidence = append(evidence, "旧负反馈锚已打开并释放流动性")
	}
	if candidate.LimitUp {
		evidence = append(evidence, fmt.Sprintf("新承接物涨停，板块跟随 %d 只", candidate.SectorFollowers))
	}

	return CandidateEvaluation{
		Candidate:           candidate,
		RecognitionScore:    round1(recognition),
		ActiveWeaknessScore: round1(activeWeakness),
		ActiveStrengthScore: round1(activeStrength),
		ClearanceScore:      round1(clearance),
		Evidence:            evidence,
	}
}

func (e *Engine) selectAnchor(kind AnchorKind, candidates []CandidateEvaluation) AnchorSelection {
	items := make([]CandidateEvaluation, 0)
	for _, candidate := range candidates {
		if candidate.Candidate.Kind == kind {
			items = append(items, candidate)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RecognitionScore > items[j].RecognitionScore
	})
	selection := AnchorSelection{Kind: kind}
	if len(items) == 0 {
		selection.Evidence = []string{"没有候选对象"}
		return selection
	}
	selection.Selected = copyCandidate(items[0])
	if len(items) > 1 {
		selection.RunnerUp = copyCandidate(items[1])
		selection.ScoreGap = round1(items[0].RecognitionScore - items[1].RecognitionScore)
	} else {
		selection.ScoreGap = 100
	}
	selection.Clear = items[0].RecognitionScore >= e.config.RecognitionThreshold && selection.ScoreGap >= e.config.MinimumAnchorGap
	selection.Evidence = []string{
		fmt.Sprintf("%s辨识度 %.1f，领先第二名 %.1f", items[0].Candidate.Name, items[0].RecognitionScore, selection.ScoreGap),
	}
	if !selection.Clear {
		selection.Evidence = append(selection.Evidence, "锚点不清晰：辨识度不足或龙一与龙二过于接近")
	}
	return selection
}

func (e *Engine) evaluateBig(
	scope Scope,
	marketStress float64,
	environmentTurn float64,
	stressFactors []FactorScore,
	environmentFactors []FactorScore,
	anchors map[AnchorKind]AnchorSelection,
) SignalEvaluation {
	oldAnchor, oldClearance := strongestClearance(anchors[AnchorOldNegative], anchors[AnchorOldProfit])
	newAnchor := anchors[AnchorNewCarrier]
	newStrength := 0.0
	newSymbol := ""
	if newAnchor.Selected != nil {
		newStrength = newAnchor.Selected.ActiveStrengthScore
		newSymbol = newAnchor.Selected.Candidate.Symbol
	}
	oldSymbol := ""
	if oldAnchor != nil {
		oldSymbol = oldAnchor.Candidate.Symbol
	}

	score := weighted([]weightedValue{
		{marketStress, e.config.BigWeights.First},
		{oldClearance, e.config.BigWeights.Second},
		{environmentTurn, e.config.BigWeights.Third},
		{newStrength, e.config.BigWeights.Fourth},
	})
	factors := []FactorScore{
		{Name: "前期连续分歧", Score: round1(marketStress), Weight: e.config.BigWeights.First, Detail: joinFactorDetails(stressFactors)},
		{Name: "旧锚充分出清", Score: round1(oldClearance), Weight: e.config.BigWeights.Second},
		{Name: "环境由恶化转向改善", Score: round1(environmentTurn), Weight: e.config.BigWeights.Third, Detail: joinFactorDetails(environmentFactors)},
		{Name: "新承接物主动性", Score: round1(newStrength), Weight: e.config.BigWeights.Fourth},
	}
	status := StatusNone
	if score >= e.config.BigCandidateScore {
		status = StatusCandidate
	}
	if score >= e.config.BigConfirmedScore &&
		marketStress >= e.config.BigStressGate &&
		oldClearance >= e.config.BigClearanceGate &&
		environmentTurn >= e.config.BigEnvironmentGate &&
		newStrength >= e.config.BigNewCarrierGate &&
		newAnchor.Clear {
		status = StatusConfirmed
	}

	evidence := make([]string, 0, 4)
	risks := make([]string, 0, 4)
	if oldAnchor != nil {
		evidence = append(evidence, fmt.Sprintf("旧锚 %s 出清得分 %.1f", oldAnchor.Candidate.Name, oldClearance))
	} else {
		risks = append(risks, "没有明确旧锚，无法确认上一周期是否充分出清")
	}
	if newAnchor.Selected != nil {
		evidence = append(evidence, fmt.Sprintf("新承接物 %s 主动性 %.1f", newAnchor.Selected.Candidate.Name, newStrength))
	} else {
		risks = append(risks, "没有新承接物")
	}
	if !newAnchor.Clear {
		risks = append(risks, "新承接物尚未形成清晰龙一")
	}
	if status == StatusCandidate {
		risks = append(risks, "大拐点仍是预期而非事实，只能视为提前量候选")
	}
	return SignalEvaluation{
		Type:             InflectionBig,
		Status:           status,
		Setup:            SmallSetupNone,
		Scope:            defaultScope(scope),
		Score:            round1(score),
		OldAnchorSymbol:  oldSymbol,
		NewCarrierSymbol: newSymbol,
		Factors:          factors,
		Evidence:         evidence,
		Risks:            risks,
	}
}

func (e *Engine) evaluateSmall(
	scope Scope,
	marketStress float64,
	environmentTurn float64,
	anchors map[AnchorKind]AnchorSelection,
) SignalEvaluation {
	oldProfit := anchors[AnchorOldProfit]
	newAnchor := anchors[AnchorNewCarrier]
	oldWeakness := 0.0
	oldSymbol := ""
	if oldProfit.Selected != nil {
		oldWeakness = oldProfit.Selected.ActiveWeaknessScore
		oldSymbol = oldProfit.Selected.Candidate.Symbol
	}
	newStrength := 0.0
	newRecognition := 0.0
	newSymbol := ""
	previousPassiveWeak := false
	if newAnchor.Selected != nil {
		newStrength = newAnchor.Selected.ActiveStrengthScore
		newRecognition = newAnchor.Selected.RecognitionScore
		newSymbol = newAnchor.Selected.Candidate.Symbol
		previousPassiveWeak = newAnchor.Selected.Candidate.PreviousPassiveWeak
	}
	environmentTradability := clamp(100-math.Max(marketStress-environmentTurn, 0), 0, 100)
	highLowScore := weighted([]weightedValue{
		{oldWeakness, e.config.SmallWeights.First},
		{newStrength, e.config.SmallWeights.Second},
		{newRecognition, e.config.SmallWeights.Third},
		{environmentTradability, e.config.SmallWeights.Fourth},
	})
	individualScore := weighted([]weightedValue{
		{newStrength, 0.50},
		{newRecognition, 0.30},
		{environmentTradability, 0.20},
	})

	setup := SmallSetupHighLowSwitch
	score := highLowScore
	if previousPassiveWeak && individualScore > highLowScore {
		setup = SmallSetupIndividualReversal
		score = individualScore
		oldSymbol = newSymbol
	}
	status := StatusNone
	if score >= e.config.SmallCandidateScore {
		status = StatusCandidate
	}
	confirmed := false
	switch setup {
	case SmallSetupHighLowSwitch:
		confirmed = oldWeakness >= e.config.SmallOldWeaknessGate &&
			newStrength >= e.config.SmallNewCarrierGate && oldProfit.Clear && newAnchor.Selected != nil
	case SmallSetupIndividualReversal:
		confirmed = previousPassiveWeak && newStrength >= e.config.SmallNewCarrierGate && newAnchor.Selected != nil
	}
	if score >= e.config.SmallConfirmedScore && confirmed {
		status = StatusConfirmed
	}

	factors := []FactorScore{
		{Name: "旧赚钱效应锚主动走弱", Score: round1(oldWeakness), Weight: e.config.SmallWeights.First},
		{Name: "新承接物主动性", Score: round1(newStrength), Weight: e.config.SmallWeights.Second},
		{Name: "新承接物辨识度", Score: round1(newRecognition), Weight: e.config.SmallWeights.Third},
		{Name: "环境可交易性", Score: round1(environmentTradability), Weight: e.config.SmallWeights.Fourth},
	}
	evidence := make([]string, 0, 3)
	risks := make([]string, 0, 4)
	if setup == SmallSetupIndividualReversal {
		evidence = append(evidence, "新承接物上一状态为被动走弱，当前相对环境主动修复")
	} else if oldProfit.Selected != nil {
		evidence = append(evidence, fmt.Sprintf("旧赚钱效应锚 %s 主动走弱 %.1f", oldProfit.Selected.Candidate.Name, oldWeakness))
	}
	if newAnchor.Selected != nil {
		evidence = append(evidence, fmt.Sprintf("候选承接物 %s 主动性 %.1f", newAnchor.Selected.Candidate.Name, newStrength))
	} else {
		risks = append(risks, "没有低位或新方向承接物")
	}
	if setup == SmallSetupHighLowSwitch && !oldProfit.Clear {
		risks = append(risks, "旧赚钱效应锚不清晰，高低切可能是事后解释")
	}
	if status != StatusNone {
		risks = append(risks, "小拐点只代表局部提前尝试，不默认形成新周期")
	}
	return SignalEvaluation{
		Type:             InflectionSmall,
		Status:           status,
		Setup:            setup,
		Scope:            defaultScope(scope),
		Score:            round1(score),
		OldAnchorSymbol:  oldSymbol,
		NewCarrierSymbol: newSymbol,
		Factors:          factors,
		Evidence:         evidence,
		Risks:            risks,
	}
}

func (e *Engine) scoreMarketStress(market MarketSnapshot) (float64, []FactorScore) {
	total := market.Advancers + market.Decliners
	downRatio := 0.0
	if total > 0 {
		downRatio = clamp((float64(market.Decliners)/float64(total)-0.5)/0.4*100, 0, 100)
	}
	limitDown := clamp(float64(market.LimitDowns)/50*100, 0, 100)
	brokenRate := 0.0
	if market.LimitUps+market.BrokenBoards > 0 {
		brokenRate = clamp(float64(market.BrokenBoards)/float64(market.LimitUps+market.BrokenBoards)*100, 0, 100)
	}
	lossEffect := negativeScale(market.PreviousLimitPremium, 5)
	stressDays := clamp(float64(market.StressDays)/3*100, 0, 100)
	factors := []FactorScore{
		{Name: "下跌宽度", Score: round1(downRatio), Weight: 0.25},
		{Name: "跌停压力", Score: round1(limitDown), Weight: 0.20},
		{Name: "炸板率", Score: round1(brokenRate), Weight: 0.20},
		{Name: "昨日涨停亏钱效应", Score: round1(lossEffect), Weight: 0.20},
		{Name: "连续分歧天数", Score: round1(stressDays), Weight: 0.15},
	}
	calculated := weightedFactors(factors)
	if market.PriorStressScore > calculated {
		calculated = clamp(market.PriorStressScore, 0, 100)
		factors = append(factors, FactorScore{Name: "前序压力快照", Score: round1(calculated), Weight: 0, Detail: "用于保留环境转折前的极端分歧状态"})
	}
	return calculated, factors
}

func (e *Engine) scoreEnvironmentTurn(market MarketSnapshot) (float64, []FactorScore) {
	breadth := clamp(market.BreadthImprovement/40*100, 0, 100)
	marketReturn := positiveScale(market.MarketChangePercent, 2)
	scopeReturn := positiveScale(market.ScopeChangePercent, 3)
	vwap := boolScore(market.IndexAboveVWAP)
	release := clamp(market.LimitDownReleaseRatio*100, 0, 100)
	factors := []FactorScore{
		{Name: "市场宽度改善", Score: round1(breadth), Weight: 0.30},
		{Name: "指数转强", Score: round1(marketReturn), Weight: 0.15},
		{Name: "观察范围转强", Score: round1(scopeReturn), Weight: 0.20},
		{Name: "指数站上VWAP", Score: round1(vwap), Weight: 0.15},
		{Name: "跌停负反馈释放", Score: round1(release), Weight: 0.20},
	}
	return weightedFactors(factors), factors
}

func strongestClearance(selections ...AnchorSelection) (*CandidateEvaluation, float64) {
	var selected *CandidateEvaluation
	best := 0.0
	for _, selection := range selections {
		if selection.Selected != nil && selection.Selected.ClearanceScore > best {
			selected = selection.Selected
			best = selection.Selected.ClearanceScore
		}
	}
	return selected, best
}

func copyCandidate(value CandidateEvaluation) *CandidateEvaluation {
	copy := value
	return &copy
}

type weightedValue struct {
	value  float64
	weight float64
}

func weighted(values []weightedValue) float64 {
	totalWeight := 0.0
	total := 0.0
	for _, value := range values {
		if value.weight <= 0 {
			continue
		}
		total += clamp(value.value, 0, 100) * value.weight
		totalWeight += value.weight
	}
	if totalWeight == 0 {
		return 0
	}
	return total / totalWeight
}

func weightedFactors(factors []FactorScore) float64 {
	values := make([]weightedValue, 0, len(factors))
	for _, factor := range factors {
		values = append(values, weightedValue{factor.Score, factor.Weight})
	}
	return weighted(values)
}

func positiveScale(value, fullScoreAt float64) float64 {
	if fullScoreAt <= 0 || value <= 0 {
		return 0
	}
	return clamp(value/fullScoreAt*100, 0, 100)
}

func negativeScale(value, fullScoreAt float64) float64 {
	return positiveScale(-value, fullScoreAt)
}

func boolScore(value bool) float64 {
	if value {
		return 100
	}
	return 0
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

func defaultScope(scope Scope) Scope {
	if scope == "" {
		return ScopeMarket
	}
	return scope
}

func joinFactorDetails(factors []FactorScore) string {
	if len(factors) == 0 {
		return ""
	}
	result := ""
	for index, factor := range factors {
		if index > 0 {
			result += "；"
		}
		result += fmt.Sprintf("%s %.1f", factor.Name, factor.Score)
	}
	return result
}
