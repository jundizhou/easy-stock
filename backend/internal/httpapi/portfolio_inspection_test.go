package httpapi

import (
	"context"
	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validPortfolioScoreJSON = `{"risk_level":"高","risk_reason":"集中持仓需要管理","style_match":"部分偏离","executive_summary":"逻辑仍需确认，组合集中风险较高。","confidence_level":"中","confidence_reason":"证据有限，原研究周期不同","dimensions":[{"key":"holding_logic","score":70,"reason":"有限证据","evidence_refs":[{"report_id":"reuse-http","source_id":"s1"}]},{"key":"portfolio_structure","score":71,"reason":"集中风险","evidence_refs":[{"fact":"equity_max_single_percent"}]},{"key":"risk_capacity","score":72,"reason":"退出条件需确认","evidence_refs":[{"fact":"stop_loss_coverage_percent"}]},{"key":"strategy_fit","score":73,"reason":"持有周期差异","evidence_refs":[{"fact":"profile.scoring_description"}]}],"holdings":[{"symbol":"600519.SH","portfolio_role":"观察","conclusion":"持有逻辑待验证","action_priority":"观察","action":"等待验证","confirmation":"趋势延续","invalidation":"趋势破坏"}],"scenarios":[{"name":"震荡分化","condition":"趋势走弱","portfolio_action":"复核持有逻辑"}],"adjustment_order":["先核实趋势"],"primary_risks":[],"concentration_findings":[],"next_checklist":[],"data_limitations":[]}`

func seedPortfolioResearch(t *testing.T, s *Server) {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	a := stockanalysis.Analysis{Symbol: "600519.SH", Name: "贵州茅台", AnalysisID: "reuse-http", AI: stockanalysis.AISynthesisStatus{Status: "ready"}, ResearchReport: &stockanalysis.ResearchReport{ResearchSynthesis: stockanalysis.ResearchSynthesis{Headline: "测试研究", Thesis: stockanalysis.ResearchClaim{Text: "有限逻辑", SourceIDs: []string{"s1"}}}, Validation: "references_checked", Sources: []stockanalysis.ResearchSource{{ID: "s1", Title: "测试来源"}}, GeneratedAt: now, CutoffAt: now, Request: stockanalysis.ResearchRequest{Symbol: "600519.SH", Purpose: "observe", Horizon: "short", AnalysisLevel: "quick"}}}
	if err := s.stockResearchStore.Save(context.Background(), stockanalysis.ResearchJob{ID: a.AnalysisID, Status: "succeeded", Request: a.ResearchReport.Request, CompletedAt: &now, UpdatedAt: now, Analysis: &a}); err != nil {
		t.Fatal(err)
	}
}
func httpPortfolioRequest(t *testing.T, s *Server, method, path, body string) portfolioinspection.Job {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	res := httptest.NewRecorder()
	s.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted && res.Code != http.StatusOK {
		t.Fatalf("status=%d %s", res.Code, res.Body.String())
	}
	var payload struct {
		Data portfolioinspection.Job `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}
func awaitPortfolioHTTP(t *testing.T, s *Server, id string) portfolioinspection.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j := httpPortfolioRequest(t, s, http.MethodGet, "/api/v1/portfolio-inspections/"+id, "")
		if j.Status != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("did not finish")
	return portfolioinspection.Job{}
}
func TestPortfolioInspectionReusesReportsAndReturnsIndependentAIScore(t *testing.T) {
	var calls atomic.Int32
	gateway := &fakeAgentGateway{status: agent.Status{Available: true, Configured: true}}
	gateway.promptFunc = func(_ context.Context, prompt string) (agent.PromptResult, error) {
		calls.Add(1)
		if !strings.Contains(prompt, "组合AI评估器") {
			t.Error("stock analysis unnecessarily ran")
		}
		return agent.PromptResult{Content: validPortfolioScoreJSON}, nil
	}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:", SettingsPath: "", AgentGateway: gateway})
	t.Cleanup(func() { server.Close() })
	seedPortfolioResearch(t, server)
	request := `{"trader_profile":"balanced","horizon":"medium","holdings":[{"symbol":"600519","weight_percent":60,"cost_price":1000}]}`
	job := httpPortfolioRequest(t, server, http.MethodPost, "/api/v1/portfolio-inspections", request)
	done := awaitPortfolioHTTP(t, server, job.ID)
	if done.Status != "succeeded" {
		t.Fatalf("job %+v", done)
	}
	if len(gateway.promptOptions) != 1 || !gateway.promptOptions[0].DisableTools || !gateway.promptOptions[0].Sandbox || gateway.promptOptions[0].MaxAttempts != 1 {
		t.Fatal("组合汇总未禁用工具或运行时重试")
	}
	r := done.Report
	if r.AlgorithmVersion != "portfolio-ai-score-v4" || r.Metrics.StopLossCoveragePercent != 0 || !r.Conclusion.ScoreAvailable || *r.Conclusion.TotalScore != 71 || r.Conclusion.RiskLevel != "高" || r.Holdings[0].ResearchOrigin != "reused" || calls.Load() != 1 {
		t.Fatalf("incorrect report %+v calls=%d", r, calls.Load())
	}
	// Changing holdings inputs only generates a new portfolio conclusion, not stock research.
	request = `{"trader_profile":"steady","horizon":"swing","holdings":[{"symbol":"600519","weight_percent":40,"cost_price":900}]}`
	job = httpPortfolioRequest(t, server, http.MethodPost, "/api/v1/portfolio-inspections", request)
	done = awaitPortfolioHTTP(t, server, job.ID)
	if done.Status != "succeeded" || calls.Load() != 2 || *done.Report.Holdings[0].Holding.CostPrice != 900 || done.Report.Request.Horizon != "swing" {
		t.Fatalf("changed request %+v", done)
	}
	original, err := server.stockResearchStore.Get(context.Background(), "reuse-http")
	if err != nil || original.Request.Purpose != "observe" || original.Request.CostPrice != nil {
		t.Fatal("cached report overwritten")
	}
}
func TestPortfolioInspectionRejectsOverAllocation(t *testing.T) {
	server := NewServer(Config{ReviewDBPath: ":memory:", PortfolioDBPath: ":memory:", SettingsPath: "", AgentGateway: &fakeAgentGateway{status: agent.Status{Available: true, Configured: true}}})
	t.Cleanup(func() { server.Close() })
	req := httptest.NewRequest(http.MethodPost, "/api/v1/portfolio-inspections", strings.NewReader(`{"trader_profile":"balanced","holdings":[{"symbol":"600519","weight_percent":60},{"symbol":"000858","weight_percent":50}]}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", res.Code)
	}
}
