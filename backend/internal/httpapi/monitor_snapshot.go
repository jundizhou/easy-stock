package httpapi

import (
	"context"
	"fmt"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/monitor"
)

// monitorSnapshotChain 按顺序问行情源：主源失败就换下一个。
//
// 这层兜底不是"优化"而是可用性问题：push2.eastmoney.com 在部分网络
// （IPv6 黑洞、运营商拦截）整段不可达，没有兜底时监控页会长期停在
// 「已降级为持仓 / 关注仓 / 规则标的范围」——页面看着正常，
// 其实从没扫过全市场，异动名单当然永远是空的。
type monitorSnapshotChain struct {
	providers []monitor.SnapshotProvider
}

func newMonitorSnapshotChain(providers ...monitor.SnapshotProvider) monitor.SnapshotProvider {
	chain := monitorSnapshotChain{}
	for _, provider := range providers {
		if provider != nil {
			chain.providers = append(chain.providers, provider)
		}
	}
	if len(chain.providers) == 0 {
		return nil
	}
	return chain
}

func (c monitorSnapshotChain) MarketSnapshot(ctx context.Context) ([]foundation.MarketSnapshotRow, foundation.SourceMeta, error) {
	var lastErr error
	var bestRows []foundation.MarketSnapshotRow
	var bestMeta foundation.SourceMeta
	for _, provider := range c.providers {
		rows, meta, err := provider.MarketSnapshot(ctx)
		if err == nil && len(rows) >= fullMarketMinRows {
			return rows, meta, nil
		}
		if err == nil && len(rows) > len(bestRows) {
			// 行数明显不足（上游截断、分页被改）不算成功，但先留作备胎：
			// 后面的源也拿不到全量时，一半的覆盖面也比空手好。
			bestRows, bestMeta = rows, meta
			lastErr = fmt.Errorf("provider %s only returned %d rows (< %d)", meta.Source, len(rows), fullMarketMinRows)
			continue
		}
		if err != nil {
			lastErr = err
		}
	}
	if bestRows != nil {
		bestMeta.Stale = true
		if bestMeta.FallbackReason == "" {
			bestMeta.FallbackReason = lastErr.Error()
		}
		return bestRows, bestMeta, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all market snapshot providers returned no rows")
	}
	return nil, foundation.SourceMeta{}, lastErr
}

// fullMarketMinRows 是「这份数据配得上叫全市场快照」的下限。
// A 股只增不减，正常在 5000 只上下；上游一旦悄悄截断（如 clist 把
// 每页行数砍到 100），返回 100 行也"没有错误"，链路会把残缺快照
// 当成好数据用——异动与雷达从此只看得见 100 只票。
const fullMarketMinRows = 1000
