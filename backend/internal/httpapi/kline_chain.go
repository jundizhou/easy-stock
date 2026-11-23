package httpapi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// marketKLineChain keeps failures local to providers that serve the requested market.
type marketKLineChain struct {
	hongKong HongKongKLineProvider
	aStock   KLineProvider
}

func newMarketKLineChain(hongKong HongKongKLineProvider, aStock KLineProvider) KLineProvider {
	return &marketKLineChain{hongKong: hongKong, aStock: aStock}
}

func (c *marketKLineChain) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	if foundation.IsHongKongSymbol(symbol) {
		if isNilProvider(c.hongKong) {
			return nil, fmt.Errorf("港股日线源不可用")
		}
		return c.hongKong.HongKongKLine(ctx, symbol, period, limit)
	}
	return c.aStock.KLine(ctx, symbol, period, limit)
}

const (
	breakerThreshold = 3
	breakerCooldown  = 2 * time.Minute
)

// klineChain 按顺序尝试多个日线源，第一个成功即返回。
//
// 全部失败时返回**第一个**源的错误而不是最后一个：调用方问的是
// 「为什么今天拿不到日线」，保留主源的失败原因供排查。
type klineChain struct {
	sources []KLineProvider

	mu        sync.Mutex
	failures  []int
	openUntil []time.Time

	// now 可注入，测试里用来推进冷却时间。
	now func() time.Time
}

func newKLineChain(sources ...KLineProvider) KLineProvider {
	kept := make([]KLineProvider, 0, len(sources))
	for _, source := range sources {
		// 接口里装着 nil 指针时 != nil 判定会失效，这里用反射避免坏源进入链路。
		if source != nil && !isNilProvider(source) {
			kept = append(kept, source)
		}
	}
	if len(kept) == 1 {
		return kept[0]
	}
	return &klineChain{
		sources:   kept,
		failures:  make([]int, len(kept)),
		openUntil: make([]time.Time, len(kept)),
		now:       time.Now,
	}
}

func (c *klineChain) KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	if len(c.sources) == 0 {
		return nil, fmt.Errorf("没有可用的日线数据源")
	}
	clock := c.now
	if clock == nil {
		clock = time.Now
	}
	available := c.availableSources(clock(), symbol, period)
	if len(available) == 0 {
		return nil, fmt.Errorf("没有支持该市场及周期的 K 线源")
	}
	var firstErr error
	for position, index := range available {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sourceCtx := ctx
		var cancel context.CancelFunc
		// 在调用方的总时限内给后续可用源保留时间，最后一个源可用完剩余时间。
		remaining := len(available) - position
		if deadline, ok := ctx.Deadline(); ok && remaining > 1 {
			sourceCtx, cancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(remaining))
		}
		bars, err := c.sources[index].KLine(sourceCtx, symbol, period, limit)
		if cancel != nil {
			cancel()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err == nil && len(bars) > 0 {
			c.record(index, nil)
			return bars, nil
		}
		if err == nil {
			err = fmt.Errorf("数据源返回空日线")
		}
		c.record(index, err)
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func (c *klineChain) availableSources(now time.Time, symbol, period string) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var supported []int
	indices := make([]int, 0, len(c.sources))
	for i, until := range c.openUntil {
		if p, ok := c.sources[i].(interface{ SupportsKLine(string, string) bool }); ok && !p.SupportsKLine(symbol, period) {
			continue
		}
		supported = append(supported, i)
		if !now.Before(until) {
			indices = append(indices, i)
		}
	}
	// 所有源都在冷却时，仍按原顺序尝试。
	if len(indices) == 0 {
		return supported
	}
	return indices
}

// record 记录一次调用的结果：成功清零，失败累计到阈值后进入冷却。
//
// 阈值取 3 而不是 1：单只标的取不到日线有很多正常原因（长期停牌、退市边缘、
// 服务端个别分区缺失），不该因此把整个源停掉。连着 3 次都失败，才更像
// "这台机器不可达"而不是"这只票没数据"。
func (c *klineChain) record(index int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.failures) {
		return
	}
	if err == nil {
		c.failures[index] = 0
		c.openUntil[index] = time.Time{}
		return
	}
	c.failures[index]++
	if c.failures[index] >= breakerThreshold {
		clock := c.now
		if clock == nil {
			clock = time.Now
		}
		c.openUntil[index] = clock().Add(breakerCooldown)
	}
}

// isNilProvider 识别「接口非 nil、底层指针为 nil」的坏源。
// 这种源一旦进入链路，会在调用时 panic，而 panic 发生在交易决策的中途，
// 代价远大于这里多做一次判断。
func isNilProvider(provider any) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}
