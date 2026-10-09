package stockanalysis

import (
	"sort"
	"strings"
	"time"
)

// These are disclosure stages, not interchangeable business outcomes. Keep the
// rule shared so portfolio aggregation cannot turn a routine caveat into a risk.
const EarningsDisclosurePolicy = `A股业绩口径：公司正式发布的业绩预告/业绩快报本身属于信息披露，不等于券商预测、市场传闻或未来订单。先按报告期与分析时点区分：已结束报告期的预告是对已发生经营结果的初步核算，可支持当期盈利及增长判断；尚未结束年度的经营展望、未来订单和量产预测仍有兑现条件，不能混为一谈。
已结束报告期的预告保留“公司预计/初步核算、未经审计、最终以定期报告为准”的口径，但不因这些常规说明或尚无正式季报而称“未兑现、待证伪、逻辑薄弱”，不据此降低评分/置信度、限制配置或列为优先处理。不能改写为已审计实绩，也不假定未来增长必然延续。
只有有来源的预告下修/更正、同口径数字冲突、明确重大核算或审计不确定性、经营反证等，才按其实际影响提高风险优先级；普通未经审计声明不构成异常。预告本身披露的亏损/下滑同样是经营证据，不能因为是预告就忽略。单票权重大不把普通披露进度升级为风险；集中度、现金流、估值、波动分别评价。定期报告发布属于常规跟踪，不作为处理已知风险的前置条件，不得让“先核实预告、再处理集中”取代真实优先事项。
输入已有预告正文时，先用正文核对期间和数字，不沿用旧报告“仅媒体标题、公告原文未取得”的说法。未取得正文仅说明资料限制；不把公司正式预告和尚未结束年度的展望一起列成反证。
`

func isResearchEarningsDisclosure(source ResearchSource) bool {
	return source.Kind == "announcement" && ResearchSourceHasBody(source) &&
		containsAnyFold(source.Title, "业绩预告", "业绩快报")
}

// Pick by publication time, so a correction replaces the older forecast. This
// selects evidence to read; it does not certify numbers or decide the outcome.
func LatestResearchEarningsDisclosure(sources []ResearchSource, cutoff time.Time) (ResearchSource, bool) {
	var latest ResearchSource
	found := false
	for _, source := range sources {
		if !isResearchEarningsDisclosure(source) || source.PublishedAt.IsZero() || (!cutoff.IsZero() && source.PublishedAt.After(cutoff)) {
			continue
		}
		if !found || source.PublishedAt.After(latest.PublishedAt) {
			latest, found = source, true
		}
	}
	return latest, found
}

// Financial clauses and their qualifications must survive product/IR keywords
// and boilerplate-heavy risk sections. The returned fragments remain verbatim.
func ResearchEarningsExcerpt(source ResearchSource, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]rune(source.Content)) <= limit {
		return source.Content
	}
	type fragment struct {
		text            string
		order, priority int
	}
	fragments := []fragment{}
	for i, text := range researchSentencePattern.FindAllString(source.Content, -1) {
		text = strings.TrimSpace(text)
		priority := 6
		switch {
		case containsAnyFold(text, "下修", "修正为", "更正为", "重大不确定", "不确定因素", "预计亏损"):
			priority = 0
		case containsAnyFold(text, "业绩预告期间", "业绩快报期间"):
			priority = 1
		case strings.Contains(text, "预计") && strings.Contains(text, "净利润") && !containsAnyFold(text, "扣除非经常", "扣非"):
			priority = 2
		case containsAnyFold(text, "未经", "初步核算", "以公司正式披露"):
			priority = 3
		case strings.Contains(text, "预计") && containsAnyFold(text, "净利润", "扣非"):
			priority = 4
		case strings.Contains(text, "预计") && containsAnyFold(text, "营业收入", "营收"):
			priority = 5
		}
		if text != "" {
			fragments = append(fragments, fragment{text, i, priority})
		}
	}
	sort.SliceStable(fragments, func(i, j int) bool { return fragments[i].priority < fragments[j].priority })
	selected := []fragment{}
	remaining := limit
	for _, part := range fragments {
		cost := len([]rune(part.text))
		if len(selected) > 0 {
			cost += 3
		}
		if cost <= remaining {
			selected = append(selected, part)
			remaining -= cost
		}
	}
	if len(selected) == 0 {
		return ResearchSourceExcerpt(source.Content, []string{"净利润", "下修", "重大不确定"}, limit)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].order < selected[j].order })
	parts := []string{}
	for _, part := range selected {
		parts = append(parts, part.text)
	}
	return strings.Join(parts, "\n…\n")
}
