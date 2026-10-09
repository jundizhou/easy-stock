package httpapi

import (
	"context"
	pi "easy-stock/backend/internal/portfolioinspection"
	po "easy-stock/backend/internal/portfoliooptimization"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/marketoverview"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Queries public data only: no store, model gateway, AI study, or saved report.
func TestLiveOptimizationCodeScreening(t *testing.T) {
	if os.Getenv("EASY_STOCK_LIVE_SCREENING") != "1" {
		t.Skip("set EASY_STOCK_LIVE_SCREENING=1 to verify the public screening providers")
	}
	em, tc, sn := eastmoney.NewClient(), tencent.NewClient(), sina.NewClient()
	s := &Server{stockDirectory: em, stockBusiness: em, marketOverview: marketoverview.New(em, tc, tc, sn), industryStocks: tc, realtimeProvider: sn, kLinePrimary: em, kLineFallback: sn}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started := time.Now()
	u, err := s.collectOptimizationUniverse(ctx, pi.Report{Request: pi.Request{Holdings: []pi.Holding{{Symbol: "600519.SH", Weight: 80}}}}, po.Request{}, started)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Candidates) > po.MaxCandidates {
		t.Fatal("unbounded screening pool")
	}
	qualified := 0
	for _, c := range u.Candidates {
		if c.Screening == nil {
			t.Fatal("screening facts missing")
		}
		if c.Screening.Qualified {
			qualified++
		}
		t.Logf("%s %s qualified=%v reason=%s", c.Symbol, c.Name, c.Screening.Qualified, c.Reason)
	}
	for _, l := range u.Limitations {
		t.Log(l)
	}
	t.Logf("public code screening: candidates=%d qualified=%d duration=%s", len(u.Candidates), qualified, time.Since(started))
	if dest := os.Getenv("EASY_STOCK_LIVE_SCREENING_OUTPUT"); dest != "" {
		data, err := json.MarshalIndent(map[string]any{"verified_at": time.Now(), "duration_ms": time.Since(started).Milliseconds(), "candidates": u.Candidates, "qualified_count": qualified, "limitations": u.Limitations}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
