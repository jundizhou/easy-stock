package portfolioinspection

import (
	"strings"
	"testing"
	"time"
)

func TestNotificationMarkdownIncludesResultsWithoutEvidence(t *testing.T) {
	request, results, metrics, conclusion := scoreFixture()
	request.PortfolioPlanName = "成长组合"
	long := strings.Repeat("完整组合结论", 400) + "结论末尾"
	conclusion.ExecutiveSummary = long
	score := 65
	conclusion.TotalScore = &score
	conclusion.ScoreAvailable = true
	conclusion.Dimensions[0].Reason = "逻辑评分说明"
	conclusion.Dimensions[0].Adjustments = []ScoreAdjustment{{Points: -5, Reason: "集中扣分说明"}}
	conclusion.Dimensions[0].Limitations = []string{"维度限制说明"}
	conclusion.Dimensions[0].EvidenceRefs = []EvidenceRef{{SourceID: "DO_NOT_SEND_REFERENCE"}}
	conclusion.PrimaryRisks = []string{"主要风险结果"}
	conclusion.AdjustmentOrder = []string{"优先调整事项"}
	conclusion.ConcentrationFinding = []string{"组合共同驱动结果"}
	conclusion.RiskGroups = []RiskGroup{{Name: "半导体分组", Weight: 60, Symbols: []string{"600519.SH"}, Reason: "分组原因", EvidenceRefs: []EvidenceRef{{SourceID: "DO_NOT_SEND_REFERENCE"}}}}
	conclusion.NextChecklist = []string{"下次核验事项"}
	conclusion.DataLimitations = []string{"数据限制事项"}
	conclusion.ExplanationDetails = map[string]ExplanationDetail{"primary_risks[0]": {EvidenceRefs: []EvidenceRef{{SourceID: "DO_NOT_SEND_EXPLANATION"}}}}
	results[0].Holding.Name = "示例持仓"
	results[0].Analysis.ResearchReport.Sources[0].Content = "DO_NOT_SEND_SOURCE_BODY"
	results[0].Analysis.ResearchReport.Sources[0].URL = "https://example.com/DO_NOT_SEND_SOURCE_URL"
	report := &Report{AlgorithmVersion: AlgorithmVersion, GeneratedAt: time.Date(2026, 10, 10, 3, 56, 0, 0, time.UTC), Profile: ProfileRules{Label: "均衡"}, Holdings: results, Metrics: metrics, Conclusion: conclusion, Facts: map[string]Fact{"raw": {Value: "DO_NOT_SEND_RAW_FACT", Available: true}, "600519.SH.price": {Value: 100, Available: true}, "600519.SH.pnl_percent": {Value: 5, Available: true}}}
	text := NotificationMarkdown(Job{ID: "task-id", Status: "succeeded", Request: request, Report: report, CompletedStocks: 1, TotalStocks: 1})
	for _, want := range []string{"成长组合", "task-id", "65 / 100", long, "逻辑评分说明", "集中扣分说明", "维度限制说明", "主要风险结果", "优先调整事项", "组合共同驱动结果", "半导体分组", "分组原因", "下次核验事项", "数据限制事项", "确认条件", "失效条件", "趋势延续", "趋势破坏", "组合情景", "震荡分化", "示例持仓", "报告行情价格", "成本盈亏", "2026-10-10 11:56"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing result %q", want)
		}
	}
	if strings.Contains(text, "DO_NOT_SEND") || strings.Contains(text, "%!") {
		t.Fatal("evidence leaked or formatting failed")
	}
}

func TestNotificationMarkdownIncompleteReportsDoNotInventScoresOrLeakErrors(t *testing.T) {
	for _, report := range []*Report{nil, {Conclusion: AIReport{ExecutiveSummary: "已有结果"}}} {
		text := NotificationMarkdown(Job{ID: "partial", Status: "partial", Report: report, Error: "PRIVATE_PROVIDER_ERROR", Message: "PRIVATE_PROVIDER_ERROR"})
		if !strings.Contains(text, "未完成") || strings.Contains(text, "PRIVATE_PROVIDER_ERROR") || strings.Contains(text, "0 / 100") {
			t.Fatal("unsafe incomplete report")
		}
		if report != nil && (!strings.Contains(text, "已有结果") || !strings.Contains(text, "待完成")) {
			t.Fatal("partial results lost")
		}
	}
}
