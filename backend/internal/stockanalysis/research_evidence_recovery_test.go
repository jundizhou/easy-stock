package stockanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestResearchPreservesLateAnnouncementFactsAndPrioritizesRequestedSource(t *testing.T) {
	_, snapshot := researchFixture(t)
	fact := "受让方为测试产业基金，转让比例为8.5%，转让价格每股26.50元，过户后12个月不减持。"
	body := strings.Repeat("董事会及全体董事保证本公告内容真实、准确、完整。", 100) + fact + "本次转让仍存在审批风险。"
	source := ResearchItemSource(foundation.MarketResearchItem{ID: "AN202609300001", Title: "股份转让公告", Content: body, PublishedAt: snapshot.CutoffAt.Add(-time.Hour)}, "announcement", snapshot.CapturedAt)
	if !strings.Contains(source.Content, fact) || len([]rune(source.Content)) <= 1800 {
		t.Fatal("snapshot discarded late notice facts before retrieval")
	}
	for index := 0; index < 18; index++ {
		snapshot.Sources = append(snapshot.Sources, NewResearchSource("announcement", fmt.Sprintf("风险公告%d", index), "公司存在经营现金流风险及减值风险。", "fixture", fmt.Sprintf("https://example.com/%d", index), snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt))
	}
	snapshot.Sources = append(snapshot.Sources, source)
	outline := ResearchOutline{Questions: []ResearchQuestion{{SourceID: source.ID, Question: "受让方、转让比例和转让价格是什么", Query: "受让方 转让比例 转让价格", Status: "available"}}}
	pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{AnalysisLevel: ResearchLevelDeep}, outline)
	for _, card := range pack.Evidence {
		if card.ID == source.ID {
			if !strings.Contains(card.Text, fact) || !strings.Contains(card.Text, "审批风险") {
				t.Fatalf("requested evidence lost terms or risk: %s", card.Text)
			}
			if len([]rune(card.Text)) > 1500 {
				t.Fatal("announcement budget exceeded")
			}
			return
		}
	}
	t.Fatal("generic risk notices displaced the source requested by the researcher")
}

func TestResearchExcerptFindsFactsInsideLongUnpunctuatedBody(t *testing.T) {
	body := strings.Repeat("资料表头", 800) + "受让方测试基金 转让比例8.5% 转让价格26.50元" + strings.Repeat("表格附注", 300)
	excerpt := ResearchSourceExcerpt(body, []string{"受让方 转让比例 转让价格"}, 700)
	if !strings.Contains(excerpt, "测试基金") || !strings.Contains(excerpt, "26.50元") || !strings.Contains(body, excerpt) || len([]rune(excerpt)) > 700 {
		t.Fatalf("long sentence retrieval lost the requested facts: %s", excerpt)
	}
}

