package portfolioinspection

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/stockanalysis"
)

const assetCardVersion = "portfolio-asset-card-v1"
const compactInputVersion = "portfolio-input-v1"

// Decision cards are deterministic views. Original reports remain immutable.
// Negative claims, gaps and execution restrictions are never count-truncated.
type PortfolioAssetCard struct {
	Symbol          string                              `json:"symbol"`
	Name            string                              `json:"name,omitempty"`
	Type            string                              `json:"instrument_type"`
	ReportID        string                              `json:"report_id"`
	CompletedAt     time.Time                           `json:"completed_at"`
	CutoffAt        time.Time                           `json:"cutoff_at"`
	ResearchVersion string                              `json:"research_version"`
	OriginalHorizon string                              `json:"original_horizon"`
	EquityWeight    float64                             `json:"equity_weight_percent"`
	Thesis          stockanalysis.ResearchClaim         `json:"thesis"`
	Support         []stockanalysis.ResearchClaim       `json:"support"`
	Counter         []stockanalysis.ResearchClaim       `json:"counter"`
	Alternatives    []stockanalysis.ResearchClaim       `json:"alternatives"`
	Conflict        string                              `json:"main_conflict"`
	TradingLogic    *stockanalysis.ResearchTradingLogic `json:"trading_logic,omitempty"`
	ETF             *etfanalysis.Judgments              `json:"etf_judgments,omitempty"`
	EvidenceLevel   string                              `json:"evidence_level"`
	EvidenceReasons []string                            `json:"evidence_reasons,omitempty"`
	Limitations     []string                            `json:"limitations"`
	Conditions      []stockanalysis.ResearchCondition   `json:"conditions"`
	Anchors         []stockanalysis.PriceAnchor         `json:"anchors,omitempty"`
	InvalidationIDs []string                            `json:"invalidation_ids"`
	DecisionStatus  string                              `json:"decision_status"`
	Blockers        []string                            `json:"blockers,omitempty"`
	Prerequisites   []string                            `json:"execution_prerequisites"`
	QuoteStatus     string                              `json:"quote_status"`
	QuoteMessage    string                              `json:"quote_message,omitempty"`
}

type compactFact struct {
	Value      any        `json:"value"`
	Available  bool       `json:"available"`
	AsOf       *time.Time `json:"as_of,omitempty"`
	Method     string     `json:"method_id,omitempty"`
	Limitation string     `json:"limitation_id,omitempty"`
}
type compactAction struct {
	ID         string   `json:"action_id"`
	Symbol     string   `json:"symbol"`
	Kind       string   `json:"kind"`
	Status     string   `json:"execution_status"`
	Conditions []string `json:"condition_ids,omitempty"`
	Anchors    []string `json:"anchor_ids,omitempty"`
}
type compactScoringInput struct {
	InputVersion    string                 `json:"input_version"`
	CardVersion     string                 `json:"card_version"`
	ContractVersion string                 `json:"contract_version"`
	PromptVersion   string                 `json:"prompt_version"`
	ScoringVersion  string                 `json:"scoring_version"`
	Request         map[string]any         `json:"request"`
	Profile         map[string]any         `json:"profile"`
	Facts           map[string]compactFact `json:"facts"`
	Definitions     map[string]string      `json:"definitions"`
	Holdings        []PortfolioAssetCard   `json:"holdings"`
	Sources         []map[string]any       `json:"sources"`
	Evidence        []scoringEvidence      `json:"evidence_catalog"`
	Actions         []compactAction        `json:"action_catalog"`
}

func compactCatalog(c scoringCatalog) scoringCatalog {
	// Clone slices: aliases only exist inside this frozen request/repair pair.
	c.Evidence = append([]scoringEvidence(nil), c.Evidence...)
	c.Actions = append([]ScoringAction(nil), c.Actions...)
	for i := range c.Evidence {
		c.Evidence[i].ID = fmt.Sprintf("ev-%d", i+1)
	}
	for i := range c.Actions {
		c.Actions[i].ID = fmt.Sprintf("act-%d", i+1)
	}
	return c
}

