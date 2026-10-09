package stockdb

import (
	"sort"
	"sync"
	"time"
)

// Stats 记录 stockdb 本地源的调用统计与滚动延迟，镜像 quant-stock 的
// sys_probe / net_probe 延迟采集与「健康冷却」逻辑。
//
// 设计要点：
//   - 每次 RawQuery 成功后记录耗时（微秒）与成败；
//   - 连续失败达到阈值后进入 degraded（降级）状态，并在 degradedUntil 时间内
//     保持降级，避免对宕机/卡死的源反复重试造成请求堆积（与 quant-stock 的
//     "healthy until" 同构）；
//   - 成功一次即清除降级，自动恢复。
type Stats struct {
	mu               sync.Mutex
	total            int64
	ok               int64
	errors           int64
	consecutiveErrs  int64
	latencies        []int64 // 微秒，滚动窗口
	lastErr          string
	lastErrAt        time.Time
	lastProbeAt      time.Time
	degradedUntil    time.Time
}

const (
	statsWindow        = 256
	degradedThreshold  = 2  // 连续失败达到该值即进入降级
	degradedCooldown   = 10 * time.Second
)

func newStats() *Stats {
	return &Stats{latencies: make([]int64, 0, statsWindow)}
}

// record 记录一次调用耗时（微秒）与成败。
func (s *Stats) record(us int64, ok bool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if ok {
		s.ok++
		s.consecutiveErrs = 0
		s.degradedUntil = time.Time{}
	} else {
		s.errors++
		s.consecutiveErrs++
		s.lastErr = errMsg
		s.lastErrAt = time.Now()
		if s.consecutiveErrs >= degradedThreshold {
			s.degradedUntil = time.Now().Add(degradedCooldown)
		}
	}
	s.latencies = append(s.latencies, us)
	if len(s.latencies) > statsWindow {
		s.latencies = s.latencies[len(s.latencies)-statsWindow:]
	}
}

// Healthy 返回当前是否处于健康（未降级）状态。
func (s *Stats) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().After(s.degradedUntil)
}

// markProbe 记录一次显式健康探测的结果。
func (s *Stats) markProbe(ok bool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastProbeAt = time.Now()
	if ok {
		s.consecutiveErrs = 0
		s.degradedUntil = time.Time{}
	} else {
		s.consecutiveErrs++
		s.lastErr = errMsg
		s.lastErrAt = time.Now()
		if s.consecutiveErrs >= degradedThreshold {
			s.degradedUntil = time.Now().Add(degradedCooldown)
		}
	}
}

// StatsSnapshot 是统计快照，供 /api/v1/stockdb/status 暴露。
type StatsSnapshot struct {
	Total   int64   `json:"total"`
	OK      int64   `json:"ok"`
	Errors  int64   `json:"errors"`
	P50Ms   float64 `json:"p50_ms"`
	P95Ms   float64 `json:"p95_ms"`
	Healthy bool    `json:"healthy"`
	CheckedAt string `json:"checked_at,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

func (s *Stats) snapshot() StatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := StatsSnapshot{
		Total:   s.total,
		OK:      s.ok,
		Errors:  s.errors,
		Healthy: time.Now().After(s.degradedUntil),
		LastError: s.lastErr,
	}
	if !s.lastProbeAt.IsZero() {
		snap.CheckedAt = s.lastProbeAt.Format(time.RFC3339)
	}
	if len(s.latencies) > 0 {
		cp := append([]int64(nil), s.latencies...)
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		snap.P50Ms = float64(percentile(cp, 50)) / 1000.0
		snap.P95Ms = float64(percentile(cp, 95)) / 1000.0
	}
	return snap
}

func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p*(len(sorted)-1) + 50) / 100
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
