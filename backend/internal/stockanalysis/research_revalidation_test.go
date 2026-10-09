package stockanalysis

import (
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func legacyHolidayJob(t *testing.T) ResearchJob {
	t.Helper()
	analysis, snapshot := researchFixture(t)
	snapshot.CutoffAt, _ = time.Parse(time.RFC3339, "2026-10-05T19:47:25+08:00")
	snapshot.DailyBars[len(snapshot.DailyBars)-1].Date = "2026-09-30"
	snapshot.Limitations = append(snapshot.Limitations, "日线距分析时点超过5天，行情可能过期或停牌，禁止生成新仓价格计划")
	raw := validResearch()
	raw.EvidenceLevel = "sufficient"
	core, _ := json.Marshal(raw)
	trade, _ := json.Marshal(raw)
	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "holding", Horizon: "swing", AnalysisLevel: ResearchLevelStandard}
	completed := snapshot.CutoffAt.Add(time.Minute)
	report := ResearchReport{ResearchSynthesis: raw, PromptVersion: "stock-research-v7", Request: request, GeneratedAt: completed, CutoffAt: snapshot.CutoffAt, Validation: "references_checked", SnapshotID: snapshot.ID, Sources: snapshot.Sources, Anchors: snapshot.Anchors}
	report.EvidenceLevel = "insufficient"
	report.Decision.Status = "no_plan"
	report.Decision.PricePlan = nil
	analysis.ResearchReport = &report
	analysis.AI.Status = "ready"
	return ResearchJob{ID: "legacy-holiday", Request: request, Status: "succeeded", CompletedAt: &completed, Analysis: &analysis, Snapshot: &snapshot, Checkpoint: &ResearchCheckpoint{PromptVersion: "stock-research-v7", Request: request, SnapshotHash: researchHash(snapshot), Outputs: map[string]ResearchStageOutput{"核心判断": {Value: core}, "交易条件": {Value: trade}}}}
}

func TestReuseReplaysVerifiedOutputsWithoutChangingHistoryOrCompletionTime(t *testing.T) {
	job := legacyHolidayJob(t)
	before, _ := json.Marshal(job)
	got := RevalidateReusableResearch(job)
	r := got.Analysis.ResearchReport
	if r.EvidenceLevel != "sufficient" || r.Decision.Status != "conditional" || r.Decision.PricePlan == nil || r.ValidationVersion != ResearchValidationVersion {
		t.Fatalf("holiday mistake retained: %+v", r.Decision)
	}
	if !ResearchCompletedAt(got).Equal(ResearchCompletedAt(job)) || !r.GeneratedAt.Equal(job.Analysis.ResearchReport.GeneratedAt) || r.PromptVersion != "stock-research-v7" {
		t.Fatal("revalidation invented new research or renewed cache")
	}
	after, _ := json.Marshal(job)
	if string(before) != string(after) {
		t.Fatal("history mutated")
	}
	if !reflect.DeepEqual(got.Snapshot, job.Snapshot) {
		t.Fatal("immutable snapshot overwritten")
	}
	job.Checkpoint.SnapshotHash = "wrong"
	if RevalidateReusableResearch(job).Analysis.ResearchReport.ValidationVersion == ResearchValidationVersion {
		t.Fatal("mismatched checkpoint unlocked research")
	}
	job.Checkpoint = nil
	if RevalidateReusableResearch(job).Analysis.ResearchReport.EvidenceLevel != "insufficient" {
		t.Fatal("missing original judgment guessed")
	}
}

// Optional read-only replay of selected saved reports; no model or provider
// is called and SQLite is explicitly opened in read-only mode.
func TestSavedResearchRevalidationAudit(t *testing.T) {
	path := os.Getenv("EASY_STOCK_REVALIDATION_DB")
	ids := []any{}
	seen := map[string]bool{}
	for _, value := range strings.Split(os.Getenv("EASY_STOCK_REVALIDATION_IDS"), ",") {
		id := strings.TrimSpace(value)
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if path == "" || len(ids) == 0 {
		t.Skip("read-only replay requires EASY_STOCK_REVALIDATION_DB and EASY_STOCK_REVALIDATION_IDS")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := db.Query("SELECT content_json FROM stock_research_jobs WHERE id IN ("+placeholders+")", ids...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var job ResearchJob
		if json.Unmarshal([]byte(raw), &job) != nil {
			t.Fatal("saved job not decodable")
		}
		got := RevalidateReusableResearch(job)
		if got.Analysis == nil || got.Analysis.ResearchReport == nil {
			t.Fatalf("report %s is not a completed research report", job.ID)
		}
		r := got.Analysis.ResearchReport
		if r.ValidationVersion != ResearchValidationVersion || strings.Contains(strings.Join(r.ValidationNotes, " "), "行情时效不足") {
			t.Fatalf("report %s not revalidated: %v", job.ID, r.ValidationNotes)
		}
		if !ResearchCompletedAt(got).Equal(ResearchCompletedAt(job)) {
			t.Fatal("reuse time changed")
		}
		t.Logf("%s: %s/%s -> %s/%s, blockers=%d", job.Request.Symbol, job.Analysis.ResearchReport.EvidenceLevel, job.Analysis.ResearchReport.Decision.Status, r.EvidenceLevel, r.Decision.Status, len(r.Decision.Blockers))
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(ids) {
		t.Fatalf("expected %d reports, got %d", len(ids), count)
	}
}
