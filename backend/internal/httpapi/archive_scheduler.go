package httpapi

import (
	"context"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/archive"
	"easy-stock/backend/internal/foundation"
)

// 常驻定时采集：按周期抓取关注清单的实时报价 + 最新资讯并落盘，
// 形成本地完整的时间序列（配合 internal/archive 的按日落盘）。
//
// 配置（环境变量，见 cmd/server/main.go）：
//   - A_STOCK_ARCHIVE_INTERVAL  采集周期，Go duration（默认 60s）
//   - A_STOCK_ARCHIVE_SYMBOLS   关注标的，逗号分隔（默认一组大盘蓝筹）
//   - A_STOCK_ARCHIVE_TRADING_ONLY 仅交易时段采集（默认 true）
//
// 抗封：单次采集串行、源内部已有限速/冷却/多源回退，不会突发高频。

// ArchiveCollectorConfig 定时采集配置。
type ArchiveCollectorConfig struct {
	Interval    time.Duration
	Symbols     []string
	NewsLimit   int
	TradingOnly bool
}

// DefaultArchiveSymbols 默认关注清单（大盘蓝筹 + 指数样本，覆盖面广）。
var DefaultArchiveSymbols = []string{
	"600519.SH", "000001.SZ", "300750.SZ", "601318.SH", "000858.SZ",
	"600036.SH", "002594.SZ", "601899.SH", "600030.SH", "000333.SZ",
}

// ArchiveCollectorConfig 返回当前采集配置（供 main 启动调度器使用）。
func (s *Server) ArchiveCollectorConfig() ArchiveCollectorConfig {
	return s.archiveCollector
}

// liveArchiveConfig 读取当前生效的归档配置与解析后的清单（热更新：每轮重新读取 settings）。
func (s *Server) liveArchiveConfig() (appsettings.Archive, []string) {
	cfg := appsettings.DefaultArchive()
	if s.settingsStore != nil {
		cfg = appsettings.NormalizeArchive(s.settingsStore.Snapshot().Archive)
	}
	return cfg, s.resolveArchiveSymbols(cfg)
}

// rebindArchivers 按目标目录重建归档器（目录变更时热切换）；已存在同目录则复用。
func (s *Server) rebindArchivers(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	s.archiveMu.Lock()
	defer s.archiveMu.Unlock()
	if s.archiveActiveDir == dir && s.realtimeArchiver != nil {
		return
	}
	base := s.realtimeProviderRaw
	if base == nil {
		base = s.realtimeProvider
	}
	newsBase := s.newsProviderRaw
	if newsBase == nil {
		newsBase = s.newsProvider
	}
	if base != nil {
		if ar, err := archive.NewRealtimeArchiver(base, dir, archive.Options{}); err == nil {
			s.realtimeArchiver = ar
			s.realtimeProvider = ar
		} else if s.logger != nil {
			s.logger.Printf("level=warn event=archive_rebind kind=realtime err=%q", err.Error())
		}
	}
	if newsBase != nil {
		if ar, err := archive.NewNewsArchiver(newsBase, dir, archive.Options{}); err == nil {
			s.newsArchiver = ar
			s.newsProvider = ar
		} else if s.logger != nil {
			s.logger.Printf("level=warn event=archive_rebind kind=news err=%q", err.Error())
		}
	}
	s.archiveActiveDir = dir
}

// RunArchiveScheduler 常驻采集循环。每轮从 settings 读取最新配置（热更新），
// 支持开关 enabled、周期/目录/清单来源的运行时变更。
func (s *Server) RunArchiveScheduler(ctx context.Context, _ ArchiveCollectorConfig) {
	if s.realtimeProviderRaw == nil && s.newsProviderRaw == nil && s.realtimeArchiver == nil {
		return
	}
	// 以最小周期（5s）轮询，实际采集时机由配置周期控制，从而实现周期热更新。
	poll := 5 * time.Second
	lastRun := time.Time{}

	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_start feature=data-archive task=collect poll=%s", poll)
		defer s.logger.Printf("level=info event=scheduler_stop feature=data-archive task=collect")
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	// 启动即尝试一次
	s.runArchiveTick(ctx, &lastRun)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runArchiveTick(ctx, &lastRun)
		}
	}
}

// runArchiveTick 检查是否到达采集周期，是则执行一轮采集（含目录热切换与开关判断）。
func (s *Server) runArchiveTick(ctx context.Context, lastRun *time.Time) {
	cfg, symbols := s.liveArchiveConfig()
	if !cfg.Enabled {
		return
	}
	if cfg.Dir != "" {
		s.rebindArchivers(s.effectiveArchiveDir(cfg))
	}
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	now := time.Now()
	if !lastRun.IsZero() && now.Sub(*lastRun) < interval {
		return
	}
	*lastRun = now
	s.collectWith(ctx, cfg.TradingOnly, symbols, cfg.NewsLimit)
}

// collectOnce 按配置执行一轮采集（兼容旧签名，供手动触发/测试使用）。
func (s *Server) collectOnce(ctx context.Context, cfg ArchiveCollectorConfig) {
	s.collectWith(ctx, cfg.TradingOnly, cfg.Symbols, cfg.NewsLimit)
}

// collectWith 执行一轮采集：实时报价（关注清单）+ 最新资讯。落盘由 archiver 包装器完成。
func (s *Server) collectWith(ctx context.Context, tradingOnly bool, symbols []string, newsLimit int) {
	startedAt := time.Now()
	if tradingOnly && !inTradingSession(time.Now()) {
		if s.logger != nil {
			s.logger.Printf("level=debug event=archive_collect skipped=off_hours")
		}
		return
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var quoteCount, newsCount int
	if s.realtimeArchiver != nil && len(symbols) > 0 {
		if quotes, err := s.realtimeArchiver.Realtime(cctx, symbols); err != nil {
			if s.logger != nil {
				s.logger.Printf("level=warn event=archive_collect kind=realtime err=%q", err.Error())
			}
		} else {
			quoteCount = len(quotes)
		}
	}
	if s.newsArchiver != nil {
		limit := newsLimit
		if limit <= 0 {
			limit = 20
		}
		if items, err := s.newsArchiver.LatestNews(cctx, limit); err != nil {
			if s.logger != nil {
				s.logger.Printf("level=warn event=archive_collect kind=news err=%q", err.Error())
			}
		} else {
			newsCount = len(items)
		}
	}
	if s.logger != nil {
		s.logger.Printf("level=info event=archive_collect quotes=%d news=%d duration_ms=%d",
			quoteCount, newsCount, time.Since(startedAt).Milliseconds())
	}
}

// inTradingSession 判断是否处于 A 股交易时段（周一至周五 09:15–11:35 / 12:55–15:05）。
func inTradingSession(t time.Time) bool {
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	mins := t.Hour()*60 + t.Minute()
	switch {
	case mins >= 9*60+15 && mins <= 11*60+35:
		return true
	case mins >= 12*60+55 && mins <= 15*60+5:
		return true
	default:
		return false
	}
}

// ParseArchiveSymbols 解析逗号分隔的关注清单并规范化；为空时返回默认清单。
func ParseArchiveSymbols(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]string(nil), DefaultArchiveSymbols...)
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := foundation.NormalizeSymbol(p); err == nil {
			out = append(out, n.Canonical)
		} else {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), DefaultArchiveSymbols...)
	}
	return out
}
