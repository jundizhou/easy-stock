package stockanalysis

import "testing"

// [ES_VALIDATION_DEGRADE_PATCH_V2] 契约测试：
// 1) 可无歧义移除的单条 condition 违规只降级（丢弃 + note），不再拖垮整份研究；
// 2) 但「伪造引用」类安全问题仍然硬失败——claim 来源/引文、失效编号、情景引用。
func TestResearchDegradesSingleBadConditionButKeepsSafetyBoundaries(t *testing.T) {
	_, snapshot := researchFixture(t)

	degradeCases := []struct {
		name   string
		modify func(*ResearchSynthesis)
	}{
		{"量比阈值越界", func(r *ResearchSynthesis) {
			threshold := 99.0
			r.Conditions = []ResearchCondition{{ID: "c1", Text: "观察量比", Metric: "volume_ratio", Operator: "gte", Window: "next_close", Threshold: &threshold, SourceIDs: []string{"m-price"}}}
		}},
		{"指标非法", func(r *ResearchSynthesis) {
			r.Conditions = []ResearchCondition{{ID: "c1", Text: "观察", Metric: "totally_unknown_metric", Operator: "gte", Window: "next_close", SourceIDs: []string{"m-price"}}}
		}},
		{"条件编号缺失", func(r *ResearchSynthesis) {
			r.Conditions[0].ID = ""
			r.InvalidationIDs = nil
		}},
		{"条件引用未知来源", func(r *ResearchSynthesis) {
			r.Conditions[0].SourceIDs = []string{"s-does-not-exist"}
		}},
	}
	for _, c := range degradeCases {
		result := validResearch()
		c.modify(&result)
		notes, err := validateResearch(&result, snapshot)
		if err != nil {
			t.Fatalf("%s 应降级而非失败，实际 err=%v", c.name, err)
		}
		if len(result.Conditions) != 0 {
			t.Fatalf("%s 应丢弃全部非法条件，实际保留 %d 条", c.name, len(result.Conditions))
		}
		if len(notes) == 0 {
			t.Fatalf("%s 降级必须留下 note 供用户审计", c.name)
		}
	}
}

// 别名归一：模型的锚点书写差异可被吸收，但编造的名称不被接受。
func TestResearchNormalizesAnchorAliasesOnly(t *testing.T) {
	_, snapshot := researchFixture(t)

	alias := validResearch()
	alias.Conditions[0].AnchorID = "MA_20"
	if _, err := validateResearch(&alias, snapshot); err != nil {
		t.Fatalf("已知锚点的别名应被归一：%v", err)
	}
	if alias.Conditions[0].AnchorID != "ma20" {
		t.Fatalf("别名未归一，实际=%q", alias.Conditions[0].AnchorID)
	}

	fabricated := validResearch()
	fabricated.Conditions[0].AnchorID = "invented_price"
	if _, err := validateResearch(&fabricated, snapshot); err == nil {
		t.Fatal("编造的 anchor_id 必须硬失败")
	}
}
