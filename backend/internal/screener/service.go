package screener

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

const (
	// defaultKlineUniverse 是 K 线策略默认计算池上限。
	defaultKlineUniverse = 400
	// maxKlineUniverse 是允许的最大计算池。
	maxKlineUniverse = 600
	// klineBars 是每只股票拉取的日K数量（需覆盖 MA60 + 指标预热）。
	klineBars = 140
	// klineWorkers 是K线拉取并发度。上游 clist/kline 接口对该并发可容忍。
	klineWorkers = 8
	// fallbackPoolSize 是未勾选快照策略时的成交额保底池。
	fallbackPoolSize = 150
	// excludeNewBars 是「排除次新股」所需的最少K线根数。
	excludeNewBars = 60
)

// SnapshotProvider 提供全市场快照行（由东财 client 实现）。
type SnapshotProvider interface {
	MarketSnapshotRows(ctx context.Context) ([]foundation.MarketQuoteRow, error)
}

// KlineLoader 按代码拉取日 K（由 httpapi 的主备 K 线链路注入）。
type KlineLoader func(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error)

// ConceptLookup 批量返回代码到概念列表的映射（来自股票概念目录）。
type ConceptLookup func(ctx context.Context, symbols []string) (map[string][]string, error)

// Service 是策略选股执行引擎。
type Service struct {
	snapshots SnapshotProvider
	loadKLine KlineLoader
	// concepts 可选：注入后为命中结果补充所属概念标签。
	concepts ConceptLookup
}

// NewService 构造引擎。前两个依赖不可为空，概念目录可选。
func NewService(snapshots SnapshotProvider, loadKLine KlineLoader, concepts ConceptLookup) (*Service, error) {
	if snapshots == nil || loadKLine == nil {
		return nil, errors.New("screener service requires snapshot provider and kline loader")
	}
	return &Service{snapshots: snapshots, loadKLine: loadKLine, concepts: concepts}, nil
}

// maxHitConcepts 是单只命中结果附带的概念数上限。
const maxHitConcepts = 3

// hitAcc 累计一只股票在多个策略下的命中信息。
type hitAcc struct {
	row        SnapshotRow
	strategies []string
	details    map[string]string
	indicators map[string]float64
}

func (h *hitAcc) add(strategyID, detail string, indicators map[string]float64) {
	h.strategies = append(h.strategies, strategyID)
	if h.details == nil {
		h.details = map[string]string{}
	}
	h.details[strategyID] = detail
	if h.indicators == nil {
		h.indicators = map[string]float64{}
	}
	for key, value := range indicators {
		h.indicators[key] = value
	}
}

// Run 执行一次选股。整体预算由调用方的 ctx 控制（建议 ≥120 秒）。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	started := time.Now()
	ids, err := normalizeStrategyIDs(req.StrategyIDs)
	if err != nil {
		return Result{}, err
	}
	opts := req.Options
	if opts.KlineUniverseLimit <= 0 {
		opts.KlineUniverseLimit = defaultKlineUniverse
	}
	if opts.KlineUniverseLimit > maxKlineUniverse {
		opts.KlineUniverseLimit = maxKlineUniverse
	}

	rows, err := s.snapshots.MarketSnapshotRows(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("全市场快照不可用: %w", err)
	}
	snapshotMs := time.Since(started).Milliseconds()

	// 全局过滤：停牌剔除；ST/成交额按选项。
	pool := make([]SnapshotRow, 0, len(rows))
	for _, row := range rows {
		if rowIsSuspended(row) || row.Symbol == "" {
			continue
		}
		if opts.ExcludeST && rowIsST(row) {
			continue
		}
		if opts.MinAmountYi > 0 && row.Amount < opts.MinAmountYi*1e8 {
			continue
		}
		pool = append(pool, row)
	}

	var warnings []string
	scanned := len(pool)
	acc := map[string]*hitAcc{}

	// ── 第一级：快照策略 ──
	var klineSelected []*strategyDef
	for _, id := range ids {
		def := strategyByID(id)
		if def == nil {
			warnings = append(warnings, "未知策略已跳过: "+id)
			continue
		}
		if def.meta.Kind == KindSnapshot {
			for _, row := range pool {
				params := Params{}
				if ok, detail := def.snapshot(row, params); ok {
					key := row.Symbol
					entry := acc[key]
					if entry == nil {
						entry = &hitAcc{row: row}
						acc[key] = entry
					}
					entry.add(def.meta.ID, detail, nil)
				}
			}
		} else {
			klineSelected = append(klineSelected, def)
		}
	}

	// ── 第二级：K 线策略 ──
	result := Result{
		Strategies:  ids,
		Options:     opts,
		Scanned:     scanned,
		SnapshotMs:  snapshotMs,
		GeneratedAt: time.Now(),
	}
	if len(klineSelected) > 0 {
		universe := klineUniverse(pool, acc, opts.KlineUniverseLimit)
		if len(universe) == opts.KlineUniverseLimit && len(pool) > opts.KlineUniverseLimit {
			warnings = append(warnings, fmt.Sprintf("K线策略计算池已达上限 %d 只（按成交额与快照命中优先），可缩小快照条件或提高上限", opts.KlineUniverseLimit))
		}
		klineStart := time.Now()
		klineHits, klineFailed := s.runKlineStrategies(ctx, universe, klineSelected, opts)
		for key, entry := range klineHits {
			if existing := acc[key]; existing != nil {
				for _, id := range entry.strategies {
					existing.add(id, entry.details[id], entry.indicators)
				}
				continue
			}
			acc[key] = entry
		}
		result.KlineCount = len(universe)
		result.KlineFailed = klineFailed
		result.KlineMs = time.Since(klineStart).Milliseconds()
	}

	// ── 汇总输出 ──
	hits := make([]Hit, 0, len(acc))
	for _, entry := range acc {
		if len(entry.strategies) == 0 {
			continue
		}
		strategies := append([]string(nil), entry.strategies...)
		sort.Strings(strategies)
		hits = append(hits, Hit{
			Symbol:        entry.row.Symbol,
			Name:          entry.row.Name,
			Close:         entry.row.Close,
			ChangePercent: entry.row.ChangePercent,
			Amount:        entry.row.Amount,
			TurnoverRate:  entry.row.TurnoverRate,
			VolumeRatio:   entry.row.VolumeRatio,
			FloatCapYi:    entry.row.FloatCapYi,
			Strategies:    strategies,
			Details:       entry.details,
			Indicators:    entry.indicators,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if len(hits[i].Strategies) != len(hits[j].Strategies) {
			return len(hits[i].Strategies) > len(hits[j].Strategies)
		}
		return hits[i].Amount > hits[j].Amount
	})
	// 概念标签：目录不可用时静默跳过，不影响选股主流程。
	if s.concepts != nil && len(hits) > 0 {
		symbols := make([]string, 0, len(hits))
		for _, hit := range hits {
			symbols = append(symbols, hit.Symbol)
		}
		lookupCtx, lookupCancel := context.WithTimeout(ctx, 15*time.Second)
		defer lookupCancel()
		if conceptMap, lookupErr := s.concepts(lookupCtx, symbols); lookupErr == nil {
			for i := range hits {
				if concepts := conceptMap[hits[i].Symbol]; len(concepts) > 0 {
					if len(concepts) > maxHitConcepts {
						concepts = concepts[:maxHitConcepts]
					}
					hits[i].Concepts = concepts
				}
			}
		} else {
			result.Warnings = append(result.Warnings, "概念目录不可用，结果未附概念标签")
		}
	}

	result.Hits = hits
	result.Matched = len(hits)
	result.ElapsedMs = time.Since(started).Milliseconds()
	result.Warnings = warnings
	return result, nil
}

