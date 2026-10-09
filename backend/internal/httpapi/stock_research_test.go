package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

type researchBodyProvider struct {
	*fakeMarketOverviewProvider
	bodyCalls, listCalls int
	requestedID          string
	body                 string
}

func (p *researchBodyProvider) MarketAnnouncementContent(_ context.Context, id string) (string, error) {
	p.bodyCalls++
	p.requestedID = id
	return p.body, nil
}

func (p *researchBodyProvider) MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	p.listCalls++
	return nil, foundation.SourceMeta{}, nil
}

func TestResearchSourceReadsKnownBodyAndUpgradesLegacyTitleByID(t *testing.T) {
	captured := time.Now().UTC()
	provider := &researchBodyProvider{body: strings.Repeat("公告正文。", 450) + "受让方测试基金，转让比例8.5%。"}
	server := &Server{marketOverview: provider}
	full := stockanalysis.NewResearchSource("announcement", "转让公告", provider.body, "fixture", "https://data.eastmoney.com/notices/detail/002074/AN202609300001.html", captured, captured)
	snapshot := stockanalysis.ResearchSnapshot{Symbol: "002074.SZ", Sources: []stockanalysis.ResearchSource{full}}
	items, err := server.supplementStockResearch(context.Background(), snapshot, stockanalysis.ResearchQuestion{Tool: "source", SourceID: full.ID})
	if err != nil || len(items) != 1 || items[0].ID != full.ID || provider.bodyCalls != 0 || provider.listCalls != 0 {
		t.Fatalf("known body unnecessarily searched or lost: %v %+v", err, items)
	}
	titleOnly := stockanalysis.NewResearchSource("announcement", full.Title, full.Title, "fixture", full.URL, captured, captured)
	snapshot.Sources = []stockanalysis.ResearchSource{titleOnly}
	items, err = server.supplementStockResearch(context.Background(), snapshot, stockanalysis.ResearchQuestion{Tool: "source", SourceID: titleOnly.ID})
	if err != nil || len(items) != 1 || provider.bodyCalls != 1 || provider.listCalls != 0 || provider.requestedID != "AN202609300001" || !strings.Contains(items[0].Content, "转让比例8.5%") {
		t.Fatalf("legacy source did not read body directly: %v %+v", err, items)
	}
	provider.body = ""
	if _, err = server.supplementStockResearch(context.Background(), snapshot, stockanalysis.ResearchQuestion{Tool: "source", SourceID: titleOnly.ID}); err == nil {
		t.Fatal("empty announcement body silently counted as evidence")
	}
}

func TestResearchSupplementSearchUsesBodiesAlreadyInSnapshot(t *testing.T) {
	provider := &researchBodyProvider{}
	server := &Server{marketOverview: provider}
	source := stockanalysis.NewResearchSource("announcement", "2026中报", "非经常性损益来自资产处置收益。", "fixture", "", time.Time{}, time.Now())
	snapshot := stockanalysis.ResearchSnapshot{Symbol: "002074.SZ", Sources: []stockanalysis.ResearchSource{source}}
	items, err := server.supplementStockResearch(context.Background(), snapshot, stockanalysis.ResearchQuestion{Tool: "announcements", Query: "2026 半年度报告 非经常性损益"})
	if err != nil || len(items) != 1 || items[0].ID != source.ID || researchSearchQuery("2026 半年度报告 非经常性损益") != "半年度报告" {
		t.Fatalf("compound keyword query overlooked existing body: %v %+v", err, items)
	}
}

func TestResearchAnnouncementSearchDoesNotPrioritizeEveryListedCompanyNotice(t *testing.T) {
	server := &Server{marketOverview: &researchBodyProvider{}}
	makeSource := func(title, body string) stockanalysis.ResearchSource {
		return stockanalysis.NewResearchSource("announcement", title, body, "fixture", "", time.Time{}, time.Now())
	}
	h := makeSource("H股备案", "公司H股发行尚需取得批准。")
	ir := makeSource("投资者关系活动记录", "产品尚在客户验证，另介绍H股发行进展。")
	unrelated := makeSource("股权激励", "上市公司拟发行限制性股票。")
	snapshot := stockanalysis.ResearchSnapshot{Symbol: "688099.SH", Name: "晶晨股份", Sources: []stockanalysis.ResearchSource{h, ir, unrelated}}
	items, err := server.supplementStockResearch(context.Background(), snapshot, stockanalysis.ResearchQuestion{Tool: "announcements", Query: "晶晨股份 H股 发行 上市 进展"})
	if err != nil || len(items) != 2 || items[0].ID != h.ID || items[1].ID != ir.ID {
		t.Fatalf("broad words matched an unrelated notice: %+v, err=%v", items, err)
	}
}

type researchFinancialProvider struct {
	stockAnalysisBusiness
	history     []foundation.StockFundamentals
	err         error
	latestCalls int
}

type missingFinancialProvider struct{ stockAnalysisBusiness }

func (missingFinancialProvider) StockFinancialHistory(context.Context, string, int) ([]foundation.StockFundamentals, error) {
	return nil, errors.New("primary unavailable")
}
func (missingFinancialProvider) StockFundamentals(context.Context, string) (foundation.StockFundamentals, error) {
	return foundation.StockFundamentals{}, errors.New("primary unavailable")
}

type financialSupplementFixture struct {
	items []foundation.StockFinancialEvidence
	err   error
}

func (p financialSupplementFixture) StockFinancialEvidence(context.Context, string, int) ([]foundation.StockFinancialEvidence, error) {
	return p.items, p.err
}

