package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type etfUnderlyingValuationFixture struct{ now time.Time }

func (p etfUnderlyingValuationFixture) StockValuations(_ context.Context, symbols []string) ([]foundation.StockValuation, error) {
	rows := make([]foundation.StockValuation, 0, len(symbols))
	for _, symbol := range symbols {
		pe, pb := 30.0, 3.0
		rows = append(rows, foundation.StockValuation{Symbol: symbol, PETTM: &pe, PB: &pb, TradeTime: p.now, Meta: foundation.SourceMeta{Source: "fixture"}})
	}
	return rows, nil
}

func TestETFUnderlyingValuationOnlySampleSerializesEmptyFinancials(t *testing.T) {
	now := time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)
	holdings := []foundation.ETFHoldings{{AssetType: "equity", ReportDate: "2026-06-30", Items: []foundation.ETFHolding{{Symbol: "688001.SH", Name: "样本", WeightPercent: 8.51}}}}
	for _, tc := range []struct {
		name     string
		provider StockFinancialSupplementProvider
	}{
		{name: "no provider"},
		{name: "provider error", provider: financialSupplementFixture{err: errors.New("unavailable")}},
		{name: "provider timeout", provider: financialSupplementFixture{err: context.DeadlineExceeded}},
		{name: "empty response", provider: financialSupplementFixture{}},
		{name: "filtered response", provider: financialSupplementFixture{items: []foundation.StockFinancialEvidence{{Symbol: "688001.SH", ReportDate: "2026-12-31", Fields: map[string]float64{"revenue_yoy": 20}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{etfUnderlyingFinancials: tc.provider, etfUnderlyingValuation: etfUnderlyingValuationFixture{now: now}}
			sample := s.collectETFUnderlying(context.Background(), holdings, now)
			if len(sample) != 1 || sample[0].Valuation == nil || sample[0].Holding.Symbol != "688001.SH" {
				t.Fatalf("valuation-only sample must remain available: %+v", sample)
			}
			encoded, err := json.Marshal(sample)
			if err != nil {
				t.Fatal(err)
			}
			var payload []map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatal(err)
			}
			if got := string(payload[0]["financials"]); got != "[]" {
				t.Fatalf("financials = %s, want [] so clients can safely render missing data", got)
			}
		})
	}
}
