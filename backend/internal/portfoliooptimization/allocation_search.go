package portfoliooptimization

import (
	"context"
	"fmt"
	"math"
	"sort"

	pi "easy-stock/backend/internal/portfolioinspection"
)

// This objective ranks feasible weights; it is not the inspection score. Only
// an independent review can produce or certify a 70-point portfolio score.
type AllocationSearch struct {
	Method            string  `json:"method"`
	Evaluated         int     `json:"evaluated_allocations"`
	ProfitablePercent int     `json:"profitable_weight_percent"`
	LossPercent       int     `json:"deducted_loss_weight_percent"`
	VolatilityProxy   float64 `json:"volatility_proxy_percent"`
}
type qualitySolution struct {
	Target []pi.Holding
	Search AllocationSearch
}
type allocationModel struct {
	job                          Job
	alt                          Alternative
	original, baseline           map[string]int
	lo, hi                       []int
	merit, volatility            []float64
	profitable, loss             []bool
	corr                         [][]float64
	groups                       [][]int
	groupLimits                  []int
	total, missingSold, topLimit int
	riskAversion                 float64
}
type allocationState struct {
	values                    []int
	total, sold, added, count int
	utility                   float64
}

func qualityAllocationModel(job Job, alt Alternative) (allocationModel, error) {
	return buildQualityAllocationModel(job, alt, true)
}

