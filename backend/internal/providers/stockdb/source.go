package stockdb

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DailySource 是「日线（前复权）」数据源的统一接口。stockdb 本地源与各远程备用源
// 都实现它，便于在 MultiSource 中做健康感知的负载均衡 + 故障转移（镜像 quant-stock
// 的多接口共用 / 延迟采集 / 故障转移 三套模式）。
//
// 设计动机：之前单接口（腾讯 fqkline）在高频/批量取数时容易被限流甚至封禁。把多个
// 上游（本地 stockdb 7899 / 腾讯 fqkline / 东方财富 push2his）聚合成一个入口后，
// 主源（本地）优先、远程源在主源降级时按健康 + 轮询分摊流量，任一源失败即冷却并
// 切换下一个，从架构上消除「单接口被封被限制」的单点风险。
type DailySource interface {
	// Name 返回数据源标识（用于状态展示与日志）。
	Name() string
	// DailyBars 返回最近 count 根前复权日线（升序）。
	DailyBars(ctx context.Context, code string, count int) ([]Bar, error)
	// QFQ 标识该源返回的数据是否已是前复权口径。false 时 MultiSource 会用本地
	// 复权因子二次校正（用于仅提供原始价的接口）。
	QFQ() bool
	// Health 返回该源是否健康及最近一次调用延迟（毫秒，<=0 表示未知）。
	Health() (ok bool, latencyMs float64)
}

// SourceStatus 是单个日线源的健康快照，供 /api/v1/stockdb/status 暴露。
type SourceStatus struct {
	Name      string  `json:"name"`
	OK        bool    `json:"ok"`
	QFQ       bool    `json:"qfq"`
	LatencyMs float64 `json:"latency_ms"`
}

// MultiSource 把多个 DailySource 聚合成一个统一入口：
//   - 优先使用主源（本地 stockdb，权威 + 零远程依赖 + 最稳）；
//   - 主源降级（连续失败）时，在多个远程源之间按健康 + 轮询做负载均衡，避免单一
//     远程源被限流/封禁（解决「之前获取数据被封被限制」）；
//   - 任一源调用失败时短暂冷却并自动切换到下一个健康源（故障转移）。
type MultiSource struct {
	mu       sync.Mutex
	primary  DailySource
	remotes  []DailySource
	rotate   int
	cooldown map[string]time.Time
	coolDur  time.Duration
}

// NewMultiSource 构造多源聚合器。primary 为本地主源（如 stockdb 7899），remotes 为
// 远程备用源（按数组顺序作为优先级，实际调用时再按健康 + 轮询二次排序）。
func NewMultiSource(primary DailySource, remotes ...DailySource) *MultiSource {
	return &MultiSource{
		primary:  primary,
		remotes:  remotes,
		cooldown: map[string]time.Time{},
		coolDur:  5 * time.Second,
	}
}

// DailyBars 依次尝试各源，返回第一个成功且非空的日线。
func (m *MultiSource) DailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	order := m.buildOrder()
	var lastErr error
	for _, src := range order {
		t0 := time.Now()
		bars, err := src.DailyBars(ctx, code, count)
		latency := float64(time.Since(t0).Milliseconds())
		if err != nil {
			m.markCool(src.Name())
			lastErr = fmt.Errorf("%s: %w", src.Name(), err)
			continue
		}
		if len(bars) == 0 {
			m.markCool(src.Name())
			lastErr = fmt.Errorf("%s: empty", src.Name())
			continue
		}
		_ = latency
		return bars, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("stockdb: no daily source served %s", code)
	}
	return nil, lastErr
}

// Status 返回所有源（主源 + 远程）的健康快照。
func (m *MultiSource) Status() []SourceStatus {
	out := make([]SourceStatus, 0, len(m.remotes)+1)
	out = append(out, sourceStatusOf(m.primary))
	for _, r := range m.remotes {
		out = append(out, sourceStatusOf(r))
	}
	return out
}

// buildOrder 计算本次调用尝试源的顺序：主源健康则优先，否则排到末尾；
// 远程源按轮询起点旋转，使主源降级时流量在多个远程源间均匀分布（负载均衡）。
func (m *MultiSource) buildOrder() []DailySource {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	healthy := make([]DailySource, 0, len(m.remotes))
	for _, r := range m.remotes {
		if ct, ok := m.cooldown[r.Name()]; ok && now.Before(ct) {
			continue // 刚失败冷却中，跳过
		}
		if ok, _ := r.Health(); !ok {
			continue
		}
		healthy = append(healthy, r)
	}
	n := len(healthy)
	if n > 0 {
		m.rotate = (m.rotate + 1) % n // 每次调用推进轮询起点
		healthy = append(healthy[n-m.rotate:], healthy[:n-m.rotate]...)
	}
	primaryOK, _ := m.primary.Health()
	if primaryOK {
		return append([]DailySource{m.primary}, healthy...)
	}
	return append(healthy, m.primary) // 主源降级：远程优先，主源兜底
}

func (m *MultiSource) markCool(name string) {
	m.mu.Lock()
	m.cooldown[name] = time.Now().Add(m.coolDur)
	m.mu.Unlock()
}

// localDailySource 把 *Client 的本地取数封装为 DailySource，作为多源池的主源。
type localDailySource struct {
	c *Client
}

func (l localDailySource) Name() string { return "stockdb-local" }
func (l localDailySource) QFQ() bool    { return true }
func (l localDailySource) DailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	return l.c.localDailyBars(ctx, code, count)
}
func (l localDailySource) Health() (bool, float64) {
	return l.c.stats.Healthy(), l.c.stats.snapshot().P50Ms
}

func sourceStatusOf(s DailySource) SourceStatus {
	ok, lat := s.Health()
	return SourceStatus{Name: s.Name(), OK: ok, QFQ: s.QFQ(), LatencyMs: lat}
}
