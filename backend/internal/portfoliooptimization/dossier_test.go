package portfoliooptimization

import (
	"easy-stock/backend/internal/etfanalysis"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDossierDoesNotGrowWithUnrelatedFullReportFields(t *testing.T) {
	j := fixtureJob()
	a, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range j.Results {
		r.Analysis.ResearchReport.Sources[0].Content = strings.Repeat("raw_provider_data", 100000)
		r.Analysis.ResearchReport.Sources[0].URL = "https://example.test/" + strings.Repeat("large", 10000)
		r.Analysis.ResearchReport.Scenarios = []stockanalysis.ResearchScenario{{Description: strings.Repeat("full scenario", 10000)}}
	}
	b, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(b) > MaxModelPromptBytes {
		t.Fatal("full report leaked across model DTO boundary", len(a), len(b))
	}
}

func TestNewsProvenanceSurvivesCompactAllocationAndReview(t *testing.T) {
	j := fixtureJob()
	for _, result := range j.Results {
		source := &result.Analysis.ResearchReport.Sources[0]
		source.Kind, source.ContentStatus = "news", "excerpt"
		source.Provider, source.URL = "eastmoney:stock-news-search:证券时报", "https://finance.example.com/forecast"
		source.PublishedAt = time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
		source.Content = "公司发布前三季度业绩预告，预计归母净利润12.6至13.1亿元。"
	}
	before, _ := json.Marshal(j)
	for _, detail := range []int{60, 0} {
		dossier := commonDossier(j, j.Results, detail)
		columns := dossier["stock_columns"].([]string)
		for _, raw := range dossier["stocks"].([]any) {
			row := raw.([]any)
			for i, column := range columns {
				if column != "sources" {
					continue
				}
				source := row[i].([]any)[0].([]any)
				news := source[3].(dossierNews)
				if !news.Traceable || news.Provider != "eastmoney:stock-news-search:证券时报" || !strings.Contains(news.Excerpt, "12.6至13.1") || news.ContentStatus != "excerpt" {
					t.Fatal("compact dossier discarded reporting provenance", news)
				}
				encoded, err := json.Marshal(news)
				var values []json.RawMessage
				if err != nil || json.Unmarshal(encoded, &values) != nil || len(values) != len(newsDossierColumns) {
					t.Fatal("provenance does not match shared columns", string(encoded), err)
				}
				restored := map[string]json.RawMessage{}
				for n, key := range newsDossierColumns {
					restored[key] = values[n]
				}
				encoded, _ = json.Marshal(restored)
				var original stockanalysis.NewsEvidenceContext
				if json.Unmarshal(encoded, &original) != nil || !reflect.DeepEqual(original, *news.NewsEvidenceContext) {
					t.Fatal("provenance table changed source fields")
				}
			}
		}
	}
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	p := fixtureProposal()
	j.Proposal = &p
	plan := Plan{Original: pi.OptimizationReport(j.Source.Request, j.Results), Proposed: pi.OptimizationReport(j.Source.Request, j.Results)}
	review, err := pairedPrompt(j, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{prompt, review} {
		if !strings.Contains(text, stockanalysis.NewsEvidencePolicy) || !strings.Contains(text, `"news_context"`) || !strings.Contains(text, "证券时报") || !strings.Contains(text, "12.6至13.1") {
			t.Fatal("allocation/review lost news semantics or provenance")
		}
	}
	j.Proposal = nil
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("dossier changed historical research")
	}
}

func TestUncitedEarningsNoticeSurvivesCompactDossier(t *testing.T) {
	j := fixtureJob()
	rr := j.Results[0].Analysis.ResearchReport
	rr.CutoffAt = time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	notice := stockanalysis.NewResearchSource("announcement", "前三季度业绩预告", "报告期1月1日至9月30日，预计归母净利润12.6至13.1亿元，未经审计；未发现重大不确定因素。", "exchange", "https://example.com/notice", rr.CutoffAt.Add(-time.Hour), rr.CutoffAt)
	rr.Sources = append(rr.Sources, notice)
	rr.Counter = []stockanalysis.ResearchClaim{{Text: "预告待兑现且未取得原文", SourceIDs: []string{"s1"}}}
	before, _ := json.Marshal(j)
	for _, detail := range []int{60, 0} {
		dossier := commonDossier(j, j.Results, detail)
		encoded, _ := json.Marshal(dossier)
		if !strings.Contains(string(encoded), notice.Content) || !strings.Contains(string(encoded), notice.ID) {
			t.Fatal("uncited earnings body was lost in allocation dossier")
		}
		// Both the excerpt and citation dictionary retain the notice ID.
		if strings.Count(string(encoded), notice.ID) != 2 {
			t.Fatal("earnings citation not resolvable")
		}
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("old research was modified")
	}
}

func TestStockFactTablePreservesCanonicalFactsAndAvailability(t *testing.T) {
	j := fixtureJob()
	facts := map[string]pi.Fact{
		"600519.SH.financial.net_profit":  {Available: true, Value: 0.0},
		"600519.SH.financial.revenue_yoy": {Available: true, Value: -20.0},
		"000858.SZ.financial.net_profit":  {Available: true, Value: 80.0},
		"000858.SZ.financial.revenue_yoy": {Available: false},
		"correlation.600519.SH.000858.SZ": {Available: true, Value: .6},
		"600519.SH.condition.test":        {Available: true, Value: true},
		"600519.SH.weight_percent":        {Available: true, Value: 60},
	}
	table := stockFactTable(facts, j.Results)
	columns := table["columns"].([]string)
	known, unknown := map[string]any{}, map[string]bool{}
	for _, item := range table["rows"].([]any) {
		row := item.(map[string]any)
		unavailable := map[int]bool{}
		for _, i := range row["unavailable"].([]int) {
			unavailable[i] = true
		}
		for i, value := range row["values"].([]any) {
			key := row["symbol"].(string) + "." + columns[i]
			if unavailable[i] {
				unknown[key] = true
			} else {
				known[key] = value
			}
		}
	}
	want := map[string]any{"600519.SH.financial.net_profit": 0.0, "600519.SH.financial.revenue_yoy": -20.0, "000858.SZ.financial.net_profit": 80.0}
	if !reflect.DeepEqual(known, want) || !unknown["000858.SZ.financial.revenue_yoy"] || len(unknown) != 1 {
		t.Fatal("lost, fabricated or changed fact", known, unknown)
	}
	cross := table["cross"].(map[string]any)["available"].(map[string]any)
	if !reflect.DeepEqual(cross, map[string]any{"correlation.600519.SH.000858.SZ": .6}) {
		t.Fatal("cross-stock fact changed", cross)
	}
}

func TestSourceDictionaryReconstructsExactCitationIdentity(t *testing.T) {
	j := fixtureJob()
	for i, r := range j.Results {
		src := &r.Analysis.ResearchReport.Sources[0]
		src.Kind = []string{"financial", "quote"}[i]
		src.TimeStatus = []string{"dated", "quote_reference_only"}[i]
	}
	before, _ := json.Marshal(j)
	d := commonDossier(j, j.Results, 60)
	kinds, times := d["source_kinds"].([]string), d["source_times"].([]string)
	for i, raw := range d["stocks"].([]any) {
		row := raw.([]any)
		index := 0
		for n, name := range stockDossierColumns {
			if name == "sources" {
				index = n
			}
		}
		sources := row[index].([]any)
		if len(sources) != 1 {
			t.Fatal("citations lost", sources)
		}
		s := sources[0].([]any)
		original := j.Results[i].Analysis.ResearchReport.Sources[0]
		if s[0] != original.ID || kinds[s[1].(int)] != original.Kind || times[s[2].(int)] != original.TimeStatus {
			t.Fatal("source dictionary changed meaning", s)
		}
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("dictionary encoding changed persisted studies")
	}
}

func TestAllocationInputKeepsCounterfactsWithoutHistoricalQualityGates(t *testing.T) {
	j := fixtureJob()
	for _, r := range j.Results {
		rr := r.Analysis.ResearchReport
		rr.EvidenceLevel, rr.Decision.Status = "insufficient", "no_plan"
		rr.Counter = []stockanalysis.ResearchClaim{{Text: "经营亏损尚未解决", SourceIDs: []string{"s1"}}}
		rr.Decision.Reason = "量能买点仍需观察"
	}
	prompt, err := proposalPrompt(j)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Split(prompt, "[资料JSON]\n")[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload["stock_columns"]), `"evidence_level"`) || strings.Contains(string(payload["stock_columns"]), `"decision"`) || !strings.Contains(string(payload["stocks"]), "经营亏损尚未解决") || !strings.Contains(string(payload["stocks"]), "量能买点仍需观察") || len(payload["initial_allocation_frontier"]) == 0 {
		t.Fatal("historical gates or missing counterfacts", string(payload["stocks"]))
	}
}

