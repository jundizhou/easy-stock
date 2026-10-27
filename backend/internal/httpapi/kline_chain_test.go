package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type stubKLine struct {
	bars []foundation.KLine
	err  error
	hits int
}

func (s *stubKLine) KLine(_ context.Context, _ string, _ string, _ int) ([]foundation.KLine, error) {
	s.hits++
	return s.bars, s.err
}

func okBars(n int) []foundation.KLine {
	bars := make([]foundation.KLine, n)
	for i := range bars {
		bars[i] = foundation.KLine{Close: 10 + float64(i)}
	}
	return bars
}

// 主源成功时不能再去打备源：备源是公网请求，白打一次就是白等一次。
func TestKLineChainStopsAtFirstSuccess(t *testing.T) {
	primary := &stubKLine{bars: okBars(3)}
	fallback := &stubKLine{bars: okBars(2)}
	bars, err := newKLineChain(primary, fallback).KLine(context.Background(), "600519", "day", 60)
	if err != nil {
		t.Fatalf("期望成功，得到错误：%v", err)
	}
	if len(bars) != 3 {
		t.Errorf("应取主源结果，得到 %d 根", len(bars))
	}
	if fallback.hits != 0 {
		t.Errorf("主源成功时不应调用备源，备源被调用 %d 次", fallback.hits)
	}
}

// 主源失败后按顺序尝试后续行情源。
func TestKLineChainFallsThroughOnError(t *testing.T) {
	primary := &stubKLine{err: errors.New("局域网不可达")}
	fallback := &stubKLine{bars: okBars(2)}
	bars, err := newKLineChain(primary, fallback).KLine(context.Background(), "600519", "day", 60)
	if err != nil {
		t.Fatalf("备源可用时应成功，得到错误：%v", err)
	}
	if len(bars) != 2 || fallback.hits != 1 {
		t.Errorf("应落到备源，实际 bars=%d hits=%d", len(bars), fallback.hits)
	}
}

// 返回空日线也算失败。否则上层拿到 0 根会以为"这只票没有历史"，
// 而真实原因是这个源今天出问题了。
func TestKLineChainTreatsEmptyAsFailure(t *testing.T) {
	primary := &stubKLine{bars: nil}
	fallback := &stubKLine{bars: okBars(2)}
	if _, err := newKLineChain(primary, fallback).KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("应落到备源，得到错误：%v", err)
	}
	if fallback.hits != 1 {
		t.Errorf("空结果应触发备源，备源命中 %d 次", fallback.hits)
	}
}

// 全部失败时报第一个源的错误：调用方问的是"主源为什么拿不到"。
func TestKLineChainReportsPrimaryError(t *testing.T) {
	primary := &stubKLine{err: errors.New("quantmind: 局域网不可达")}
	fallback := &stubKLine{err: errors.New("sina: 429 限流")}
	_, err := newKLineChain(primary, fallback).KLine(context.Background(), "600519", "day", 60)
	if err == nil {
		t.Fatal("全部失败时应返回错误")
	}
	if !strings.Contains(err.Error(), "quantmind") {
		t.Errorf("应报主源错误，得到：%v", err)
	}
}

// 上下文被取消时不应再试后面的源，否则一次已超时的决策会被拖到更久。
func TestKLineChainStopsOnContextError(t *testing.T) {
	primary := &stubKLine{err: context.DeadlineExceeded}
	fallback := &stubKLine{bars: okBars(2)}
	if _, err := newKLineChain(primary, fallback).KLine(context.Background(), "600519", "day", 60); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应直接返回超时，得到：%v", err)
	}
	if fallback.hits != 0 {
		t.Errorf("超时后不应再试备源，备源命中 %d 次", fallback.hits)
	}
}

// 只有一个源时直接返回它，不必包一层——少一次间接调用，错误信息也更干净。
func TestKLineChainSingleSourceUnwrapped(t *testing.T) {
	single := &stubKLine{bars: okBars(2)}
	chain := newKLineChain(single)
	if _, ok := chain.(*klineChain); ok {
		t.Error("单源不应包装成 klineChain")
	}
	if _, err := chain.KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("单源应可用：%v", err)
	}
}

// nil 源必须被剔除，否则链路里留一个 nil 会在调用时 panic。
func TestKLineChainSkipsNilSources(t *testing.T) {
	var typedNil *stubKLine
	single := &stubKLine{bars: okBars(2)}
	chain := newKLineChain(typedNil, single, nil)
	if _, ok := chain.(*klineChain); ok {
		t.Error("剔除 nil 后只剩一个源，不应包装成 klineChain")
	}
	if _, err := chain.KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("应使用唯一有效源：%v", err)
	}
}