func buildQualityAllocationModel(job Job, alt Alternative, investmentValidated bool) (allocationModel, error) {
	m := allocationModel{job: job, alt: alt}
	var err error
	m.original, _, err = weights(job.Source.Request.Holdings, false)
	if err != nil {
		return m, err
	}
	m.baseline, m.total, err = weights(job.Baseline, false)
	if err != nil {
		return m, err
	}
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	m.topLimit = max(rules.MaxTopThreePercent, topWeight(job.Source.Request.Holdings))
	m.riskAversion = .22
	if job.Source.Request.TraderProfile == pi.ProfileSteady {
		m.riskAversion = .45
	}
	if job.Source.Request.TraderProfile == pi.ProfileAggressive {
		m.riskAversion = .1
	}
	elig := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		elig[e.Symbol] = e
	}
	facts := pi.OptimizationUnionReport(job.Source.Request, job.Results).Facts
	number := func(key string) (float64, bool) {
		f := facts[key]
		v, ok := f.Value.(float64)
		return v, ok && f.Available && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	seen := map[string]bool{}
	index := map[string]int{}
	for i, a := range alt.Allocations {
		if seen[a.Symbol] {
			return m, fmt.Errorf("重复股票%s", a.Symbol)
		}
		seen[a.Symbol] = true
		index[a.Symbol] = i
		r, ok := researchFor(job, a.Symbol)
		if !ok {
			return m, fmt.Errorf("未研究股票%s", a.Symbol)
		}
		if a.Minimum < 0 || a.Maximum > 100 || a.Minimum > a.Maximum || a.Preferred < a.Minimum || a.Preferred > a.Maximum {
			return m, fmt.Errorf("%s权重范围无效", a.Symbol)
		}
		lo, hi := a.Minimum, a.Maximum
		if investmentValidated {
			lo, hi = allocationBounds(job, a, m.original[a.Symbol], rules, elig[a.Symbol])
		} else {
			// Pre-investment reference only: trading and profile constraints apply,
			// but no investment permission has been claimed or granted yet.
			if !elig[a.Symbol].CanIncrease {
				hi = min(hi, m.original[a.Symbol])
			}
			if elig[a.Symbol].Locked {
				lo, hi = max(lo, m.original[a.Symbol]), min(hi, m.original[a.Symbol])
			}
			hi = min(hi, max(m.original[a.Symbol], rules.MaxSinglePercent))
		}
		if lo > hi {
			return m, fmt.Errorf("%s投资范围与交易权限冲突", a.Symbol)
		}
		m.lo = append(m.lo, lo)
		m.hi = append(m.hi, hi)
		// Compare signs and bounded growth within each company, never absolute
		// bank/industrial profits, raw debt ratios or low-base growth multiples.
		prefix := a.Symbol + ".financial."
		profit, pok := number(prefix + "deducted_net_profit")
		net, nok := number(prefix + "net_profit")
		merit := 0.0
		if pok {
			if profit > 0 {
				merit += 3
			} else if profit < 0 {
				merit -= 3
			}
		}
		if nok {
			if net > 0 {
				merit += .5
			} else if net < 0 {
				merit -= 1
			}
		}
		if growth, ok := number(prefix + "revenue_yoy"); ok {
			merit += .35 * math.Max(-1, math.Min(1, growth/30))
		}
		if growth, ok := number(prefix + "deducted_net_profit_yoy"); ok && pok && profit > 0 {
			merit += .35 * math.Max(-1, math.Min(1, growth/50))
		}
		bank := IndustryGroup(r.Analysis.Theme.Primary) == "银行"
		for _, c := range job.Candidates {
			if c.Symbol == a.Symbol && IndustryGroup(c.Industry) == "银行" {
				bank = true
			}
		}
		if !bank {
			// A tiny positive deducted profit does not have the same quality as
			// recurring earnings dominating the company's own net profit. The
			// ratio is within-company; no absolute-profit comparison across sectors.
			if pok && nok && profit > 0 && net > 0 {
				merit -= 2 * math.Max(0, 1-math.Min(1, profit/net))
			}
			if debt, ok := number(prefix + "debt_ratio"); ok {
				merit -= math.Max(0, math.Min(1, (debt-65)/20))
			}
			if cash, ok := number(prefix + "operating_cash_flow_per_share"); ok {
				if cash > 0 {
					merit += .5
				} else if cash < 0 {
					merit -= .5
				}
			}
		}
		// Stable, reasonably valued earnings can offset the entire capped growth
		// bonus. Ratios are never scored as "the cheaper, the better".
		peMax, pbMax := CandidatePolicy.ValuePEMax, CandidatePolicy.ValuePBMax
		if bank {
			peMax, pbMax = CandidatePolicy.BankPEMax, CandidatePolicy.BankPBMax
		}
		pe, peOK := number(a.Symbol + ".valuation.pe_ttm")
		pb, pbOK := number(a.Symbol + ".valuation.pb")
		rev, revOK := number(prefix + "revenue_yoy")
		growth, growthOK := number(prefix + "deducted_net_profit_yoy")
		cash, cashOK := number(prefix + "operating_cash_flow_per_share")
		if pok && nok && profit > 0 && net > 0 && profit/net >= .5 &&
			peOK && pbOK && pe > 0 && pe <= peMax && pb > 0 && pb <= pbMax &&
			revOK && rev >= CandidatePolicy.StableRevenueMin && growthOK && growth >= CandidatePolicy.StableProfitMin && cashOK && cash > 0 {
			merit += .7
		}
		m.merit = append(m.merit, merit)
		m.profitable = append(m.profitable, pok && nok && profit > 0 && net > 0)
		m.loss = append(m.loss, pok && profit < 0)
		vol := r.Analysis.Trend.ATR14Percent
		if !finitePositive(vol) {
			vol = 5
		} // unknown is not zero risk
		m.volatility = append(m.volatility, vol)
	}
	for s, w := range m.baseline {
		if !seen[s] {
			m.missingSold += w
		}
	}
	for s := range m.original {
		if !seen[s] {
			return m, fmt.Errorf("缺少原持仓%s", s)
		}
	}
	for i, a := range alt.Allocations {
		row := make([]float64, len(alt.Allocations))
		for k, b := range alt.Allocations {
			row[k] = 1 // unknown correlation is conservatively fully linked
			if i == k {
				continue
			}
			v, ok := number("correlation." + a.Symbol + "." + b.Symbol)
			if !ok {
				v, ok = number("correlation." + b.Symbol + "." + a.Symbol)
			}
			if ok {
				row[k] = math.Max(0, math.Min(1, v))
			} // no credit for unstable negative hedges
		}
		m.corr = append(m.corr, row)
	}
	for _, g := range groupRiskChecks(job, job.Source.Request.Holdings) {
		if g.Hard {
			members := []int{}
			for _, s := range g.Symbols {
				if i, ok := index[s]; ok {
					members = append(members, i)
				}
			}
			m.groups = append(m.groups, members)
			m.groupLimits = append(m.groupLimits, g.Limit)
		}
	}
	highRisk, originalRisk := []int{}, 0
	for _, r := range job.Results {
		if r.Analysis != nil && r.Analysis.RiskControl.Score >= 70 {
			if i, ok := index[r.Holding.Symbol]; ok {
				highRisk = append(highRisk, i)
			}
			originalRisk += m.original[r.Holding.Symbol]
		}
	}
	if len(highRisk) > 0 {
		m.groups = append(m.groups, highRisk)
		m.groupLimits = append(m.groupLimits, max(originalRisk, rules.MaxHighRiskPercent))
	}
	return m, nil
}

