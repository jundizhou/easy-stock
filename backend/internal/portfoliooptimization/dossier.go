package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const ModelPromptVersion = "portfolio-optimization-prompts-v31"
const MaxModelPromptBytes = 32 * 1024

// Proposal responses contain all stock judgments. Reviews contain two concise
// scores, so each stage reserves room appropriate to its expected response.
const MaxInitialModelPromptBytes = 22 * 1024

// More than ten researched stocks use compact cards and at most 28 KiB.
// This remains below the 32 KiB hard request ceiling, including repairs.
const MaxLargeInitialModelPromptBytes = 28 * 1024
const MaxReviewModelPromptBytes = 24 * 1024

// A revision includes prior deficiencies; reserve the same 8 KiB response
// space as a review. The shared 32 KiB hard call limit remains unchanged.
const MaxRevisionModelPromptBytes = 24 * 1024

// Deliberately separate the model DTO from persisted research. Adding a field
// to a full report must never silently enlarge optimization model input.
type dossierClaim struct {
	Text    string   `json:"text"`
	Sources []string `json:"source_ids,omitempty"`
}

// Claims use one shared column definition instead of repeating JSON keys.
func (c dossierClaim) MarshalJSON() ([]byte, error) { return json.Marshal([2]any{c.Text, c.Sources}) }

type dossierDriver struct {
	Name          string       `json:"name"`
	EvidenceLevel string       `json:"evidence_level"`
	MarketStatus  string       `json:"market_status"`
	Evidence      dossierClaim `json:"evidence"`
}

func (d dossierDriver) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]any{d.Name, d.MarketStatus, d.Evidence})
}

var stockDossierColumns = []string{"symbol", "name", "report_id", "cutoff_at", "purpose", "horizon", "decision_reason", "prior_position_opinion", "prior_blockers", "thesis", "support", "counter", "business", "drivers", "catalysts", "limitations", "anchors", "sources"}

func shortText(value string, limit int) string {
	r := []rune(value)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return value
}
func stockDossier(r pi.HoldingResult, detail int) map[string]any {
	rr := r.Analysis.ResearchReport
	used := map[string]bool{}
	known := map[string]bool{}
	for _, src := range rr.Sources {
		known[src.ID] = true
	}
	refs := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if known[id] {
				used[id] = true
				out = append(out, id)
				if len(out) == 2 {
					break
				}
			}
		}
		return out
	}
	thesis := dossierClaim{shortText(rr.Thesis.Text, max(detail, 60)), refs(rr.Thesis.SourceIDs)}
	d := map[string]any{"symbol": r.Holding.Symbol, "name": r.Holding.Name, "report_id": r.AnalysisID, "cutoff_at": r.ResearchCutoffAt, "purpose": rr.Request.Purpose, "horizon": rr.Request.Horizon, "evidence_level": rr.EvidenceLevel, "decision": rr.Decision.Status, "thesis": thesis, "decision_reason": shortText(rr.Decision.Reason, detail)}
	counter := []dossierClaim{}
	for i, c := range rr.Counter {
		if i == 2 {
			break
		}
		counter = append(counter, dossierClaim{shortText(c.Text, max(detail, 30)), refs(c.SourceIDs)})
	}
	d["counter"] = counter
	support := []dossierClaim{}
	for _, c := range rr.Support[:min(2, len(rr.Support))] {
		support = append(support, dossierClaim{shortText(c.Text, max(detail, 40)), refs(c.SourceIDs)})
	}
	d["support"] = support
	opinion := rr.Decision.NewPosition
	if r.Holding.Weight > 0 {
		opinion = rr.Decision.ExistingPosition
	}
	d["prior_position_opinion"] = shortText(opinion, max(detail, 40))
	d["prior_blockers"] = rr.Decision.Blockers
	for _, src := range rr.Sources {
		if src.ID == "f-financial" {
			used[src.ID] = true
		}
	}

	limits := []string{}
	for i, v := range rr.Limitations {
		if i == 2 {
			break
		}
		limits = append(limits, shortText(v, detail))
	}
	d["limitations"] = limits
	if logic := rr.TradingLogic; logic != nil {
		if logic.Business != nil {
			d["business"] = dossierClaim{shortText(logic.Business.Text, max(detail, 30)), refs(logic.Business.SourceIDs)}
		}
		drivers := []dossierDriver{}
		for i, v := range logic.Mainlines {
			if i == 3 {
				break
			}
			drivers = append(drivers, dossierDriver{shortText(v.Name, 40), v.EvidenceLevel, v.MarketStatus, dossierClaim{shortText(v.Explanation.Text, max(detail, 30)), refs(v.Explanation.SourceIDs)}})
		}
		d["drivers"] = drivers
		catalysts := []dossierClaim{}
		for _, c := range logic.Catalysts[:min(2, len(logic.Catalysts))] {
			catalysts = append(catalysts, dossierClaim{shortText(c.Text, max(detail, 40)), refs(c.SourceIDs)})
		}
		d["catalysts"] = catalysts
	}
	// Original conditions remain in the report. New portfolio-level conditions
	// use frozen facts and anchors, so legacy ID/threshold narratives are not duplicated.
	anchors := [][4]any{}
	for _, a := range rr.Anchors {
		if finitePositive(a.Price) && known[a.SourceID] {
			used[a.SourceID] = true
			anchors = append(anchors, [4]any{a.ID, a.Price, a.SourceID, a.AsOf})
		}
	}
	d["anchors"] = anchors
	sources := [][3]string{}
	for _, src := range rr.Sources {
		if used[src.ID] {
			sources = append(sources, [3]string{src.ID, src.Kind, src.TimeStatus})
		}
	}
	d["sources"] = sources
	return d
}