func TestProposalRangeMustCoverFixedStockPosition(t *testing.T) {
	j, p := fixtureJob(), fixtureProposal()
	p.Alternatives[0].Allocations[1].Minimum = 20
	p.Alternatives[0].Allocations[1].Maximum = 34
	p.Alternatives[0].Allocations[1].Preferred = 34
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "权重范围不可行") {
		t.Fatal("underfunded plan was not identified before solving", err)
	}
	p = fixtureProposal()
	p.Alternatives[0].Allocations[1].Minimum = 36
	p.Alternatives[0].Allocations[1].Maximum = 40
	p.Alternatives[0].Allocations[1].Preferred = 36
	if err := validateProposal(j, p); err == nil || !strings.Contains(err.Error(), "权重范围不可行") {
		t.Fatal("overfunded plan was not identified before solving", err)
	}
}

func TestComparisonSendsStockFactsOnceAndNoRejectedCandidates(t *testing.T) {
	j := fixtureJob()
	p := fixtureProposal()
	j.Proposal = &p
	before := j.Source
	req := before.Request
	req.Holdings = holds(45, 35)
	// The report factory is needed to recalculate actual B metrics.
	plan := Plan{Original: before, Proposed: pi.OptimizationReport(req, j.Results)}
	prompt, err := pairedPrompt(j, plan)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Split(prompt, "[资料JSON]\n")[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt, "可核验逻辑") != 2 || len(payload["stock_facts"]) == 0 || strings.Contains(string(payload["a"]), "condition.") || strings.Contains(prompt, "funding_reason") {
		t.Fatal("repeated facts or optimizer rationale leaked to reviewer")
	}
}

