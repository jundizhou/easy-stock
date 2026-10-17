package portfoliooptimization

// ResultStatus separates historical visibility from eligibility under today's
// acceptance rules. It is recomputed on reads, never trusted from stored JSON.
type ResultStatus struct {
	Viewable   bool     `json:"viewable"`
	Applicable bool     `json:"applicable"`
	Reasons    []string `json:"reasons"`
}

func resultStatus(j Job) ResultStatus {
	s := ResultStatus{Reasons: []string{}}
	if j.SelectedPlan == nil || *j.SelectedPlan < 0 || *j.SelectedPlan >= len(j.Plans) {
		s.Reasons = append(s.Reasons, "没有已选定的目标方案")
		return s
	}
	s.Viewable = true
	if j.Status != "succeeded" {
		s.Reasons = append(s.Reasons, "优化尚未完成")
	}
	if j.Version != Version || j.ModelPromptVersion != ModelPromptVersion {
		s.Reasons = append(s.Reasons, "历史方案仍可查看；应用前须按当前规则重新评估")
	}
	p := j.Plans[*j.SelectedPlan]
	if p.Status != "conditional" && p.Status != "accepted" {
		s.Reasons = append(s.Reasons, "目标方案未通过采纳校验")
	}
	s.Reasons = append(s.Reasons, acceptanceReasons(j, p)...)
	s.Applicable = len(s.Reasons) == 0
	return s
}