func TestResearchReusesBodyWithoutCarryingResolvedInitialGaps(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	fact := "受让方为测试基金，转让比例8.5%，锁定12个月。"
	source := NewResearchSource("announcement", "转让公告", fact, "fixture", "https://example.com/notice", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, source)
	initialGap := "受让方身份与锁定安排尚未核实"
	outline := ResearchOutline{Questions: []ResearchQuestion{{Question: "受让方身份与锁定安排是什么", Why: "改变事件判断", Tool: "source", SourceID: source.ID}}, MissingFacts: []string{initialGap}}
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	result.Support[0] = ResearchClaim{Text: fact, Kind: "fact", SourceIDs: []string{source.ID}, Quote: fact}
	outlineJSON, _ := json.Marshal(outline)
	resultJSON, _ := json.Marshal(result)
	prompter := &researchTestPrompter{respond: func(call int, prompt string) (string, error) {
		if call == 1 {
			return string(outlineJSON), nil
		}
		if call == 2 && (!strings.Contains(prompt, fact) || !strings.Contains(prompt, `"status":"available"`)) {
			t.Fatal("existing body was not delivered to synthesis as usable evidence")
		}
		return string(resultJSON), nil
	}}
	err := RunResearch(context.Background(), prompter, &snapshot, &analysis, ResearchRequest{Symbol: analysis.Symbol, Horizon: "swing", AnalysisLevel: ResearchLevelDeep}, "fixture", func(context.Context, ResearchSnapshot, ResearchQuestion) ([]ResearchSource, error) {
		return []ResearchSource{source}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	report := analysis.ResearchReport
	if report.Questions[0].Status != "available" || report.EvidenceLevel != "sufficient" || len(report.Questions[0].EvidenceSourceIDs) != 1 {
		t.Fatalf("existing evidence was treated as missing: %+v", report)
	}
	if strings.Contains(strings.Join(report.Limitations, " "), initialGap) {
		t.Fatal("resolved outline gap remained a permanent report limitation")
	}
}

func TestResearchTitleOnlyAndFutureSourcesAreNotUsableSupplement(t *testing.T) {
	_, snapshot := researchFixture(t)
	title := NewResearchSource("announcement", "仅标题", "仅标题", "fixture", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	future := NewResearchSource("announcement", "未来披露", "未来披露的正文", "fixture", "", snapshot.CutoffAt.Add(time.Hour), snapshot.CapturedAt)
	AppendResearchSources(&snapshot, []ResearchSource{title, future})
	if ids := availableResearchSourceIDs(snapshot, []ResearchSource{title, future}); len(ids) != 0 {
		t.Fatalf("title-only or future material became verified body evidence: %v", ids)
	}
}

func TestResearchTitleOnlyAnnouncementCannotMakeEvidenceSufficient(t *testing.T) {
	_, snapshot := researchFixture(t)
	title := strings.Repeat("重大事项公告", 40)
	source := NewResearchSource("announcement", title, title, "fixture", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, source)
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	result.Support[0].SourceIDs = []string{source.ID}
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "limited" || ResearchSourceHasBody(source) {
		t.Fatal("title-only notice counted as fully read evidence")
	}
}

func TestResearchFinancialHistoryKeepsPeriodsAndPublicationCutoff(t *testing.T) {
	analysis, snapshot := researchFixture(t)
	cutoff := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	q1 := foundation.StockFundamentals{ReportDate: "2026-03-31", Revenue: 100, PublishedAt: time.Date(2026, 4, 29, 0, 0, 0, 0, time.UTC)}
	q2 := foundation.StockFundamentals{ReportDate: "2026-06-30", Revenue: 260, PublishedAt: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)}
	unpublished := foundation.StockFundamentals{ReportDate: "2026-09-30", Revenue: 400, PublishedAt: cutoff.Add(30 * 24 * time.Hour)}
	input := Input{Fundamentals: &q2, FinancialHistory: []foundation.StockFundamentals{q1, unpublished, q2}}
	got := BuildResearchSnapshot(input, analysis, cutoff)
	for _, source := range got.Sources {
		if source.ID != "f-financial" {
			continue
		}
		var payload struct {
			History []foundation.StockFundamentals `json:"history"`
		}
		if err := json.Unmarshal([]byte(source.Content), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.History) != 2 || payload.History[0].Revenue != 260 || payload.History[1].Revenue != 100 || source.TimeStatus != "dated" {
			t.Fatalf("financial history lost period/cutoff integrity: %+v", payload)
		}
		if strings.Contains(strings.Join(got.Limitations, " "), "财务仅含单期") {
			t.Fatal("multi-period data was labeled single-period")
		}
		pack := buildResearchCoreEvidencePack(got, ResearchRequest{AnalysisLevel: ResearchLevelDeep}, ResearchOutline{})
		for _, card := range pack.Evidence {
			if card.ID == source.ID && !strings.Contains(card.Text, "2026-03-31") {
				t.Fatal("compression dropped comparable history")
			}
		}
		return
	}
	t.Fatalf("no financial disclosure generated from valid history; prior snapshot %s", snapshot.ID)
}

func TestResearchEvidenceReasonsExplainActualDowngrade(t *testing.T) {
	_, snapshot := researchFixture(t)
	result := validResearch()
	result.EvidenceLevel = "sufficient"
	news := NewResearchSource("news", "订单传闻", "订单仍待公司确认", "fixture", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, news)
	result.Thesis.SourceIDs = []string{news.ID}
	if _, err := validateResearch(&result, snapshot); err != nil {
		t.Fatal(err)
	}
	if result.EvidenceLevel != "limited" || !strings.Contains(strings.Join(result.EvidenceReasons, " "), "第三方摘要") {
		t.Fatalf("coverage downgrade has no specific reason: %v", result.EvidenceReasons)
	}
}

func TestResearchFinancialAndPriceRowsLeaveRoomForRequestedAnnouncements(t *testing.T) {
	lines := syntheticTrendLines("600519.SH", 300, 10, .05, 1_000_000_000)
	cutoff := lines[len(lines)-1].Time.Add(18 * time.Hour)
	periods := []foundation.StockFundamentals{}
	for i := 0; i < 8; i++ {
		day := cutoff.AddDate(0, -3*i-1, 0)
		periods = append(periods, foundation.StockFundamentals{Symbol: "600519.SH", ReportDate: day.Format("2006-01-02"), PublishedAt: day.Add(time.Hour), Revenue: 5e10, RevenueYearOverYear: 12.52, NetProfit: 5e9, NetProfitYearOverYear: 24.57, DeductedNetProfit: 4e9, DeductedNetProfitAvailable: true, ROE: 12.51, EPS: 4.82, GrossMargin: 32.32, DebtRatio: 28.83, OperatingCashFlowPerShare: 2.75})
	}
	input := Input{Symbol: "600519.SH", KLines: lines, Business: strings.Repeat("公司主营产品研发与生产业务。", 150), Fundamentals: &periods[0], FinancialHistory: periods}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := BuildResearchSnapshot(input, analysis, cutoff)
	outline := ResearchOutline{}
	for i := 0; i < 2; i++ {
		body := ""
		for n := 0; n < 100; n++ {
			body += fmt.Sprintf("受让方%d的投资金额为%d万元，转让比例为8.5%%，仍存在审批风险。", n, n+100)
		}
		source := NewResearchSource("announcement", fmt.Sprintf("投资公告%d", i), body, "fixture", fmt.Sprintf("https://example.com/body-%d", i), cutoff.Add(-time.Hour), cutoff)
		snapshot.Sources = append(snapshot.Sources, source)
		outline.Questions = append(outline.Questions, ResearchQuestion{SourceID: source.ID, Query: "受让方 投资金额 转让比例"})
	}
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{AnalysisLevel: level}, outline)
		ids := map[string]bool{}
		for _, card := range pack.Evidence {
			ids[card.ID] = true
		}
		if !ids["f-financial"] {
			t.Fatalf("%s dropped the financial facts", level)
		}
		if level == ResearchLevelDeep {
			for _, question := range outline.Questions {
				if !ids[question.SourceID] {
					t.Fatal("price rows or financial history displaced requested notices")
				}
			}
		}
	}
}
