package review

import (
	"fmt"
	"strings"
	"time"
)

// DailySummaryMarkdown exports all analysis sections, excluding article bodies,
// evidence/source details, and raw generation errors.
func DailySummaryMarkdown(job DailySummaryJob, r *DailySummary) string {
	var b strings.Builder
	heading := func(v string) { fmt.Fprintf(&b, "## %s\n\n", v) }
	field := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&b, "**%s**：%s\n\n", k, v)
		}
	}
	list := func(k string, vs []string) {
		if len(vs) > 0 {
			fmt.Fprintf(&b, "**%s**\n\n", k)
			for _, v := range vs {
				fmt.Fprintf(&b, "- %s\n", v)
			}
			b.WriteString("\n")
		}
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.In(shanghaiLocation()).Format("2006-01-02 15:04")
	}
	field("目标交易日", job.ScheduledTargetDate)
	status := "完成"
	if job.Status != "succeeded" {
		status = "未完成（以下为已生成结果）"
	}
	field("复盘状态", status)
	if r == nil {
		b.WriteString("复盘报告尚未生成，请在应用内检查文章同步与模型配置后重试。\n")
		return b.String()
	}
	field("复盘日期", r.TradeDate)
	field("生成时间（北京时间）", stamp(r.GeneratedAt))
	field("文章窗口（北京时间）", stamp(r.WindowStart)+" 至 "+stamp(r.WindowEnd))
	field("文章覆盖", fmt.Sprintf("%d 位作者 · %d 篇文章", r.AuthorCount, r.ArticleCount))
	list("作者", r.Authors)
	heading("综合判断")
	field("核心结论", r.ExecutiveSummary)
	field("市场状态", r.MarketRegime)
	field("市场分析", r.MarketAnalysis)
	field("情绪周期", r.MarketFramework.Cycle)
	field("资金定价", r.MarketFramework.CapitalPricing)
	field("方向竞争", r.MarketFramework.DirectionCompetition)
	field("交易方法", r.MarketFramework.TradingMethod)
	heading("美股与外部市场")
	field("综合解读", r.USMarketSummary)
	field("行情日期", r.USMarket.AsOf)
	field("采集时间", stamp(r.USMarket.CapturedAt))
	for _, v := range r.USMarket.Indexes {
		field(v.Name, fmt.Sprintf("%g · 涨跌 %+.2f%% · %s %s", v.Price, v.ChangePercent, stamp(v.TradeTime), v.Status))
	}
	for _, g := range []struct {
		label string
		items []DailyUSMarketSector
	}{{"领涨板块代理", r.USMarket.LeadingSectors}, {"领跌板块代理", r.USMarket.LaggingSectors}} {
		for _, v := range g.items {
			field(g.label, fmt.Sprintf("%s（%s）· %g · 涨跌 %+.2f%% · %s", v.Name, v.ProxySymbol, v.Price, v.ChangePercent, stamp(v.TradeTime)))
		}
	}
	heading("共识与分歧")
	for _, v := range r.Consensus {
		field("共识", v.Topic)
		field("判断", v.Conclusion)
		field("支持人数", fmt.Sprint(v.SupportCount))
		list("支持作者", v.Authors)
	}
	for _, v := range r.Disagreements {
		field("分歧", v.Topic)
		list("不同观点", v.Views)
		list("涉及作者", v.Authors)
		for _, p := range v.Positions {
			field(p.Author+" · "+p.Stance, p.View)
		}
	}
	heading("方向研判")
	for _, v := range r.Directions {
		field("方向", v.Name)
		field("态度", v.Stance)
		field("分析", v.Summary)
		list("支持作者", v.SupportingAuthors)
		list("反对作者", v.OpposingAuthors)
		list("涉及标的", v.Stocks)
		field("触发条件", v.Trigger)
		field("失效条件", v.Invalidation)
		list("风险", v.Risks)
	}
	stocks := func(label string, items []DailyStockView) {
		if len(items) == 0 {
			return
		}
		heading(label)
		for _, v := range items {
			field("标的", strings.TrimSpace(v.Name+" "+v.Symbol))
			field("逻辑", v.Logic)
			field("支持人数", fmt.Sprint(v.SupportCount))
			list("提及作者", v.Authors)
			field("触发条件", v.Trigger)
			field("失效条件", v.Invalidation)
			field("风险", v.Risk)
		}
	}
	stocks("今日超预期", r.TodaySurprises)
	stocks("明日关注", r.TomorrowFocus)
	heading("明日展望与情景")
	field("展望", r.TomorrowOutlook)
	if r.TomorrowPlanDegraded {
		field("计划状态", "明日计划未完整生成，请结合研究限制阅读")
	}
	for _, v := range r.Scenarios {
		field("情景", v.Name)
		field("情景判断", v.Summary)
		field("触发条件", v.Trigger)
		field("确认条件", v.Confirmation)
		field("失效条件", v.Invalidation)
		list("观察方向", v.Focus)
	}
	heading("交易观察计划")
	list("盘前", r.TomorrowPlaybook.PreOpen)
	list("开盘", r.TomorrowPlaybook.Opening)
	list("盘中", r.TomorrowPlaybook.Intraday)
	list("收盘", r.TomorrowPlaybook.Close)
	list("催化因素", r.Catalysts)
	list("主要风险", r.Risks)
	list("验证清单", r.VerificationChecklist)
	list("研究限制", r.Limitations)
	for _, v := range r.AuthorViews {
		heading("作者观点 · " + v.Author)
		field("平台", v.Source)
		field("文章覆盖", fmt.Sprintf("%d / %d 篇 · %s", v.ArticleCount, v.AvailableArticleCount, v.TimeRange))
		field("核心观点", v.CoreView)
		field("市场解读", v.MarketInterpretation)
		list("观点演变", v.ViewEvolution)
		list("关注题材", v.Themes)
		stocks("作者今日超预期", v.TodaySurprises)
		stocks("作者明日关注", v.TomorrowFocus)
		field("明日展望", v.TomorrowOutlook)
		list("催化因素", v.Catalysts)
		list("风险", v.Risks)
		field("置信度", v.Confidence)
	}
	b.WriteString("以上为完整复盘分析结果，省略原文、具体证据与来源明细。作者观点不代表事实或投资建议，仅用于研究与复盘。\n")
	return b.String()
}
