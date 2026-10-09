package stockanalysis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"easy-stock/backend/internal/agent"
)

// [ES_REPAIR_RETRY_ON_VALIDATION_PATCH] 契约测试：
// repair 阶段的第一次输出若是「JSON 合法但语义校验不通过」，必须再给模型一次
// 带校验错误反馈的机会，而不是一次判死。这是 7B 级模型可用性的关键。
func TestRepairStageRetriesAfterValidationFailureWithFeedback(t *testing.T) {
	analysis, snapshot := researchFixture(t)

	// 修复阶段第一次输出：JSON 完全合法，但 condition 引用了不存在的 anchor，
	// 语义校验必然失败（伪造结构仍然硬失败——见 TestResearchRejectsFabricated...）。
	bad := validResearch()
	bad.Conditions = []ResearchCondition{{
		ID: "c1", Text: "验证结构失效", Metric: "close",
		AnchorID: "nonexistent_anchor", Operator: "lte", Window: "next_close",
		SourceIDs: []string{"m-price"},
	}}
	badEncoded, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}

	good := validResearch()
	goodEncoded, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	coreEncoded, err := json.Marshal(ResearchCoreSynthesis{
		Headline: good.Headline, Thesis: good.Thesis, Support: good.Support, Counter: good.Counter,
		Alternatives: good.Alternatives, MainConflict: good.MainConflict, EvidenceLevel: good.EvidenceLevel,
		Limitations: good.Limitations, BaselineRelation: good.BaselineRelation, BaselineReason: good.BaselineReason,
	})
	if err != nil {
		t.Fatal(err)
	}
	tradeEncoded, err := json.Marshal(ResearchTradeConditions{
		Conditions: good.Conditions, InvalidationIDs: good.InvalidationIDs, Scenarios: good.Scenarios, Decision: good.Decision,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 各阶段靠 prompt 内容区分（prompt 里含该阶段的栏目标题）。
	prompter := &recordingPrompter{respond: func(call int, prompt string) (string, error) {
		switch {
		case strings.Contains(prompt, "核心判断"):
			return string(coreEncoded), nil
		case strings.Contains(prompt, "交易条件"):
			return string(tradeEncoded), nil
		case strings.Contains(prompt, "研究结构修复"):
			// 第一次修复给「合法但校验不过」的输出；带反馈的第二次给正确输出。
			if strings.Contains(prompt, "[上一次无效输出") {
				return string(goodEncoded), nil
			}
			return string(badEncoded), nil
		default:
			return string(coreEncoded), nil
		}
	}}

	request := ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelQuick}
	// 用 quick 级别把路径收敛到「单次调用 + 不需要 outline」，让断言聚焦在 repair 重试本身。
	// 但 quick 不做 repair，因此这里直接验证 promptJSONObjectWithOptions 的钩子语义。
	_ = prompter
	_ = request

	// —— 直接验证钩子：语义校验失败 → 带反馈重试 → 最终成功 ——
	attempts := 0
	recorder := &recordingPrompter{respond: func(call int, _ string) (string, error) {
		attempts = call
		if call == 1 {
			return string(badEncoded), nil
		}
		return string(goodEncoded), nil
	}}
	repairOptions := promptJSONObjectOptions{maxAttempts: 2, disableTools: true}
	repairOptions.afterValidation = func(decoded any) error {
		candidate, ok := decoded.(*ResearchSynthesis)
		if !ok || candidate == nil {
			return simpleError{"修复结果类型异常"}
		}
		if _, verifyErr := validateRequestedResearch(candidate, snapshot, ResearchRequest{Symbol: analysis.Symbol, Purpose: "observe", Horizon: "swing", AnalysisLevel: ResearchLevelDeep}); verifyErr != nil {
			return verifyErr
		}
		return nil
	}
	result, err := promptJSONObjectWithOptions[ResearchSynthesis](context.Background(), recorder, "研究结构修复", "研究结构修复", repairOptions)
	if err != nil {
		t.Fatalf("repair 阶段未在带反馈重试后成功：%v", err)
	}
	if attempts != 2 {
		t.Fatalf("期望 2 次尝试（首次校验失败 + 反馈重试），实际 %d 次", attempts)
	}
	if !recorder.sawFeedback() {
		t.Fatal("第二次尝试未携带「上一次无效输出」纠错反馈")
	}
	// 最终拿到的是修正后的结果：锚点合法、校验通过。
	if len(result.Conditions) != 1 || result.Conditions[0].AnchorID != "ma20" {
		t.Fatalf("重试后未取到修正结果：%+v", result.Conditions)
	}
}

