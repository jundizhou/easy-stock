package catalyst

import (
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func newsItem(id, title, content string) foundation.NewsItem {
	return foundation.NewsItem{
		ID:          id,
		Title:       title,
		Content:     content,
		PublishedAt: time.Date(2026, 9, 10, 10, 0, 0, 0, time.Local),
	}
}

func TestPreFilterDropsBroadcastNoise(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("1", "三大指数集体高开，沪指涨0.3%", "截至发稿，两市成交额突破5000亿，板块涨幅居前"),
		newsItem("2", "半导体板块异动拉升", "盘中多股异动拉升，市场情绪回暖"),
	}
	candidates, filtered := PreFilter(items, MaxCandidates)
	if len(candidates) != 0 {
		t.Fatalf("纯播报应被淘汰，实际留下 %d 条：%+v", len(candidates), candidates)
	}
	if filtered != 2 {
		t.Fatalf("filtered = %d, want 2", filtered)
	}
}

func TestPreFilterDropsRoutineAnnouncements(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("1", "某公司关于召开2026年第一次临时股东大会的通知", ""),
		newsItem("2", "某公司投资者关系活动记录表", "公司接待了多家机构调研"),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 0 {
		t.Fatalf("例行公告应被淘汰，实际留下 %d 条：%+v", len(candidates), candidates)
	}
}

func TestPreFilterKeepsAuthorityCatalyst(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("1", "摩根士丹利发布厄尔尼诺影响时间表", "预计影响时间为今年四季度至明年一季度，涉及农产品与电力"),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 1 {
		t.Fatalf("权威来源的时间表应保留，实际 %d 条", len(candidates))
	}
	if candidates[0].Score <= 0 {
		t.Fatalf("score 应为正，实际 %d", candidates[0].Score)
	}
	joined := strings.Join(candidates[0].Hints, "|")
	if !strings.Contains(joined, "权威来源") {
		t.Fatalf("应给出权威来源线索，实际 %v", candidates[0].Hints)
	}
}

func TestPreFilterKeepsConcreteCompanyAction(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("1", "芒果台首次播出AI制作的电视剧", "该剧全流程由AI生成，首播收视率破1"),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 1 {
		t.Fatalf("具体事实性动作应保留，实际 %d 条", len(candidates))
	}
	joined := strings.Join(candidates[0].Hints, "|")
	if !strings.Contains(joined, "事实性动作") {
		t.Fatalf("应给出事实性动作线索，实际 %v", candidates[0].Hints)
	}
}

func TestPreFilterKeepsStrongSignalInsideBroadcast(t *testing.T) {
	// 「涨停」既是播报词也是强信号，且带权威政策口径，不应被误杀。
	items := []foundation.NewsItem{
		newsItem("1", "发改委印发新型储能方案，相关个股涨停", ""),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 1 {
		t.Fatalf("带强信号的消息不应被播报规则误杀，实际 %d 条", len(candidates))
	}
}

func TestPreFilterRanksAuthorityAboveWeakOpinion(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("weak", "分析师表示某行业长期看好", "业内人士称有望成为主线"),
		newsItem("strong", "工信部批准首款国产车规级芯片量产", "已通过审批并开始投产"),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) < 2 {
		t.Fatalf("两条都应进入候选，实际 %d 条", len(candidates))
	}
	if candidates[0].Item.ID != "strong" {
		t.Fatalf("权威+量产应排在弱观点之前，实际首位 %s", candidates[0].Item.ID)
	}
}

