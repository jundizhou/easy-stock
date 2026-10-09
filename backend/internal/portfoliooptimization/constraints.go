package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func weights(holdings []pi.Holding, allowZero bool) (map[string]int, int, error) {
	out := map[string]int{}
	total := 0
	for _, h := range holdings {
		symbol, err := foundation.NormalizeSymbol(h.Symbol)
		if err != nil || symbol.Canonical != h.Symbol {
			return nil, 0, fmt.Errorf("股票代码必须规范化：%s", h.Symbol)
		}
		if _, ok := out[h.Symbol]; ok {
			return nil, 0, errors.New("重复股票代码")
		}
		if h.Weight < 0 || h.Weight > 100 || (!allowZero && h.Weight == 0) {
			return nil, 0, errors.New("仓位必须为1至100的整数")
		}
		out[h.Symbol] = h.Weight
		total += h.Weight
	}
	if total < 1 || total > 100 {
		return nil, 0, errors.New("总股票仓位必须在1%至100%之间")
	}
	return out, total, nil
}
func Check(baseline, target []pi.Holding) Checks {
	c := Checks{Maximum: 70, Mode: "replacement_share", Errors: []string{}}
	old, p, err := weights(baseline, false)
	if err != nil {
		c.Errors = append(c.Errors, err.Error())
		return c
	}
	next, n, err := weights(target, true)
	if err != nil {
		c.Errors = append(c.Errors, err.Error())
		return c
	}
	c.Total = n
	c.Cash = 100 - n
	if p != n {
		c.Errors = append(c.Errors, "总股票仓位及现金必须保持不变")
	}
	count, added := 0, 0
	for s, w := range next {
		if w > 0 {
			count++
			if old[s] == 0 {
				added++
			}
		}
		if w > old[s] {
			c.Bought += w - old[s]
		}
	}
	for s, w := range old {
		c.Retained += min(w, next[s])
		if w > next[s] {
			c.Sold += w - next[s]
		}
	}
	c.Replacement = float64(c.Sold) * 100 / float64(p)
	if c.Sold != c.Bought {
		c.Errors = append(c.Errors, "卖出资金与买入资金不相等")
	}
	if c.Sold*100 > 70*p {
		c.Errors = append(c.Errors, "相对初始组合替换比例超过70%")
	}
	if count > 10 || added > 2 {
		c.Errors = append(c.Errors, "目标最多10只持仓，最多新增2只")
	}
	c.Valid = len(c.Errors) == 0
	return c
}

