package httpapi

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"fmt"
	"time"
)

func (s *Server) resolvePortfolioResearch(ctx context.Context, holding portfolioinspection.Holding, request portfolioinspection.Request, asOf time.Time, force bool, resumeID string, notify func(portfolioinspection.HoldingResult)) (portfolioinspection.HoldingResult, error) {
	result := portfolioinspection.HoldingResult{Holding: holding}
	purpose := "holding"
	if holding.Weight == 0 {
		purpose = "new_position"
	}
	job, origin, err := s.stockResearch.ResolveForPortfolio(ctx, stockanalysis.ResearchRequest{Symbol: holding.Symbol, Purpose: purpose, Horizon: request.Horizon, CostPrice: holding.CostPrice, AnalysisLevel: request.ResearchLevel}, asOf, force, resumeID)
	if err != nil {
		return result, err
	}
	result.AnalysisID = job.ID
	result.ResearchOrigin = origin
	result.ResearchStartedAt = job.StartedAt
	started := time.Now()
	for job.Status == "queued" || job.Status == "running" {
		result.Status = job.Status
		if notify != nil {
			notify(result)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
		job, err = s.stockResearchStore.Get(ctx, job.ID)
		if err != nil {
			return result, err
		}
	}
	result.ResearchDurationMS = time.Since(started).Milliseconds()
	job = stockanalysis.RevalidateReusableResearch(job)
	result.ReportCompletedAt = stockanalysis.ResearchCompletedAt(job)
	result.Analysis = job.Analysis
	if job.Analysis != nil && job.Analysis.ResearchReport != nil {
		result.ResearchCutoffAt = job.Analysis.ResearchReport.CutoffAt
	}
	if !stockanalysis.SuccessfulResearch(job) {
		return result, fmt.Errorf("%s 个股AI研究未成功：%s；量化快照不算AI报告", holding.Symbol, firstPortfolioMessage(job.Error, job.Message))
	}
	result.Status = "succeeded"
	return result, nil
}
func firstPortfolioMessage(errorText, message string) string {
	if errorText != "" {
		return errorText
	}
	return message
}
func (s *Server) refreshPortfolioQuotes(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if s.realtimeProvider == nil {
		return nil, fmt.Errorf("行情服务不可用")
	}
	return s.realtimeProvider.Realtime(ctx, symbols)
}