func TestPreFilterKeepsAuthorityForecastWithTimeline(t *testing.T) {
	// 用户点名的典型形态：机构发布带时间表的预测。这条不放进来就是漏召回。
	items := []foundation.NewsItem{
		newsItem("1", "美国气象预报机构CPC表示，厄尔尼诺现象正在增强", "预计2026-27年北半球秋冬期间出现极强厄尔尼诺事件"),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 1 {
		t.Fatalf("机构+时间表预测应保留，实际 %d 条", len(candidates))
	}
	joined := strings.Join(candidates[0].Hints, "|")
	if !strings.Contains(joined, "含时间表/预测") {
		t.Fatalf("应给出时间表线索，实际 %v", candidates[0].Hints)
	}
}

func TestPreFilterDoesNotTreatBroadWordsAsAuthority(t *testing.T) {
	// 回归：曾经把「研究所/协会/联盟」当权威来源，导致长正文里随机命中，
	// 把与 A 股无关的国际社会新闻拉进候选。
	items := []foundation.NewsItem{
		newsItem("1", "荷兰已有3人因感染西尼罗病毒死亡", "据某研究所通报，报告称疫情仍在扩散"),
		newsItem("2", "苏丹首都喀土穆遭无人机袭击 致1死9伤", ""),
	}
	candidates, _ := PreFilter(items, MaxCandidates)
	if len(candidates) != 0 {
		t.Fatalf("无关国际社会新闻不应进入候选，实际 %d 条：%+v", len(candidates), candidates)
	}
}

func TestPreFilterKeepsMarketMovingOrganization(t *testing.T) {
	// 真正影响大宗与相关板块的机构必须保留。
	for _, title := range []string{
		"OPEC+同意延长减产协议至明年一季度",
		"国际能源署下调今年全球原油需求增速预期",
		"上期所调整部分品种交易保证金比例",
	} {
		candidates, _ := PreFilter([]foundation.NewsItem{newsItem("1", title, "")}, MaxCandidates)
		if len(candidates) != 1 {
			t.Fatalf("%q 应保留为候选", title)
		}
	}
}

func TestPreFilterRespectsLimit(t *testing.T) {
	items := make([]foundation.NewsItem, 0, 20)
	for index := 0; index < 20; index++ {
		items = append(items, newsItem(string(rune('a'+index)), "国家能源局批准新建储能项目并开工", ""))
	}
	candidates, _ := PreFilter(items, 5)
	if len(candidates) != 5 {
		t.Fatalf("应按 limit 裁剪，实际 %d 条", len(candidates))
	}
}

func TestEnrichFallsBackToRulesWhenSelectorMissing(t *testing.T) {
	items := []foundation.NewsItem{
		newsItem("1", "工信部批准首款国产车规级芯片量产", "已通过审批并开始投产"),
	}
	candidates, filtered := PreFilter(items, MaxCandidates)
	result := Enrich(candidates, filtered, foundation.SourceMeta{Source: "cls"}, nil, nil)
	if result.Meta.ModelUsed {
		t.Fatal("无模型时不应标记为已精筛")
	}
	if result.Meta.Note == "" {
		t.Fatal("降级时应给出说明")
	}
	if len(result.Items) != 1 {
		t.Fatalf("降级路径应输出规则高分项，实际 %d 条", len(result.Items))
	}
}

func TestEnrichDropsLowScoreCandidatesInFallback(t *testing.T) {
	// 仅含弱观点、分数为负的候选，在降级路径里不应被当成催化。
	candidates := []Candidate{{Item: newsItem("1", "机构认为该板块有望走强", ""), Score: 10}}
	result := Enrich(candidates, 0, foundation.SourceMeta{}, nil, nil)
	if len(result.Items) != 0 {
		t.Fatalf("低分候选不应输出，实际 %d 条", len(result.Items))
	}
}

func TestNormalizeItemsDropsUnknownImpactAndCapsList(t *testing.T) {
	items := []Item{
		{NewsItem: newsItem("1", "首位", ""), Impact: "bullish", Strength: 92, Sectors: []string{" 半导体 ", "半导体", ""}},
		{NewsItem: newsItem("2", "末位", ""), Impact: "不存在的方向", Strength: 41, Horizon: "whatever"},
	}
	cleaned := NormalizeItems(items)
	if len(cleaned) != 2 {
		t.Fatalf("两条都应保留，实际 %d", len(cleaned))
	}
	if cleaned[0].Title != "首位" {
		t.Fatalf("应按强度降序，首位为 %s", cleaned[0].Title)
	}
	if len(cleaned[0].Sectors) != 1 || cleaned[0].Sectors[0] != "半导体" {
		t.Fatalf("板块应去重去空，实际 %v", cleaned[0].Sectors)
	}
	if cleaned[1].Impact != "neutral" {
		t.Fatalf("非法方向应归一为 neutral，实际 %s", cleaned[1].Impact)
	}
	if cleaned[1].Horizon != "short" {
		t.Fatalf("非法时间尺度应归一为 short，实际 %s", cleaned[1].Horizon)
	}
}

func TestNormalizeItemsClampsOutOfRangeStrength(t *testing.T) {
	items := []Item{
		{NewsItem: newsItem("1", "负数", ""), Strength: -20},
		{NewsItem: newsItem("2", "越界", ""), Strength: 200},
	}
	cleaned := NormalizeItems(items)
	if len(cleaned) != 2 {
		t.Fatalf("两条都应保留，实际 %d", len(cleaned))
	}
	// 200 被裁到 100 后仍居首、-20 被抬到 0 后居末，说明裁剪先于排序生效。
	if cleaned[0].Strength != 100 {
		t.Fatalf("上界应裁到 100，实际 %d", cleaned[0].Strength)
	}
	if cleaned[1].Strength != 0 {
		t.Fatalf("下界应抬到 0，实际 %d", cleaned[1].Strength)
	}
}

func TestNormalizeItemsCapsToTen(t *testing.T) {
	items := make([]Item, 0, 15)
	for index := 0; index < 15; index++ {
		items = append(items, Item{NewsItem: newsItem(string(rune('a'+index)), "x", ""), Strength: index})
	}
	cleaned := NormalizeItems(items)
	if len(cleaned) != MaxItems {
		t.Fatalf("应裁到 %d 条，实际 %d", MaxItems, len(cleaned))
	}
}
