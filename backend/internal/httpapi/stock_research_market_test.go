package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

type researchPeerCatalog struct{}

func (researchPeerCatalog) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	items, _ := (stockAnalysisCatalog{}).StockCatalog(context.Background())
	for i := 0; i < 36; i++ {
		items = append(items, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("600%03d.SH", 520+i), Name: fmt.Sprintf("同业%d", i)}, Industry: "食品饮料", Concepts: []string{"消费"}})
	}
	return items, nil
}

type researchPeerCall struct {
	symbol   string
	limit    int
	deadline time.Time
}

type researchPeerKLines struct {
	mu        sync.Mutex
	calls     []researchPeerCall
	active    int
	maxActive int
}

func (p *researchPeerKLines) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	deadline, _ := ctx.Deadline()
	p.mu.Lock()
	p.calls = append(p.calls, researchPeerCall{symbol: symbol, limit: limit, deadline: deadline})
	if limit == stockanalysis.ResearchPeerDailyBars {
		p.active++
		p.maxActive = max(p.maxActive, p.active)
	}
	p.mu.Unlock()
	if limit == stockanalysis.ResearchPeerDailyBars {
		defer func() { p.mu.Lock(); p.active--; p.mu.Unlock() }()
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if symbol == "600520.SH" {
		return nil, errors.New("peer history unavailable")
	}
	lines, err := (stockAnalysisKLines{}).KLine(ctx, symbol, period, 180)
	if limit < len(lines) {
		lines = lines[len(lines)-limit:]
	}
	return lines, err
}

func TestStockResearchCollectsBoundedPeerHistoryAndKeepsPartialEvidence(t *testing.T) {
	provider := &researchPeerKLines{}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: provider, KLineFallback: provider, LimitUp: stockAnalysisLimitUps{}, StockConcept: researchPeerCatalog{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, ReviewDBPath: ":memory:"})
	defer server.Close()
	started := time.Now()
	_, snapshot, err := server.collectStockResearch(context.Background(), "600519.SH")
	if err != nil {
		t.Fatal(err)
	}
	peerSymbols := map[string]bool{}
	for _, call := range provider.calls {
		if call.limit != stockanalysis.ResearchPeerDailyBars {
			continue
		}
		peerSymbols[call.symbol] = true
		if call.symbol == "600519.SH" || call.deadline.IsZero() || call.deadline.Sub(started) > 9*time.Second {
			t.Fatalf("unbounded or self peer request: %+v", call)
		}
	}
	if len(peerSymbols) != stockanalysis.ResearchPeerLimit {
		t.Fatalf("peer limit mismatch: %v", peerSymbols)
	}
	if provider.maxActive > 6 || provider.maxActive < 2 {
		t.Fatalf("peer collection did not bound parallelism: %d", provider.maxActive)
	}
	if !strings.Contains(strings.Join(snapshot.Limitations, " "), fmt.Sprintf("1/%d个目录样本未取得日线", stockanalysis.ResearchPeerLimit)) {
		t.Fatalf("partial peer failure silently ignored: %v", snapshot.Limitations)
	}
	for _, source := range snapshot.Sources {
		if source.ID != "m-sector" {
			continue
		}
		var data struct {
			Requested int `json:"requested_peer_count"`
			Available int `json:"available_peer_count"`
			Peers     []struct {
				Symbol string                     `json:"symbol"`
				Bars   []stockanalysis.AIDailyBar `json:"daily_bars"`
			} `json:"peers"`
		}
		if err := json.Unmarshal([]byte(source.Content), &data); err != nil {
			t.Fatal(err)
		}
		if data.Requested != stockanalysis.ResearchPeerLimit || data.Available != stockanalysis.ResearchPeerLimit-1 || len(data.Peers) != stockanalysis.ResearchPeerLimit-1 {
			t.Fatalf("missing peer counted as valid or valid peers lost: %+v", data)
		}
		for _, peer := range data.Peers {
			if len(peer.Bars) != stockanalysis.ResearchPeerDailyBars || peer.Symbol == "600520.SH" {
				t.Fatalf("bad peer history: %+v", peer)
			}
		}
		return
	}
	t.Fatal("stock research snapshot has no sector and peer evidence")
}
