package portfoliooptimization

import (
	"context"
	"easy-stock/backend/internal/foundation"
	pi "easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/stockanalysis"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Expand frozen research to exercise capacity, not to simulate investment returns.
// The optional private input never leaves the machine and starts no AI request.
func TestSixCandidatePromptCapacity(t *testing.T) {
	base := richCapacityBaseJob()
	if path := os.Getenv("EASY_STOCK_OPTIMIZATION_AUDIT"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &base); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		count int
		news  bool
	}{{10, false}, {16, false}, {10, true}, {16, true}} {
		count := tc.count
		t.Run(fmt.Sprintf("%d/news=%t", count, tc.news), func(t *testing.T) {
			j := base
			j.Results = nil
			j.Eligibility = nil
			j.Candidates = nil
			j.RevisionCount = 0
			originals := count - MaxCandidateResearch
			j.Source.Request.Holdings = nil
			for i := 0; i < count; i++ {
				old := base.Results[i%len(base.Results)]
				data, _ := json.Marshal(old)
				symbol := fmt.Sprintf("600%03d.SH", 200+i)
				data = []byte(strings.ReplaceAll(string(data), old.Holding.Symbol, symbol))
				var r pi.HoldingResult
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				// Capacity includes fresh valuation evidence for every original and candidate.
				pe, pb := float64(1855+i*10)/100, float64(123+i)/100
				at := foundation.LatestCompletedAStockSession(time.Now()).Add(15 * time.Hour)
				if tc.news {
					source := &r.Analysis.ResearchReport.Sources[0]
					source.Kind, source.ContentStatus = "news", "excerpt"
					source.Provider = "eastmoney:stock-news-search:证券时报"
					source.URL = "https://finance.example.com/forecast/" + symbol
					source.PublishedAt = at.Add(-time.Hour)
					source.Content = "公司发布2026年前三季度业绩预告，预计归母净利润12.6至13.1亿元。最终以正式季报披露为准，尚不证明增长已经兑现或推动股价上涨。"
				}
				r.CurrentQuote = &foundation.Quote{Symbol: symbol, Price: 100, TradeTime: at, Valuation: &foundation.StockValuation{Symbol: symbol, PETTM: &pe, PB: &pb, TradeTime: at, Meta: foundation.SourceMeta{Source: "test-valuation"}}}
				r.Holding.Weight = 0
				if i < originals {
					r.Holding.Weight = 100 / originals
					if i == 0 {
						r.Holding.Weight += 100 % originals
					}
					j.Source.Request.Holdings = append(j.Source.Request.Holdings, r.Holding)
				} else {
					j.Candidates = append(j.Candidates, Candidate{Symbol: symbol, Selected: true})
				}
				j.Results = append(j.Results, r)
				j.Eligibility = append(j.Eligibility, Eligibility{Symbol: symbol, CanIncrease: true})
			}
			j.Baseline = j.Source.Request.Holdings
			j.Source = pi.OptimizationReport(j.Source.Request, j.Results)
			j.UnionFacts = pi.OptimizationUnionReport(j.Source.Request, j.Results).Facts
			prompt, err := proposalPrompt(j)
			if err != nil {
				for key, val := range commonDossier(j, j.Results, 0) {
					d, _ := json.Marshal(val)
					t.Logf("%s=%d", key, len(d))
				}
				t.Fatal(err)
			}
			if len(prompt) > MaxEvidenceModelPromptBytes {
				t.Fatal(len(prompt))
			}
			t.Logf("%d original + 6 candidates: %d bytes", originals, len(prompt))
			proposal := fixtureProposal()
			row := proposal.Alternatives[0].Allocations[0]
			proposal.Alternatives[0].Allocations = nil
			for _, r := range j.Results {
				a := row
				raw, _ := json.Marshal(row.Investment)
				fields := map[string]string{}
				_ = json.Unmarshal(raw, &fields)
				for key, value := range fields {
					if key != "role" && key != "action" && key != "horizon" {
						fields[key] = shortText(value, 12)
					}
				}
				raw, _ = json.Marshal(fields)
				a.Investment = &InvestmentJudgment{}
				_ = json.Unmarshal(raw, a.Investment)
				a.Symbol = r.Holding.Symbol
				a.EvidenceRefs = []pi.EvidenceRef{{ReportID: r.AnalysisID, SourceID: r.Analysis.ResearchReport.Sources[0].ID}}
				proposal.Alternatives[0].Allocations = append(proposal.Alternatives[0].Allocations, a)
			}
			response, _ := json.Marshal(proposal)
			rangePrompt, err := feasibilityRepairPrompt(context.Background(), j, proposal, errors.New("权重范围不可行：最小合计60%、最大合计88%，须覆盖固定股票总仓位100%"))
			if err != nil || len(rangePrompt) > MaxEvidenceModelPromptBytes {
				t.Fatal("feasibility repair exceeds input budget", len(rangePrompt), err)
			}
			t.Logf("feasibility repair bytes=%d", len(rangePrompt))
			// Match program-mode output: legacy preferred/suitability fields
			// are explicitly excluded by the planner schema.
			var rawProposal map[string]any
			_ = json.Unmarshal(response, &rawProposal)
			rawProposal["weight_mode"] = "program"
			for _, alt := range rawProposal["alternatives"].([]any) {
				for _, row := range alt.(map[string]any)["allocations"].([]any) {
					for _, key := range []string{"preferred_weight", "suitable_for_increase", "suitability_reason", "confirmation_ids", "invalidation_ids"} {
						delete(row.(map[string]any), key)
					}
				}
			}
			response, _ = json.Marshal(rawProposal)
			if len(response) > 16*1024 {
				t.Fatalf("capacity fixture exceeds requested output budget: %d", len(response))
			}
			repair := modelRepairPrompt(prompt, string(response), errors.New("修复引用格式"))
			if len(repair) > MaxModelPromptBytes {
				t.Fatalf("repair too large: %d", len(repair))
			}
			_, payload, _ := strings.Cut(prompt, "[资料JSON]\n")
			if !strings.Contains(repair, payload) {
				t.Fatal("repair altered input facts")
			}
			t.Logf("repair bytes=%d", len(repair))
			parts := &proposalPartsError{proposal: proposal, parts: []proposalPart{{Key: "allocation:" + j.Results[0].Holding.Symbol, Problem: "不可用事实引用" + j.Results[0].Holding.Symbol + ".valuation.pe_ttm", Value: programAllocation(proposal.Alternatives[0].Allocations[0])}}}
			for _, r := range j.Results[originals:] {
				parts.parts = append(parts.parts, proposalPart{Key: "allocation:" + r.Holding.Symbol, Problem: "缺少投资比较；由AI补齐判断"})
			}
			localRepair := modelRepairPrompt(prompt, string(response), parts)
			if len(localRepair) > MaxModelPromptBytes || !strings.Contains(localRepair, payload) {
				t.Fatal("partial repair lost facts or exceeded existing capacity", len(localRepair))
			}
			t.Logf("one invalid row + six missing candidates repair bytes=%d", len(localRepair))
			// Largest independent review union is ten old holdings plus two new names.
			req := j.Source.Request
			req.Holdings = append([]pi.Holding(nil), req.Holdings...)
			for n := 0; n < 2; n++ {
				h := j.Results[originals+n].Holding
				h.Weight = req.Holdings[n].Weight
				req.Holdings[n] = h
			}
			p := Proposal{}
			j.Proposal = &p
			review, err := pairedPrompt(j, Plan{Original: j.Source, Proposed: pi.OptimizationReport(req, j.Results)})
			if err != nil {
				t.Fatal(err)
			}
			if len(review) > MaxEvidenceModelPromptBytes {
				t.Fatal("review evidence exceeds reserved capacity", len(review))
			}
			t.Logf("review bytes=%d", len(review))
		})
	}
}

