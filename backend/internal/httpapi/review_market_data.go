package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/review"
)

type reviewUSSectorProvider interface {
	USSectorMomentum(ctx context.Context, limit int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error)
}

type reviewDailyMarketProvider struct {
	overview MarketOverviewProvider
}

func newReviewDailyMarketProvider(overview MarketOverviewProvider) review.DailyMarketProvider {
	if overview == nil {
		return nil
	}
	return reviewDailyMarketProvider{overview: overview}
}

func (p reviewDailyMarketProvider) Snapshot(ctx context.Context, capturedAt time.Time) (review.DailyUSMarket, error) {
	result := review.DailyUSMarket{
		CapturedAt: capturedAt.UTC(), Indexes: []review.DailyUSMarketIndex{}, LeadingSectors: []review.DailyUSMarketSector{},
		LaggingSectors: []review.DailyUSMarketSector{}, DataQuality: []string{},
	}
	var (
		indexes               []foundation.MarketIndexSnapshot
		sectors               []foundation.MarketUSSectorMomentum
		indexMeta, sectorMeta foundation.SourceMeta
		indexErr, sectorErr   error
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		indexes, indexMeta, indexErr = p.overview.MarketIndexes(ctx, "core")
	}()
	sectorProvider, hasSectorProvider := p.overview.(reviewUSSectorProvider)
	if hasSectorProvider {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sectors, sectorMeta, sectorErr = sectorProvider.USSectorMomentum(ctx, 11)
		}()
	} else {
		result.DataQuality = append(result.DataQuality, "美股板块ETF服务不可用")
	}
	wg.Wait()

	for _, item := range indexes {
		if item.Market != "US" && !isUSIndexID(item.ID) {
			continue
		}
		result.Indexes = append(result.Indexes, review.DailyUSMarketIndex{
			ID: item.ID, Name: item.Name, Price: item.Price, ChangePercent: item.ChangePercent,
			TradeTime: item.TradeTime, Status: item.Status, Source: item.Meta.Source,
		})
	}
	if len(result.Indexes) == 0 {
		if indexErr != nil {
			result.DataQuality = append(result.DataQuality, "美股指数获取失败："+indexErr.Error())
		} else {
			result.DataQuality = append(result.DataQuality, "行情源未返回道琼斯、标普500或纳斯达克数据")
		}
	} else if indexMeta.FallbackReason != "" {
		result.DataQuality = append(result.DataQuality, indexMeta.FallbackReason)
	}
	if sectorErr != nil {
		result.DataQuality = append(result.DataQuality, "美股板块ETF获取失败："+sectorErr.Error())
	} else if sectorMeta.FallbackReason != "" {
		result.DataQuality = append(result.DataQuality, sectorMeta.FallbackReason)
	}
	for _, item := range sectors {
		resultAsOf(&result, item.TradeTime)
	}
	for _, item := range result.Indexes {
		resultAsOf(&result, item.TradeTime)
	}
	if len(sectors) > 0 {
		leaders, laggards := splitUSSectorLeaders(sectors)
		result.LeadingSectors = leaders
		result.LaggingSectors = laggards
	}
	result.DataQuality = uniqueNonEmpty(result.DataQuality)
	if len(result.Indexes) == 0 && len(sectors) == 0 {
		return result, fmt.Errorf("美股指数和板块ETF均未返回数据")
	}
	return result, nil
}

func isUSIndexID(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "dow", "sp500", "nasdaq":
		return true
	default:
		return false
	}
}

func splitUSSectorLeaders(items []foundation.MarketUSSectorMomentum) ([]review.DailyUSMarketSector, []review.DailyUSMarketSector) {
	if len(items) == 0 {
		return nil, nil
	}
	sorted := append([]foundation.MarketUSSectorMomentum(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ChangePercent > sorted[j].ChangePercent })
	count := len(sorted) / 2
	if count < 1 {
		count = 1
	}
	if count > 3 {
		count = 3
	}
	toReview := func(item foundation.MarketUSSectorMomentum) review.DailyUSMarketSector {
		return review.DailyUSMarketSector{ProxySymbol: item.ProxySymbol, Name: item.Name, Price: item.Price, ChangePercent: item.ChangePercent, TradeTime: item.TradeTime, Source: item.Meta.Source}
	}
	leaders := make([]review.DailyUSMarketSector, 0, count)
	laggards := make([]review.DailyUSMarketSector, 0, count)
	for _, item := range sorted[:count] {
		leaders = append(leaders, toReview(item))
	}
	for _, item := range sorted[len(sorted)-count:] {
		laggards = append(laggards, toReview(item))
	}
	return leaders, laggards
}

func resultAsOf(result *review.DailyUSMarket, tradeTime time.Time) {
	if tradeTime.IsZero() {
		return
	}
	if result.AsOf == "" {
		result.AsOf = tradeTime.Format("2006-01-02 15:04")
		return
	}
	current, err := time.ParseInLocation("2006-01-02 15:04", result.AsOf, tradeTime.Location())
	if err == nil && tradeTime.After(current) {
		result.AsOf = tradeTime.Format("2006-01-02 15:04")
	}
}

func uniqueNonEmpty(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}