// normalizeStrategyIDs 去重并校验非空。
func normalizeStrategyIDs(ids []string) ([]string, error) {
	cleaned := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if strategyByID(id) == nil {
			return nil, fmt.Errorf("未知策略: %s", id)
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return nil, errors.New("至少选择一个策略")
	}
	return cleaned, nil
}

// klineUniverse 组装 K 线策略计算池：快照命中优先，成交额保底，封顶 limit。
func klineUniverse(pool []SnapshotRow, acc map[string]*hitAcc, limit int) []SnapshotRow {
	inAcc := func(symbol string) bool { _, ok := acc[symbol]; return ok }
	selected := make([]SnapshotRow, 0, limit)
	selectedSet := map[string]bool{}
	add := func(row SnapshotRow) {
		if selectedSet[row.Symbol] {
			return
		}
		selectedSet[row.Symbol] = true
		selected = append(selected, row)
	}
	for _, row := range pool {
		if len(selected) >= limit {
			break
		}
		if inAcc(row.Symbol) {
			add(row)
		}
	}
	if len(selected) < limit {
		rest := make([]SnapshotRow, 0, len(pool))
		for _, row := range pool {
			if !selectedSet[row.Symbol] {
				rest = append(rest, row)
			}
		}
		sort.SliceStable(rest, func(i, j int) bool { return rest[i].Amount > rest[j].Amount })
		for _, row := range rest {
			if len(selected) >= limit {
				break
			}
			add(row)
		}
	}
	return selected
}

// runKlineStrategies 并发对计算池逐只拉K线并执行全部 K 线策略。
func (s *Service) runKlineStrategies(ctx context.Context, universe []SnapshotRow,
	strategies []*strategyDef, opts Options) (map[string]*hitAcc, int) {
	hits := map[string]*hitAcc{}
	var mu sync.Mutex
	failed := 0

	jobs := make(chan SnapshotRow)
	var wg sync.WaitGroup
	for worker := 0; worker < klineWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				bars, err := s.loadKLine(ctx, row.Symbol, klineBars)
				if err != nil || len(bars) < 15 {
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}
				if opts.ExcludeNew && len(bars) < excludeNewBars {
					continue
				}
				// KLine 的 ChangePercent 可能缺省：K线策略里统一用收盘价现算，不依赖该字段。
				entry := &hitAcc{row: row}
				for _, def := range strategies {
					ok, detail, indicators := def.kline(bars, Params{})
					if ok {
						entry.add(def.meta.ID, detail, indicators)
					}
				}
				if len(entry.strategies) == 0 {
					continue
				}
				mu.Lock()
				hits[row.Symbol] = entry
				mu.Unlock()
			}
		}()
	}
	for _, row := range universe {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return hits, failed
		case jobs <- row:
		}
	}
	close(jobs)
	wg.Wait()
	return hits, failed
}
