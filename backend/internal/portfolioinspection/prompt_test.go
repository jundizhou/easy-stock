package portfolioinspection

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/stockanalysis"
)

func TestBuildPromptUsesAIResearchRubric(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"prompt_version":"` + PromptVersion + `"`, `"scoring_version":"portfolio-ai-score-v5"`, "holding_logic", "不得调用工具", "没有静态止损价不阻断评分"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	if strings.Contains(prompt, "deterministic_summary") || strings.Contains(prompt, "必须原样复制") {
		t.Fatal("AI scoring still overridden")
	}
}

func TestInspectionKeepsReportedFactProvenanceAndSeparatesDataGaps(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	rr := results[0].Analysis.ResearchReport
	source := &rr.Sources[0]
	source.Kind, source.ContentStatus = "news", "excerpt"
	source.Provider, source.URL = "eastmoney:stock-news-search:证券时报", "https://finance.example.com/forecast"
	source.PublishedAt = time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	source.Content = "公司发布前三季度业绩预告，预计归母净利润12.6至13.1亿元。"
	rr.Thesis.Kind = "fact"
	rr.Limitations = []string{"本次未取得公告原文"}
	before, _ := json.Marshal(rr)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{stockanalysis.NewsEvidencePolicy, "不进入primary_risks、扣分项或调整理由", `"news_context"`, `"traceable":true`, source.Provider, source.URL, source.Content, "本次未取得公告原文"} {
		if !strings.Contains(prompt, text) {
			t.Fatal("inspection lost source context or gap boundary", text)
		}
	}
	after, _ := json.Marshal(rr)
	if string(before) != string(after) {
		t.Fatal("inspection rewrote original research")
	}
}

func TestInspectionReadsEarningsBodyOmittedByOldResearch(t *testing.T) {
	req, results, metrics, _ := scoreFixture()
	rules, _ := RulesFor(req.TraderProfile)
	rr := results[0].Analysis.ResearchReport
	rr.CutoffAt = time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	rr.Counter = []stockanalysis.ResearchClaim{{Text: "预告未兑现且原文未取得", Kind: "inference", SourceIDs: []string{"s1"}}}
	notice := stockanalysis.NewResearchSource("announcement", "前三季度业绩预告", "报告期1月1日至9月30日，预计归母净利润12.6至13.1亿元，未经审计；公司未发现重大不确定因素。", "exchange", "https://example.com/notice", rr.CutoffAt.Add(-time.Hour), rr.CutoffAt)
	rr.Sources = append(rr.Sources, notice)
	before, _ := json.Marshal(rr)
	prompt, err := buildPrompt(req, results, metrics, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{stockanalysis.EarningsDisclosurePolicy, notice.ID, notice.Content, "不据此降低评分/置信度、限制配置或列为优先处理"} {
		if !strings.Contains(prompt, text) {
			t.Fatal("scorer cannot correct stale disclosure limitation", text)
		}
	}
	_, _, _, scored := scoreFixture()
	scored.Dimensions[0].EvidenceRefs = []EvidenceRef{{ReportID: results[0].AnalysisID, SourceID: notice.ID}}
	scored.RiskGroups = []RiskGroup{{Name: "披露验证", Symbols: []string{results[0].Holding.Symbol}, Reason: "最新公告与原报告一起核对", EvidenceRefs: scored.Dimensions[0].EvidenceRefs}}
	if err := validateScoringReportFactsAt(&scored, req, results, metrics, true, nil, rr.CutoffAt); err != nil {
		t.Fatal("real earnings source in prompt rejected by validator", err)
	}
	after, _ := json.Marshal(rr)
	if string(before) != string(after) {
		t.Fatal("scoring rewrote historical counterclaim")
	}
}

func TestScoringSourceMembershipMatchesPromptAndRejectsUnexposedSources(t *testing.T) {
	for _, ref := range []string{"latest", "old", "future", "title", "unrelated", "invented", "wrong_report"} {
		t.Run(ref, func(t *testing.T) {
			req, results, metrics, report := scoreFixture()
			rules, _ := RulesFor(req.TraderProfile)
			rr := results[0].Analysis.ResearchReport
			rr.CutoffAt = time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
			for _, id := range []string{"latest", "old", "future", "title", "unrelated"} {
				source := stockanalysis.NewResearchSource("announcement", "前三季度业绩预告", "预计归母净利润增长，未经审计。", "fixture", "https://example.com/"+id, rr.CutoffAt.Add(-time.Hour), rr.CutoffAt)
				source.ID = id
				switch id {
				case "old":
					source.PublishedAt = rr.CutoffAt.Add(-48 * time.Hour)
				case "future":
					source.PublishedAt = rr.CutoffAt.Add(time.Hour)
				case "title":
					source.ContentStatus = "title_only"
				case "unrelated":
					source.Title = "临时股东会公告"
				}
				rr.Sources = append(rr.Sources, source)
			}
			before, _ := json.Marshal(rr)
			prompt, err := buildScoringPromptAt(req, results, metrics, rules, rr.CutoffAt)
			if err != nil {
				t.Fatal(err)
			}
			_, payload, _ := strings.Cut(prompt, "[组合证据JSON]\n")
			var input struct {
				Sources []struct{ ID string } `json:"sources"`
			}
			if err := json.Unmarshal([]byte(payload), &input); err != nil {
				t.Fatal(err)
			}
			provided := map[string]bool{}
			for _, s := range input.Sources {
				provided[s.ID] = true
			}
			if !provided["latest"] || provided["old"] || provided["future"] || provided["title"] || provided["unrelated"] {
				t.Fatal("changed evidence cutoff/selection", provided)
			}
			id, reportID := ref, results[0].AnalysisID
			if ref == "wrong_report" {
				id, reportID = "latest", "other-report"
			}
			report.Dimensions[0].EvidenceRefs = []EvidenceRef{{ReportID: reportID, SourceID: id}}
			err = validateScoringReportFactsAt(&report, req, results, metrics, true, nil, rr.CutoffAt)
			if (err == nil) != (ref == "latest") {
				t.Fatal("prompt/source whitelist mismatch", ref, err)
			}
			after, _ := json.Marshal(rr)
			if string(before) != string(after) {
				t.Fatal("source selection mutated frozen research")
			}
		})
	}
}
