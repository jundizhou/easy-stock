package themeindex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

const historyLimit = 301
const maxSample = 128
const samplingTolerancePercent = .2

type BoardProvider interface {
	ThemeIndexBoards(context.Context) ([]foundation.Board, error)
	BoardKLine(context.Context, string, int) ([]foundation.KLine, error)
}
type Config struct {
	Resolve func(context.Context, string, string) (foundation.ThemeIndexTarget, error)
	Members func(context.Context, string, string) (foundation.SectorMap, error)
	KLines  func(context.Context, string, int) ([]foundation.KLine, error)
	Boards  BoardProvider
	Now     func() time.Time
}
type Service struct {
	config  Config
	series  *cache[foundation.ThemeIndexSeries]
	bars    *cache[[]foundation.KLine]
	catalog *cache[[]foundation.Board]
	workers chan struct{}
}

func New(config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config, series: newCache[foundation.ThemeIndexSeries](32), bars: newCache[[]foundation.KLine](512), catalog: newCache[[]foundation.Board](1), workers: make(chan struct{}, 6)}
}
func (s *Service) Series(ctx context.Context, theme, snapshot string, limit int) (foundation.ThemeIndexSeries, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	value, err := s.series.load(ctx, theme+"@"+snapshot, s.config.Now(), 5*time.Minute, func(ctx context.Context) (foundation.ThemeIndexSeries, error) { return s.build(ctx, theme, snapshot) })
	if err == nil {
		if limit <= 0 || limit > 300 {
			limit = 240
		}
		start := max(0, len(value.Lines)-limit)
		value.Lines = append([]foundation.KLine(nil), value.Lines[start:]...)
	}
	return value, err
}

func (s *Service) Refresh(theme, snapshot string) {
	s.series.invalidate(theme+"@"+snapshot, false)
	s.bars.invalidate("", true)
	s.catalog.invalidate("", true)
}

func markFreshness(value *foundation.ThemeIndexSeries, now time.Time) {
	day := now.In(location)
	if day.Hour() < 15 {
		day = day.AddDate(0, 0, -1)
	}
	for !foundation.IsAStockTradingDay(day) {
		day = day.AddDate(0, 0, -1)
	}
	if value.Meta.TradeDate < day.Format("2006-01-02") {
		value.Meta.Stale = true
		value.Warnings = append(value.Warnings, "历史行情落后于最近收盘交易日，请留意数据日期。")
	}
}

var boardCodePattern = regexp.MustCompile(`^BK[0-9]{4,6}$`)

func nameKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range []string{"行业", "Ⅱ", "Ⅰ", "ⅱ", "ⅰ"} {
		name = strings.TrimSuffix(name, strings.ToLower(suffix))
	}
	return name
}

