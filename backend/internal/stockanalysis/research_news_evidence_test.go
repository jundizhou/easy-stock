package stockanalysis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func reportedForecast(snapshot ResearchSnapshot) ResearchSource {
	source := NewResearchSource("news", "测试股票发布前三季度业绩预告",
		"新闻检索摘要（非全文）：\n测试股票公告，预计2026年前三季度归母净利润12.6至13.1亿元，最终以正式季报为准。公司从事芯片设计。",
		"eastmoney:stock-news-search:证券时报", "https://finance.example.com/forecast", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	source.ContentStatus = "excerpt"
	return source
}

func forecastResearch(source ResearchSource) ResearchSynthesis {
	r := validResearch()
	r.Thesis = ResearchClaim{Text: "据证券时报报道，公司预计2026年前三季度归母净利润12.6至13.1亿元，尚非已实现利润", Kind: "fact", SourceIDs: []string{source.ID}}
	r.Support = []ResearchClaim{{Text: "报道转述公司业绩预告，最终以正式季报为准", Kind: "fact", SourceIDs: []string{source.ID}}}
	r.EvidenceLevel = "sufficient"
	r.Limitations = []string{"本次未取得公告原文"}
	return r
}

func TestReportedFactDoesNotNeedOriginalAnnouncementOrTwoClaims(t *testing.T) {
	_, snapshot := researchFixture(t)
	source := reportedForecast(snapshot)
	AppendResearchSources(&snapshot, []ResearchSource{source})
	r := forecastResearch(source)
	if _, err := validateResearch(&r, snapshot); err != nil {
		t.Fatal(err)
	}
	if r.EvidenceLevel != "sufficient" || r.Thesis.Kind != "fact" || r.Support[0].Kind != "fact" || len(r.EvidenceReasons) != 0 || r.Decision.Status != "conditional" {
		t.Fatalf("traceable reported fact was downgraded: %+v", r)
	}
	if !strings.Contains(strings.Join(r.Limitations, " "), "未取得公告原文") || source.Kind != "news" || source.ContentStatus != "excerpt" {
		t.Fatal("reporting was silently upgraded to primary disclosure")
	}
}

func TestNewsProvenanceGapsRemainLimited(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ResearchSource)
	}{
		{"no_link", func(s *ResearchSource) { s.URL = "" }},
		{"invalid_link", func(s *ResearchSource) { s.URL = "https:///forecast" }},
		{"no_publisher", func(s *ResearchSource) { s.Provider = "eastmoney:stock-news-search:" }},
		{"no_date", func(s *ResearchSource) { s.PublishedAt = time.Time{} }},
		{"title_only", func(s *ResearchSource) { s.Content = "新闻检索摘要（非全文）：\n" + s.Title }},
		{"legacy_empty_wrapper", func(s *ResearchSource) {
			s.Content = "新闻检索摘要（非全文，第三方报道需核实）：\n"
		}},
		{"explicit_title_status", func(s *ResearchSource) { s.ContentStatus = "title_only" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, snapshot := researchFixture(t)
			source := reportedForecast(snapshot)
			tc.change(&source)
			AppendResearchSources(&snapshot, []ResearchSource{source})
			r := forecastResearch(source)
			if _, err := validateResearch(&r, snapshot); err != nil {
				t.Fatal(err)
			}
			if r.EvidenceLevel != "limited" || r.Thesis.Kind != "inference" || len(r.EvidenceReasons) == 0 {
				t.Fatalf("uncheckable reporting became sufficient: %+v", r)
			}
		})
	}
}

func TestTraceableNewsDoesNotUpgradeRumorOrOpinion(t *testing.T) {
	_, snapshot := researchFixture(t)
	source := reportedForecast(snapshot)
	source.Content = "市场传闻公司获得新订单，尚未经公司确认。"
	AppendResearchSources(&snapshot, []ResearchSource{source})
	for _, level := range []string{"limited", "insufficient"} {
		r := forecastResearch(source)
		r.Thesis = ResearchClaim{Text: "订单传闻尚未经公司确认", Kind: "inference", SourceIDs: []string{source.ID}}
		r.EvidenceLevel = level
		r.Limitations = []string{"订单真实性尚待核实"}
		if _, err := validateResearch(&r, snapshot); err != nil || r.EvidenceLevel != level || r.Thesis.Kind != "inference" {
			t.Fatalf("traceability overrode semantic assessment: %+v, %v", r, err)
		}
	}
	r := forecastResearch(source)
	r.Thesis.Kind = "opinion"
	if _, err := validateResearch(&r, snapshot); err != nil || r.EvidenceLevel != "limited" || r.Thesis.Kind != "opinion" {
		t.Fatalf("news opinion became a sufficient factual thesis: %+v, %v", r, err)
	}
}

