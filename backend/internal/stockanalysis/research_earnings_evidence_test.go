package stockanalysis

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEarningsDisclosureSurvivesOlderIRAndNews(t *testing.T) {
	_, snapshot := researchFixture(t)
	for i := 0; i < 12; i++ {
		s := NewResearchSource("announcement", fmt.Sprintf("投资者关系活动记录表%d", i), strings.Repeat("公司产品、订单、客户、量产和供应链仍有风险。", 100), "fixture", "https://example.com/ir", snapshot.CutoffAt.AddDate(0, 0, -20-i), snapshot.CapturedAt)
		snapshot.Sources = append(snapshot.Sources, s)
	}
	fact := "预计前三季度归属于母公司所有者的净利润126000万元至131000万元；第三季度净利润64920万元至69920万元。"
	earnings := NewResearchSource("announcement", "2026年前三季度业绩预告", "本期业绩预告期间为2026年1月1日至9月30日。"+fact+"未经审计，最终以定期报告为准。"+strings.Repeat("产品客户供应链研发业务介绍。", 100), "fixture", "https://example.com/earnings", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, earnings, reportedForecast(snapshot))
	for _, level := range []ResearchLevel{ResearchLevelStandard, ResearchLevelDeep} {
		request := ResearchRequest{AnalysisLevel: level}
		for stage, pack := range map[string]researchEvidencePack{
			"core":  buildResearchCoreEvidencePack(snapshot, request, ResearchOutline{}),
			"trade": buildResearchTradeEvidencePack(snapshot, request, ResearchOutline{}, ResearchCoreSynthesis{Thesis: ResearchClaim{SourceIDs: []string{"m-price"}}}),
		} {
			found := false
			for _, card := range pack.Evidence {
				if card.ID == earnings.ID {
					found = strings.Contains(card.Text, "126000") && strings.Contains(card.Text, "64920") && strings.Contains(card.Text, "未经审计")
				}
			}
			if !found {
				t.Fatalf("%s/%s lost earnings numbers or qualifications", level, stage)
			}
			policy := researchLevelPolicyFor(level)
			limit := policy.MaxEvidenceBytes
			if stage == "trade" {
				limit = policy.TradeEvidenceBytes
			}
			if pack.Stats.SelectedContentBytes > limit {
				t.Fatal("evidence budget enlarged")
			}
		}
	}
}

func TestEarningsCorrectionRetainsRealCounterevidence(t *testing.T) {
	_, snapshot := researchFixture(t)
	old := NewResearchSource("announcement", "三季度业绩预告", "预计净利润增长。", "fixture", "https://example.com/old", snapshot.CutoffAt.Add(-48*time.Hour), snapshot.CapturedAt)
	correction := NewResearchSource("announcement", "三季度业绩预告修正公告", "由于新增资产减值，净利润下修至亏损5000万元，存在重大核算不确定性。", "fixture", "https://example.com/correction", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	future := correction
	future.ID = "future"
	future.PublishedAt = snapshot.CutoffAt.Add(time.Hour)
	title := correction
	title.ID = "title"
	title.ContentStatus = "title_only"
	title.PublishedAt = snapshot.CutoffAt
	latest, ok := LatestResearchEarningsDisclosure([]ResearchSource{old, future, title, correction}, snapshot.CutoffAt)
	if !ok || latest.ID != correction.ID || !strings.Contains(ResearchEarningsExcerpt(latest, 180), "亏损5000万元") {
		t.Fatal("correction lost, or future/title-only disclosure selected")
	}
}

// Read-only replay of the exact frozen snapshot; no model or data-provider calls.
func TestSavedEarningsEvidenceAudit(t *testing.T) {
	path := os.Getenv("EASY_STOCK_EARNINGS_RESEARCH_AUDIT")
	if path == "" {
		t.Skip("set frozen research job path for evidence audit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var job ResearchJob
	if err = json.Unmarshal(data, &job); err != nil || job.Snapshot == nil || job.Analysis == nil {
		t.Fatal("missing saved research snapshot", err)
	}
	before, _ := json.Marshal(job.Snapshot)
	source, ok := LatestResearchEarningsDisclosure(job.Snapshot.Sources, job.Snapshot.CutoffAt)
	if !ok {
		t.Fatal("no earnings disclosure body")
	}
	rr := job.Analysis.ResearchReport
	core := ResearchCoreSynthesis{Thesis: rr.Thesis, Support: rr.Support, Counter: rr.Counter, TradingLogic: rr.TradingLogic}
	for stage, pack := range map[string]researchEvidencePack{
		"core":  buildResearchCoreEvidencePack(*job.Snapshot, job.Request, ResearchOutline{}),
		"trade": buildResearchTradeEvidencePack(*job.Snapshot, job.Request, ResearchOutline{}, core),
	} {
		found := false
		ids := map[string]bool{}
		for _, card := range pack.Evidence {
			ids[card.ID] = true
			if card.ID == source.ID {
				found = true
				t.Logf("%s: %d body bytes; earnings excerpt: %s", stage, pack.Stats.SelectedContentBytes, card.Text)
				if expected := os.Getenv("EASY_STOCK_EARNINGS_EXPECTED_TEXT"); expected != "" && !strings.Contains(card.Text, expected) {
					t.Fatal("requested earnings detail lost")
				}
			}
		}
		if !found {
			t.Fatalf("%s lost the saved earnings disclosure", stage)
		}
		if !ids["m-price"] || !ids["f-financial"] {
			t.Fatalf("%s lost price/financial evidence: %v", stage, ids)
		}
	}
	after, _ := json.Marshal(job.Snapshot)
	if string(before) != string(after) {
		t.Fatal("frozen snapshot changed")
	}
}