func buildCompactScoringInput(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules, at time.Time) compactScoringInput {
	catalog := compactCatalog(buildScoringCatalog(request, results, metrics, at))
	in := compactScoringInput{InputVersion: compactInputVersion, CardVersion: assetCardVersion, ContractVersion: scoringContractVersion, PromptVersion: PromptVersion, ScoringVersion: AlgorithmVersion,
		Request: map[string]any{"horizon": request.Horizon, "trader_profile": request.TraderProfile}, Profile: ScoringProfile(rules), Evidence: catalog.Evidence, Facts: map[string]compactFact{}, Definitions: map[string]string{}}
	definitions := map[string]string{}
	intern := func(text string) string {
		if text == "" {
			return ""
		}
		if id := definitions[text]; id != "" {
			return id
		}
		id := fmt.Sprintf("d%d", len(definitions)+1)
		definitions[text] = id
		in.Definitions[id] = text
		return id
	}
	facts := ScoringFacts(portfolioFactsAt(request, results, metrics, at))
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		// A symmetric correlation is one fact with two valid selector aliases.
		if strings.HasPrefix(key, "correlation.") {
			p := strings.Split(key, ".")
			if len(p) == 5 {
				reverse := "correlation." + p[3] + "." + p[4] + "." + p[1] + "." + p[2]
				if key > reverse {
					continue
				}
			}
		}
		f := facts[key]
		cf := compactFact{Value: f.Value, Available: f.Available, Method: intern(f.Method), Limitation: intern(f.Limitation)}
		if !f.AsOf.IsZero() {
			t := f.AsOf
			cf.AsOf = &t
		}
		in.Facts[key] = cf
	}
	for _, a := range catalog.Actions {
		in.Actions = append(in.Actions, compactAction{a.ID, a.Symbol, a.Kind, a.ExecutionStatus, a.ConditionIDs, a.AnchorIDs})
	}
	for _, r := range results {
		if !validHoldingResearch(r) {
			continue
		}
		rr := r.Analysis.ResearchReport
		bounded := boundedSynthesis(rr.ResearchSynthesis)
		card := PortfolioAssetCard{Symbol: r.Holding.Symbol, Name: r.Holding.Name, Type: "stock", ReportID: r.AnalysisID, CompletedAt: r.ReportCompletedAt, CutoffAt: r.ResearchCutoffAt, ResearchVersion: rr.PromptVersion, OriginalHorizon: rr.Request.Horizon,
			EquityWeight: EquityPercent(r.Holding.Weight, metrics.TotalPositionPercent), Thesis: bounded.Thesis, Support: bounded.Support, Counter: rr.Counter, Alternatives: rr.Alternatives, Conflict: rr.MainConflict, TradingLogic: rr.TradingLogic, ETF: rr.ETFJudgments,
			EvidenceLevel: rr.EvidenceLevel, EvidenceReasons: rr.EvidenceReasons, Limitations: rr.Limitations, Conditions: rr.Conditions, InvalidationIDs: rr.InvalidationIDs, DecisionStatus: rr.Decision.Status, Blockers: rr.Decision.Blockers,
			Prerequisites: append([]string(nil), rr.Decision.ExecutionPrerequisites...), QuoteStatus: r.QuoteStatus, QuoteMessage: r.QuoteMessage}
		if isETF(r.Analysis) {
			card.Type = "etf"
			card.Prerequisites = uniqueScoringStrings(append(card.Prerequisites, r.Analysis.ETF.ExecutionBlockers...))
		}
		for _, anchor := range rr.Anchors {
			for _, condition := range card.Conditions {
				if condition.AnchorID == anchor.ID {
					card.Anchors = append(card.Anchors, anchor)
					break
				}
			}
		}
		in.Holdings = append(in.Holdings, card)
		used := scoringSourceIDs(r, rr.ResearchSynthesis)
		earnings, hasEarnings := stockanalysis.LatestResearchEarningsDisclosure(rr.Sources, rr.CutoffAt)
		for _, source := range rr.Sources {
			if !used[source.ID] {
				continue
			}
			s := map[string]any{"report_id": r.AnalysisID, "id": source.ID, "title": clip(source.Title, 160), "kind": source.Kind, "published_at": source.PublishedAt, "excerpt": clip(source.Content, 320)}
			if hasEarnings && source.ID == earnings.ID {
				s["excerpt"] = stockanalysis.ResearchEarningsExcerpt(source, 700)
				s["content_status"] = source.ContentStatus
			}
			if card.Type == "etf" && strings.HasPrefix(source.ID, "e-direction") {
				s["excerpt"] = clip(source.Content, 1800)
			}
			if news := stockanalysis.ResearchNewsEvidenceContext(source, 0); news != nil {
				s["news_context"] = news
			}
			in.Sources = append(in.Sources, s)
		}
	}
	return in
}

func buildScoringPromptAt(request Request, results []HoldingResult, metrics Metrics, rules ProfileRules, at time.Time) (string, error) {
	legacy, err := buildLegacyScoringPromptAt(request, results, metrics, rules, at)
	mode := os.Getenv("EASY_STOCK_PORTFOLIO_INPUT")
	if err != nil || mode == "legacy" {
		return legacy, err
	}
	input := buildCompactScoringInput(request, results, metrics, rules, at)
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	instructions, _, _ := strings.Cut(legacy, "[组合证据JSON]\n")
	prompt := instructions + "决策卡是原报告的确定性视图。definitions解析method_id/limitation_id；相关性对称项只列一次。反证、缺口、条件与执行前提不可忽略。动作种类observe/hold/reassess仅观察；conditional_reduce/conditional_exit须满足卡片全部执行前提及对应条件，当前不代表可成交。编号仅在本次输入内有效。每条理由建议120字以内，不重复转述原报告，必须保留关键冲突与风险。\n[组合证据JSON]\n" + string(data)
	// Refuse pathological inputs rather than clipping mandatory adverse facts.
	if len(prompt) > 256*1024 {
		return "", fmt.Errorf("组合必要证据%d字节超过输入上限，请缩小组合或补齐结构化研究；未删减风险证据", len(prompt))
	}
	if mode == "shadow" {
		return legacy, nil
	}
	return prompt, nil
}
