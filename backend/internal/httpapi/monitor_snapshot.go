package httpapi

import (
	"context"
	"fmt"
	"time"

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
	for index, provider := range c.providers {
		if err := ctx.Err(); err != nil {
			lastErr = err
			break
		}
		// 主源不能耗尽整轮时限：按剩余源数分配时间，给备用源留下请求机会。
		timeout := 12 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			remaining := len(c.providers) - index
			// 最后一个备用源不再需要给后续源留时间，可用完父请求剩余额度。
			if share := time.Until(deadline) / time.Duration(remaining); remaining == 1 || share < timeout {
				timeout = share
			}
		}
		providerCtx, cancel := context.WithTimeout(ctx, timeout)
		rows, meta, err := provider.MarketSnapshot(providerCtx)
		if err == nil {
			err = providerCtx.Err()
		}
		cancel()
		if err == nil && len(rows) >= fullMarketMinRows && !meta.Stale && !meta.CarryForward {
			return rows, meta, nil
		}
		switch {
		case err != nil:
			lastErr = err
		case meta.Stale || meta.CarryForward:
			lastErr = fmt.Errorf("provider %s snapshot incomplete or stale: %s", meta.Source, meta.FallbackReason)
		default:
			lastErr = fmt.Errorf("provider %s only returned %d rows (< %d)", meta.Source, len(rows), fullMarketMinRows)
		}
		if len(rows) > len(bestRows) {
			// 不完整快照只保留作降级展示，不能因行数够多而跳过备用源。
			bestRows, bestMeta = rows, meta
			bestMeta.Stale = true
			if bestMeta.FallbackReason == "" {
				bestMeta.FallbackReason = lastErr.Error()
			}
		}
	}
	if bestRows != nil {
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
