package httpapi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

// Peer collection has its own small deadline and never prevents the target
// stock from producing a snapshot when one comparison symbol is unavailable.
func (s *Server) collectResearchPeers(ctx context.Context, input stockanalysis.Input) (stockanalysis.ResearchPeerGroup, []string) {
	group := stockanalysis.SelectResearchPeers(input, stockanalysis.ResearchPeerLimit)
	if len(group.Members) == 0 {
		if input.Industry != "" || len(input.Concepts) > 0 {
			return group, []string{"同业量价对照缺少可匹配的目录样本，不能判断是否为板块共同反弹"}
		}
		return group, nil
	}
	started := time.Now()
	peerCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	errors := make([]error, len(group.Members))
	workers := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range group.Members {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case workers <- struct{}{}:
				defer func() { <-workers }()
			case <-peerCtx.Done():
				errors[index] = peerCtx.Err()
				return
			}
			group.Members[index].KLines, errors[index] = s.loadKLine(peerCtx, group.Members[index].Symbol, "day", stockanalysis.ResearchPeerDailyBars)
		}(i)
	}
	wg.Wait()
	missing := 0
	for i, err := range errors {
		if err != nil || len(group.Members[i].KLines) == 0 {
			missing++
		}
	}
	status := "completed"
	gaps := []string{}
	if missing > 0 {
		status = "degraded"
		gaps = append(gaps, fmt.Sprintf("同业量价对照中%d/%d个目录样本未取得日线，不能将缺失样本当作未上涨", missing, len(group.Members)))
	}
	s.logStockAnalysisStage(input.Symbol, "peer_market_context", status, started, len(group.Members)-missing, nil)
	return group, gaps
}