// Available fact keys keep their exact canonical IDs and values. Unavailable
// facts remain explicit and cannot be cited as known values. Definitions and
// uncertainty notes occur once in the prompt instead of in every A/B copy.
func dossierFacts(facts map[string]pi.Fact, include func(string) bool) map[string]any {
	known := map[string]any{}
	unknown := []string{}
	for key, f := range facts {
		if !include(key) {
			continue
		}
		if f.Available {
			known[key] = f.Value
		} else {
			unknown = append(unknown, key)
		}
	}
	// Map iteration must not alter payloads or cache identity.
	sort.Strings(unknown)
	return map[string]any{"available": known, "unavailable": unknown}
}

// Share stock fact suffixes across rows without dropping values, availability
// or canonical reference identities. Cross-stock facts keep their exact keys.
func stockFactTable(facts map[string]pi.Fact, results []pi.HoldingResult) map[string]any {
	columns, symbols := map[string]bool{}, map[string]bool{}
	for _, r := range results {
		symbols[r.Holding.Symbol] = true
	}
	include := func(key string) bool {
		return strings.Contains(key, ".") && !strings.HasPrefix(key, "profile.") && !strings.HasSuffix(key, ".weight_percent") && !strings.HasSuffix(key, ".equity_weight_percent") && !strings.Contains(key, ".condition.")
	}
	for key := range facts {
		for symbol := range symbols {
			if include(key) && strings.HasPrefix(key, symbol+".") {
				columns[strings.TrimPrefix(key, symbol+".")] = true
			}
		}
	}
	names := make([]string, 0, len(columns))
	for column := range columns {
		names = append(names, column)
	}
	sort.Strings(names)
	rows := []any{}
	for _, r := range results {
		values, unavailable := make([]any, len(names)), []int{}
		for i, name := range names {
			f, exists := facts[r.Holding.Symbol+"."+name]
			if exists && f.Available {
				values[i] = f.Value
			} else {
				unavailable = append(unavailable, i)
			}
		}
		rows = append(rows, map[string]any{"symbol": r.Holding.Symbol, "values": values, "unavailable": unavailable})
	}
	cross := dossierFacts(facts, func(key string) bool {
		if !include(key) {
			return false
		}
		for symbol := range symbols {
			if strings.HasPrefix(key, symbol+".") {
				return false
			}
		}
		return true
	})
	return map[string]any{"columns": names, "rows": rows, "cross": cross}
}
func commonDossier(job Job, results []pi.HoldingResult, detail int) map[string]any {
	stocks := []any{}
	columns := stockDossierColumns
	if detail <= 0 {
		columns = []string{}
		for _, key := range stockDossierColumns {
			if key != "name" && key != "support" && key != "catalysts" && key != "anchors" && key != "limitations" {
				columns = append(columns, key)
			}
		}
	}
	kinds, times := []string{}, []string{}
	index := func(values *[]string, s string) int {
		for i, v := range *values {
			if v == s {
				return i
			}
		}
		*values = append(*values, s)
		return len(*values) - 1
	}
	for _, r := range results {
		d := stockDossier(r, detail)
		if detail <= 0 {
			d = compactStockDossier(r)
		}
		// Dictionary encode repeated source metadata, keeping actual source IDs
		// untouched. This is lossless and never abbreviates citations.
		sources := []any{}
		for _, s := range d["sources"].([][3]string) {
			sources = append(sources, []any{s[0], index(&kinds, s[1]), index(&times, s[2])})
		}
		d["sources"] = sources
		row := make([]any, len(columns))
		for i, key := range columns {
			row[i] = d[key]
		}
		stocks = append(stocks, row)
	}
	// All successful candidate facts remain available for investment comparisons.
	facts := pi.OptimizationUnionReport(job.Source.Request, results).Facts
	table := stockFactTable(facts, results)
	if detail <= 0 {
		compactCorrelationTable(table, results)
		compactSharedStockFacts(table)
		// Lossless row encoding: null already represents unknown columns.
		rows := []any{}
		for _, item := range table["rows"].([]any) {
			row := item.(map[string]any)
			rows = append(rows, []any{row["symbol"], row["values"]})
		}
		table["rows"] = rows
		table["row_columns"] = []string{"symbol", "values"}
	}
	return map[string]any{"prompt_version": ModelPromptVersion, "horizon": job.Source.Request.Horizon, "profile": job.Source.Profile, "snapshot_at": job.SnapshotAt, "stock_columns": columns, "driver_columns": []string{"name", "market_status", "claim"}, "claim_columns": []string{"text", "source_ids"}, "anchor_columns": []string{"id", "price", "source_id", "as_of"}, "source_columns": []string{"id", "kind_index", "time_index"}, "source_kinds": kinds, "source_times": times, "stocks": stocks, "stock_facts": table}
}

