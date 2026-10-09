package portfolioinspection

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"
)

var investmentFinancialFields = []string{"revenue", "revenue_yoy", "net_profit", "net_profit_yoy", "deducted_net_profit", "deducted_net_profit_yoy", "roe", "gross_margin", "debt_ratio", "operating_cash_flow_per_share", "eps"}

// Financial facts come from the immutable source payload, never narrative numbers
// or default values in FundamentalAnalysis. Conflicting fields remain unavailable.
func InvestmentFinancialFacts(r HoldingResult) map[string]Fact {
	out := map[string]Fact{}
	if !validHoldingResearch(r) {
		return out
	}
	rr := r.Analysis.ResearchReport
	for _, src := range rr.Sources {
		if src.ID != "f-financial" {
			continue
		}
		var payload struct {
			Data   map[string]json.RawMessage `json:"data"`
			Checks []struct {
				ReportDate string   `json:"report_date"`
				Fields     []string `json:"conflicting_fields"`
			} `json:"cross_checks"`
		}
		if json.Unmarshal([]byte(src.Content), &payload) != nil {
			continue
		}
		var date string
		_ = json.Unmarshal(payload.Data["report_date"], &date)
		date = strings.Split(date, " ")[0]
		day, err := time.Parse("2006-01-02", date)
		cutoff := rr.CutoffAt
		if cutoff.IsZero() {
			cutoff = r.ResearchCutoffAt
		}
		validPeriod := err == nil && !cutoff.IsZero() && !day.After(cutoff) && cutoff.Sub(day) <= 270*24*time.Hour && !src.PublishedAt.After(cutoff)
		var meta struct {
			Source    string   `json:"source"`
			Available []string `json:"available_fields"`
			Stale     bool     `json:"stale"`
		}
		_ = json.Unmarshal(payload.Data["meta"], &meta)
		validPeriod = validPeriod && !meta.Stale
		conflicts := map[string]bool{}
		for _, c := range payload.Checks {
			if strings.Split(c.ReportDate, " ")[0] == date {
				for _, field := range c.Fields {
					conflicts[field] = true
				}
			}
		}
		prefix := r.Holding.Symbol + ".financial."
		var periodValue any
		if validPeriod {
			periodValue = date
		}
		out[prefix+"report_date"] = Fact{Value: periodValue, Available: validPeriod, Method: "原财务来源报告期", AsOf: src.CapturedAt}
		var deductedAvailable bool
		_ = json.Unmarshal(payload.Data["deducted_net_profit_available"], &deductedAvailable)
		for _, field := range investmentFinancialFields {
			var value float64
			raw, exists := payload.Data[field]
			available := exists && json.Unmarshal(raw, &value) == nil && string(raw) != "null" && !math.IsNaN(value) && !math.IsInf(value, 0) && validPeriod && !conflicts[field]
			// Primary structs historically serialized absent fields as zero. Explicit
			// provider fields (or sparse supplemental rows) can legitimately report zero.
			if value == 0 && meta.Source != "" && !slices.Contains(meta.Available, field) && !(field == "deducted_net_profit" && deductedAvailable) {
				available = false
			}
			if strings.HasPrefix(field, "deducted_net_profit") {
				if !deductedAvailable {
					available = false
				}
			}
			limitation := "累计财务口径；不能直接年化或跨行业横比；每股现金流不等于现金流总额"
			if conflicts[field] {
				limitation = "同报告期跨来源字段冲突，不能用于判断或计算"
			}
			var factValue any
			if available {
				factValue = value
			}
			out[prefix+field] = Fact{Value: factValue, Available: available, Method: "原来源f-financial；" + date, AsOf: src.CapturedAt, Limitation: limitation}
		}
		break
	}
	return out
}
