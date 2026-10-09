package stockanalysis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func researchReboundLines(symbol string, falling bool) []foundation.KLine {
	lines := []foundation.KLine{}
	for i := 0; i < ResearchPeerDailyBars; i++ {
		price := 100 - float64(i)*.4
		if i > 115 {
			step := 2.0
			if falling {
				step = -2
			}
			price = 54 + float64(i-115)*step
		}
		lines = append(lines, foundation.KLine{Symbol: symbol, Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i), Open: price, High: price + 1, Low: price - 1, Close: price, Volume: 1000, Meta: foundation.SourceMeta{Source: "peer-fixture"}})
	}
	return lines
}

func TestResearchPeerSelectionDoesNotPickOnlyWinnersOrGenericConcepts(t *testing.T) {
	input := Input{Symbol: "002074.SZ", Industry: "电池", Concepts: []string{"锂电池", "深股通"}}
	for i := 0; i < 12; i++ {
		input.Catalog = append(input.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("002%03d.SZ", 100+i), Name: fmt.Sprintf("同业%d", i), ChangePercent: float64(i)}, Industry: "电池", Concepts: []string{"锂电池"}})
	}
	input.Catalog = append(input.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: "002074.SZ"}, Industry: "电池"}, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: "600001.SH"}, Industry: "运输", Concepts: []string{"深股通"}}, input.Catalog[0])
	selection := SelectResearchPeers(input, ResearchPeerLimit)
	if len(selection.Members) != 12 || selection.CandidateCount != 12 || selection.Basis != "行业：电池" {
		t.Fatalf("unexpected cohort: %+v", selection)
	}
	if selection.Members[0].Symbol != "002100.SZ" || selection.Members[11].Symbol != "002111.SZ" {
		t.Fatal("sampling lost weak or strong members")
	}
	for i := range input.Catalog {
		input.Catalog[i].ChangePercent = -input.Catalog[i].ChangePercent
	}
	for i, j := 0, len(input.Catalog)-1; i < j; i, j = i+1, j-1 {
		input.Catalog[i], input.Catalog[j] = input.Catalog[j], input.Catalog[i]
	}
	if other := SelectResearchPeers(input, ResearchPeerLimit); !reflect.DeepEqual(selection, other) {
		t.Fatalf("quote ranks or catalog order changed peer selection: %+v", other)
	}
	input.Industry = ""
	if other := SelectResearchPeers(input, ResearchPeerLimit); other.Basis != "概念目录：锂电池" || other.CandidateCount != 12 {
		t.Fatalf("business-concept fallback missing: %+v", other)
	}
}

func TestResearchPeerSelectionPrefersBatteryConceptOverBroadPowerEquipment(t *testing.T) {
	input := Input{Symbol: "002074.SZ", Industry: "电源设备", Business: "储能设备", BusinessDetail: "主营磷酸铁锂材料及电芯、动力电池组及储能电池组", Concepts: []string{"锂电池概念", "储能概念", "低空经济", "深股通"}}
	for i := 0; i < 40; i++ {
		concepts := []string{"储能概念"}
		if i < 30 {
			concepts = append(concepts, "锂电池概念")
		}
		input.Catalog = append(input.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("002%03d.SZ", 100+i)}, Industry: "电源设备", Concepts: concepts})
	}
	// Concept membership alone must not pull automotive/software firms into
	// the target's battery/power-equipment comparison cohort.
	for i := 0; i < 12; i++ {
		input.Catalog = append(input.Catalog, foundation.StockCatalogEntry{BoardStock: foundation.BoardStock{Symbol: fmt.Sprintf("600%03d.SH", 100+i)}, Industry: "汽车整车", Concepts: []string{"锂电池概念"}})
	}
	selected := SelectResearchPeers(input, ResearchPeerLimit)
	if selected.Basis != "行业概念：电源设备 / 锂电池" || selected.SelectionScope != "industry_business_concept_intersection" || selected.CandidateCount != 30 || len(selected.Members) != 24 {
		t.Fatalf("broad industry displaced the primary battery cohort: %+v", selected)
	}
	for _, peer := range selected.Members {
		if peer.Symbol >= "002130.SZ" {
			t.Fatalf("non-battery concept peer included: %+v", peer)
		}
	}
	input.Concepts = []string{"低空经济", "深股通"}
	if fallback := SelectResearchPeers(input, ResearchPeerLimit); fallback.SelectionScope != "broad_industry_fallback" || fallback.CandidateCount != 40 {
		t.Fatalf("broad fallback lost its explicit scope: %+v", fallback)
	}
}