// A shared column retains the exact per-stock fact identity and availability.
// Repeated dates and unknown fields need not consume one value per stock.
func compactSharedStockFacts(table map[string]any) {
	rows := table["rows"].([]any)
	if len(rows) < 4 {
		return
	}
	names := table["columns"].([]string)
	shared, columns, indices := map[string]any{}, []string{}, []int{}
	for i, name := range names {
		value := rows[0].(map[string]any)["values"].([]any)[i]
		equal := true
		for _, item := range rows[1:] {
			if !reflect.DeepEqual(value, item.(map[string]any)["values"].([]any)[i]) {
				equal = false
				break
			}
		}
		if equal {
			shared[name] = value
		} else {
			columns = append(columns, name)
			indices = append(indices, i)
		}
	}
	if len(shared) == 0 {
		return
	}
	for _, item := range rows {
		row := item.(map[string]any)
		old, values := row["values"].([]any), []any{}
		for _, i := range indices {
			values = append(values, old[i])
		}
		row["values"] = values
	}
	table["columns"], table["shared"] = columns, shared
}

// Capacity mode uses a short investment card instead of repeating support,
// catalysts and price anchors already in the individual report. It preserves
// both counterclaim excerpts, prior blockers, business/thesis and numeric facts.
// No price anchors are offered in this mode: new conditions must be semantic.
func compactStockDossier(r pi.HoldingResult) map[string]any {
	d := stockDossier(r, 12)
	for _, key := range []string{"support", "catalysts", "anchors", "limitations"} {
		d[key] = nil
	}
	for key, length := range map[string]int{"thesis": 24, "business": 20} {
		if c, ok := d[key].(dossierClaim); ok {
			c.Text = shortText(c.Text, length)
			d[key] = c
		}
	}
	for _, key := range []string{"prior_position_opinion", "decision_reason"} {
		d[key] = shortText(d[key].(string), 16)
	}
	counter := d["counter"].([]dossierClaim)
	for i := range counter {
		counter[i].Text = shortText(counter[i].Text, 24)
	}
	d["counter"] = counter
	// Keep driver identity and status; the main explanation is in the thesis.
	if drivers, ok := d["drivers"].([]dossierDriver); ok {
		for i := range drivers {
			drivers[i].Evidence.Text = ""
		}
		d["drivers"] = drivers
	}
	used := map[string]bool{"f-financial": true}
	claim := func(c dossierClaim) {
		for _, id := range c.Sources {
			used[id] = true
		}
	}
	for _, key := range []string{"thesis", "business"} {
		if c, ok := d[key].(dossierClaim); ok {
			claim(c)
		}
	}
	for _, c := range counter {
		claim(c)
	}
	if drivers, ok := d["drivers"].([]dossierDriver); ok {
		for _, v := range drivers {
			claim(v.Evidence)
		}
	}
	sources := [][3]string{}
	for _, src := range d["sources"].([][3]string) {
		if used[src[0]] {
			sources = append(sources, src)
		}
	}
	d["sources"] = sources
	return d
}

