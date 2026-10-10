package stockanalysis

import (
	"strings"
	"testing"
)

func TestNotificationMarkdownFullAndPartialResults(t *testing.T) {
	body := strings.Repeat("完整研究结论", 1000)
	job := ResearchJob{ID: "task", Status: "succeeded", Error: "private-error", Message: "private-error", Request: ResearchRequest{Symbol: "600519.SH"}, Analysis: &Analysis{Name: "测试股票", ResearchReport: &ResearchReport{
		ResearchSynthesis: ResearchSynthesis{Headline: "研究标题", Thesis: ResearchClaim{Text: body, Quote: "private-quote", SourceIDs: []string{"private-source"}}, Conditions: []ResearchCondition{{ID: "c1", Text: "失效触发条件"}}, InvalidationIDs: []string{"c1"}, Scenarios: []ResearchScenario{{Name: "情景测试", ConditionIDs: []string{"c1"}, Response: "应对测试"}}, Decision: ResearchDecision{NewPosition: "新仓测试", ExistingPosition: "已有仓位测试"}, Limitations: []string{"限制测试"}}, Sources: []ResearchSource{{Content: "private-content", URL: "private-url"}}, Attempts: []ResearchAttempt{{Error: "private-attempt"}},
	}}}
	for _, status := range []string{"succeeded", "partial", "failed"} {
		job.Status = status
		got := NotificationMarkdown(job)
		for _, want := range []string{body, "task", "600519.SH", "研究标题", "失效条件", "失效触发条件", "情景测试", "应对测试", "新仓测试", "已有仓位测试", "限制测试"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s missing %q", status, want[:min(20, len(want))])
			}
		}
		if strings.Contains(got, "private-") {
			t.Fatal("private data leaked")
		}
		if status != "succeeded" && !strings.Contains(got, "未完成") {
			t.Fatal("partial report marked complete")
		}
	}
	job.Analysis = nil
	if got := NotificationMarkdown(job); !strings.Contains(got, "尚未生成") || strings.Contains(got, "private-") {
		t.Fatal("invalid empty failure notification")
	}
	job.Analysis = &Analysis{Conclusion: Conclusion{Summary: "阶段结果"}}
	if !strings.Contains(NotificationMarkdown(job), "阶段结果") {
		t.Fatal("lost available fallback results")
	}
}