// Solve allocates exact 1% units inside validated investment ranges. Dynamic
// programming keeps target sum and cumulative turnover in its state; it never
// fills a missing preferred total. Minimum deviation, then least trading.
func Solve(job Job, alt Alternative) ([]pi.Holding, error) {
	return SolveContext(context.Background(), job, alt)
}
func SolveContext(ctx context.Context, job Job, alt Alternative) ([]pi.Holding, error) {
	original, _, err := weights(job.Source.Request.Holdings, false)
	if err != nil {
		return nil, err
	}
	base, p, err := weights(job.Baseline, false)
	if err != nil {
		return nil, err
	}
	if len(alt.Allocations) > 30 {
		return nil, errors.New("建议股票数量过多")
	}
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, r := range job.Results {
		known[r.Holding.Symbol] = pi.ValidOptimizationResearch(r)
	}
	elig := map[string]Eligibility{}
	for _, e := range job.Eligibility {
		elig[e.Symbol] = e
	}
	rules, _ := pi.RulesFor(job.Source.Request.TraderProfile)
	type key struct{ total, sold, added, count int }
	type state struct {
		score  int
		values []int
	}
	dp := map[key]state{{}: {}}
	for _, a := range alt.Allocations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[a.Symbol] || !known[a.Symbol] {
			return nil, fmt.Errorf("未研究或重复股票：%s", a.Symbol)
		}
		seen[a.Symbol] = true
		if a.Minimum < 0 || a.Maximum > 100 || a.Maximum < a.Minimum || a.Preferred < a.Minimum || a.Preferred > a.Maximum {
			return nil, errors.New("AI建议范围无效")
		}
		ow := original[a.Symbol]
		lo, hi := allocationBounds(job, a, ow, rules, elig[a.Symbol])
		next := map[key]state{}
		for k, v := range dp {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for w := lo; w <= hi; w++ {
				nk := key{k.total + w, k.sold + max(base[a.Symbol]-w, 0), k.added, k.count}
				if w > 0 {
					nk.count++
					if base[a.Symbol] == 0 {
						nk.added++
					}
				}
				if nk.total > p || nk.sold*100 > 70*p || nk.added > 2 || nk.count > 10 {
					continue
				}
				score := v.score + abs(w-a.Preferred)*1000 + abs(w-ow)
				prior, ok := next[nk]
				if !ok || score < prior.score || (score == prior.score && lexLess(append(append([]int{}, v.values...), w), prior.values)) {
					vals := append(append([]int{}, v.values...), w)
					next[nk] = state{score, vals}
				}
			}
		}
		dp = next
	}
	for s := range original {
		if !seen[s] {
			return nil, fmt.Errorf("建议范围缺少原持仓%s（清仓也须包含）", s)
		}
	}
	// Initial stocks cleared earlier in a chain still consume cumulative turnover.
	missingSold := 0
	for s, w := range base {
		if !seen[s] {
			missingSold += w
		}
	}
	best := state{score: math.MaxInt}
	var bestKey key
	keys := make([]key, 0, len(dp))
	for k := range dp {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].sold != keys[j].sold {
			return keys[i].sold < keys[j].sold
		}
		if keys[i].added != keys[j].added {
			return keys[i].added < keys[j].added
		}
		return keys[i].count < keys[j].count
	})
	for _, k := range keys {
		v := dp[k]
		if k.total == p && (k.sold+missingSold)*100 <= 70*p && (v.score < best.score || (v.score == best.score && lexLess(v.values, best.values))) {
			best = v
			bestKey = k
		}
	}
	_ = bestKey
	if best.values == nil {
		return nil, errors.New("投资权重范围、交易资格和变动预算下无可行整数仓位")
	}
	out := []pi.Holding{}
	for i, a := range alt.Allocations {
		if best.values[i] == 0 {
			continue
		}
		h := pi.Holding{Symbol: a.Symbol, Weight: best.values[i]}
		for _, r := range job.Results {
			if r.Holding.Symbol == a.Symbol {
				h.Name = r.Holding.Name
				break
			}
		}
		for _, old := range job.Source.Request.Holdings {
			if old.Symbol == a.Symbol && h.Weight <= old.Weight {
				h.CostPrice = old.CostPrice
			}
		}
		out = append(out, h)
	}
	c := Check(job.Baseline, out)
	if !c.Valid {
		return nil, errors.New(strings.Join(c.Errors, "；"))
	}
	if err := styleCheck(job.Source.Request.Holdings, out, rules); err != nil {
		return nil, err
	}
	return out, nil
}

// Shared effective bounds keep numeric repair and integer solving consistent.
func allocationBounds(job Job, a Allocation, original int, rules pi.ProfileRules, e Eligibility) (int, int) {
	lo, hi := a.Minimum, a.Maximum
	if e.Locked {
		lo = max(lo, original)
		hi = min(hi, original)
	}
	if !e.CanIncrease || !canIncrease(job, a) {
		hi = min(hi, original)
	}
	hi = min(hi, max(original, rules.MaxSinglePercent))
	return lo, hi
}