// Lossless triangular correlation encoding avoids repeating long pair keys.
// Canonical citation IDs are reconstructed from the explicit symbol table.
func compactCorrelationTable(table map[string]any, results []pi.HoldingResult) {
	cross := table["cross"].(map[string]any)
	known := cross["available"].(map[string]any)
	unknown := cross["unavailable"].([]string)
	symbols := []string{}
	for _, r := range results {
		symbols = append(symbols, r.Holding.Symbol)
	}
	unknownSet := map[string]bool{}
	for _, key := range unknown {
		unknownSet[key] = true
	}
	rows := []any{}
	for i, a := range symbols {
		for k, b := range symbols {
			if i == k {
				continue
			}
			key := "correlation." + a + "." + b
			if value, ok := known[key]; ok {
				rows = append(rows, []any{i, k, value})
				delete(known, key)
			} else if unknownSet[key] {
				rows = append(rows, []any{i, k, nil})
				delete(unknownSet, key)
			}
		}
	}
	remaining := []string{}
	for _, key := range unknown {
		if unknownSet[key] {
			remaining = append(remaining, key)
		}
	}
	cross["unavailable"] = remaining
	table["correlation_symbols"] = symbols
	table["correlations"] = rows
}
func modelConfiguration(r pi.Report, groups []pi.RiskGroup) map[string]any {
	weights := map[string]int{}
	for _, h := range r.Request.Holdings {
		weights[h.Symbol] = h.Weight
	}
	// Driver names, membership and evidence are shared. Only exposure differs.
	exposures := map[string]int{}
	for _, g := range frozenGroups(groups, r.Request.Holdings) {
		exposures[g.Name] = g.Weight
	}
	// Stock weights already occur above. Their canonical fact identity remains
	// symbol + ".weight_percent"; do not repeat the same values in both maps.
	return map[string]any{"weights": weights, "portfolio_facts": dossierFacts(r.Facts, func(key string) bool { return !strings.Contains(key, ".") && key != "max_high_risk_percent" }), "risk_exposures": exposures}
}

// Conditions are common research inputs for both sides, never an assertion that
// orders have executed. The configuration lists only exits for actual holdings.
func reviewConfiguration(job Job, plan Plan, r pi.Report) map[string]any {
	weights := map[string]float64{}
	for _, h := range r.Request.Holdings {
		weights[h.Symbol] = pi.EquityPercent(h.Weight, r.Metrics.TotalPositionPercent)
	}
	exposures := map[string]float64{}
	for _, g := range frozenGroups(job.Proposal.RiskGroups, r.Request.Holdings) {
		exposures[g.Name] = pi.EquityPercent(g.Weight, r.Metrics.TotalPositionPercent)
	}
	data := map[string]any{"weights": weights, "portfolio_facts": dossierFacts(pi.ScoringFacts(r.Facts), func(key string) bool { return !strings.Contains(key, ".") }), "equity_risk_exposures": exposures}
	exits := map[string][]any{}
	for _, holding := range r.Holdings {
		symbol := holding.Holding.Symbol
		rr := holding.Analysis.ResearchReport
		for _, id := range rr.InvalidationIDs {
			for _, c := range rr.Conditions {
				if c.ID == id && c.Text != "" {
					exits[symbol] = append(exits[symbol], []any{"research", c.ID})
				}
			}
		}
		for _, allocation := range plan.Allocations {
			if allocation.Symbol != symbol || validateAllocationConditions(job, allocation) != nil {
				continue
			}
			for index, c := range allocation.Conditions {
				if c.Kind == "exit" {
					exits[symbol] = append(exits[symbol], []any{"portfolio", index})
				}
			}
		}
	}
	data["exit_plans"] = exits
	return data
}
func boundedModelPrompt(build func(int) (string, error)) (string, error) {
	return boundedModelPromptWithLimit(MaxInitialModelPromptBytes, build)
}
func boundedModelPromptWithLimit(limit int, build func(int) (string, error)) (string, error) {
	smallest := 0
	for _, detail := range []int{90, 60, 30, 12, 0} {
		prompt, err := build(detail)
		if err != nil {
			return "", err
		}
		if len(prompt) <= limit {
			return prompt, nil
		}
		smallest = len(prompt)
	}
	return "", fmt.Errorf("优化必要证据仍为%d字节，超过%d KiB本阶段初次输入上限；已保留报告，不启动超大模型请求", smallest, limit/1024)
}
func modelJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	return string(data), err
}
