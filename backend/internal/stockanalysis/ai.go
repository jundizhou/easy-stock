package stockanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"easy-stock/backend/internal/hermes"
)

type aiConclusion struct {
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
	Action   string `json:"action"`
	BestPath string `json:"best_path"`
	MainRisk string `json:"main_risk"`
}

type aiThemeEvidenceResponse struct {
	Items []ThemeEvidence `json:"items"`
}

// ExtractThemeEvidence asks Hermes to normalize facts that are difficult to
// recover from short announcement titles (for example an investment in a
// compound-semiconductor subsidiary). The response is only a candidate-evidence
// layer; enrichTheme still requires deterministic market confirmation before it
// promotes a theme to the primary hot label.
func ExtractThemeEvidence(ctx context.Context, prompter hermes.Prompter, input Input) ([]ThemeEvidence, error) {
	if prompter == nil {
		return nil, errors.New("AI分析底座不可用")
	}
	if len(input.Announcements) == 0 && len(input.News) == 0 {
		return nil, nil
	}
	var source strings.Builder
	source.WriteString("股票：" + input.Quote.Name + "（" + input.Symbol + "）\n")
	source.WriteString("主营：" + firstNonEmpty(input.Business, input.Industry) + "\n")
	source.WriteString("概念目录：" + strings.Join(input.Concepts, "、") + "\n")
	source.WriteString("公告：\n")
	for _, item := range input.Announcements {
		source.WriteString("- " + item.PublishedAt.Format("2006-01-02") + " | " + item.Title + " | " + item.Category + " | " + truncateText(item.Content, 900) + "\n")
	}
	source.WriteString("资讯：\n")
	for _, item := range input.News {
		if containsAnyFold(item.Title+" "+item.Content, input.Quote.Name, strings.Split(input.Symbol, ".")[0]) {
			source.WriteString("- " + item.PublishedAt.Format("2006-01-02 15:04") + " | " + item.Title + " | " + truncateText(item.Content, 240) + "\n")
		}
	}
	prompt := `你是A股题材归因器。请仅基于输入事实，提取可能影响当前炒作的具体题材。
规则：
1. 区分 fact（公告或公司明确事实）、market_mapping（产业链/市场映射）、inference（弱推断）。
2. 不要把主营行业、宽泛概念目录直接当成热点；不要编造公告未出现的公司事实。
3. 题材名称使用短而具体的中文，例如“化合物半导体”“砷化镓”“光电子器件”“CPO”。
4. 每项必须给出原文标题或摘要作为snippet，strength为0到1。
5. 只输出JSON：{"items":[{"theme":"...","type":"fact|market_mapping|inference","source":"...","title":"...","snippet":"...","strength":0.0}]}

[输入]
` + source.String()
	result, err := prompter.Prompt(ctx, prompt)
	if err != nil {
		return nil, err
	}
	var decoded aiThemeEvidenceResponse
	if err := decodeJSONObject(result.Content, &decoded); err != nil {
		return nil, err
	}
	out := make([]ThemeEvidence, 0, min(len(decoded.Items), 12))
	for _, item := range decoded.Items {
		item.Theme = strings.TrimSpace(item.Theme)
		if item.Theme == "" {
			continue
		}
		item.Strength = clamp(item.Strength, .15, .98)
		item.Freshness = freshnessForTime(item.PublishedAt)
		out = append(out, item)
		if len(out) >= 12 {
			break
		}
	}
	return out, nil
}

func EnrichWithAI(ctx context.Context, prompter hermes.Prompter, analysis *Analysis, methodologyContext string) error {
	if prompter == nil || analysis == nil {
		return errors.New("AI分析底座不可用")
	}
	payload := map[string]any{
		"symbol":       analysis.Symbol,
		"name":         analysis.Name,
		"quote":        analysis.Quote,
		"profile":      analysis.Profile,
		"trend":        analysis.Trend,
		"short_term":   analysis.ShortTerm,
		"theme":        analysis.Theme,
		"fundamental":  analysis.Fundamental,
		"research":     analysis.Research,
		"market":       analysis.Market,
		"scorecard":    analysis.Scorecard,
		"timeframes":   analysis.Timeframes,
		"relative":     analysis.Relative,
		"signals":      analysis.Signals,
		"next_day":     analysis.NextDay,
		"risk_control": analysis.RiskControl,
		"action_plan":  analysis.ActionPlan,
		"risks":        analysis.Risks,
		"data_quality": analysis.DataQuality,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	methodologyContext = truncateText(methodologyContext, 6_000)
	prompt := `你是 easy-stock 的个股AI分析器。请基于输入的结构化事实，生成简洁、克制、可执行的中文研判。

必须遵守：
1. 不编造输入JSON之外的实时数据、机构持仓、游资席位或公司事实。
2. 同时考虑趋势股票和短线情绪股票；服从 profile.primary_type 的分析路由。
3. 不模拟任何投资名人的口吻，不使用“大佬投票”或人格化结论。
4. action 必须是条件化建议，不能承诺收益；隔日预期只能描述情景，不得表述为确定性预测。
5. 必须尊重 scorecard、relative、next_day 与 risk_control 的结构化结论，不得擅自修改价格和分数。
6. 非情绪连板型必须结合 fundamental 与 research；机构评级仅代表第三方观点，不得当作确定性结论。
7. 严格输出单个JSON对象，不要Markdown，不要额外解释。

输出格式：
{"headline":"不超过35字","summary":"80至180字","action":"当前动作","best_path":"最优验证路径","main_risk":"最主要风险"}

[结构化分析JSON]
` + string(encoded)
	if strings.TrimSpace(methodologyContext) != "" {
		prompt += "\n\n[本地游资心法的相关历史经验，仅作为短线风险与执行约束，不代表真人观点]\n" + methodologyContext
	}
	response, err := prompter.Prompt(ctx, prompt)
	if err != nil {
		return fmt.Errorf("Hermes个股分析失败: %w", err)
	}
	var result aiConclusion
	if err := decodeJSONObject(response.Content, &result); err != nil {
		return fmt.Errorf("Hermes未返回有效个股分析JSON: %w", err)
	}
	if strings.TrimSpace(result.Headline) == "" || strings.TrimSpace(result.Summary) == "" || strings.TrimSpace(result.Action) == "" {
		return errors.New("Hermes个股分析缺少必要字段")
	}
	analysis.Conclusion = Conclusion{
		Headline: truncateText(result.Headline, 60),
		Summary:  truncateText(result.Summary, 360),
		Action:   truncateText(result.Action, 120),
		BestPath: truncateText(firstNonEmpty(result.BestPath, analysis.Conclusion.BestPath), 180),
		MainRisk: truncateText(firstNonEmpty(result.MainRisk, analysis.Conclusion.MainRisk), 180),
		Source:   "hermes-ai",
	}
	analysis.AI = AISynthesisStatus{Status: "ready", Message: "Hermes已基于结构化证据完成综合研判"}
	return nil
}

func decodeJSONObject(content string, target any) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("empty response")
	}
	if strings.HasPrefix(content, "```") {
		content = strings.TrimSpace(strings.TrimPrefix(content, "```json"))
		content = strings.TrimSpace(strings.TrimPrefix(content, "```"))
		content = strings.TrimSpace(strings.TrimSuffix(content, "```"))
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return errors.New("JSON object not found")
	}
	decoder := json.NewDecoder(strings.NewReader(content[start : end+1]))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}