func styleCheck(old, next []pi.Holding, rules pi.ProfileRules) error {
	top := func(hs []pi.Holding) int {
		ws := []int{}
		for _, h := range hs {
			ws = append(ws, h.Weight)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(ws)))
		n := 0
		for i := 0; i < min(3, len(ws)); i++ {
			n += ws[i]
		}
		return n
	}
	if top(next) > max(top(old), rules.MaxTopThreePercent) {
		return errors.New("目标制造或加重前三大仓位超限")
	}
	return nil
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func LatestSession(now time.Time) string {
	return foundation.LatestAStockSession(now)
}
func QuoteCurrent(q foundation.Quote, now time.Time) bool {
	return finitePositive(q.Price) && !q.Meta.Stale && !q.TradeTime.IsZero() && q.TradeTime.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02") == LatestSession(now) && !q.TradeTime.After(now.Add(time.Minute))
}
func TradingEligibility(entry foundation.StockCatalogEntry, q foundation.Quote, now time.Time) Eligibility {
	e := Eligibility{Symbol: q.Symbol, Locked: true, TradeDate: LatestSession(now), Volume: entry.Volume, Amount: entry.Amount}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	if !q.TradeTime.IsZero() {
		e.QuoteTradeDate = q.TradeTime.In(zone).Format("2006-01-02")
	}
	if strings.TrimSpace(entry.Name) == "" {
		e.Reason = "未在真实股票目录确认，不能核验交易资格"
		e.MissingFields = []string{"catalog_entry"}
		return e
	}
	if !QuoteCurrent(q, now) {
		e.MissingFields = []string{"current_quote"}
		switch {
		case !finitePositive(q.Price) || q.TradeTime.IsZero():
			e.Reason = "最新有效交易日行情缺失，暂时锁定调整"
		case q.Meta.Stale:
			e.Reason = "行情被数据源标记为过期，暂时锁定调整"
		case e.QuoteTradeDate != e.TradeDate:
			e.Reason = fmt.Sprintf("行情交易日%s，需核验最近有效交易日%s", e.QuoteTradeDate, e.TradeDate)
		default:
			e.Reason = "行情时点异常，暂时锁定调整"
		}
		return e
	}
	// Quote session totals have an actual market timestamp. Sina daily bars only
	// carry volume, so a missing bar amount must not invalidate complete realtime
	// totals. Never assign a fetch date to undated catalog liquidity.
	if finitePositive(q.Volume) && finitePositive(q.Amount) {
		e.Volume = q.Volume
		e.Amount = q.Amount
		e.LiquidityTradeDate = e.QuoteTradeDate
		e.LiquiditySource = q.Meta.Source + ":realtime"
	} else if entry.Meta.TradeDate == e.TradeDate && !entry.Meta.Stale && finitePositive(entry.Volume) && finitePositive(entry.Amount) {
		e.LiquidityTradeDate = entry.Meta.TradeDate
		e.LiquiditySource = entry.Meta.Source + ":daily"
	} else {
		if entry.Meta.TradeDate != e.TradeDate || entry.Meta.Stale {
			e.MissingFields = append(e.MissingFields, "liquidity_trade_date")
		}
		if !finitePositive(q.Volume) && !finitePositive(entry.Volume) {
			e.MissingFields = append(e.MissingFields, "volume")
		}
		if !finitePositive(q.Amount) && !finitePositive(entry.Amount) {
			e.MissingFields = append(e.MissingFields, "amount")
		}
		e.Reason = "无法确认最近有效交易日的成交数据，暂时锁定调整"
		if len(e.MissingFields) == 1 && e.MissingFields[0] == "amount" {
			e.Reason = "成交量已获取，但数据源未提供有效成交额；不能据此判为停牌"
		}
		if q.Volume == 0 && q.Amount == 0 && entry.Meta.TradeDate == e.TradeDate && entry.Volume == 0 && entry.Amount == 0 {
			e.Reason = "最近有效交易日未确认成交，需核验停牌或交易状态"
		}
		return e
	}
	name := strings.ToUpper(entry.Name + " " + q.Name)
	riskyName := strings.Contains(name, "ST") || strings.Contains(name, "退")
	e.Locked = false
	e.CanIncrease = !riskyName
	e.Conditional = true
	e.Reason = "最近有效交易日成交数据已核验；入场条件、实际成交及T+1待确认"
	if riskyName {
		e.Reason = "ST或退市风险，不允许新增/增持；减持须核验实际成交"
	}
	limit := 9.8
	if strings.HasPrefix(q.Symbol, "30") || strings.HasPrefix(q.Symbol, "68") {
		limit = 19.8
	}
	if strings.HasSuffix(q.Symbol, ".BJ") {
		limit = 29.5
	}
	if math.Abs(q.ChangePercent) >= limit {
		e.CanIncrease = false
		e.Locked = true
		e.Reason = "接近涨跌停，等待实际可成交核验"
	}
	return e
}
func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func lexLess(a, b []int) bool {
	for i := 0; i < min(len(a), len(b)); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// FundingFlows pairs each unit of this step's released allocation once. The
// cumulative replacement check remains relative to the immutable chain root.
func FundingFlows(current, target []pi.Holding) ([]FundingTransfer, int, int) {
	old := map[string]int{}
	next := map[string]int{}
	for _, h := range current {
		old[h.Symbol] = h.Weight
	}
	for _, h := range target {
		next[h.Symbol] = h.Weight
	}
	type leg struct {
		symbol string
		weight int
	}
	sells := []leg{}
	buys := []leg{}
	sold, bought := 0, 0
	for _, h := range current {
		delta := h.Weight - next[h.Symbol]
		if delta > 0 {
			sells = append(sells, leg{h.Symbol, delta})
			sold += delta
		}
	}
	for _, h := range target {
		delta := h.Weight - old[h.Symbol]
		if delta > 0 {
			buys = append(buys, leg{h.Symbol, delta})
			bought += delta
		}
	}
	flows := []FundingTransfer{}
	i, j := 0, 0
	for i < len(sells) && j < len(buys) {
		weight := min(sells[i].weight, buys[j].weight)
		flows = append(flows, FundingTransfer{sells[i].symbol, buys[j].symbol, weight})
		sells[i].weight -= weight
		buys[j].weight -= weight
		if sells[i].weight == 0 {
			i++
		}
		if buys[j].weight == 0 {
			j++
		}
	}
	return flows, sold, bought
}

// FundingFlowsCompared maximizes the funding covered by validated structured
// investment comparisons. This only pairs fungible released cash; neither stock
// selection nor target weights change. Remaining unmatched flows stay visible.
func FundingFlowsCompared(current, target []pi.Holding, comparisons []InvestmentComparison) ([]FundingTransfer, int, int) {
	fallback, sold, bought := FundingFlows(current, target)
	if len(comparisons) == 0 || sold == 0 {
		return fallback, sold, bought
	}
	old, next := map[string]int{}, map[string]int{}
	for _, h := range current {
		old[h.Symbol] = h.Weight
	}
	for _, h := range target {
		next[h.Symbol] = h.Weight
	}
	donors, receivers := []string{}, []string{}
	for s, w := range old {
		if w > next[s] {
			donors = append(donors, s)
		}
	}
	for s, w := range next {
		if w > old[s] {
			receivers = append(receivers, s)
		}
	}
	sort.Strings(donors)
	sort.Strings(receivers)
	source, sink := 0, 1+len(donors)+len(receivers)
	n := sink + 1
	capacity := make([][]int, n)
	residual := make([][]int, n)
	for i := range capacity {
		capacity[i] = make([]int, n)
		residual[i] = make([]int, n)
	}
	donorID, receiverID := map[string]int{}, map[string]int{}
	for i, s := range donors {
		donorID[s] = i + 1
		capacity[source][i+1] = old[s] - next[s]
	}
	for i, s := range receivers {
		receiverID[s] = 1 + len(donors) + i
		capacity[receiverID[s]][sink] = next[s] - old[s]
	}
	for _, c := range comparisons {
		from, fok := donorID[c.FromSymbol]
		to, tok := receiverID[c.ToSymbol]
		if fok && tok {
			capacity[from][to] = sold
		}
	}
	for i := range capacity {
		copy(residual[i], capacity[i])
	}
	// Deterministic integral max flow. Reverse edges avoid greedy pairing traps.
	for {
		parent := make([]int, n)
		for i := range parent {
			parent[i] = -1
		}
		parent[source] = source
		queue := []int{source}
		for len(queue) > 0 && parent[sink] < 0 {
			v := queue[0]
			queue = queue[1:]
			for u := 0; u < n; u++ {
				if parent[u] < 0 && residual[v][u] > 0 {
					parent[u] = v
					queue = append(queue, u)
				}
			}
		}
		if parent[sink] < 0 {
			break
		}
		amount := sold
		for u := sink; u != source; u = parent[u] {
			amount = min(amount, residual[parent[u]][u])
		}
		for u := sink; u != source; u = parent[u] {
			v := parent[u]
			residual[v][u] -= amount
			residual[u][v] += amount
		}
	}
	flows := []FundingTransfer{}
	remainingSold, remainingBought := map[string]int{}, map[string]int{}
	for _, s := range donors {
		remainingSold[s] = residual[source][donorID[s]]
	}
	for _, s := range receivers {
		remainingBought[s] = residual[receiverID[s]][sink]
	}
	for _, from := range donors {
		for _, to := range receivers {
			w := capacity[donorID[from]][receiverID[to]] - residual[donorID[from]][receiverID[to]]
			if w > 0 {
				flows = append(flows, FundingTransfer{from, to, w})
			}
		}
	}
	for _, from := range donors {
		for _, to := range receivers {
			w := min(remainingSold[from], remainingBought[to])
			if w > 0 {
				flows = append(flows, FundingTransfer{from, to, w})
				remainingSold[from] -= w
				remainingBought[to] -= w
			}
		}
	}
	return flows, sold, bought
}
