package sector

import (
	"context"
	"fmt"
	"log"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type radarProgressEvent struct {
	step       string
	snapshot   duanxianxia.Snapshot
	fetchMeta  duanxianxia.FetchMeta
	industries []foundation.MarketIndustryMomentum
	meta       foundation.SourceMeta
	quotes     map[string]foundation.Quote
	strengths  map[string]themeStrengthScore
	err        error
}

// ProgressiveOverviews publishes local membership and each upstream as soon as
// it is ready. One coordinator owns all state; workers only send immutable events.
func (p *RadarProvider) ProgressiveOverviews(ctx context.Context, publish func(foundation.ThemeProgress)) {
	events := make(chan radarProgressEvent, 3)
	steps := map[string]string{"industry": "loading", "kaipanla": "loading", "strength": "loading"}
	errors := map[string]string{}
	var snapshot duanxianxia.Snapshot
	var fetchMeta duanxianxia.FetchMeta
	var industries []foundation.MarketIndustryMomentum
	var industryMeta foundation.SourceMeta
	var quotes map[string]foundation.Quote
	var strengths map[string]themeStrengthScore
	emit := func(refreshing bool) {
		industryItems := buildIndustryRadarOverviews(industries, industryMeta, p.now())
		var kaipanlaItems []foundation.ThemeOverview
		if len(snapshot.Themes) > 0 && tradingDayAge(snapshot.TradeDate, p.now()) <= 2 {
			themes := snapshot.Themes[:min(len(snapshot.Themes), max(24, p.fallbackFill))]
			kaipanlaItems = p.buildKaipanlaRadarOverviews(snapshot, themes, quotes, strengths, tradingDayAge(snapshot.TradeDate, p.now()))
		}
		items := rankAndSelectRadarOverviews(mergeRadarOverviews(industryItems, kaipanlaItems), p.fallbackFill)
		for i := range items {
			items[i].Provisional = steps["strength"] != "ready" || steps["industry"] != "ready" || steps["kaipanla"] != "ready"
		}
		meta := fusedRadarMeta(p.now(), snapshot, fetchMeta, nil, industryMeta, nil, len(kaipanlaItems) > 0, len(industryItems) > 0)
		stage := "base"
		if steps["strength"] == "ready" {
			stage = "enriched"
		}
		stepCopy := map[string]string{}
		errorCopy := map[string]string{}
		for key, value := range steps {
			stepCopy[key] = value
		}
		for key, value := range errors {
			errorCopy[key] = value
		}
		publish(foundation.ThemeProgress{Data: items, Meta: meta, Stage: stage, Refreshing: refreshing, Steps: stepCopy, Errors: errorCopy})
	}
	if cached, ok := p.source.(interface {
		CachedSnapshot(context.Context) (duanxianxia.Snapshot, bool, error)
	}); ok {
		if value, exists, err := cached.CachedSnapshot(ctx); err == nil && exists {
			snapshot = value
			emit(true)
		}
	}
	go func() {
		start := time.Now()
		e := radarProgressEvent{step: "industry"}
		if p.industry == nil {
			e.err = fmt.Errorf("行业数据源不可用")
		} else {
			e.industries, e.meta, e.err = p.industry.IndustryMomentum(ctx, max(24, p.fallbackFill))
			if e.err == nil && len(e.industries) == 0 {
				e.err = fmt.Errorf("行业数据为空")
			}
		}
		log.Printf("event=theme_stage stage=industry duration_ms=%d", time.Since(start).Milliseconds())
		events <- e
	}()
	go func() {
		start := time.Now()
		e := radarProgressEvent{step: "kaipanla"}
		if p.source == nil {
			e.err = fmt.Errorf("开盘啦数据源不可用")
		} else {
			e.snapshot, e.fetchMeta, e.err = p.source.Snapshot(ctx)
			if e.err == nil && (len(e.snapshot.Themes) == 0 || tradingDayAge(e.snapshot.TradeDate, p.now()) > 2) {
				e.err = fmt.Errorf("开盘啦暂无有效题材快照")
			}
		}
		log.Printf("event=theme_stage stage=kaipanla duration_ms=%d", time.Since(start).Milliseconds())
		events <- e
		strength := radarProgressEvent{step: "strength", err: e.err}
		if e.err == nil {
			start = time.Now()
			themes := e.snapshot.Themes[:min(len(e.snapshot.Themes), max(24, p.fallbackFill))]
			strength.quotes = p.quoteLookup(ctx, themes)
			strength.strengths = p.realtimeStrengthScores(ctx, themes)
			if ctx.Err() != nil {
				strength.err = ctx.Err()
			} else if len(strength.strengths) == 0 {
				strength.err = fmt.Errorf("题材强度暂不可用")
			}
			log.Printf("event=theme_stage stage=strength duration_ms=%d", time.Since(start).Milliseconds())
		}
		events <- strength
	}()
	for remaining := 3; remaining > 0; remaining-- {
		select {
		case <-ctx.Done():
			for key, value := range steps {
				if value == "loading" {
					steps[key] = "error"
					errors[key] = ctx.Err().Error()
				}
			}
			emit(false)
			return
		case e := <-events:
			steps[e.step] = "ready"
			if e.err != nil {
				steps[e.step] = "error"
				errors[e.step] = e.err.Error()
			} else {
				switch e.step {
				case "industry":
					industries = e.industries
					industryMeta = e.meta
					p.rememberIndustryLeaders(industries)
				case "kaipanla":
					snapshot = e.snapshot
					fetchMeta = e.fetchMeta
				case "strength":
					quotes = e.quotes
					strengths = e.strengths
				}
			}
			emit(remaining > 1)
		}
	}
}

func industryLeaderStocks(item foundation.MarketIndustryMomentum) []foundation.BoardStock {
	if item.LeaderSymbol == "" {
		return nil
	}
	return []foundation.BoardStock{{Symbol: item.LeaderSymbol, Name: item.LeaderName, ChangePercent: item.LeaderChangePercent, RankRole: "行业领涨", RankScore: 100, Meta: item.Meta}}
}

// BuildLeaders deliberately never invokes the remote quote or constituent APIs.
func (p *RadarProvider) BuildLeaders(ctx context.Context, themeID, snapshotID string) (foundation.SectorMap, error) {
	if fusion, ok := parseRadarFusionThemeID(themeID); ok {
		result, err := p.buildKaipanla(ctx, "kpl:"+fusion.KaipanlaCode, snapshotID, true)
		result.Theme = themeID
		return result, err
	}
	if industry, ok := parseRadarIndustryThemeID(themeID); ok {
		result := emptyIndustrySectorMap(themeID, industry, p.now())
		leader := p.industryLeader(themeID)
		if leader.Symbol != "" {
			result.Groups[0].Nodes[0].Stocks = []foundation.BoardStock{{Symbol: leader.Symbol, Name: leader.Name, ChangePercent: leader.ChangePercent, RankRole: "行业领涨", RankScore: 100}}
		}
		return result, nil
	}
	if snapshotID == "" {
		return foundation.SectorMap{}, fmt.Errorf("snapshot_id is required for leader membership")
	}
	return p.buildKaipanla(ctx, themeID, snapshotID, true)
}