func TestCompactDossierPreservesCounterFactsAndCorrelationIdentity(t *testing.T) {
	j := fixtureJob()
	for _, r := range j.Results {
		rr := r.Analysis.ResearchReport
		rr.Counter = []stockanalysis.ResearchClaim{{Text: "经营仍亏损", SourceIDs: []string{"s1"}}, {Text: "增长存在反转风险", SourceIDs: []string{"s1"}}}
		rr.Decision.Blockers = []string{"待核验主营恢复"}
	}
	before, _ := json.Marshal(j)
	for _, r := range j.Results {
		d := compactStockDossier(r)
		cs := d["counter"].([]dossierClaim)
		if len(cs) != 2 || cs[0].Text != "经营仍亏损" || cs[1].Text != "增长存在反转风险" || len(cs[0].Sources) != 1 {
			t.Fatal("counter evidence lost", d)
		}
		if !reflect.DeepEqual(d["prior_blockers"], r.Analysis.ResearchReport.Decision.Blockers) {
			t.Fatal("blockers lost")
		}
		if d["anchors"] != nil {
			t.Fatal("capacity card must not imply numerical price targets")
		}
	}
	facts := map[string]pi.Fact{
		"600519.SH.financial.net_profit":  {Available: true, Value: -100.0},
		"000858.SZ.financial.net_profit":  {Available: false},
		"correlation.600519.SH.000858.SZ": {Available: true, Value: 0.0},
		"correlation.000858.SZ.600519.SH": {Available: false},
	}
	table := stockFactTable(facts, j.Results)
	rowsBefore, _ := json.Marshal(table["rows"])
	compactCorrelationTable(table, j.Results)
	rowsAfter, _ := json.Marshal(table["rows"])
	if string(rowsBefore) != string(rowsAfter) {
		t.Fatal("stock numeric facts or availability changed")
	}
	symbols := table["correlation_symbols"].([]string)
	seen := map[string]bool{}
	for _, raw := range table["correlations"].([]any) {
		row := raw.([]any)
		key := "correlation." + symbols[row[0].(int)] + "." + symbols[row[1].(int)]
		seen[key] = true
		f := facts[key]
		if f.Available && row[2] != f.Value || !f.Available && row[2] != nil {
			t.Fatal("zero confused with unknown", row)
		}
	}
	if len(seen) != 2 {
		t.Fatal("correlation missing")
	}
	after, _ := json.Marshal(j)
	if string(before) != string(after) {
		t.Fatal("mutated full research")
	}
}

