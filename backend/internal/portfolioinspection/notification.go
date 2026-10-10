package portfolioinspection

import (
	"fmt"
	"strings"
)

// NotificationMarkdown deliberately enumerates result fields. It never walks
// research sources, evidence references, raw facts, or provider error strings.
func NotificationMarkdown(job Job) string {
	var b strings.Builder
	field := func(label, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&b, "**%s**：%s\n\n", label, value)
		}
	}
	heading := func(value string) { fmt.Fprintf(&b, "**%s**\n\n", value) }
	list := func(label string, items []string) {
		if len(items) == 0 {
			return
		}
		heading(label)
		for i, item := range items {
			fmt.Fprintf(&b, "%d. %s\n", i+1, item)
		}
		b.WriteString("\n")
	}
	field("方案", firstNonEmpty(job.Request.PortfolioPlanName, "未命名方案"))
	field("任务", job.ID)
	status := "完成"
	if job.Status != "succeeded" {
		status = "未完成（以下为已生成结果）"
	}
	field("巡检状态", status)
	field("个股研究", fmt.Sprintf("完成 %d/%d 只 · 复用 %d 份 · 新研究 %d 份 · 共享 %d 份", job.CompletedStocks, job.TotalStocks, job.ReusedStocks, job.NewStocks, job.SharedStocks))
	if job.Report == nil {
		b.WriteString("组合报告尚未生成，已有个股报告已保留，请在应用中继续研究或重试组合评估。\n")
		return b.String()
	}
	r := job.Report
	ai := r.Conclusion
	if !r.GeneratedAt.IsZero() {
		field("报告时间", r.GeneratedAt.In(scheduleZone).Format("2006-01-02 15:04")+"（北京时间）")
	}
	field("交易风格", r.Profile.Label)
	horizon := map[string]string{"short": "超短", "swing": "波段", "medium": "中期"}[job.Request.Horizon]
	field("持有周期", horizon)
	score := "待完成"
	if ai.ScoreAvailable && ai.TotalScore != nil {
		score = fmt.Sprintf("%d / 100", *ai.TotalScore)
	}
	field("巡检综合评分", score)
	field("风险等级", ai.RiskLevel)
	field("组合结论", ai.ExecutiveSummary)
	field("风险判断", ai.RiskReason)
	field("策略匹配", ai.StyleMatch)
	field("判断置信度", ai.ConfidenceLevel)
	field("置信度说明", ai.ConfidenceReason)
	if r.AlgorithmVersion == AlgorithmVersion {
		b.WriteString("评分只评价股票组合，总仓位与现金比例不加扣分；集中度按股票内部配比衡量。\n\n")
	}
	if len(ai.Dimensions) > 0 {
		heading("四维评分")
	}
	labels := map[string]string{"holding_logic": "持仓逻辑质量", "portfolio_structure": "组合结构合理性", "risk_capacity": "风险管理质量", "strategy_fit": "策略匹配度"}
	for _, d := range ai.Dimensions {
		score := "待完成"
		if d.Score != nil {
			score = fmt.Sprintf("%d", *d.Score)
		}
		heading(fmt.Sprintf("%s：%s 分 · 权重 %d%%", firstNonEmpty(d.Label, labels[d.Key], d.Key), score, d.Weight))
		field("评价", d.Reason)
		for _, a := range d.Adjustments {
			field("评分调整", fmt.Sprintf("%+d 分 · %s", a.Points, a.Reason))
		}
		list("维度限制", d.Limitations)
	}
	list("处理顺序", ai.AdjustmentOrder)
	list("主要风险", ai.PrimaryRisks)
	heading("逐股持仓判断")
	holdings := r.Holdings
	if len(holdings) == 0 {
		holdings = job.Results
	}
	names := map[string]string{}
	conclusions := map[string]HoldingConclusion{}
	for _, h := range ai.Holdings {
		conclusions[h.Symbol] = h
	}
	for _, result := range holdings {
		h := result.Holding
		name := firstNonEmpty(h.Name, h.Symbol)
		names[h.Symbol] = name
		heading(name + "（" + h.Symbol + "）")
		field("仓位", fmt.Sprintf("%d%%", h.Weight))
		if h.CostPrice != nil {
			field("持仓成本", fmt.Sprintf("%g", *h.CostPrice))
		} else {
			field("持仓成本", "未填写")
		}
		for _, f := range []struct{ key, label, suffix string }{{"price", "报告行情价格", ""}, {"pnl_percent", "成本盈亏", "%"}} {
			fact, ok := r.Facts[h.Symbol+"."+f.key]
			if ok && fact.Available {
				field(f.label, fmt.Sprintf("%v%s", fact.Value, f.suffix))
			}
		}
		if c, ok := conclusions[h.Symbol]; ok {
			field("组合角色", c.PortfolioRole)
			field("处理优先级", c.ActionPriority)
			field("判断", c.Conclusion)
			field("动作", c.Action)
			field("确认条件", c.Confirmation)
			field("失效条件", c.Invalidation)
		} else {
			field("判断", "组合逐股判断尚未生成，请在应用中查看已完成的个股研究")
		}
	}
	list("组合结构与联动", ai.ConcentrationFinding)
	for _, group := range ai.RiskGroups {
		members := []string{}
		for _, symbol := range group.Symbols {
			members = append(members, firstNonEmpty(names[symbol], symbol))
		}
		heading(fmt.Sprintf("风险分组：%s · 仓位 %d%%", group.Name, group.Weight))
		field("涉及持仓", strings.Join(members, "、"))
		field("分组判断", group.Reason)
	}
	if len(ai.RiskGroups) > 1 {
		b.WriteString("风险分组可能重叠，占比不可直接相加。\n\n")
	}
	if len(ai.Scenarios) > 0 {
		heading("组合情景")
	}
	for _, scenario := range ai.Scenarios {
		heading(scenario.Name)
		field("触发条件", scenario.Condition)
		field("组合动作", scenario.PortfolioAction)
	}
	list("下次检查", ai.NextChecklist)
	heading("组合概况与研究限制")
	field("组合配置", fmt.Sprintf("持仓 %d%% · 现金 %d%% · AI 研究覆盖 %g%%", r.Metrics.TotalPositionPercent, r.Metrics.CashPercent, r.Metrics.AIResearchCoveragePercent))
	if r.Metrics.StopLossCoveragePercent > 0 {
		field("静态止损预算", fmt.Sprintf("总资产损失估算 %.2f%% · 覆盖 %g%% 持仓，仅代表已知部分，不含跳空与滑点", r.Metrics.StopLossRiskPercent, r.Metrics.StopLossCoveragePercent))
	} else {
		field("静态止损预算", "未设置有效静态止损方案，无法估算；不代表零风险")
	}
	list("数据限制", ai.DataLimitations)
	b.WriteString("以上为巡检结果，省略具体证据与来源明细。评分不代表胜率或预期收益，仅用于研究与复盘。\n")
	return b.String()
}