func TestSecondaryOpinionDoesNotEraseSupportedFact(t *testing.T) {
	_, snapshot := researchFixture(t)
	source := reportedForecast(snapshot)
	opinion := NewResearchSource("opinion", "研报观点", "预计增长", "fixture", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	AppendResearchSources(&snapshot, []ResearchSource{source, opinion})
	for _, ids := range [][]string{{source.ID, opinion.ID}, {opinion.ID, source.ID}, {"m-price", opinion.ID}, {opinion.ID, "m-price"}} {
		r := forecastResearch(source)
		r.Thesis.SourceIDs = ids
		if _, err := validateResearch(&r, snapshot); err != nil || r.Thesis.Kind != "fact" {
			t.Fatalf("citation order or secondary opinion erased fact: %v, %v", ids, err)
		}
	}
}

func TestReportedCompanyLogicIsSeparateFromUnverifiedMarketAttribution(t *testing.T) {
	_, snapshot := researchFixture(t)
	source := reportedForecast(snapshot)
	AppendResearchSources(&snapshot, []ResearchSource{source})
	r := forecastResearch(source)
	r.TradingLogic = &ResearchTradingLogic{
		Business:  &ResearchClaim{Text: "据报道公司从事芯片设计", Kind: "fact", SourceIDs: []string{source.ID}},
		Mainlines: []ResearchLogicItem{{Name: "业绩增长预期", Explanation: r.Thesis, EvidenceLevel: "sufficient", MarketStatus: "supported"}},
		Catalysts: []ResearchClaim{r.Thesis},
	}
	if _, err := validateResearch(&r, snapshot); err != nil {
		t.Fatal(err)
	}
	logic := r.TradingLogic
	if logic.Business == nil || logic.Business.Kind != "fact" || len(logic.Catalysts) != 1 || logic.Catalysts[0].Kind != "fact" || logic.Mainlines[0].EvidenceLevel != "sufficient" {
		t.Fatal("reported company facts were downgraded in trading logic", logic)
	}
	if logic.Mainlines[0].MarketStatus != "unverified" || logic.Mainlines[0].Explanation.Kind != "inference" {
		t.Fatal("reported event was treated as proven market causation", logic)
	}
}

func TestNewsRevalidationReplaysOnlySavedJudgmentWithoutChangingHistory(t *testing.T) {
	for _, level := range []string{"sufficient", "limited", "insufficient"} {
		job := legacyHolidayJob(t)
		source := reportedForecast(*job.Snapshot)
		AppendResearchSources(job.Snapshot, []ResearchSource{source})
		raw := forecastResearch(source)
		raw.EvidenceLevel = level
		encoded, _ := json.Marshal(raw)
		job.Analysis.ResearchReport.PromptVersion = "stock-research-v8"
		job.Analysis.ResearchReport.ValidationVersion = "stock-research-validation-v3"
		job.Analysis.ResearchReport.Sources = job.Snapshot.Sources
		job.Checkpoint.PromptVersion = "stock-research-v8"
		job.Checkpoint.SnapshotHash = researchHash(job.Snapshot)
		job.Checkpoint.Outputs["核心判断"] = ResearchStageOutput{Value: encoded}
		job.Checkpoint.Outputs["交易条件"] = ResearchStageOutput{Value: encoded}
		before, _ := json.Marshal(job)
		got := RevalidateReusableResearch(job)
		if got.Analysis.ResearchReport.EvidenceLevel != level || got.Analysis.ResearchReport.ValidationVersion != ResearchValidationVersion || !ResearchCompletedAt(got).Equal(ResearchCompletedAt(job)) {
			t.Fatalf("reuse invented judgment or renewed research: %+v", got.Analysis.ResearchReport)
		}
		after, _ := json.Marshal(job)
		if string(before) != string(after) {
			t.Fatal("news revalidation rewrote historical report")
		}
	}
}

func TestStockResearchCarriesNewsPolicyAndProvenanceThroughBothModes(t *testing.T) {
	for _, level := range []ResearchLevel{ResearchLevelStandard, ResearchLevelDeep} {
		t.Run(string(level), func(t *testing.T) {
			analysis, snapshot := researchFixture(t)
			source := reportedForecast(snapshot)
			AppendResearchSources(&snapshot, []ResearchSource{source})
			encoded, _ := json.Marshal(forecastResearch(source))
			prompter := &researchTestPrompter{respond: func(_ int, prompt string) (string, error) {
				if !strings.Contains(prompt, NewsEvidencePolicy) || !strings.Contains(prompt, `"news_context"`) || !strings.Contains(prompt, "证券时报") {
					t.Fatal("research stage lost news policy or provenance")
				}
				if strings.Contains(prompt, "[压缩证据包]") {
					return `{"questions":[],"hypotheses":[],"missing_facts":[]}`, nil
				}
				return string(encoded), nil
			}}
			if err := RunResearch(context.Background(), prompter, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: level}, "fixture", nil, nil); err != nil {
				t.Fatal(err)
			}
			if analysis.ResearchReport == nil || analysis.ResearchReport.EvidenceLevel != "sufficient" || analysis.ResearchReport.Thesis.Kind != "fact" {
				t.Fatal("stock analysis lost supported reported fact", analysis.ResearchReport)
			}
		})
	}
}