// Synthetic rich reports, never copied user holdings. The old two-short-claim
// fixture missed the real 10-stock failure: several cited articles, full IDs,
// disclosure excerpts and independent counterevidence all need input space.
func richCapacityBaseJob() Job {
	j := fixtureJob()
	for _, r := range j.Results {
		rr := r.Analysis.ResearchReport
		claim := func(text string, ids ...string) stockanalysis.ResearchClaim {
			return stockanalysis.ResearchClaim{Text: text, SourceIDs: ids}
		}
		for n := 0; n < 4; n++ {
			id := fmt.Sprintf("s-%016d", n+1)
			rr.Sources = append(rr.Sources, stockanalysis.ResearchSource{ID: id, Kind: "news", TimeStatus: "dated", Provider: "eastmoney:stock-news-search:测试财经媒体", URL: fmt.Sprintf("https://finance.example.test/article/%s/%d", r.Holding.Symbol, n), PublishedAt: rr.CutoffAt.Add(-time.Hour), ContentStatus: "excerpt", Content: "公司披露经营增长并说明需求变化，盈利改善仍取决于成本及产品结构。" + strings.Repeat("不同业务景气变化存在差异，原报告保留这些经营不确定性。", 6)})
		}
		notice := stockanalysis.NewResearchSource("announcement", "前三季度业绩预告", "业绩预告期间为1月1日至9月30日。预计归母净利润12.6至13.1亿元。未经审计，最终以正式报告为准，未发现重大不确定因素。", "exchange", "https://example.test/notice/"+r.Holding.Symbol, rr.CutoffAt.Add(-time.Hour), rr.CutoffAt)
		rr.Sources = append(rr.Sources, notice)
		rr.Thesis = claim("主营经营恢复与产品结构改善支持盈利增长，但仍需结合估值、现金流及行业波动判断配置价值", "s-0000000000000001", "s1")
		rr.Counter = []stockanalysis.ResearchClaim{claim("原材料成本上涨可能侵蚀利润空间，不能仅凭历史盈利改善假定未来增长延续", "s-0000000000000002"), claim("行业需求下行导致收入承压，已观察到部分产品销量下降", "s-0000000000000003")}
		rr.Support = []stockanalysis.ResearchClaim{claim("公司产品结构改善且营业利润增长，现金回款仍须持续跟踪", "s-0000000000000004")}
		rr.Decision.ExistingPosition = "保留原持仓，结合本周期需求、量价与组合集中度管理"
		rr.Decision.NewPosition = "结合经营变化与组合资金用途判断是否配置"
		rr.Decision.Blockers = []string{"需求下行仍需持续跟踪"}
		rr.TradingLogic = &stockanalysis.ResearchTradingLogic{Business: &stockanalysis.ResearchClaim{Text: "主营产品生产和销售，利润取决于产品结构与成本变化", SourceIDs: []string{"s1"}}}
	}
	return j
}