func TestResearchReturnWindowsMatchActualGuoxuanSnapshotAcrossEvidenceLevels(t *testing.T) {
	// The former m-price summary used 9/23 instead of 9/22 as the 5d base,
	// producing 8.14% while the same-date peer module produced 6.38%.
	closes := []float64{27.73, 27.28, 26.76, 26.07, 28.68, 29.5}
	input := Input{Symbol: "002074.SZ", Industry: "电池"}
	for i, close := range closes {
		day := []int{22, 23, 24, 28, 29, 30}[i]
		input.KLines = append(input.KLines, foundation.KLine{Symbol: input.Symbol, Time: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC), Open: close, High: close, Low: close, Close: close, Volume: 100})
	}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := BuildResearchSnapshot(input, analysis, input.KLines[5].Time.Add(time.Hour))
	context := researchMarketContext(input, snapshot.CutoffAt)
	returns := context["stock_returns_percent"].(map[string]float64)
	if returns["5d"] != 6.38 || returns["2d"] != 13.16 || round2(windowReturn(closes, 5)) != 6.38 {
		t.Fatalf("N+1 closes not used consistently: %v", returns)
	}
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		pack := buildResearchCoreEvidencePack(snapshot, ResearchRequest{AnalysisLevel: level}, ResearchOutline{})
		found := false
		for _, card := range pack.Evidence {
			if card.ID != "m-price" {
				continue
			}
			var data struct {
				Summary struct {
					Returns map[string]float64 `json:"window_returns_percent"`
				} `json:"summary"`
			}
			if err := json.Unmarshal([]byte(card.Text), &data); err != nil {
				t.Fatal(err)
			}
			if data.Summary.Returns["5d"] != returns["5d"] || data.Summary.Returns["2d"] != returns["2d"] {
				t.Fatalf("%s model evidence has inconsistent returns: %+v", level, data)
			}
			if _, exists := data.Summary.Returns["20d"]; exists {
				t.Fatal("incomplete return window presented as 20d")
			}
			found = true
		}
		if !found {
			t.Fatalf("%s dropped the price evidence", level)
		}
	}
}

func TestResearchRecentReboundIsNotNegatedByFiveDayDecline(t *testing.T) {
	input, _, snapshot := researchMarketFixture(t)
	values := []float64{100, 97, 95, 90, 91, 93}
	for i, close := range values {
		input.KLines[len(input.KLines)-len(values)+i].Close = close
		for index := range input.ResearchPeers.Members {
			value := close
			if index == 2 && i >= 4 {
				value = 90 - float64(i-3)
			}
			bars := input.ResearchPeers.Members[index].KLines
			bars[len(bars)-len(values)+i].Close = value
		}
	}
	context := researchMarketContext(input, snapshot.CutoffAt)
	windows := context["windows"].(map[string]researchPeerWindow)
	if windows["5d"].RisingCount != 0 || windows["2d"].RisingCount != 5 || windows["2d"].SampleSize != 6 {
		t.Fatalf("failed to distinguish event and cumulative windows: %+v", windows)
	}
	recent := context["recent_move_window"].(researchMoveWindow)
	if recent.Window != "2d" || recent.StartDate != windows["2d"].StartDate || recent.BaseDate != windows["2d"].BaseDate || recent.EndDate != windows["2d"].EndDate {
		t.Fatalf("recent move and peer comparison use different dates: %+v %+v", recent, windows["2d"])
	}
	if !strings.Contains(context["window_interpretation"].(string), "最近2日5/6上涨，近5日0/6上涨") {
		t.Fatal("lost the distinction between recent rebound and five-day decline")
	}
}