func TestCompactStockFactsPreserveSharedUnknownAndZero(t *testing.T) {
	j := fixtureJob()
	seed := j.Results[0]
	j.Results = nil
	facts := map[string]pi.Fact{}
	for _, symbol := range []string{"600001.SH", "600002.SH", "600003.SH", "600004.SH"} {
		r := seed
		r.Holding.Symbol = symbol
		j.Results = append(j.Results, r)
		facts[symbol+".valuation.trade_date"] = pi.Fact{Value: "2026-09-30", Available: true}
		facts[symbol+".financial.revenue_yoy"] = pi.Fact{Value: 0.0, Available: true}
		facts[symbol+".financial.eps"] = pi.Fact{Available: false}
		facts[symbol+".price"] = pi.Fact{Value: symbol, Available: true}
	}
	table := stockFactTable(facts, j.Results)
	compactSharedStockFacts(table)
	shared := table["shared"].(map[string]any)
	if shared["financial.revenue_yoy"] != 0.0 || shared["financial.eps"] != nil || shared["valuation.trade_date"] != "2026-09-30" {
		t.Fatal(shared)
	}
	for _, item := range table["rows"].([]any) {
		row := item.(map[string]any)
		symbol := row["symbol"].(string)
		rebuilt := map[string]any{}
		for key, v := range shared {
			rebuilt[symbol+"."+key] = v
		}
		for i, key := range table["columns"].([]string) {
			rebuilt[symbol+"."+key] = row["values"].([]any)[i]
		}
		for key, v := range rebuilt {
			f := facts[key]
			if !reflect.DeepEqual(f.Value, v) {
				t.Fatal("fact value changed", key, f, v)
			}
		}
	}
}

