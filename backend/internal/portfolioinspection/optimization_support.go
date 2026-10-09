package portfolioinspection

import (
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Optimization uses the inspection rubric and validators, with one frozen union
// of research and quotes. These helpers do not run a second stock analysis.
func OptimizationReport(request Request, union []HoldingResult) Report {
	rules, _ := RulesFor(request.TraderProfile)
	results := make([]HoldingResult, 0, len(request.Holdings))
	for _, h := range request.Holdings {
		for _, r := range union {
			if r.Holding.Symbol == h.Symbol {
				r.Holding = h
				results = append(results, r)
				break
			}
		}
	}
	metrics := metricsForReport(request, results, rules)
	return Report{Request: request, Profile: rules, Holdings: results, Metrics: metrics, Facts: portfolioFacts(request, results, metrics), AlgorithmVersion: AlgorithmVersion}
}
func OptimizationScoringPrompt(r Report) (string, error) {
	return buildScoringPrompt(r.Request, r.Holdings, r.Metrics, r.Profile)
}
func DecodeOptimizationScore(data json.RawMessage, r Report) (AIReport, error) {
	score, err := decodeScoringReport(string(data))
	if err != nil {
		return score, err
	}
	err = validateScoringReport(&score, r.Request, r.Holdings, r.Metrics)
	return score, err
}

// Comparison asks the model for four-dimensional judgments, not two complete
// stock reports. Per-stock study conclusions and conditions remain sourced
// from their canonical reports, rather than manufactured in the comparison.
func DecodeOptimizationComparisonScore(data json.RawMessage, r Report) (AIReport, error) {
	score, err := decodeScoringReport(string(data))
	if err != nil {
		return score, err
	}
	if len(score.Holdings) == 0 {
		for _, h := range r.Holdings {
			if !validHoldingResearch(h) {
				return score, fmt.Errorf("%s缺少有效原研究，不能补齐逐股判断", h.Holding.Symbol)
			}
			rr := h.Analysis.ResearchReport
			confirm, invalid := []string{}, []string{}
			invalidIDs := map[string]bool{}
			for _, id := range rr.InvalidationIDs {
				invalidIDs[id] = true
			}
			for _, c := range rr.Conditions {
				if invalidIDs[c.ID] {
					invalid = append(invalid, c.Text)
				} else {
					confirm = append(confirm, c.Text)
				}
			}
			if len(confirm) == 0 {
				confirm = append(confirm, "确认条件未补齐，继续核验原研究")
			}
			if len(invalid) == 0 {
				invalid = append(invalid, "失效条件未补齐，继续核验原研究")
			}
			conclusion := rr.Headline
			if conclusion == "" {
				conclusion = rr.Thesis.Text
			}
			if conclusion == "" {
				conclusion = "个股结论见已完成研究"
			}
			score.Holdings = append(score.Holdings, HoldingConclusion{Symbol: h.Holding.Symbol, Conclusion: conclusion, ActionPriority: "观察", Action: fmt.Sprintf("组合目标仓位%d%%；依原研究条件核验，尚未成交", h.Holding.Weight), Confirmation: strings.Join(confirm, "；"), Invalidation: strings.Join(invalid, "；")})
		}
	}
	err = validateScoringReportFacts(&score, r.Request, r.Holdings, r.Metrics, false, optimizationRiskFacts(r))
	return score, err
}

// A model may echo its own configuration label before an otherwise exact fact.
// Resolve only that side's existing available facts; the opposite side and
// invented paths remain invalid. Scores and evidence values are unchanged.
func DecodeNamedOptimizationComparisonScore(data json.RawMessage, r Report, label string) (AIReport, error) {
	known := portfolioFacts(r.Request, r.Holdings, r.Metrics)
	for key, f := range optimizationRiskFacts(r) {
		known[key] = f
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return AIReport{}, err
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if len(x) == 1 {
				if key, ok := x["fact"].(string); ok {
					canonical := OptimizationFactAlias(strings.TrimPrefix(key, label+"."), known)
					if f, exists := known[canonical]; exists && f.Available {
						x["fact"] = canonical
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
	normalized, err := json.Marshal(value)
	if err != nil {
		return AIReport{}, err
	}
	return DecodeOptimizationComparisonScore(normalized, r)
}

// These are lossless aliases of the actual dossier's fact table. Resolving an
// alias never makes an unavailable field or an opposite-side value available.
func OptimizationFactKey(key string) string {
	for _, prefix := range []string{"portfolio_facts.available.", "portfolio_facts.", "stock_facts.cross.available.", "stock_facts.cross.", "cross.available.", "cross."} {
		if strings.HasPrefix(key, prefix) {
			return strings.TrimPrefix(key, prefix)
		}
	}
	return key
}

// Pearson correlation is symmetric. Resolve a reversed pair only when the
// opposite ordering exists as an available measured fact in this portfolio.
func OptimizationFactAlias(key string, facts map[string]Fact) string {
	key = OptimizationFactKey(key)
	if _, exists := facts[key]; exists {
		return key
	}
	parts := strings.Split(key, ".")
	if len(parts) == 5 && parts[0] == "correlation" {
		reversed := "correlation." + strings.Join(parts[3:5], ".") + "." + strings.Join(parts[1:3], ".")
		if f, exists := facts[reversed]; exists && f.Available {
			return reversed
		}
	}
	// The stock-table instructions can make a model prepend a row symbol to
	// a shared correlation path. Accept only a participant of that exact pair,
	// and only if the measured pair is available; never strip arbitrary scopes.
	for _, marker := range []string{".cross.", ".stock_facts.cross."} {
		index := strings.Index(key, marker)
		if index < 0 {
			continue
		}
		owner := key[:index]
		candidate := OptimizationFactKey(key[index+1:])
		pair := strings.Split(candidate, ".")
		if len(pair) != 5 || pair[0] != "correlation" || (owner != strings.Join(pair[1:3], ".") && owner != strings.Join(pair[3:5], ".")) {
			continue
		}
		canonical := OptimizationFactAlias(candidate, facts)
		if f, ok := facts[canonical]; ok && f.Available {
			return canonical
		}
	}
	return key
}

func optimizationRiskFacts(r Report) map[string]Fact {
	weights := map[string]int{}
	for _, h := range r.Request.Holdings {
		weights[h.Symbol] = h.Weight
	}
	facts := map[string]Fact{}
	for _, g := range r.Conclusion.RiskGroups {
		weight := 0
		seen := map[string]bool{}
		for _, symbol := range g.Symbols {
			if !seen[symbol] {
				weight += weights[symbol]
				seen[symbol] = true
			}
		}
		facts["risk_exposures."+g.Name] = Fact{Value: weight, Available: true, Method: "按已冻结风险分组的声明成员计算本组权重", Limitation: "分组可重叠；权重不证明风险发生概率或公司属于同一物理行业"}
		facts["equity_risk_exposures."+g.Name] = Fact{Value: EquityPercent(weight, r.Metrics.TotalPositionPercent), Available: r.Metrics.TotalPositionPercent > 0, Method: "冻结共同驱动组占股票持仓比例%", Limitation: "仅用于组合结构；分组可重叠，不证明同一物理行业或未来损失概率"}
	}
	return facts
}

func OptimizationComparisonFacts(r Report) map[string]Fact {
	facts := portfolioFacts(r.Request, r.Holdings, r.Metrics)
	for key, f := range optimizationRiskFacts(r) {
		facts[key] = f
	}
	return facts
}
func ValidOptimizationResearch(r HoldingResult) bool { return validHoldingResearch(r) }

// Recheck persisted totals against the same rubric used when decoding a real
// review. A model-supplied total or changed dimension weights cannot admit a plan.
func VerifiedOptimizationScore(r AIReport) (int, bool) {
	if !r.ScoreAvailable || r.TotalScore == nil || len(r.Dimensions) != len(scoreRubric) {
		return 0, false
	}
	scores := map[string]int{}
	for _, d := range r.Dimensions {
		if d.Score == nil || *d.Score < 0 || *d.Score > 100 {
			return 0, false
		}
		if _, exists := scores[d.Key]; exists {
			return 0, false
		}
		scores[d.Key] = *d.Score
	}
	total := 0.0
	for _, rubric := range scoreRubric {
		score, exists := scores[rubric.Key]
		if !exists {
			return 0, false
		}
		total += float64(score*rubric.Weight) / 100
	}
	computed := int(math.Round(total))
	return computed, computed == *r.TotalScore
}
func OptimizationEvidence(r HoldingResult) stockEvidence {
	research := compactOptimizationSynthesis(r.Analysis.ResearchReport.ResearchSynthesis)
	sources := make([]optimizationSource, 0)
	used := synthesisSources(research)
	for _, src := range r.Analysis.ResearchReport.Sources {
		if used[src.ID] {
			sources = append(sources, optimizationSource{ID: src.ID, Title: clip(src.Title, 60), Kind: src.Kind, Excerpt: clip(src.Content, 90), PublishedAt: src.PublishedAt, TimeStatus: src.TimeStatus})
		}
	}
	return stockEvidence{Holding: r.Holding, ReportID: r.AnalysisID, CompletedAt: r.ReportCompletedAt, CutoffAt: r.ResearchCutoffAt, OriginalRequest: r.Analysis.ResearchReport.Request, Research: research, Sources: sources}
}

// Keep the decision, drivers, supporting/counter evidence and all trade
// conditions. Historical narratives and repeated scenarios remain in the
// original report; the optimizer receives a focused dossier, never raw reports.
func compactOptimizationSynthesis(s stockanalysis.ResearchSynthesis) stockanalysis.ResearchSynthesis {
	c := boundedSynthesis(s)
	c.Headline = clip(c.Headline, 100)
	c.Thesis.Text = clip(c.Thesis.Text, 160)
	c.Thesis.Quote = ""
	c.MainConflict = clip(c.MainConflict, 100)
	claims := func(items []stockanalysis.ResearchClaim) []stockanalysis.ResearchClaim {
		if len(items) > 2 {
			items = items[:2]
		}
		for i := range items {
			items[i].Text = clip(items[i].Text, 100)
			items[i].Quote = ""
		}
		return items
	}
	c.Support, c.Counter = claims(c.Support), claims(c.Counter)
	c.Alternatives = nil
	c.Scenarios = nil
	c.BaselineReason = ""
	c.EvidenceReasons = limitStrings(c.EvidenceReasons, 3)
	for i := range c.EvidenceReasons {
		c.EvidenceReasons[i] = clip(c.EvidenceReasons[i], 80)
	}
	for i := range c.Limitations {
		c.Limitations[i] = clip(c.Limitations[i], 80)
	}
	c.Decision.Reason = clip(c.Decision.Reason, 100)
	c.Decision.NewPosition = clip(c.Decision.NewPosition, 100)
	c.Decision.ExistingPosition = clip(c.Decision.ExistingPosition, 100)
	// Allocations do not contain prices. Price directives are still validated
	// against the original report and its anchors on the server.
	c.Decision.PricePlan = nil
	// Do not lose invalidation IDs when the general scoring summary limits six
	// conditions. Preserve every original condition and all its typed thresholds.
	c.Conditions = append([]stockanalysis.ResearchCondition(nil), s.Conditions...)
	for i := range c.Conditions {
		c.Conditions[i].Text = clip(c.Conditions[i].Text, 120)
	}
	if logic := c.TradingLogic; logic != nil {
		if logic.Business != nil {
			logic.Business.Text = clip(logic.Business.Text, 100)
			logic.Business.Quote = ""
		}
		logic.Catalysts = claims(logic.Catalysts)
		for _, items := range [][]stockanalysis.ResearchLogicItem{logic.Mainlines, logic.Secondary} {
			for i := range items {
				items[i].Explanation.Text = clip(items[i].Explanation.Text, 100)
				items[i].Explanation.Quote = ""
				for k := range items[i].Gaps {
					items[i].Gaps[k] = clip(items[i].Gaps[k], 100)
				}
				if items[i].MarketEvidence != nil {
					items[i].MarketEvidence.Text = clip(items[i].MarketEvidence.Text, 100)
					items[i].MarketEvidence.Quote = ""
				}
			}
		}
		for i := range logic.Gaps {
			logic.Gaps[i] = clip(logic.Gaps[i], 100)
		}
	}
	return c
}

// Preserve all values, availability and reference keys. Conditions are already
// included with IDs and typed thresholds in each stock dossier; unknown
// condition facts need not repeat their full narrative for every stock.
func OptimizationFacts(facts map[string]Fact) map[string]any {
	result := make(map[string]any, len(facts))
	for key, f := range facts {
		v := map[string]any{"value": f.Value, "available": f.Available, "method": clip(f.Method, 120)}
		if !f.AsOf.IsZero() {
			v["as_of"] = f.AsOf
		}
		if f.Limitation != "" {
			v["limitation"] = clip(f.Limitation, 100)
		}
		if !f.Available && strings.Contains(key, ".condition.") {
			v["method"] = "条件定义见个股研究；未核验成立"
			v["limitation"] = "新报价不能证明原研究条件成立"
		}
		result[key] = v
	}
	return result
}

type optimizationSource struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Kind        string    `json:"kind"`
	Excerpt     string    `json:"excerpt"`
	PublishedAt time.Time `json:"published_at"`
	TimeStatus  string    `json:"time_status"`
}

type stockEvidence struct {
	AllocationBlocked bool                            `json:"allocation_blocked,omitempty"`
	Holding           Holding                         `json:"holding"`
	ReportID          string                          `json:"report_id"`
	CompletedAt       time.Time                       `json:"completed_at"`
	CutoffAt          time.Time                       `json:"cutoff_at"`
	OriginalRequest   stockanalysis.ResearchRequest   `json:"original_request"`
	Research          stockanalysis.ResearchSynthesis `json:"research"`
	Sources           []optimizationSource            `json:"sources"`
}

func ValidateOptimizationAction(h HoldingConclusion, r HoldingResult) error {
	return validateHoldingPriceDirectives(h, r)
}

// Portfolio investment decisions can use real anchors without inheriting the
// old report's action label. Ordinary inspection price validation is unchanged.
func ValidateOptimizationInvestmentAction(h HoldingConclusion, r HoldingResult) error {
	if !validHoldingResearch(r) {
		return fmt.Errorf("个股研究未完成")
	}
	rr := r.Analysis.ResearchReport
	sources := map[string]bool{}
	for _, s := range rr.Sources {
		sources[s.ID] = true
	}
	for _, match := range holdingPriceDirective.FindAllStringSubmatch(h.Action+"\n"+h.Confirmation+"\n"+h.Invalidation, -1) {
		price, err := strconv.ParseFloat(match[1], 64)
		valid := false
		for _, a := range rr.Anchors {
			if err == nil && a.Price > 0 && math.Abs(a.Price-price) < .005 && sources[a.SourceID] {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("组合交易价格没有真实原研究锚点")
		}
	}
	return nil
}

// Zero-weight candidates contribute research/quote/correlation facts to the
// shared dossier, without pretending they are already held.
func OptimizationUnionReport(request Request, union []HoldingResult) Report {
	weights := map[string]Holding{}
	for _, h := range request.Holdings {
		weights[h.Symbol] = h
	}
	request.Holdings = make([]Holding, 0, len(union))
	for _, r := range union {
		h, ok := weights[r.Holding.Symbol]
		if !ok {
			h = Holding{Symbol: r.Holding.Symbol, Name: r.Holding.Name, Weight: 0}
		}
		request.Holdings = append(request.Holdings, h)
	}
	return OptimizationReport(request, union)
}