func TestKLineChainEmptyReturnsError(t *testing.T) {
	if _, err := newKLineChain(nil, nil).KLine(context.Background(), "600519", "day", 60); err == nil {
		t.Fatal("没有任何源时应返回错误而不是空结果")
	}
}

// 连续失败的源要进冷却，否则主源一断，每个标的都要先等一次连接超时。
//
// 选股现在每轮为 200 多只候选取日线，这条路径的放大效应是 200 倍：
// 3 秒的连接超时乘以 270 只，就是十几分钟的等待。
func TestKLineChainBreakerSkipsFailingSource(t *testing.T) {
	primary := &stubKLine{err: errors.New("局域网不可达")}
	fallback := &stubKLine{bars: okBars(2)}
	provider := newKLineChain(primary, fallback)
	chain, ok := provider.(*klineChain)
	if !ok {
		t.Fatal("两个源应包成 klineChain")
	}
	for attempt := 0; attempt < breakerThreshold; attempt++ {
		if _, err := provider.KLine(context.Background(), "600519", "day", 60); err != nil {
			t.Fatalf("备源可用时应成功：%v", err)
		}
	}
	if primary.hits != breakerThreshold {
		t.Fatalf("冷却前主源应被调用 %d 次，实际 %d", breakerThreshold, primary.hits)
	}
	if _, err := provider.KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("冷却期内仍应由备源返回：%v", err)
	}
	if primary.hits != breakerThreshold {
		t.Fatalf("冷却期内不应再打主源，实际累计 %d 次", primary.hits)
	}
	if fallback.hits != breakerThreshold+1 {
		t.Fatalf("备源应承接全部请求，实际 %d 次", fallback.hits)
	}
	// 冷却结束后必须重新给主源机会，否则一次网络抖动会把它永久踢出链路。
	chain.now = func() time.Time { return time.Now().Add(breakerCooldown + time.Second) }
	primary.err = nil
	primary.bars = okBars(3)
	bars, err := provider.KLine(context.Background(), "600519", "day", 60)
	if err != nil || len(bars) != 3 {
		t.Fatalf("冷却结束后应重新使用主源：bars=%d err=%v", len(bars), err)
	}
	if primary.hits != breakerThreshold+1 {
		t.Fatalf("冷却结束后主源应被再次调用，实际 %d 次", primary.hits)
	}
}

// 成功一次就把计数清零：偶发的单只票无数据不该累积成"整台机器不可达"。
func TestKLineChainBreakerResetsOnSuccess(t *testing.T) {
	primary := &stubKLine{err: errors.New("这只票没数据")}
	fallback := &stubKLine{bars: okBars(2)}
	provider := newKLineChain(primary, fallback)
	for attempt := 0; attempt < breakerThreshold-1; attempt++ {
		_, _ = provider.KLine(context.Background(), "600519", "day", 60)
	}
	primary.err = nil
	primary.bars = okBars(3)
	if _, err := provider.KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("主源恢复后应成功：%v", err)
	}
	// 计数已清零，接下来两次失败不足以触发冷却。
	primary.err = errors.New("又断了")
	primary.bars = nil
	for attempt := 0; attempt < breakerThreshold-1; attempt++ {
		_, _ = provider.KLine(context.Background(), "600519", "day", 60)
	}
	hits := primary.hits
	if _, err := provider.KLine(context.Background(), "600519", "day", 60); err != nil {
		t.Fatalf("应由备源返回：%v", err)
	}
	if primary.hits != hits+1 {
		t.Fatalf("计数清零后不该已经进入冷却，主源应被调用")
	}
}

// 所有源都在冷却时仍要尝试，不能因为熔断状态本身让整轮决策拿不到数据。
func TestKLineChainBreakerStillTriesWhenAllOpen(t *testing.T) {
	primary := &stubKLine{err: errors.New("断")}
	fallback := &stubKLine{err: errors.New("也断")}
	provider := newKLineChain(primary, fallback)
	for attempt := 0; attempt < breakerThreshold; attempt++ {
		_, _ = provider.KLine(context.Background(), "600519", "day", 60)
	}
	before := primary.hits + fallback.hits
	_, _ = provider.KLine(context.Background(), "600519", "day", 60)
	if primary.hits+fallback.hits != before+2 {
		t.Fatalf("全冷却时仍应尝试全部源，实际新增 %d 次", primary.hits+fallback.hits-before)
	}
}