func TestResearchThemeNodeScopeAndCarryForwardSurviveAllPromptLevels(t *testing.T) {
	input, analysis, snapshot := researchMarketFixture(t)
	input.Themes = []foundation.ThemeOverview{{Name: "锂电池", ChangePercent: 10.01, TradeDate: snapshot.CutoffAt.Format("2006-01-02"), CarryForward: true, RisingNodes: 5, TotalNodes: 5}}
	snapshot = BuildResearchSnapshot(input, analysis, snapshot.CutoffAt)
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		policy := researchLevelPolicyFor(level)
		for _, source := range snapshot.Sources {
			if source.ID != "m-sector" && source.ID != "m-themes" {
				continue
			}
			card := compressResearchSource(researchSourceForLevel(source, snapshot, policy), nil, researchPromptSynthesis, policy)
			for _, required := range []string{`"metric_scope":"theme_nodes_not_sector_index"`, `"data_status":"carried_forward_nodes"`, `"carry_forward":true`, `"usable_for_current_move":false`, `"node_change_percent":10.01`} {
				if !strings.Contains(card.Text, required) {
					t.Fatalf("%s %s lost node provenance %s: %s", level, source.ID, required, card.Text)
				}
			}
			if strings.Contains(card.Text, `"change_percent":10.01`) {
				t.Fatal("node return exposed as an unqualified sector return")
			}
		}
	}
}

func researchMarketFixture(t *testing.T) (Input, Analysis, ResearchSnapshot) {
	t.Helper()
	input := Input{Symbol: "002074.SZ", Industry: "电池", Concepts: []string{"锂电池"}, Business: "电池生产", KLines: researchReboundLines("002074.SZ", false), ResearchPeers: ResearchPeerGroup{Basis: "行业：电池", CandidateCount: 20}}
	cutoff := input.KLines[len(input.KLines)-1].Time.Add(18 * time.Hour)
	for i := 0; i < 13; i++ {
		input.Themes = append(input.Themes, foundation.ThemeOverview{Name: fmt.Sprintf("无关题材%d", i), TrendScore: 99 - i})
	}
	input.Themes = append(input.Themes, foundation.ThemeOverview{Name: "锂电池", TrendScore: 20, ChangePercent: 4.5, TradeDate: cutoff.Format("2006-01-02"), Source: "theme-fixture", RisingNodes: 30, FallingNodes: 5})
	for i := 0; i < 6; i++ {
		symbol := fmt.Sprintf("002%03d.SZ", 100+i)
		input.ResearchPeers.Members = append(input.ResearchPeers.Members, ResearchPeerInput{Symbol: symbol, Name: fmt.Sprintf("同业%d", i), KLines: researchReboundLines(symbol, i == 2)})
	}
	analysis, err := Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	return input, analysis, BuildResearchSnapshot(input, analysis, cutoff)
}

func TestResearchRelatedSectorSurvivesGlobalTopTwelve(t *testing.T) {
	_, _, snapshot := researchMarketFixture(t)
	for _, id := range []string{"m-themes", "m-sector"} {
		found := false
		for _, source := range snapshot.Sources {
			if source.ID == id {
				found = strings.Contains(source.Content, "锂电池")
			}
		}
		if !found {
			t.Fatalf("relevant low-ranked sector dropped from %s", id)
		}
	}
}

func TestResearchPeerWindowsExcludeStaleSuspendedAndFutureBars(t *testing.T) {
	input, _, snapshot := researchMarketFixture(t)
	input.ResearchPeers.Members = input.ResearchPeers.Members[:5]
	input.ResearchPeers.Members[3].KLines = input.ResearchPeers.Members[3].KLines[:120]
	missing := input.ResearchPeers.Members[4].KLines
	input.ResearchPeers.Members[4].KLines = append(append([]foundation.KLine{}, missing[:119]...), missing[120:]...)
	future := input.ResearchPeers.Members[0].KLines[120]
	future.Time = future.Time.Add(24 * time.Hour)
	future.Close = 999
	input.ResearchPeers.Members[0].KLines = append(input.ResearchPeers.Members[0].KLines, future)
	context := researchMarketContext(input, snapshot.CutoffAt)
	windows := context["windows"].(map[string]researchPeerWindow)
	if item := windows["2d"]; item.SampleSize != 3 || item.RisingCount != 2 || item.MeanReturn != 1.67 || item.StockExcess != 5 {
		t.Fatalf("same-date peer window comparison incorrect: %+v", item)
	}
	if context["prior_drawdown_sample_size"] != 3 || context["prior_drawdown_20pct_and_5d_rebound_count"] != 2 {
		t.Fatalf("rebound sample has stale, missing or future observations: %+v", context)
	}
	for _, peer := range context["peers"].([]researchPeerPerformance) {
		for _, bar := range peer.Bars {
			if bar.Date > snapshot.CutoffAt.Format("2006-01-02") || bar.Close == 999 {
				t.Fatal("future bar entered persisted market evidence")
			}
		}
	}
	input.Themes = append(input.Themes, foundation.ThemeOverview{Name: "电池", TradeDate: snapshot.CutoffAt.Add(48 * time.Hour).Format("2006-01-02")})
	for _, theme := range researchMarketContext(input, snapshot.CutoffAt)["related_themes"].([]map[string]any) {
		if theme["name"] == "电池" {
			t.Fatal("future sector snapshot included")
		}
	}
}

