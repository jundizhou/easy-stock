package foundation

import (
	"context"
	"sync"
)

// FanOut 用有界 worker 池并发处理 items，镜像 quant-stock 的
// ThreadPoolExecutor(max_workers) + as_completed(timeout=deadline) 模式。
//
// 语义要点：
//   - 最多 workers 个 goroutine 同时运行，避免对本地/远程源打满连接；
//   - 每个 item 通过 handle(ctx, item) 处理，成功结果由 handle 自行收集
//     （通常用调用方持有的带锁切片/映射），本函数只负责并发编排与错误归集；
//   - ctx 一旦取消（调用方 deadline 到期或请求断开），尚未开始的 item 直接跳过，
//     正在运行的 handle 也会因 ctx 取消而快速退出——即「deadline 兜底」。
//
// 返回按原索引归集的处理错误；无错误时返回 nil。
func FanOut[T any](ctx context.Context, items []T, workers int, handle func(ctx context.Context, item T) error) map[int]error {
	if workers <= 0 {
		workers = 8
	}
	if len(items) == 0 {
		return nil
	}

	type job struct {
		idx  int
		item T
	}
	jobs := make(chan job)
	errs := make(map[int]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := handle(ctx, j.item); err != nil {
					mu.Lock()
					errs[j.idx] = err
					mu.Unlock()
				}
			}
		}()
	}

	// 投递阶段：ctx 取消时立即停手，避免向已退出的 worker 阻塞。
	go func() {
		defer close(jobs)
		for i, it := range items {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{idx: i, item: it}:
			}
		}
	}()

	wg.Wait()
	if len(errs) == 0 {
		return nil
	}
	return errs
}
