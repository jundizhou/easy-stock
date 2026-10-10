package stockanalysis

import (
	"fmt"
	"strings"
	"time"
)

// NotificationMarkdown includes result fields, never source bodies, quotes,
// provider errors, model attempts, or credentials.
func NotificationMarkdown(job ResearchJob) string {
	var b strings.Builder
	field := func(label, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&b, "**%s**：%s\n\n", label, value)
		}
	}
	list := func(label string, values []string) {
		if len(values) > 0 {
			fmt.Fprintf(&b, "**%s**\n\n", label)
			for _, v := range values {
				if strings.TrimSpace(v) != "" {
					fmt.Fprintf(&b, "- %s\n", v)
				}
			}
			b.WriteString("\n")
		}
	}
	claims := func(label string, values []ResearchClaim) {
		items := make([]string, 0, len(values))
		for _, v := range values {
			items = append(items, v.Text)
		}
		list(label, items)
	}
	field("股票", job.Request.Symbol)
	field("任务", job.ID)
	status := "完成"
	if job.Status != "succeeded" {
		status = "未完成（以下为已生成结果）"
	}
	field("研究状态", status)
	a := job.Analysis
	if a == nil {
		b.WriteString("研究报告尚未生成，请在应用中查看任务并继续研究或重试。\n")
		return b.String()
	}
	field("名称", a.Name)
	r := a.ResearchReport
	if r == nil {
		field("阶段结论", a.Conclusion.Headline)
		field("阶段摘要", a.Conclusion.Summary)
		field("动作", a.Conclusion.Action)
		field("观察路径", a.Conclusion.BestPath)
		field("主要风险", a.Conclusion.MainRisk)
		list("风险提示", a.Risks)
		b.WriteString("AI 研究报告尚未生成，以上为已保存的分析结果。\n")
		return b.String()
	}
	if !r.GeneratedAt.IsZero() {
		field("报告时间", r.GeneratedAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")+"（北京时间）")
	}
	field("研究结论", r.Headline)
	field("核心判断", r.Thesis.Text)
	claims("支持判断", r.Support)
	claims("反向判断", r.Counter)
	claims("替代解释", r.Alternatives)
	field("主要分歧", r.MainConflict)
	field("证据充分度", r.EvidenceLevel)
	list("充分度说明", r.EvidenceReasons)
	if logic := r.TradingLogic; logic != nil {
		if logic.Business != nil {
			field("主营业务", logic.Business.Text)
		}
		for _, group := range []struct {
			label string
			items []ResearchLogicItem
		}{{"交易主线", logic.Mainlines}, {"次要逻辑", logic.Secondary}} {
			for _, item := range group.items {
				field(group.label, item.Name)
				field("逻辑判断", item.Explanation.Text)
				field("市场状态", item.MarketStatus)
				field("逻辑充分度", item.EvidenceLevel)
				list("逻辑缺口", item.Gaps)
			}
		}
		claims("催化因素", logic.Catalysts)
		list("交易逻辑限制", logic.Gaps)
	}
	invalid := map[string]bool{}
	for _, id := range r.InvalidationIDs {
		invalid[id] = true
	}
	conditions := map[string]string{}
	for _, c := range r.Conditions {
		conditions[c.ID] = c.Text
		label := "观察条件"
		if invalid[c.ID] {
			label = "失效条件"
		}
		field(label, c.Text)
		field("条件状态", c.Status)
		if c.Threshold != nil {
			field("条件阈值", fmt.Sprintf("%s %s %g", c.Metric, c.Operator, *c.Threshold))
		}
		field("观察窗口", c.Window)
	}
	for _, scenario := range r.Scenarios {
		field("情景", scenario.Name)
		field("情景假设", scenario.Description)
		for _, id := range scenario.ConditionIDs {
			field("触发条件", conditions[id])
		}
		field("应对", scenario.Response)
	}
	d := r.Decision
	field("计划状态", d.Status)
	field("新仓计划", d.NewPosition)
	field("已有仓位", d.ExistingPosition)
	field("计划说明", d.Reason)
	list("计划限制", d.Blockers)
	if p := d.PricePlan; p != nil {
		anchors := map[string]PriceAnchor{}
		for _, a := range r.Anchors {
			anchors[a.ID] = a
		}
		for _, v := range []struct{ label, id string }{{"入场参考", p.EntryAnchor}, {"止损参考", p.StopAnchor}, {"目标参考", p.TargetAnchor}} {
			if a, ok := anchors[v.id]; ok {
				field(v.label, fmt.Sprintf("%s · %g", a.Label, a.Price))
			}
		}
		field("价格计划说明", p.Reason)
	}
	field("与量化基线的关系", r.BaselineRelation)
	field("基线差异说明", r.BaselineReason)
	list("研究限制", r.Limitations)
	list("风险提示", a.Risks)
	b.WriteString("以上为研究结果，省略具体证据与来源明细，仅用于研究与复盘。\n")
	return b.String()
}
