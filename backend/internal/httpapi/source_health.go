package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
)

type sourceProbe struct {
	name string
	run  func(context.Context) error
}

type sourceHealthCache struct {
	mu      sync.Mutex
	checks  map[string][]sourceProbe
	results []foundation.SourceHealth
	expires time.Time
}

func newSourceHealthCache(baseURL string, eastMoney *eastmoney.Client, sinaClient *sina.Client, tencentClient *tencent.Client, clsClient *cls.Client) *sourceHealthCache {
	kaipanla := duanxianxia.NewClient(duanxianxia.ClientConfig{BaseURL: baseURL})
	return &sourceHealthCache{checks: map[string][]sourceProbe{
		"duanxianxia": {{"涨停池", func(ctx context.Context) error {
			_, err := kaipanla.FetchLimitUpPool(ctx)
			return err
		}}},
		"eastmoney": {
			{"日 K 线", func(ctx context.Context) error {
				rows, err := eastMoney.KLine(ctx, "600519.SH", "daily", 2)
				return sourceProbeResult(len(rows), err)
			}},
			{"公司资料", func(ctx context.Context) error {
				profile, err := eastMoney.StockBusinessProfile(ctx, "600519.SH")
				if err == nil && profile.Name == "" {
					return errors.New("接口未返回公司名称")
				}
				return err
			}},
		},
		"sina": {{"实时行情", func(ctx context.Context) error {
			rows, err := sinaClient.Realtime(ctx, []string{"600519.SH"})
			return sourceProbeResult(len(rows), err)
		}}},
		"tencent": {{"指数", func(ctx context.Context) error {
			rows, _, err := tencentClient.MarketIndexes(ctx, "core")
			return sourceProbeResult(len(rows), err)
		}}},
		"cls": {{"快讯", func(ctx context.Context) error {
			rows, err := clsClient.LatestNews(ctx, 1)
			return sourceProbeResult(len(rows), err)
		}}},
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
	probeErrors := make([][]error, len(results))
	for i, item := range results {
		if !item.Enabled {
			continue
		}
		checks := cache.checks[item.ID]
		probeErrors[i] = make([]error, len(checks))
		for j, check := range checks {
			pending.Go(func() {
				probeErrors[i][j] = check.run(probeCtx)
			})
		}
	}
	pending.Wait()
	for i := range results {
		item := &results[i]
		if !item.Enabled {
			continue
		}
		checks := cache.checks[item.ID]
		if len(checks) == 0 {
			item.Status, item.Message = "unknown", "尚未检测"
			continue
		}
		var labels, failures []string
		for j, check := range checks {
			if err := probeErrors[i][j]; err != nil {
				labels = append(labels, check.name+"失败")
				failures = append(failures, check.name+"："+err.Error())
			} else {
				labels = append(labels, check.name+"通过")
			}
		}
		item.CheckedAt = time.Now().UTC()
		item.ProbeSummary = strings.Join(labels, " · ")
		item.OK = len(failures) == 0
		switch len(failures) {
		case 0:
			item.Status, item.Message = "healthy", item.ProbeSummary
		case len(checks):
			item.Status, item.Message = "unhealthy", strings.Join(failures, "；")
		default:
			item.Status, item.Message = "partial", item.ProbeSummary+"。"+strings.Join(failures, "；")
		}
	}
	if ctx.Err() == nil {
		cache.results = results
		cache.expires = time.Now().Add(time.Minute)
	}
	return results
}
