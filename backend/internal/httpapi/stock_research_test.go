package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/stockanalysis"
)

func TestResearchAPIHistorySnapshotAndReportBoundChat(t *testing.T) {
	gateway := &fakeHermesGateway{status: hermes.Status{Available: true, Configured: true}, promptFunc: func(_ context.Context, prompt string) (hermes.PromptResult, error) {
		if strings.Contains(prompt, "独立提出需要核实的问题") {
			return hermes.PromptResult{Content: `{"questions":[]}`}, nil
		}
		return hermes.PromptResult{Content: validHTTPResearchJSON}, nil
	}}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", HermesGateway: gateway})
	defer server.Close()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	created := call("POST", "/api/v1/stocks/research", `{"symbol":"600519","purpose":"holding","cost_price":20}`)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var response struct {
		Data stockanalysis.ResearchJob `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := server.stockResearch.Wait(ctx, response.Data.ID)
	if err != nil || job.Status != "succeeded" {
		t.Fatalf("job: %+v %v", job, err)
	}
	if job.Analysis.AnalysisID != job.ID || job.Analysis.ResearchReport.Request.CostPrice == nil || *job.Analysis.ResearchReport.Request.CostPrice != 20 {
		t.Fatal("request identity lost")
	}
	for _, route := range []string{"/api/v1/stocks/research", "/api/v1/stocks/research/" + job.ID, "/api/v1/stocks/research/" + job.ID + "/snapshot"} {
		if result := call("GET", route, ""); result.Code != 200 {
			t.Fatalf("read failed: %s", result.Body.String())
		}
	}
	frame := []byte(`{"method":"prompt.submit","params":{"text":"反证是什么？","analysis_id":"` + job.ID + `"}}`)
	enriched := string(server.enrichHermesPrompt(ctx, frame))
	if !strings.Contains(enriched, "完整研判完成") || !strings.Contains(enriched, "m-price") || !strings.Contains(enriched, "cutoff_at") {
		t.Fatal("chat was not bound to saved evidence")
	}
	var decoded struct {
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal([]byte(enriched), &decoded)
	if _, exists := decoded.Params["analysis_id"]; exists {
		t.Fatal("private metadata forwarded to Hermes")
	}
	if result := call("DELETE", "/api/v1/stocks/research/"+job.ID, ""); result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	missing := string(server.enrichHermesPrompt(ctx, frame))
	if !strings.Contains(missing, "未找到") {
		t.Fatal("missing report silently unbound")
	}
	if result := call("GET", "/api/v1/stocks/research/"+job.ID, ""); result.Code != 404 {
		t.Fatalf("deleted record: %d", result.Code)
	}
	if result := call("POST", "/api/v1/stocks/research", `{"symbol":"600519","tool":"shell"}`); result.Code != 400 {
		t.Fatal("unknown request fields accepted")
	}
}

func TestStockResearchRecordsTokenUsageByModule(t *testing.T) {
	gateway := &fakeHermesGateway{
		status: hermes.Status{Available: true, Configured: true},
		promptFunc: func(_ context.Context, prompt string) (hermes.PromptResult, error) {
			if strings.Contains(prompt, "独立提出需要核实的问题") {
				return hermes.PromptResult{Content: `{"questions":[]}`, Usage: hermes.TokenUsage{PromptTokens: 120, CompletionTokens: 30, TotalTokens: 150}}, nil
			}
			return hermes.PromptResult{Content: validHTTPResearchJSON, Usage: hermes.TokenUsage{PromptTokens: 400, CompletionTokens: 100, TotalTokens: 500}}, nil
		},
	}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", HermesGateway: gateway})
	defer server.Close()
	created := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/research", strings.NewReader(`{"symbol":"600519","purpose":"observe"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, created)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data stockanalysis.ResearchJob `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := server.stockResearch.Wait(ctx, payload.Data.ID)
	if err != nil || job.Status != "succeeded" {
		t.Fatalf("job: %+v %v", job, err)
	}
	if len(server.tokenUsage.Entries) == 0 || server.tokenUsage.Entries[0].Module != "stock-analysis" || server.tokenUsage.Entries[0].Total != 1150 {
		t.Fatalf("token usage = %+v", server.tokenUsage.Entries)
	}
}

func TestResearchChatWithoutStoreRemovesMetadataAndRefusesToInventReport(t *testing.T) {
	server := &Server{}
	data := server.enrichHermesPrompt(context.Background(), []byte(`{"method":"prompt.submit","params":{"text":"继续","analysis_id":"missing"}}`))
	var frame struct {
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal(data, &frame)
	if _, exists := frame.Params["analysis_id"]; exists || !strings.Contains(frame.Params["text"].(string), "不可用") {
		t.Fatal("missing-store fallback lost binding constraint")
	}
}

func TestResearchModelIdentityGuardPreservesOptions(t *testing.T) {
	consistent := true
	gateway := &fakeHermesGateway{promptFunc: func(context.Context, string) (hermes.PromptResult, error) {
		consistent = false
		return hermes.PromptResult{Content: `{"ok":true}`}, nil
	}}
	p := researchPrompter{prompter: gateway, consistent: func() bool { return consistent }}
	_, err := p.Prompt(context.Background(), "research")
	if err == nil || len(gateway.promptOptions) != 1 || !gateway.promptOptions[0].DisableTools {
		t.Fatal("model changed mid-call without guard")
	}
}

func TestResearchPromptStageUsesTaskNotSharedJSONFields(t *testing.T) {
	for _, tc := range []struct{ prompt, stage string }{
		{"你是A股证据研究员。任务是独立提出需要核实的问题。核心判断、交易条件", "outline"},
		{"你是A股快速研究员。核心判断 conditions", "quick"},
		{"你是A股证据研究员。只基于输入证据形成“核心判断”。不输出交易条件", "core"},
		{"你是A股交易条件整理器。核心判断 core_judgment conditions", "trade"},
		{"你是A股研究决策器。核心判断 conditions\n[结构修复要求]", "repair"},
	} {
		if got := researchPromptStage(tc.prompt); got != tc.stage {
			t.Errorf("stage = %q, want %q", got, tc.stage)
		}
	}
}