// These are the same hard bounds used by final admission. Prune while weights
// are built, instead of spending an AI review on an already blocked solution.
func (m allocationModel) riskWithinLimits(values []int) bool {
	for n, group := range m.groups {
		weight := 0
		for _, i := range group {
			if i < len(values) {
				weight += values[i]
			} else {
				weight += m.lo[i]
			}
		}
		if weight > m.groupLimits[n] {
			return false
		}
	}
	return true
}

func topWeight(h []pi.Holding) int {
	v := []int{}
	for _, s := range h {
		v = append(v, s.Weight)
	}
	return topValues(v)
}
func topValues(v []int) int {
	a, b, c := 0, 0, 0
	for _, w := range v {
		if w > a {
			a, b, c = w, a, b
		} else if w > b {
			b, c = w, b
		} else if w > c {
			c = w
		}
	}
	return a + b + c
}

func (m allocationModel) utility(v []int, risk float64) (float64, float64) {
	quality, variance, hhi := 0.0, 0.0, 0.0
	for i, w := range v {
		p := float64(w) / float64(m.total)
		quality += p * m.merit[i]
		hhi += p * p
		for k, x := range v {
			variance += p * float64(x) / float64(m.total) * m.volatility[i] * m.volatility[k] * m.corr[i][k]
		}
	}
	penalty := 0.0
	for _, group := range m.groups {
		weight := 0
		for _, i := range group {
			if i < len(v) {
				weight += v[i]
			}
		}
		excess := math.Max(0, float64(weight)/float64(m.total)-.55)
		penalty += 6 * excess * excess
	}
	return quality - risk*variance - 2*hhi - penalty, math.Sqrt(variance)
}

// A bounded deterministic beam explores exact 1% weights inside validated
// investment ranges. Partial states retain many alternatives at each total;
// style, turnover, new-stock limits and suffix feasibility apply during search,
// rather than rejecting a nearest-preference allocation afterward.
func SearchAllocations(ctx context.Context, job Job, alt Alternative) ([]qualitySolution, error) {
	m, err := qualityAllocationModel(job, alt)
	if err != nil {
		return nil, err
	}
	return searchAllocationModel(ctx, job, alt, m)
}

