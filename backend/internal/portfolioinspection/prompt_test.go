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
	for _, expected := range []string{`"prompt_version":"portfolio-inspection-v6"`, `"scoring_version":"portfolio-ai-score-v4"`, "holding_logic", "不得调用工具", "没有静态止损价不阻断评分"} {
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
	after, _ := json.Marshal(rr)
	if string(before) != string(after) {
		t.Fatal("scoring rewrote historical counterclaim")
	}
}