// etfCapacityFixture builds an ETF holding with the bulky evidence a real
// research report carries: six direction news items, ten technical metrics,
// disclosed holdings and five research judgments.
func etfCapacityFixture(symbol string) pi.HoldingResult {
	at := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	value := 1.0
	news := []etfanalysis.DirectionNews{}
	for i := 0; i < 6; i++ {
		news = append(news, etfanalysis.DirectionNews{
			SourceID:     "e-news",
			Item:         foundation.NewsItem{ID: fmt.Sprint(i), Title: strings.Repeat("方向消息标题", 8), Content: strings.Repeat("方向消息正文", 40)},
			MatchedTerms: []string{"主题", "行业"},
			Relevance:    "high",
		})
	}
	metrics := []etfanalysis.Metric{}
	for i := 0; i < 10; i++ {
		metrics = append(metrics, etfanalysis.Metric{Label: fmt.Sprintf("MA%d", i), Value: &value, Unit: "%", AsOf: "2026-11-06", Method: strings.Repeat("口径说明", 20)})
	}
	holdings := []foundation.ETFHoldings{}
	for i := 0; i < 3; i++ {
		items := []foundation.ETFHolding{}
		for n := 0; n < 10; n++ {
			items = append(items, foundation.ETFHolding{Code: fmt.Sprintf("6000%02d", n), Symbol: fmt.Sprintf("6000%02d.SH", n), Name: strings.Repeat("成分股", 4), WeightPercent: float64(10 - n)})
		}
		holdings = append(holdings, foundation.ETFHoldings{Symbol: symbol, AssetType: "equity", ReportDate: fmt.Sprintf("2026-0%d-30", i+1), Scope: strings.Repeat("部分持仓口径", 4), Items: items})
	}
	data := etfanalysis.Analysis{
		Profile: foundation.ETFProfile{Instrument: foundation.Instrument{Symbol: symbol}, AssetClass: "equity", TrackingTarget: strings.Repeat("跟踪标的", 8)},
		Direction: &etfanalysis.DirectionAnalysis{
			Classification: etfanalysis.DirectionClassification{
				Primary:       etfanalysis.DirectionTag{Key: "tech", Label: "科技"},
				Tags:          []etfanalysis.DirectionTag{{Key: "growth", Label: "成长"}},
				Focus:         strings.Repeat("方向焦点", 8),
				Basis:         strings.Repeat("披露依据", 20),
				Status:        "unclassified",
				Queries:       []string{strings.Repeat("查询", 20)},
				MatchTerms:    []string{strings.Repeat("匹配", 20)},
				ResearchFocus: []string{strings.Repeat("研究焦点", 20)},
			},
			Technical: &etfanalysis.DirectionTechnical{Symbol: symbol, Name: symbol, Relationship: "etf_price_proxy", AsOf: "2026-11-06", Trend: "mixed", Metrics: metrics, Limitations: []string{strings.Repeat("技术限制", 20)}},
			News:      news,
			NewsCoverage: etfanalysis.DirectionNewsCoverage{
				Status: "covered", Count: 6, Queries: []string{strings.Repeat("检索词", 20)}, FailedQueries: []string{strings.Repeat("失败检索", 20)},
			},
			Limitations: []string{strings.Repeat("方向限制", 20)},
		},
		Holdings:          holdings,
		Limitations:       []string{strings.Repeat("基金限制", 40)},
		ExecutionBlockers: []string{strings.Repeat("执行阻断", 40)},
	}
	judgment := func() etfanalysis.Judgment {
		return etfanalysis.Judgment{Status: "mixed", Text: strings.Repeat("研究方向判断", 25), EvidenceLevel: "sufficient", SourceIDs: []string{"e-news", "e-extra"}}
	}
	judgments := &etfanalysis.Judgments{Technical: &etfanalysis.Judgment{Status: "mixed", Text: strings.Repeat("技术判断", 25), EvidenceLevel: "sufficient", SourceIDs: []string{"e-missing", "e-news"}}, News: &etfanalysis.Judgment{Status: "supported", Text: strings.Repeat("消息判断", 25), EvidenceLevel: "limited", SourceIDs: []string{"e-news"}}, Underlying: judgment(), Product: judgment(), Entry: judgment()}
	rr := &stockanalysis.ResearchReport{
		ResearchSynthesis: stockanalysis.ResearchSynthesis{Headline: "测试逻辑", Thesis: stockanalysis.ResearchClaim{Text: "ETF研究逻辑", SourceIDs: []string{"e-profile"}}, EvidenceLevel: "sufficient", ETFJudgments: judgments},
		Sources:           []stockanalysis.ResearchSource{{ID: "e-profile", Kind: "fund_profile", Content: "冻结基金档案"}, {ID: "e-news", Kind: "news", ContentStatus: "excerpt", Content: strings.Repeat("新闻正文", 40)}},
		Request:           stockanalysis.ResearchRequest{Symbol: symbol, Horizon: "swing"},
		GeneratedAt:       at, CutoffAt: at, Validation: "references_checked",
	}
	return pi.HoldingResult{
		Holding:           pi.Holding{Symbol: symbol, Name: symbol, Weight: 10},
		Status:            "succeeded",
		AnalysisID:        "report-" + symbol,
		ResearchCutoffAt:  at,
		ReportCompletedAt: at,
		Analysis:          &stockanalysis.Analysis{InstrumentType: "etf", Symbol: symbol, ETF: &data, AI: stockanalysis.AISynthesisStatus{Status: "ready"}, ResearchReport: rr},
	}
}