func TestResearchCollectsSecondFinancialSourceAndFallsBackWithoutInventingValues(t *testing.T) {
	for _, missing := range []bool{false, true} {
		primary := StockBusinessProfileProvider(&researchFinancialProvider{history: []foundation.StockFundamentals{{Symbol: "600519.SH", ReportDate: "2026-06-30", Revenue: 1000000000, NetProfit: 100000000, Meta: foundation.SourceMeta{Source: "eastmoney:f10-financials"}}}})
		if missing {
			primary = missingFinancialProvider{}
		}
		secondary := financialSupplementFixture{items: []foundation.StockFinancialEvidence{{Symbol: "600519.SH", ReportDate: "2026-06-30", Fields: map[string]float64{"revenue": 1000000000, "net_profit": 100000000}, Meta: foundation.SourceMeta{Source: "sina:financial-indicators"}}}}
		server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, StockBusiness: primary, StockFinancialSupplement: secondary, MarketOverview: &fakeMarketOverviewProvider{}, ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, ReviewDBPath: ":memory:"})
		_, snapshot, err := server.collectStockResearch(context.Background(), "600519.SH")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, source := range snapshot.Sources {
			if source.ID != "f-financial" {
				continue
			}
			found = true
			var value struct {
				Data       map[string]any                      `json:"data"`
				Supplement []foundation.StockFinancialEvidence `json:"supplemental_history"`
				Checks     []map[string]any                    `json:"cross_checks"`
			}
			if json.Unmarshal([]byte(source.Content), &value) != nil || len(value.Supplement) != 1 {
				t.Fatal("second source not included")
			}
			if missing {
				if source.Provider != "sina:financial-indicators" || value.Data["revenue"] != float64(1000000000) {
					t.Fatal("source failure lost financial fallback")
				}
				if _, ok := value.Data["operating_cash_flow_per_share"]; ok {
					t.Fatal("missing cash flow invented as zero")
				}
			} else if len(value.Checks) != 1 {
				t.Fatal("matching-period cross-check skipped")
			}
		}
		if !found {
			t.Fatal("no financial evidence")
		}
	}
}

func (p *researchFinancialProvider) StockFinancialHistory(_ context.Context, _ string, limit int) ([]foundation.StockFundamentals, error) {
	if limit != 8 {
		return nil, errors.New("unexpected financial history limit")
	}
	return p.history, p.err
}

func (p *researchFinancialProvider) StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error) {
	p.latestCalls++
	return p.stockAnalysisBusiness.StockFundamentals(ctx, symbol)
}

func TestStockResearchCollectsFinancialHistoryAndFallsBackOnFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		provider := &researchFinancialProvider{history: []foundation.StockFundamentals{{ReportDate: "2026-06-30", Revenue: 260}, {ReportDate: "2026-03-31", Revenue: 100}}}
		if failed {
			provider.err = errors.New("financial history unavailable")
		}
		server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, LimitUp: stockAnalysisLimitUps{}, StockConcept: stockAnalysisCatalog{}, StockBusiness: provider, MarketOverview: &fakeMarketOverviewProvider{}, ThemeOverview: stockAnalysisThemes{}, News: stockAnalysisNews{}, ReviewDBPath: ":memory:"})
		_, snapshot, err := server.collectStockResearch(context.Background(), "600519.SH")
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range snapshot.Sources {
			if source.ID != "f-financial" {
				continue
			}
			var payload struct {
				History []foundation.StockFundamentals `json:"history"`
			}
			if err := json.Unmarshal([]byte(source.Content), &payload); err != nil {
				t.Fatal(err)
			}
			if !failed && (len(payload.History) != 2 || provider.latestCalls != 0) {
				t.Fatal("collection ignored available financial history")
			}
			if failed && (len(payload.History) != 1 || provider.latestCalls != 1 || !strings.Contains(strings.Join(snapshot.Limitations, " "), "多期财务资料不可用")) {
				t.Fatal("history failure did not preserve the latest-period fallback and gap")
			}
		}
	}
}

func TestResearchAPIHistorySnapshotAndReportBoundChat(t *testing.T) {
	gateway := &fakeAgentGateway{status: agent.Status{Available: true, Configured: true}, promptFunc: func(_ context.Context, prompt string) (agent.PromptResult, error) {
		if strings.Contains(prompt, "独立提出需要核实的问题") {
			return agent.PromptResult{Content: `{"questions":[]}`}, nil
		}
		return agent.PromptResult{Content: validHTTPResearchJSON}, nil
	}}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", AgentGateway: gateway})
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
	// Allow race instrumentation of the quantitative fixture without changing production timeouts.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	gateway := &fakeAgentGateway{
		status: agent.Status{Available: true, Configured: true},
		promptFunc: func(_ context.Context, prompt string) (agent.PromptResult, error) {
			if strings.Contains(prompt, "独立提出需要核实的问题") {
				return agent.PromptResult{Content: `{"questions":[]}`, Usage: agent.TokenUsage{PromptTokens: 120, CompletionTokens: 30, TotalTokens: 150}}, nil
			}
			return agent.PromptResult{Content: validHTTPResearchJSON, Usage: agent.TokenUsage{PromptTokens: 400, CompletionTokens: 100, TotalTokens: 500}}, nil
		},
	}
	server := NewServer(Config{Realtime: stockAnalysisRealtime{}, KLinePrimary: stockAnalysisKLines{}, KLineFallback: stockAnalysisKLines{}, StockBusiness: stockAnalysisBusiness{}, MarketOverview: &fakeMarketOverviewProvider{}, ReviewDBPath: ":memory:", AgentGateway: gateway})
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
	// Allow race instrumentation of the quantitative fixture without changing production timeouts.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	gateway := &fakeAgentGateway{promptFunc: func(context.Context, string) (agent.PromptResult, error) {
		consistent = false
		return agent.PromptResult{Content: `{"ok":true}`}, nil
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
