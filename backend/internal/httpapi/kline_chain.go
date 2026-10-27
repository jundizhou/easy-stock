package httpapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// 熔断参数。
//
// 监控会批量读取标的日线。连续失败的数据源暂时冷却，避免每只标的
// 都重复等待同一次故障；冷却结束后重新尝试主源。
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
	var firstErr error
	attempted := 0
	// 所有源都在冷却里时照样按顺序试一遍：宁可多等一次超时，
	// 也不要因为熔断状态本身让整轮决策拿不到数据。
	forceAll := !c.anyAvailable(clock())
	for index, source := range c.sources {
		if !forceAll && c.isOpen(index, clock()) {
			continue
		}
		attempted++
		bars, err := source.KLine(ctx, symbol, period, limit)
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
		// 上下文取消就没必要继续试后面的源，重试只会拖长超时。
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}
	if attempted == 0 {
		return nil, fmt.Errorf("没有可尝试的日线数据源")
	}
	return nil, firstErr
}

// isOpen 表示该源在冷却期内应被跳过。
func (c *klineChain) isOpen(index int, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.openUntil) {
		return false
	}
	return now.Before(c.openUntil[index])
}

func (c *klineChain) anyAvailable(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := range c.openUntil {
		if !now.Before(c.openUntil[index]) {
			return true
		}
	}
	return false
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
func isNilProvider(provider KLineProvider) bool {
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}
