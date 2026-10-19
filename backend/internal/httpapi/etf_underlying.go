package httpapi

import (
	"context"
	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

func (s *Server) collectETFUnderlying(ctx context.Context, holdings []foundation.ETFHoldings, cutoff time.Time) []etfanalysis.UnderlyingEvidence {
	candidates := []etfanalysis.UnderlyingEvidence{}
	seen := map[string]bool{}
	for _, h := range holdings {
		if _, err := time.Parse("2006-01-02", h.ReportDate); err != nil {
			continue
		}
		if h.AssetType != "equity" || h.ReportDate > cutoff.In(foundation.AStockLocation).Format("2006-01-02") {
			continue
		}
		for _, v := range h.Items {
			if v.Symbol == "" || seen[v.Symbol] || foundation.IsListedFundCandidate(v.Symbol) || !(strings.HasSuffix(v.Symbol, ".SH") || strings.HasSuffix(v.Symbol, ".SZ")) {
				continue
			}
			seen[v.Symbol] = true
			candidates = append(candidates, etfanalysis.UnderlyingEvidence{Holding: v, HoldingReportDate: h.ReportDate, Financials: []foundation.StockFinancialEvidence{}})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Holding.WeightPercent > candidates[j].Holding.WeightPercent })
	candidates = candidates[:min(3, len(candidates))]
	if len(candidates) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	symbols := []string{}
	for _, c := range candidates {
		symbols = append(symbols, c.Holding.Symbol)
	}
	var wg sync.WaitGroup
	for i := range candidates {
		if s.etfUnderlyingFinancials == nil {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows, err := s.etfUnderlyingFinancials.StockFinancialEvidence(ctx, candidates[i].Holding.Symbol, 2)
			if err != nil {
				return
			}
			for _, row := range rows {
				if row.Symbol != candidates[i].Holding.Symbol || row.Meta.Stale || row.PublishedAt.After(cutoff) || row.ReportDate > cutoff.In(foundation.AStockLocation).Format("2006-01-02") {
					continue
				}
				if _, e := time.Parse("2006-01-02", row.ReportDate); e != nil {
					continue
				}
				fields := map[string]float64{}
				for k, v := range row.Fields {
					if !math.IsNaN(v) && !math.IsInf(v, 0) {
						fields[k] = v
					}
				}
				if len(fields) == 0 {
					continue
				}
				row.Fields = fields
				candidates[i].Financials = append(candidates[i].Financials, row)
			}
			sort.SliceStable(candidates[i].Financials, func(a, b int) bool {
				return candidates[i].Financials[a].ReportDate > candidates[i].Financials[b].ReportDate
			})
			candidates[i].Financials = candidates[i].Financials[:min(2, len(candidates[i].Financials))]
		}(i)
	}
	// Separate fields, then merge only after all workers finish.
	valuations := map[string]foundation.StockValuation{}
	if s.etfUnderlyingValuation != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := s.etfUnderlyingValuation.StockValuations(ctx, symbols)
			if err != nil {
				return
			}
			for _, v := range rows {
				if v.Current(v.Symbol, cutoff) {
					valuations[v.Symbol] = v
				}
			}
		}()
	}
	wg.Wait()
	result := []etfanalysis.UnderlyingEvidence{}
	for _, c := range candidates {
		if v, ok := valuations[c.Holding.Symbol]; ok {
			c.Valuation = &v
		}
		if len(c.Financials) > 0 || c.Valuation != nil {
			result = append(result, c)
		}
	}
	return result
}
