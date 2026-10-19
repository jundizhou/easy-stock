package sectorflowsample

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// DefaultInterval 是采样间隔。
//
// 榜单本身就是按分钟刷新的累计值，3 分钟一次足以画出趋势，
// 同时把「2 个维度 × 10 个字段」的上游请求摊薄到每 3 分钟一轮。
const DefaultInterval = 3 * time.Minute

// SnapshotLimit 覆盖概念（约 508 个）与行业（约 496 个）全量板块。
const SnapshotLimit = 600

// RankProvider 只需要板块资金流榜单能力。
type RankProvider interface {
	SectorFlowRanking(ctx context.Context, dimension string, limit int) ([]foundation.SectorFlowRank, foundation.SourceMeta, error)
}

type SamplerConfig struct {
	Interval time.Duration
	Limit    int
	Logger   *log.Logger
	// Now 便于测试注入固定时间；为零值时使用 time.Now。
	Now func() time.Time
}

// Sampler 定时把板块资金流榜单的累计值写成采样点。
//
// 它只负责「记下来」，不负责出接口：曲线是在 HTTP 层按需从采样表拼出来的，
// 因此采样期间没有任何页面打开也不会丢数据。
type Sampler struct {
	store    *Store
	provider RankProvider
	config   SamplerConfig
}

func NewSampler(store *Store, provider RankProvider, config SamplerConfig) *Sampler {
	if config.Interval <= 0 {
		config.Interval = DefaultInterval
	}
	if config.Limit <= 0 || config.Limit > SnapshotLimit {
		config.Limit = SnapshotLimit
	}
	return &Sampler{store: store, provider: provider, config: config}
}

// Run 常驻采样：启动时先试一次（盘中立刻有第一个点），之后按间隔轮询。
// 非交易日与休市时段只跳过，不做任何上游请求。
func (s *Sampler) Run(ctx context.Context) {
	if s == nil || s.store == nil || s.provider == nil {
		return
	}
	interval := s.config.Interval
	s.logger().Printf("level=info event=scheduler_start feature=market-data task=sector_flow_sampler interval=%s", interval)

	lastPurgeDay := ""
	s.purgeIfNewDay(ctx, &lastPurgeDay)
	s.sampleIfDue(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger().Printf("level=info event=scheduler_stop feature=market-data task=sector_flow_sampler")
			return
		case <-ticker.C:
			s.purgeIfNewDay(ctx, &lastPurgeDay)
			s.sampleIfDue(ctx)
		}
	}
}

// purgeIfNewDay 每个自然日清一次过期采样，避免长期运行的后端把库越积越大。
func (s *Sampler) purgeIfNewDay(ctx context.Context, lastPurgeDay *string) {
	day := s.now().In(foundation.AStockLocation).Format("2006-01-02")
	if day == *lastPurgeDay {
		return
	}
	*lastPurgeDay = day
	if err := s.store.PurgeBefore(ctx, s.now().AddDate(0, 0, -RetentionDays).Format("2006-01-02")); err != nil {
		s.logger().Printf("level=warn event=scheduler_error feature=market-data task=sector_flow_sampler stage=purge error=%q", err.Error())
	}
}

func (s *Sampler) sampleIfDue(ctx context.Context) {
	now := s.now()
	if !InTradingWindow(now) {
		return
	}
	startedAt := time.Now()
	saved, err := s.SampleOnce(ctx, now)
	if err != nil {
		s.logger().Printf("level=warn event=scheduler_error feature=market-data task=sector_flow_sampler rows=%d duration_ms=%d error=%q", saved, time.Since(startedAt).Milliseconds(), err.Error())
		return
	}
	s.logger().Printf("level=info event=scheduler_run feature=market-data task=sector_flow_sampler rows=%d duration_ms=%d", saved, time.Since(startedAt).Milliseconds())
}

// SampleOnce 抓取两个维度的榜单并写入采样表。
//
// 两个维度各自独立：一个维度失败不影响另一个维度的采样入库，
// 只有两个都失败时才把错误返回给调用方记录。
func (s *Sampler) SampleOnce(ctx context.Context, now time.Time) (int, error) {
	local := now.In(foundation.AStockLocation)
	tradeDate := local.Format("2006-01-02")
	minute := SampleMinute(local)

	samples := make([]Sample, 0, s.config.Limit*2)
	failures := make([]string, 0, 2)
	for _, dimension := range []string{"concept", "industry"} {
		ranks, _, err := s.provider.SectorFlowRanking(ctx, dimension, s.config.Limit)
		if err != nil {
			failures = append(failures, dimension+": "+err.Error())
			continue
		}
		for _, rank := range ranks {
			if strings.TrimSpace(rank.Code) == "" {
				continue
			}
			samples = append(samples, Sample{
				TradeDate:     tradeDate,
				Minute:        minute,
				Code:          rank.Code,
				Name:          rank.Name,
				Dimension:     dimension,
				Main:          rank.MainNetInflow,
				SuperLarge:    rank.SuperLargeNet,
				Large:         rank.LargeNet,
				Medium:        rank.MediumNet,
				Small:         rank.SmallNet,
				ChangePercent: rank.ChangePercent,
				SampledAt:     now,
			})
		}
	}
	if len(samples) == 0 {
		if len(failures) == 0 {
			return 0, fmt.Errorf("sector flow sampler got no boards")
		}
		return 0, fmt.Errorf("sector flow sampler failed: %s", strings.Join(failures, "; "))
	}
	saved, err := s.store.Save(ctx, samples)
	if err != nil {
		return saved, err
	}
	if len(failures) > 0 {
		s.logger().Printf("level=warn event=scheduler_error feature=market-data task=sector_flow_sampler rows=%d error=%q", saved, strings.Join(failures, "; "))
	}
	return saved, nil
}

func (s *Sampler) now() time.Time {
	if s.config.Now != nil {
		return s.config.Now()
	}
	return time.Now()
}

func (s *Sampler) logger() *log.Logger {
	if s.config.Logger != nil {
		return s.config.Logger
	}
	return log.Default()
}

// InTradingWindow 判断是否处于可以采样的时段：交易日 09:25–11:35 与 12:55–15:05。
//
// 两端各留 5 分钟余量，用来覆盖集合竞价结束和收盘后的最终累计值；
// 其余时间不发请求，避免无效轮询。
func InTradingWindow(value time.Time) bool {
	local := value.In(foundation.AStockLocation)
	if !foundation.IsAStockTradingDay(local) {
		return false
	}
	total := local.Hour()*60 + local.Minute()
	morning := total >= 9*60+25 && total <= 11*60+35
	afternoon := total >= 12*60+55 && total <= 15*60+5
	return morning || afternoon
}

// SampleMinute 把时刻规整成交易时间轴上的分钟标签。
//
// 午休与开盘前的采样会夹到 09:31 / 13:01，收盘后夹到 15:00，
// 这样本地曲线不会在午休区间的空白处多出一个折点。
func SampleMinute(value time.Time) string {
	local := value.In(foundation.AStockLocation)
	total := local.Hour()*60 + local.Minute()
	switch {
	case total < 9*60+31:
		total = 9*60 + 31
	case total > 11*60+30 && total < 13*60+1:
		total = 13*60 + 1
	case total > 15*60:
		total = 15 * 60
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}
