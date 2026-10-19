package sectorflowsample

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeRankProvider struct {
	ranks  map[string][]foundation.SectorFlowRank
	errors map[string]error
	calls  []string
}

func (p *fakeRankProvider) SectorFlowRanking(_ context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error) {
	p.calls = append(p.calls, dimension)
	if err := p.errors[dimension]; err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	return p.ranks[dimension], foundation.SourceMeta{Source: "eastmoney:bkzj"}, nil
}

func TestSampleOnceStoresBothDimensions(t *testing.T) {
	store := openTestStore(t)
	provider := &fakeRankProvider{ranks: map[string][]foundation.SectorFlowRank{
		"concept":  {{Code: "BK0917", Name: "半导体概念", MainNetInflow: 120, ChangePercent: 0.6, SuperLargeNet: 80, LargeNet: 40, SmallNet: -20, MediumNet: -100}},
		"industry": {{Code: "BK0475", Name: "银行", MainNetInflow: -80, ChangePercent: -0.3}},
	}}
	sampler := NewSampler(store, provider, SamplerConfig{Interval: time.Minute})

	now := time.Date(2026, 10, 19, 10, 3, 0, 0, foundation.AStockLocation)
	saved, err := sampler.SampleOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("sample once: %v", err)
	}
	if saved != 2 {
		t.Fatalf("saved = %d, want 2", saved)
	}

	samples, err := store.DaySamples(context.Background(), "2026-10-19", []string{"BK0917", "BK0475"})
	if err != nil {
		t.Fatalf("day samples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if samples[0].Minute != "10:03" {
		t.Fatalf("minute = %q, want 10:03", samples[0].Minute)
	}
	if samples[0].Code != "BK0475" || samples[1].Code != "BK0917" {
		t.Fatalf("unexpected codes: %+v", samples)
	}
	if samples[1].Main != 120 || samples[1].ChangePercent != 0.6 || samples[1].Medium != -100 {
		t.Fatalf("unexpected sample values: %+v", samples[1])
	}
}

func TestSampleOnceKeepsOtherDimensionWhenOneFails(t *testing.T) {
	store := openTestStore(t)
	provider := &fakeRankProvider{
		ranks: map[string][]foundation.SectorFlowRank{
			"industry": {{Code: "BK0475", Name: "银行", MainNetInflow: -80}},
		},
		errors: map[string]error{"concept": errors.New("concept ranking unavailable")},
	}
	sampler := NewSampler(store, provider, SamplerConfig{Interval: time.Minute})

	now := time.Date(2026, 10, 19, 10, 6, 0, 0, foundation.AStockLocation)
	saved, err := sampler.SampleOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("sample once: %v", err)
	}
	if saved != 1 {
		t.Fatalf("saved = %d, want 1", saved)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("calls = %+v, want both dimensions attempted", provider.calls)
	}
}

func TestSampleOnceFailsWhenBothDimensionsFail(t *testing.T) {
	store := openTestStore(t)
	provider := &fakeRankProvider{
		ranks:  map[string][]foundation.SectorFlowRank{},
		errors: map[string]error{"concept": errors.New("down"), "industry": errors.New("down")},
	}
	sampler := NewSampler(store, provider, SamplerConfig{Interval: time.Minute})
	if _, err := sampler.SampleOnce(context.Background(), time.Date(2026, 10, 19, 10, 0, 0, 0, foundation.AStockLocation)); err == nil {
		t.Fatal("expected error when every dimension fails")
	}
}
