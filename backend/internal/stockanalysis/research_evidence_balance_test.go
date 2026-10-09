package stockanalysis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResearchOperatingFactsSurviveMarketDataAndCapitalHypothesis(t *testing.T) {
	_, snapshot := researchFixture(t)
	// Long investor lists and unrelated capital questions precede the actual
	// operating answers, as in the production report that lost its IR record.
	fact := "公司新产品X1正在客户导入，端侧AI项目最快有望三季度量产，尚未确认收入，验证周期存在不确定性。"
	body := strings.Repeat("机构名称及接待名单 ", 300) + "。公司境外发行尚需审批。" + fact + "新品覆盖多种产品，客户订单、研发与供应链进展仍需核实。"
	ir := NewResearchSource("announcement", "投资者关系活动记录表", body, "fixture", "https://example.com/ir", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	notice := NewResearchSource("announcement", "关于参加行业集体业绩说明会的公告", "会议将介绍产品、客户、订单、研发、供应链与技术布局，投资者届时可提问。", "fixture", "https://example.com/invite", snapshot.CutoffAt.Add(-time.Minute), snapshot.CapturedAt)
	capital := NewResearchSource("announcement", "境外发行公告", "公司境外发行已备案，但仍存在审批风险。", "fixture", "https://example.com/capital", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, notice, capital, ir)
	// Fill the peer source with member detail while keeping same-date aggregate
	// windows that must survive compression.
	peers := []any{}
	for i := 0; i < 24; i++ {
		peers = append(peers, map[string]any{"symbol": fmt.Sprintf("%06d.SZ", i), "name": fmt.Sprintf("同行%d", i), "date_aligned": true, "end_date": "2026-09-30", "sample_days": 120, "returns_percent": map[string]any{"2d": -2.82, "5d": -4.5}, "daily_bars": strings.Repeat("逐日数据", 100)})
	}
	market, _ := json.Marshal(map[string]any{"peer_group": "宽行业目录样本", "selection_scope": "目录取样而非板块指数", "peers": peers, "available_peer_count": 24, "recent_move_window": map[string]any{"window": "2d"}, "windows": map[string]any{"2d": map[string]any{"base_date": "2026-09-28", "start_date": "2026-09-29", "end_date": "2026-09-30", "sample_size": 24, "rising_count": 3, "equal_weight_mean_return_percent": -2.82, "stock_excess_percentage_points": 12.48}}})
	snapshot.Sources = append(snapshot.Sources, ResearchSource{ID: "m-sector", Kind: "calculation", Content: string(market)})
	news := NewResearchSource("news", "公司新品跟踪", "第三方摘要：新品仍需客户验证，报道不能证明上涨原因。", "eastmoney:stock-news-search:fixture", "https://example.com/news", snapshot.CutoffAt.Add(-time.Minute), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, news)
	for i := 0; i < 16; i++ {
		snapshot.Sources = append(snapshot.Sources, NewResearchSource("announcement", fmt.Sprintf("一般风险公告%d", i), "公司存在经营现金流风险、减值风险与审批不确定性。", "fixture", fmt.Sprintf("https://example.com/risk/%d", i), snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt))
	}
	before, _ := json.Marshal(snapshot)
	outline := ResearchOutline{Questions: []ResearchQuestion{{Question: "境外发行是否有新进展", Query: "境外 发行 上市", SourceID: capital.ID, EvidenceSourceIDs: []string{capital.ID, ir.ID}, Status: "available"}}}
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		t.Run(string(level), func(t *testing.T) {
			request := ResearchRequest{AnalysisLevel: level}
			packs := []researchEvidencePack{buildResearchEvidencePack(snapshot, request, researchPromptOutline, nil), buildResearchCoreEvidencePack(snapshot, request, outline)}
			for _, pack := range packs {
				cards := map[string]researchEvidenceCard{}
				for _, card := range pack.Evidence {
					cards[card.ID] = card
				}
				for _, id := range []string{"m-price", "m-quote", "f-business", "m-sector", ir.ID} {
					if _, ok := cards[id]; !ok {
						t.Fatalf("%s lost essential source %s", pack.Phase, id)
					}
				}
				if !strings.Contains(cards[ir.ID].Text, "有望") || !strings.Contains(cards[ir.ID].Text, "不确定性") {
					t.Fatalf("IR excerpt lost operating facts or qualifications: %s", cards[ir.ID].Text)
				}
				if _, ok := cards[news.ID]; level != ResearchLevelQuick && !ok {
					t.Fatal("old capital/risk notices displaced recent news")
				}
				if !strings.Contains(cards["m-sector"].Text, "2026-09-30") || !strings.Contains(cards["m-sector"].Text, `"rising_count":3`) {
					t.Fatal("peer aggregates or matching dates were dropped")
				}
				policy := researchLevelPolicyFor(level)
				if pack.Stats.SelectedContentBytes > policy.MaxEvidenceBytes || len(pack.Evidence) > policy.MaxCards && level != ResearchLevelDeep {
					t.Fatal("evidence budget was enlarged")
				}
			}
		})
	}
	after, _ := json.Marshal(snapshot)
	if string(before) != string(after) {
		t.Fatal("compression changed the frozen snapshot")
	}
	if isResearchBusinessDisclosure(notice) {
		t.Fatal("a meeting invitation became primary operating evidence")
	}
}