func TestResearchMarketEvidenceIsRetainedWithinEveryLevelBudget(t *testing.T) {
	input, analysis, snapshot := researchMarketFixture(t)
	input.Fundamentals = &foundation.StockFundamentals{ReportDate: snapshot.CutoffAt.AddDate(0, -1, 0).Format("2006-01-02"), Revenue: 1e10, NetProfit: 1e9, DeductedNetProfit: 1e8, DeductedNetProfitAvailable: true, RevenueYearOverYear: 13.3, NetProfitYearOverYear: 23.3, ROE: 18, GrossMargin: 34, DebtRatio: 53, EPS: 1.3, OperatingCashFlowPerShare: 3.3}
	snapshot = BuildResearchSnapshot(input, analysis, snapshot.CutoffAt)
	for i := 0; i < 20; i++ {
		snapshot.Sources = append(snapshot.Sources, NewResearchSource("announcement", fmt.Sprintf("公告%d", i), strings.Repeat("公司项目合作与订单进展需要核实，存在经营风险。", 150), "fixture", "", snapshot.CutoffAt.Add(-time.Hour), snapshot.CutoffAt))
	}
	for _, level := range []ResearchLevel{ResearchLevelQuick, ResearchLevelStandard, ResearchLevelDeep} {
		request := ResearchRequest{AnalysisLevel: level}
		pack := buildResearchCoreEvidencePack(snapshot, request, ResearchOutline{})
		ids := map[string]bool{}
		for _, card := range pack.Evidence {
			ids[card.ID] = true
			if card.ID == "m-sector" {
				if strings.Contains(card.Text, "daily_bars") || !strings.Contains(card.Text, "锂电池") || !strings.Contains(card.Text, "rising_count") {
					t.Fatalf("%s market card lost comparisons or retained raw peer rows: %s", level, card.Text)
				}
				var data map[string]any
				if json.Unmarshal([]byte(card.Text), &data) != nil {
					t.Fatalf("%s invalid market JSON", level)
				}
				t.Logf("%s market card: %d bytes, %v displayed peers", level, len(card.Text), data["displayed_peer_count"])
			}
		}
		if !ids["m-sector"] || !ids["m-price"] || !ids["f-financial"] {
			for _, source := range snapshot.Sources {
				if source.ID == "m-sector" || source.ID == "m-price" || source.ID == "f-financial" {
					card := compressResearchSource(researchSourceForLevel(source, snapshot, researchLevelPolicyFor(level)), nil, researchPromptSynthesis, researchLevelPolicyFor(level))
					t.Logf("%s %s: %d bytes", level, source.ID, len(card.Text))
				}
			}
			t.Fatalf("%s let news displace market or financial evidence: %v", level, ids)
		}
		if pack.Stats.SelectedContentBytes > researchLevelPolicyFor(level).MaxEvidenceBytes {
			t.Fatalf("%s evidence budget exceeded", level)
		}
		core := ResearchCoreSynthesis{Thesis: ResearchClaim{SourceIDs: []string{"m-sector", "m-price"}}}
		trade := buildResearchTradeEvidencePack(snapshot, request, ResearchOutline{}, core)
		retained := false
		for _, card := range trade.Evidence {
			retained = retained || card.ID == "m-sector"
		}
		if !retained {
			t.Fatalf("%s trade conditions dropped the thesis's sector evidence", level)
		}
	}
	outline := ResearchOutlinePrompt(snapshot, ResearchRequest{})
	core := researchCorePromptWithPack(snapshot, ResearchRequest{}, ResearchOutline{}, buildResearchCoreEvidencePack(snapshot, ResearchRequest{}, ResearchOutline{}))
	if !strings.Contains(outline, "板块共同反弹/超跌修复") || !strings.Contains(core, "不能只围绕公告归因") || !strings.Contains(core, "m-sector") {
		t.Fatal("research does not compare sector rebound and company events")
	}
}