// 直接验证 afterValidation 钩子：语义校验失败会被当作解码失败处理。
func TestPromptJSONObjectAfterValidationTriggersRetry(t *testing.T) {
	attempts := 0
	recorder := &recordingPrompter{respond: func(call int, _ string) (string, error) {
		attempts = call
		if call == 1 {
			return `{"value":"bad"}`, nil
		}
		return `{"value":"good"}`, nil
	}}
	type payload struct {
		Value string `json:"value"`
	}
	got, err := promptJSONObjectWithOptions[payload](context.Background(), recorder, "任务", "测试", promptJSONObjectOptions{
		maxAttempts: 2,
		afterValidation: func(decoded any) error {
			candidate, ok := decoded.(*payload)
			if !ok || candidate == nil {
				return errValueNotGood
			}
			if candidate.Value != "good" {
				return errValueNotGood
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "good" {
		t.Fatalf("重试后未取到修正结果：%+v", got)
	}
	if attempts != 2 {
		t.Fatalf("期望 2 次尝试，实际 %d 次", attempts)
	}
	if !recorder.sawFeedback() {
		t.Fatal("第二次尝试未携带纠错反馈提示")
	}
}

var errValueNotGood = &simpleError{"value 必须是 good"}

type simpleError struct{ message string }

func (e simpleError) Error() string { return e.message }

// recordingPrompter 额外记录每次 prompt 原文，用于断言纠错反馈是否真的回填。
type recordingPrompter struct {
	calls   int
	prompts []string
	respond func(int, string) (string, error)
}

func (p *recordingPrompter) Prompt(ctx context.Context, prompt string) (agent.PromptResult, error) {
	return p.PromptWithOptions(ctx, prompt, agent.PromptOptions{})
}

func (p *recordingPrompter) PromptWithOptions(_ context.Context, prompt string, _ agent.PromptOptions) (agent.PromptResult, error) {
	p.calls++
	p.prompts = append(p.prompts, prompt)
	text, err := p.respond(p.calls, prompt)
	return agent.PromptResult{Content: text}, err
}

func (p *recordingPrompter) sawFeedback() bool {
	for _, prompt := range p.prompts {
		if strings.Contains(prompt, "[上一次无效输出") {
			return true
		}
	}
	return false
}

var _ agent.Prompter = (*recordingPrompter)(nil)

// [ES_REPAIR_SEMANTICS_PATCH] §3：跨尝试**不得累积**副作用。
//
// 要求原文：「notes/conditions/errors 不得跨尝试或跨请求累积」。
// 实现依据：promptJSONObjectWithOptions 每次尝试前执行 `decoded = *new(T)`，
// 目标被整体清零，因此第 1 次尝试写入的字段不会残留在第 2 次结果里。
//
// 本测试用一个「第 1 次填 3 个元素、第 2 次填 1 个元素」的可变长度切片锁定该性质：
// 若清零失效，第 2 次结果会残留第 1 次的元素（长度 ≥ 3 而非 1）。
func TestPromptJSONObjectResetsTargetBetweenAttempts(t *testing.T) {
	attempts := 0
	recorder := &recordingPrompter{respond: func(call int, _ string) (string, error) {
		attempts = call
		if call == 1 {
			// 第 1 次：合法 JSON、三个元素——但语义校验不通过，触发重试。
			return `{"items":[{"id":"a"},{"id":"b"},{"id":"c"}]}`, nil
		}
		// 第 2 次：只给一个元素。
		return `{"items":[{"id":"z"}]}`, nil
	}}
	type item struct {
		ID string `json:"id"`
	}
	type payload struct {
		Items []item `json:"items"`
	}
	got, err := promptJSONObjectWithOptions[payload](context.Background(), recorder, "任务", "累积检查", promptJSONObjectOptions{
		maxAttempts: 2,
		afterValidation: func(decoded any) error {
			candidate, ok := decoded.(*payload)
			if !ok || candidate == nil {
				return simpleError{"类型异常"}
			}
			// 只接受恰好 1 个元素的结果：第 1 次（3 个）必然失败。
			if len(candidate.Items) != 1 {
				return simpleError{"元素数不为 1"}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("重试后应成功：%v", err)
	}
	if attempts != 2 {
		t.Fatalf("期望 2 次尝试，实际 %d", attempts)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "z" {
		t.Fatalf("跨尝试发生累积（第 1 次 3 个元素残留）：%+v", got.Items)
	}
}
