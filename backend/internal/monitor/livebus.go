package monitor

import (
	"context"
	"sync"
	"time"
)

// LiveTick 是快照里一只标的的实时价快照，用于监控中心各页签的实时价推送。
type LiveTick struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	ChangePercent float64 `json:"change_percent"`
	FetchedAt     int64   `json:"ts"`
}

// liveSubscriber 是广播总线的一个订阅者；ch 收到信号表示有新批次可用。
type liveSubscriber struct {
	ch chan struct{}
}

// liveBus 是引擎内的进程内实时价广播：快照刷新后发布全量 ticks，
// SSE 端点按自己关心的 symbols 订阅并过滤。一次上游拉取（快照）被所有
// 订阅者共享，不会因多页签/多客户端而放大上游请求。
type liveBus struct {
	mu   sync.Mutex
	subs map[*liveSubscriber]struct{}
	last map[string]LiveTick
}

func newLiveBus() *liveBus {
	return &liveBus{
		subs: map[*liveSubscriber]struct{}{},
		last: map[string]LiveTick{},
	}
}

// publish 用一份新快照覆盖并通知所有订阅者。symbols 为空表示「全部」。
func (b *liveBus) publish(rows []LiveTick) {
	m := make(map[string]LiveTick, len(rows))
	for _, r := range rows {
		m[r.Symbol] = r
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = m
	for s := range b.subs {
		select {
		case s.ch <- struct{}{}:
		default:
			// 订阅者处理慢就丢这一帧，下一帧再推；不阻塞发布方。
		}
	}
}

func (b *liveBus) hasSubscribers() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs) > 0
}

const liveSnapshotInterval = 5 * time.Second

// 价格刷新独立于规则、偏离与雷达计算；客户端数量不改变取数频率。
func (e *Engine) runLiveQuotes(ctx context.Context) {
	ticker := time.NewTicker(liveSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.refreshLiveQuotes(ctx)
		}
	}
}

func (e *Engine) refreshLiveQuotes(ctx context.Context) {
	if ctx.Err() != nil || !e.liveBus.hasSubscribers() || !e.settings().Enabled {
		return
	}
	_, _, sessionOpen := sessionState(e.now())
	if !sessionOpen {
		return
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, _ = e.snapshotRowsWithTTL(refreshCtx, liveSnapshotInterval)
}

// subscribe 返回一个只收信号的 channel 与退订函数。
func (b *liveBus) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s := &liveSubscriber{ch: ch}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	unsub := func() {
		b.mu.Lock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, unsub
}

// snapshot 返回 last 中属于 symbols 的子集；symbols 为空表示全部。
func (b *liveBus) snapshot(symbols map[string]bool) []LiveTick {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(symbols) == 0 {
		out := make([]LiveTick, 0, len(b.last))
		for _, t := range b.last {
			out = append(out, t)
		}
		return out
	}
	out := make([]LiveTick, 0, len(symbols))
	for sym := range symbols {
		if t, ok := b.last[sym]; ok {
			out = append(out, t)
		}
	}
	return out
}