func searchAllocationModel(ctx context.Context, job Job, alt Alternative, m allocationModel) ([]qualitySolution, error) {
	if len(alt.Allocations) > pi.MaxHoldings+MaxCandidateResearch {
		return nil, fmt.Errorf("研究股票数量超过10只原股加6只候选")
	}
	solutions := []qualitySolution{}
	evaluated := 0
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	allowedTop := m.topLimit
	for mode, risk := range []float64{m.riskAversion, m.riskAversion * 2, m.riskAversion * .5} {
		// First solve inside the profile reference. Merely staying below an
		// already excessive original top-three weight does not repair strategy
		// fit. If that subproblem is infeasible, the second search keeps the
		// original non-worsening boundary rather than dropping a valid solution.
		m.topLimit = allowedTop
		if mode != 1 {
			m.topLimit = min(allowedTop, rules.MaxTopThreePercent)
		}
		beam := []allocationState{{}}
		for i, a := range alt.Allocations {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			suffixLo, suffixHi, suffixSold := 0, 0, m.missingSold
			for k := i + 1; k < len(m.lo); k++ {
				suffixLo += m.lo[k]
				suffixHi += m.hi[k]
				suffixSold += max(0, m.baseline[alt.Allocations[k].Symbol]-m.hi[k])
			}
			buckets := map[int][]allocationState{}
			for _, v := range beam {
				for w := m.lo[i]; w <= m.hi[i]; w++ {
					evaluated++
					if evaluated%1024 == 0 {
						if err := ctx.Err(); err != nil {
							return nil, err
						}
					}
					s := allocationState{total: v.total + w, sold: v.sold + max(0, m.baseline[a.Symbol]-w), added: v.added, count: v.count}
					if w > 0 {
						s.count++
						if m.baseline[a.Symbol] == 0 {
							s.added++
						}
					}
					if s.total+suffixLo > m.total || s.total+suffixHi < m.total || (s.sold+suffixSold)*100 > 70*m.total || s.count > 10 || s.added > 2 {
						continue
					}
					s.values = append(append([]int{}, v.values...), w)
					if topValues(s.values) > m.topLimit || !m.riskWithinLimits(s.values) {
						continue
					}
					s.utility, _ = m.utility(s.values, risk)
					// Separate new-stock counts so a attractive third candidate cannot
					// evict every path that leaves capacity for later stocks.
					key := s.total*3 + s.added
					buckets[key] = append(buckets[key], s)
				}
			}
			beam = nil
			keys := []int{}
			for key := range buckets {
				keys = append(keys, key)
			}
			sort.Ints(keys)
			for _, key := range keys {
				states := buckets[key]
				sort.Slice(states, func(i, j int) bool {
					if math.Abs(states[i].utility-states[j].utility) > 1e-9 {
						return states[i].utility > states[j].utility
					}
					if states[i].sold != states[j].sold {
						return states[i].sold < states[j].sold
					}
					return lexLess(states[i].values, states[j].values)
				})
				beam = append(beam, states[:min(64, len(states))]...)
			}
		}
		if len(beam) == 0 {
			continue
		}
		sort.Slice(beam, func(i, j int) bool {
			if math.Abs(beam[i].utility-beam[j].utility) > 1e-9 {
				return beam[i].utility > beam[j].utility
			}
			if beam[i].sold != beam[j].sold {
				return beam[i].sold < beam[j].sold
			}
			return lexLess(beam[i].values, beam[j].values)
		})
		for _, state := range beam {
			target := m.holdings(state.values)
			if !Check(job.Baseline, target).Valid {
				continue
			}
			duplicate := false
			for _, s := range solutions {
				if !materiallyDifferent(s.Target, target) {
					duplicate = true
				}
			}
			if duplicate || repeatedRejectedTarget(job, target) {
				continue
			}
			_, vol := m.utility(state.values, risk)
			d := AllocationSearch{Method: []string{"风格内盈利质量与风险均衡", "优先降低组合波动", "风格内盈利增长与互补"}[mode], VolatilityProxy: math.Round(vol*100) / 100}
			for i, w := range state.values {
				if m.profitable[i] {
					d.ProfitablePercent += w
				}
				if m.loss[i] {
					d.LossPercent += w
				}
			}
			solutions = append(solutions, qualitySolution{Target: target, Search: d})
			break
		}
	}
	for i := range solutions {
		solutions[i].Search.Evaluated = evaluated
	}
	if len(solutions) == 0 {
		return nil, fmt.Errorf("当前投资范围、交易锁定、风险集中及累计变动约束内未找到新的可行仓位")
	}
	return solutions, nil
}

// A one-point reshuffle is not a fresh investment solution. Do not spend a
// model call resampling nearly identical portfolios until a score happens to pass.
func materiallyDifferent(a, b []pi.Holding) bool {
	x, total, _ := weights(a, false)
	y, _, _ := weights(b, false)
	delta := 0
	for symbol, w := range x {
		delta += abs(w - y[symbol])
		delete(y, symbol)
	}
	for _, w := range y {
		delta += w
	}
	return delta >= 2*max(1, (total+19)/20)
}
func (m allocationModel) holdings(v []int) []pi.Holding {
	out := []pi.Holding{}
	for i, w := range v {
		if w == 0 {
			continue
		}
		r, _ := researchFor(m.job, m.alt.Allocations[i].Symbol)
		h := r.Holding
		h.Weight = w
		h.CostPrice = nil
		for _, old := range m.job.Source.Request.Holdings {
			if old.Symbol == h.Symbol && w <= old.Weight {
				h.CostPrice = old.CostPrice
			}
		}
		out = append(out, h)
	}
	return out
}