func TestCompactETFEvidenceBoundsDirectionAndJudgments(t *testing.T) {
	result := etfCapacityFixture("510300.SH")
	dossier := compactStockDossier(result)
	encoded, err := json.Marshal(dossier["etf"])
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 1500 {
		t.Fatalf("capacity ETF card still %d bytes", len(encoded))
	}
	etf := dossier["etf"].(map[string]any)
	direction := etf["direction"].(map[string]any)
	if _, ok := direction["news"]; ok {
		t.Fatal("direction news repeated in the card")
	}
	if _, ok := direction["basis"]; ok {
		t.Fatal("classification basis repeated in the card")
	}
	if direction["primary"].(etfanalysis.DirectionTag).Key != "tech" || direction["trend"] != "mixed" {
		t.Fatal("direction judgment lost", direction)
	}
	for key, raw := range etf["judgments"].(map[string]any) {
		judgment := raw.(map[string]any)
		if text := judgment["text"].(string); len([]rune(text)) > 31 {
			t.Fatalf("judgment %s text not bounded: %d runes", key, len([]rune(text)))
		}
		if ids := judgment["source_ids"].([]string); len(ids) != 1 {
			t.Fatalf("judgment %s sources not bounded: %v", key, ids)
		} else if ids[0] != "e-news" {
			t.Fatalf("judgment %s kept an unresolvable source: %v", key, ids)
		}
	}
}

func TestCompactETFStockFactsTrimStructuredEvidence(t *testing.T) {
	results := []pi.HoldingResult{etfCapacityFixture("510300.SH"), etfCapacityFixture("510500.SH")}
	facts := map[string]pi.Fact{}
	for _, result := range results {
		symbol := result.Holding.Symbol
		etf := result.Analysis.ETF
		facts[symbol+".price"] = pi.Fact{Value: 4.0, Available: true}
		facts[symbol+".etf.holdings"] = pi.Fact{Value: etf.Holdings, Available: true}
		facts[symbol+".etf.direction.technical"] = pi.Fact{Value: etf.Direction.Technical, Available: true}
		facts[symbol+".etf.direction.classification"] = pi.Fact{Value: etf.Direction.Classification, Available: true}
		facts[symbol+".etf.direction.news_coverage"] = pi.Fact{Value: etf.Direction.NewsCoverage, Available: true}
		facts[symbol+".etf.execution_blockers"] = pi.Fact{Value: etf.ExecutionBlockers, Available: true}
	}
	table := stockFactTable(facts, results)
	before, _ := json.Marshal(table)
	compactETFStockFacts(table, results)
	after, _ := json.Marshal(table)
	if len(after) >= len(before) {
		t.Fatalf("ETF facts not bounded: %d -> %d", len(before), len(after))
	}
	columns := table["columns"].([]string)
	for _, item := range table["rows"].([]any) {
		row := item.(map[string]any)
		symbol := row["symbol"].(string)
		values := row["values"].([]any)
		for i, name := range columns {
			value := values[i]
			switch name {
			case "price":
				if value != 4.0 {
					t.Fatal("numeric fact changed", symbol, value)
				}
			case "etf.direction.technical":
				technical := value.(map[string]any)
				if _, ok := technical["metrics"]; ok || len(technical) != 3 {
					t.Fatalf("technical metrics not bounded: %v", technical)
				}
			case "etf.holdings":
				groups := value.([]any)
				if len(groups) != 2 {
					t.Fatalf("holding groups not bounded: %d", len(groups))
				}
				if items := groups[0].(map[string]any)["items"].([]any); len(items) != 3 {
					t.Fatalf("holding items not bounded: %d", len(items))
				}
			case "etf.execution_blockers":
				if list := value.([]any); len(list) != 1 {
					t.Fatalf("execution blockers not bounded: %v", list)
				}
			}
		}
	}
}

func TestETFHeavyCapacityDossierFitsEvidenceBudget(t *testing.T) {
	job := fixtureJob()
	job.Results = nil
	request := job.Source.Request
	request.Holdings = nil
	for i := 0; i < 8; i++ {
		result := etfCapacityFixture(fmt.Sprintf("51030%d.SH", i))
		job.Results = append(job.Results, result)
		request.Holdings = append(request.Holdings, result.Holding)
	}
	job.Source.Request = request
	dossier := commonDossier(job, job.Results, 0)
	encoded, _ := json.Marshal(dossier)
	if len(encoded) > 32*1024 {
		t.Fatalf("eight researched ETFs still need %d bytes of evidence", len(encoded))
	}
	prompt, err := proposalPrompt(job)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompt) > MaxEvidenceModelPromptBytes {
		t.Fatalf("ETF-heavy prompt still %d bytes", len(prompt))
	}
}
