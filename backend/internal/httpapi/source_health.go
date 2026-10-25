package httpapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

type sourceHealthCache struct {
	mu      sync.Mutex
	checks  map[string]func(context.Context) error
	results []foundation.SourceHealth
	expires time.Time
}

func newSourceHealthCache(baseURL string, eastMoney *eastmoney.Client, sinaClient *sina.Client, tencentClient *tencent.Client, clsClient *cls.Client) *sourceHealthCache {
	kaipanla := duanxianxia.NewClient(duanxianxia.ClientConfig{BaseURL: baseURL})
	return &sourceHealthCache{checks: map[string]func(context.Context) error{
		"duanxianxia": func(ctx context.Context) error {
			_, err := kaipanla.FetchLimitUpPool(ctx)
			return err
		},
		"eastmoney": func(ctx context.Context) error {
			rows, err := eastMoney.KLine(ctx, "600519.SH", "daily", 2)
			return sourceProbeResult(len(rows), err)
		},
		"sina": func(ctx context.Context) error {
			rows, err := sinaClient.Realtime(ctx, []string{"600519.SH"})
			return sourceProbeResult(len(rows), err)
		},
		"tencent": func(ctx context.Context) error {
			rows, _, err := tencentClient.MarketIndexes(ctx, "core")
			return sourceProbeResult(len(rows), err)
		},
		"cls": func(ctx context.Context) error {
			rows, err := clsClient.LatestNews(ctx, 1)
			return sourceProbeResult(len(rows), err)
		},
	}}
}

func sourceProbeResult(count int, err error) error {
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("接口未返回有效数据")
	}
	return nil
}

func (cache *sourceHealthCache) load(ctx context.Context) []foundation.SourceHealth {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if time.Now().Before(cache.expires) {
		return append([]foundation.SourceHealth(nil), cache.results...)
	}
	results := []foundation.SourceHealth{
		{ID: "duanxianxia", Name: "短线侠 / 开盘啦", Category: "theme,leaders,limit-up,concept", Enabled: true},
		{ID: "eastmoney", Name: "东方财富", Category: "quote,kline,f10,report", Enabled: true},
		{ID: "sina", Name: "新浪财经", Category: "quote,kline,money-flow", Enabled: true},
		{ID: "tencent", Name: "腾讯财经", Category: "quote,index,hk", Enabled: true},
		{ID: "cls", Name: "财联社", Category: "news,calendar", Enabled: true},
		{ID: "tradingview", Name: "TradingView", Category: "news", Status: "disabled", Message: "预留数据源，尚未接入"},
		{ID: "tushare", Name: "Tushare", Category: "basic,daily,index", Status: "disabled", Message: "预留数据源，尚未接入；不参与健康统计"},
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var pending sync.WaitGroup
	for index := range results {
		item := &results[index]
		if !item.Enabled {
			continue
		}
		check := cache.checks[item.ID]
		if check == nil {
			item.Status, item.Message = "unknown", "尚未检测"
			continue
		}
		pending.Add(1)
		go func() {
			defer pending.Done()
			err := check(probeCtx)
			item.CheckedAt = time.Now().UTC()
			item.OK = err == nil
			if err != nil {
				item.Status, item.Message = "unhealthy", "探测失败："+err.Error()
			} else {
				item.Status, item.Message = "healthy", "轻量接口探测通过；不代表所有接口均可用"
			}
		}()
	}
	pending.Wait()
	if ctx.Err() == nil {
		cache.results = results
		cache.expires = time.Now().Add(time.Minute)
	}
	return results
}