func TestResearchPromptDistinguishesCollectedSourcesFromVisibleEvidence(t *testing.T) {
	outline := ResearchOutline{Questions: []ResearchQuestion{{Status: "available", EvidenceSourceIDs: []string{"shown", "omitted"}}, {Status: "retrieved", EvidenceSourceIDs: []string{"omitted"}}}}
	questions := researchQuestionsForPack(outline, researchEvidencePack{Evidence: []researchEvidenceCard{{ID: "shown"}}})
	if !reflect.DeepEqual(questions[0].EvidenceSourceIDs, []string{"shown"}) || questions[1].Status != "not_in_input" || !strings.Contains(questions[1].Outcome, "不代表未披露") {
		t.Fatalf("unseen body was presented as read evidence: %+v", questions)
	}
	if len(outline.Questions[0].EvidenceSourceIDs) != 2 || outline.Questions[1].Status != "retrieved" {
		t.Fatal("model view overwrote original retrieval results")
	}
}

func TestResearchTradeKeepsSourceUsedOnlyByTradingLogic(t *testing.T) {
	_, snapshot := researchFixture(t)
	ir := NewResearchSource("announcement", "投资者关系活动记录表", "公司新产品正进行客户验证，量产尚不确定，需要后续披露核实。", "fixture", "https://example.com/ir", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, ir)
	core := ResearchCoreSynthesis{Thesis: ResearchClaim{SourceIDs: []string{"m-price"}}, TradingLogic: &ResearchTradingLogic{Mainlines: []ResearchLogicItem{{Explanation: ResearchClaim{SourceIDs: []string{ir.ID}}}}}}
	pack := buildResearchTradeEvidencePack(snapshot, ResearchRequest{AnalysisLevel: ResearchLevelDeep}, ResearchOutline{}, core)
	for _, card := range pack.Evidence {
		if card.ID == ir.ID {
			if !strings.Contains(card.Text, "尚不确定") {
				t.Fatal("trade evidence lost the execution qualification")
			}
			return
		}
	}
	t.Fatal("a source cited only by trading logic was dropped")
}

func TestResearchRecentNewsGetsCoverageBeforeOldRiskHeavySummary(t *testing.T) {
	_, snapshot := researchFixture(t)
	old := NewResearchSource("news", "旧业绩报道", "产品、订单、现金流、减值和经营风险均需核实。", "eastmoney:stock-news-search:fixture", "https://example.com/old", snapshot.CutoffAt.Add(-30*24*time.Hour), snapshot.CapturedAt)
	latest := NewResearchSource("news", "近期产品进展", "产品仍在客户验证阶段。", "eastmoney:stock-news-search:fixture", "https://example.com/latest", snapshot.CutoffAt.Add(-time.Hour), snapshot.CapturedAt)
	recent := NewResearchSource("news", "近期机构调研", "机构关注后续经营兑现。", "eastmoney:stock-news-search:fixture", "https://example.com/recent", snapshot.CutoffAt.Add(-2*time.Hour), snapshot.CapturedAt)
	snapshot.Sources = append(snapshot.Sources, old, latest, recent)
	pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{AnalysisLevel: ResearchLevelDeep}, ResearchOutline{Questions: []ResearchQuestion{{Query: "现金流 减值 风险", EvidenceSourceIDs: []string{old.ID}}}})
	positions := map[string]int{}
	for i, card := range pack.Evidence {
		positions[card.ID] = i
	}
	for _, id := range []string{latest.ID, recent.ID} {
		position, ok := positions[id]
		if !ok {
			t.Fatal("recent news lost its reserved coverage")
		}
		if oldPosition, included := positions[old.ID]; included && oldPosition < position {
			t.Fatal("old keyword-heavy summary dominated recent coverage")
		}
	}
}

func TestResearchQuoteAfterCloseCarriesDailyCrossCheck(t *testing.T) {
	_, snapshot := researchFixture(t)
	snapshot.DailyBars = []AIDailyBar{{Date: "2026-09-30", Close: 96.8}}
	for _, tc := range []struct {
		stamp, context string
		daily          bool
	}{
		{"2026-09-30T15:34:59+08:00", "after_close_15h", true},
		{"2026-09-30T06:34:59Z", "before_close_15h", false},
		{"2026-09-29T15:34:59+08:00", "after_close_15h", false},
	} {
		source := ResearchSource{ID: "m-quote", Kind: "calculation", Content: fmt.Sprintf(`{"trade_time":%q,"price":96.8}`, tc.stamp)}
		card := researchSourceForLevel(source, snapshot, researchLevelPolicyFor(ResearchLevelDeep))
		var value map[string]any
		if err := json.Unmarshal([]byte(card.Content), &value); err != nil {
			t.Fatal(err)
		}
		_, hasDaily := value["same_date_daily_close"]
		if value["session_context"] != tc.context || hasDaily != tc.daily {
			t.Fatalf("incorrect session/date comparison: %s", card.Content)
		}
	}
}
