package monitor

import (
	"context"
	"time"

	"easy-stock/backend/internal/foundation"
)

type WatchRuleCoverage struct {
	Symbol    string `json:"symbol"`
	Status    string `json:"status"`
	RuleCount int    `json:"rule_count"`
	Reason    string `json:"reason,omitempty"`
}

type WatchCoverageView struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Engine      Status              `json:"engine"`
	Items       []WatchRuleCoverage `json:"items"`
	Error       string              `json:"error,omitempty"`
}

// WatchCoverage 只读当前启用规则的适用范围，不评估条件、不发送通知。
// 非全市场范围复用引擎的作用域解析；全市场范围复用过滤器及各任务的选股上限。
func (e *Engine) WatchCoverage(ctx context.Context, items []WatchItem) WatchCoverageView {
	view := WatchCoverageView{GeneratedAt: e.now(), Engine: e.CurrentStatus(), Items: []WatchRuleCoverage{}}
	if e.settings != nil {
		view.Engine.Enabled = e.settings().Enabled
	}
	rules, err := e.store.ListRules(ctx, "", true)
	if err != nil {
		view.Error = "无法读取监控规则"
	}
	e.mu.Lock()
	rows := e.snapRows
	e.mu.Unlock()
	knownRows := map[string]bool{}
	for _, row := range rows {
		knownRows[row.Symbol] = true
		knownRows[sixDigitCode(row.Symbol)] = true
	}
	type selection struct {
		rule    Rule
		symbols map[string]bool
		err     error
	}
	selections := []selection{}
	for _, rule := range rules {
		if !quoteRules[rule.Type] && rule.Type != RuleTypeSignal && rule.Type != RuleTypeDeviation {
			continue // 大盘、板块和日期规则不等同于个股行情提醒。
		}
		entry := selection{rule: rule, symbols: map[string]bool{}}
		if rule.Scope == ScopeAll {
			entry.symbols = e.coverageSnapshotSymbols(ctx, rule, rows)
		} else {
			var symbols []string
			symbols, entry.err = e.resolveRuleSymbols(ctx, rule)
			for _, symbol := range symbols {
				entry.symbols[symbol] = true
			}
		}
		selections = append(selections, entry)
	}
	for _, item := range items {
		coverage := WatchRuleCoverage{Symbol: item.Symbol, Status: "unconfigured"}
		unknown := err != nil
		for _, entry := range selections {
			if entry.err != nil || (entry.rule.Scope == ScopeAll && !knownRows[item.Symbol] && !knownRows[sixDigitCode(item.Symbol)]) {
				unknown = true
				continue
			}
			if entry.symbols[item.Symbol] || (entry.rule.Scope == ScopeAll && entry.symbols[sixDigitCode(item.Symbol)]) {
				coverage.RuleCount++
			}
		}
		switch {
		case coverage.RuleCount > 0:
			coverage.Status = "covered"
		case unknown:
			coverage.Status, coverage.Reason = "unknown", "规则范围或全市场快照不可用，暂无法确认"
		default:
			coverage.Reason = "当前没有适用于该标的的启用个股规则"
		}
		view.Items = append(view.Items, coverage)
	}
	return view
}

func (e *Engine) coverageSnapshotSymbols(ctx context.Context, rule Rule, rows []foundation.MarketSnapshotRow) map[string]bool {
	selected := map[string]bool{}
	add := func(symbol string) {
		selected[symbol] = true
		if code := sixDigitCode(symbol); code != "" {
			selected[code] = true
		}
	}
	limit := scanRowLimit
	if rule.Type == RuleTypeSignal && rule.Filters.MaxSymbols > 0 && rule.Filters.MaxSymbols < limit {
		limit = rule.Filters.MaxSymbols
	}
	if rule.Type != RuleTypeDeviation {
		count := 0
		for _, row := range rows {
			if !rule.Filters.accepts(row) {
				continue
			}
			if count >= limit {
				break
			}
			add(row.Symbol)
			count++
		}
	}
	highLow := false
	if rule.Type == RuleTypeAbnormal {
		for _, condition := range rule.Conditions {
			highLow = highLow || condition.Field == "high_60d" || condition.Field == "low_60d"
		}
	}
	if rule.Type == RuleTypeDeviation || highLow {
		for _, symbol := range e.deviationCandidates(ctx, rows) {
			if covered, err := e.ruleCoversSymbol(ctx, rule, symbol); err == nil && covered {
				add(symbol)
			}
		}
	}
	return selected
}
