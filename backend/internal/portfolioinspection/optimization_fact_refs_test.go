package portfolioinspection

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestComparisonFactPathsResolveExactOwnAvailableValues(t *testing.T) {
	req, results, _, score := scoreFixture()
	r := OptimizationReport(req, results)
	r.Conclusion.RiskGroups = []RiskGroup{{Name: "共同驱动", Symbols: []string{req.Holdings[0].Symbol}}}
	score.Holdings, score.Scenarios = nil, nil
	for _, key := range []string{"concentration_hhi", "profile.equity_max_top_three_percent", "profile.scoring_description", "equity_risk_exposures.共同驱动"} {
		for _, path := range []string{"a." + key, "a.portfolio_facts.available." + key, "portfolio_facts.available." + key, "portfolio_facts." + key, "a.portfolio_facts." + key} {
			score.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: path}}
			data, _ := json.Marshal(score)
			decoded, err := DecodeNamedOptimizationComparisonScore(data, r, "a")
			if err != nil || decoded.Dimensions[0].EvidenceRefs[0].Fact != key || *decoded.TotalScore != 71 {
				t.Fatal("available path must preserve original score and canonical fact", path, decoded, err)
			}
		}
	}
	for _, path := range []string{"b.portfolio_facts.available.concentration_hhi", "a.portfolio_facts.available.invented", "a.portfolio_facts.available.known_stop_loss_risk_percent", "a.risk_exposures.未知驱动", "profile.unknown"} {
		score.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: path}}
		data, _ := json.Marshal(score)
		if _, err := DecodeNamedOptimizationComparisonScore(data, r, "a"); err == nil {
			t.Fatal("unknown, unavailable or opposite-side path accepted", path)
		}
	}
}

func TestComparisonRulesAndFrozenExposuresAreComputedFacts(t *testing.T) {
	req, results, _, score := scoreFixture()
	r := OptimizationReport(req, results)
	r.Conclusion.RiskGroups = []RiskGroup{{Name: "共同驱动", Symbols: []string{req.Holdings[0].Symbol, req.Holdings[0].Symbol, "absent"}}, {Name: "未持有驱动", Symbols: []string{"absent"}}}
	facts := OptimizationComparisonFacts(r)
	rules, _ := RulesFor(req.TraderProfile)
	for key, want := range map[string]any{"profile.max_single_percent": rules.MaxSinglePercent, "profile.max_top_three_percent": rules.MaxTopThreePercent, "profile.minimum_cash_percent": rules.MinimumCashPercent, "profile.max_high_risk_percent": rules.MaxHighRiskPercent, "profile.max_stop_loss_risk_percent": rules.MaxStopLossRisk, "profile.preferred_short_term_max_percent": rules.PreferredShortTermMax, "risk_exposures.共同驱动": 60, "risk_exposures.未持有驱动": 0} {
		f := facts[key]
		if !f.Available || !reflect.DeepEqual(f.Value, want) || f.Method == "" || f.Limitation == "" {
			t.Fatal("rule/exposure differs from supplied configuration", key, f, want)
		}
	}
	if _, ok := facts["risk_exposures.未知驱动"]; ok {
		t.Fatal("invented group became a fact")
	}
	score.Holdings, score.Scenarios = nil, nil
	score.Dimensions[0].EvidenceRefs = []EvidenceRef{{Fact: "equity_risk_exposures.共同驱动"}}
	data, _ := json.Marshal(score)
	if _, err := DecodeOptimizationComparisonScore(data, r); err != nil {
		t.Fatal("actual frozen exposure rejected", err)
	}
	// Ordinary inspection has no frozen optimization group, so this extension
	// must not make an arbitrary group a fact in its validator.
	if err := validateScoringReportMode(&score, req, r.Holdings, r.Metrics, false); err == nil || !strings.Contains(err.Error(), "不可用事实引用") {
		t.Fatal("ordinary inspection accepted optimization-only fact", err)
	}
	r.Request.TraderProfile = "unknown"
	if OptimizationComparisonFacts(r)["profile.max_single_percent"].Available {
		t.Fatal("unknown profile must not supply a valid rule")
	}
}

func TestCorrelationAliasesRequireAnExistingMeasuredPair(t *testing.T) {
	facts := map[string]Fact{"correlation.600519.SH.000858.SZ": {Value: .4, Available: true}, "correlation.600519.SH.000001.SZ": {Available: false}}
	for _, key := range []string{"correlation.000858.SZ.600519.SH", "cross.correlation.000858.SZ.600519.SH", "stock_facts.cross.available.correlation.000858.SZ.600519.SH"} {
		if got := OptimizationFactAlias(key, facts); got != "correlation.600519.SH.000858.SZ" {
			t.Fatal("actual symmetric pair not resolved", got)
		}
	}
	for _, key := range []string{"correlation.000001.SZ.600519.SH", "correlation.unknown.unknown", "correlation.600519.SH.601169.SH"} {
		if facts[OptimizationFactAlias(key, facts)].Available {
			t.Fatal("unknown/unmeasured pair accepted", key)
		}
	}
}