// MatchBoard requires either an exact provider code or an unambiguous exact
// name/crosswalk. Broad substring matches can chart an entirely different theme.
func MatchBoard(target foundation.ThemeIndexTarget, boards []foundation.Board) (foundation.Board, bool) {
	if boardCodePattern.MatchString(target.BoardCode) {
		return foundation.Board{Code: target.BoardCode, Name: target.Name}, true
	}
	for _, name := range target.Names {
		var match foundation.Board
		ambiguous := false
		for _, board := range boards {
			if nameKey(name) == "" || nameKey(name) != nameKey(board.Name) || !boardCodePattern.MatchString(board.Code) {
				continue
			}
			if match.Code != "" && match.Code != board.Code {
				ambiguous = true
				break
			}
			match = board
		}
		if match.Code != "" && !ambiguous {
			return match, true
		}
	}
	return foundation.Board{}, false
}
func (s *Service) build(ctx context.Context, theme, snapshot string) (foundation.ThemeIndexSeries, error) {
	now := s.config.Now()
	target, err := s.config.Resolve(ctx, theme, snapshot)
	if err != nil {
		return foundation.ThemeIndexSeries{}, err
	}
	value := foundation.ThemeIndexSeries{Theme: theme, Name: target.Name, Warnings: []string{}, Meta: foundation.SourceMeta{FetchedAt: now, SnapshotID: snapshot}}
	var boardErr error
	if s.config.Boards != nil {
		boardCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		board, matched := MatchBoard(target, nil)
		if !matched {
			boards, loadErr := s.catalog.load(boardCtx, "all", now, 6*time.Hour, s.config.Boards.ThemeIndexBoards)
			boardErr = loadErr
			board, matched = MatchBoard(target, boards)
		}
		if matched {
			lines, loadErr := s.config.Boards.BoardKLine(boardCtx, board.Code, historyLimit)
			boardErr = loadErr
			lines = CleanBars(lines, now)
			lines = lines[max(0, len(lines)-historyLimit):]
			if loadErr == nil && len(lines) >= 2 {
				cancel()
				value.Method = "provider-index"
				value.IndexCode, value.IndexName = board.Code, board.Name
				value.Lines = lines
				value.Meta = lines[len(lines)-1].Meta
				value.Meta.SnapshotID = snapshot
				value.Meta.TradeDate = lines[len(lines)-1].Time.Format("2006-01-02")
				markFreshness(&value, now)
				return value, nil
			}
		}
		cancel()
	}
	if s.config.Members == nil || s.config.KLines == nil {
		return value, fmt.Errorf("题材指数数据源不可用")
	}
	memberCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	members, err := s.config.Members(memberCtx, theme, snapshot)
	cancel()
	if err != nil {
		return value, err
	}
	symbols := MemberSymbols(theme, members)
	if len(symbols) < 2 {
		return value, fmt.Errorf("有效题材成分不足，不能用单只领涨股替代题材指数")
	}
	paths := map[string][]foundation.KLine{}
	sampled := 0
	for sampled < min(len(symbols), maxSample) {
		batchSize := 32
		if len(symbols) <= maxSample {
			batchSize = len(symbols)
		}
		end := min(sampled+batchSize, len(symbols), maxSample)
		s.loadBatch(ctx, symbols[sampled:end], paths)
		sampled = end
		lines, quality := Compose(theme, paths, len(symbols), now)
		quality.Sampled = sampled
		quality.HistoryCoverage = float64(quality.Loaded) / float64(sampled)
		// Missing fetches are not a uniform random sample: their bias cannot be
		// quantified by the finite-population sampling variance.
		quality.SamplingEstimateAvailable = quality.SamplingEstimateAvailable && sampled < len(symbols) && quality.Loaded == sampled
		if !quality.SamplingEstimateAvailable {
			quality.SamplingErrorPercent = 0
		}
		value.Lines, value.Quality = lines, quality
		if len(symbols) <= maxSample || (len(paths) >= 32 && quality.SamplingEstimateAvailable && quality.SkippedDays == 0 && quality.SamplingErrorPercent <= samplingTolerancePercent) || ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return value, ctx.Err()
	}
	if len(value.Lines) < 2 || float64(len(paths))/float64(sampled) < .8 {
		return value, fmt.Errorf("题材历史行情覆盖不足（%d/%d），请重试", len(paths), sampled)
	}
	value.Method = "equal-weight"
	value.BaseValue = baseValue
	baseDay := value.Lines[0].Time.AddDate(0, 0, -1)
	for !foundation.IsAStockTradingDay(baseDay) {
		baseDay = baseDay.AddDate(0, 0, -1)
	}
	value.BaseDate = baseDay.Format("2006-01-02")
	value.Meta.Source = "theme-index:equal-weight"
	value.Meta.TradeDate = value.Lines[len(value.Lines)-1].Time.Format("2006-01-02")
	value.Warnings = append(value.Warnings, "按当前成分回溯，不能用于无前视偏差的历史回测；合成高低价为成分股极值包络，成交量为已加载成分合计。")
	value.Warnings = append(value.Warnings, "参考指数以历史窗口基准日收盘为1000点；窗口滚动会重标点位，涨跌幅口径保持一致。")
	if boardErr != nil {
		value.Warnings = append(value.Warnings, "来源指数暂不可用，显示合成参考指数。")
	}
	if sampled < len(symbols) {
		value.Warnings = append(value.Warnings, fmt.Sprintf("稳定均匀抽样 %d/%d 只；日收益抽样误差为条件性估计，不代表累计走势误差上限。", sampled, len(symbols)))
	}
	if value.Quality.SamplingEstimateAvailable && value.Quality.SamplingErrorPercent > samplingTolerancePercent {
		value.Warnings = append(value.Warnings, "题材分化较大，已达到抽样上限；日收益误差参考值未达到0.20个百分点的目标。")
	}
	if len(paths) < sampled {
		value.Meta.Stale = true
		value.Warnings = append(value.Warnings, "部分成分历史缺失，参考指数可能存在偏差，无法估计缺失造成的误差。")
	}
	if value.Quality.SkippedDays > 0 {
		value.Meta.Stale = true
		value.Warnings = append(value.Warnings, "历史行情覆盖不足或异常，末尾区间已截断。")
	} else if value.Quality.MinimumCoverage < 1 {
		value.Warnings = append(value.Warnings, "部分日期成分历史不完整，按当日有效成分计算。")
	}
	markFreshness(&value, now)
	return value, nil
}
func MemberSymbols(theme string, members foundation.SectorMap) []string {
	set := map[string]struct{}{}
	for _, group := range members.Groups {
		for _, node := range group.Nodes {
			for _, stock := range node.Stocks {
				if symbol, err := foundation.NormalizeSymbol(stock.Symbol); err == nil {
					set[symbol.Canonical] = struct{}{}
				}
			}
		}
	}
	symbols := make([]string, 0, len(set))
	for symbol := range set {
		symbols = append(symbols, symbol)
	}
	// Hash order samples the complete basket independently of price, cap,
	// provider rank and current winners; it is stable across pagination/sorting.
	sort.Slice(symbols, func(i, j int) bool {
		left := sha256.Sum256([]byte(theme + "\x00" + symbols[i]))
		right := sha256.Sum256([]byte(theme + "\x00" + symbols[j]))
		return string(left[:]) < string(right[:])
	})
	return symbols
}
func (s *Service) loadBatch(ctx context.Context, symbols []string, paths map[string][]foundation.KLine) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, symbol := range symbols {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			bars, err := s.bars.load(ctx, symbol, s.config.Now(), 5*time.Minute, func(ctx context.Context) ([]foundation.KLine, error) {
				select {
				case s.workers <- struct{}{}:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				defer func() { <-s.workers }()
				bars, err := s.config.KLines(ctx, symbol, historyLimit)
				if err != nil {
					return nil, err
				}
				bars = CleanBars(bars, s.config.Now())
				if len(bars) < 2 {
					return nil, fmt.Errorf("%s 复权历史行情不足", symbol)
				}
				return bars[max(0, len(bars)-historyLimit):], nil
			})
			if err == nil {
				mu.Lock()
				paths[symbol] = bars
				mu.Unlock()
			}
		}(symbol)
	}
	wg.Wait()
}
