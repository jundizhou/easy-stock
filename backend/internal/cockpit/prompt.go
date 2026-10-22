package cockpit

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const previousNarrativeRunes = 600

var phaseTasks = map[Phase]string{
	PhasePreOpen: `任务：盘前准备。
1. 外围市场发生了什么：依据「海外指数」与「快讯」，概括美股及亚太、商品等关键变化，区分已确认的数据与快讯口径；
2. 对 A 股开盘的可能传导：只写传导逻辑与需要验证的条件，不作涨跌预测；
3. 盘前预案：至少 3 条条件化预案，格式为「若 …（可观察的条件），则 …；失效条件：…」；
4. 需要开盘前核验的数据缺口。`,
	PhaseIntraday: `任务：盘中巡查。
1. 正在发生什么：指数与涨停结构的当前状态，若提供了上一次巡查，明确指出相对上次发生了什么变化；
2. 市场情绪：依据「盘中情绪」与「情绪周期」，说明强弱与风险；
3. 资金方向：依据「行业资金」与「概念资金」的净流入与净流出，指出主要方向，注明资金数据的口径（主力净流入）；
4. 需要盯住的风险与条件；
5. 数据缺口：标出失败或缺失的数据源及其影响。`,
	PhasePostClose: `任务：盘后复盘。
1. 今日市场概况：指数表现与成交结构；
2. 情绪与连板：情绪阶段、最高连板、涨停与炸板的变化；
3. 主线与资金：行业与概念资金的主要流入与流出方向；
4. 消息面：快讯中与盘面相关的要点；
5. 今日结论：用 2–3 句话概括市场状态；
6. 明日观察条件：列出 3 条可验证的观察点。`,
	PhaseRest: `任务：周末外围巡查。`,
}

// BuildPrompt 把结构化事实交给模型，并约束它只使用这些事实。
func BuildPrompt(phase Phase, sessionDate, label string, facts Facts, previous *Brief) string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是 A 股盘面巡查助手，正在生成「%s」（会话日 %s）简报。\n\n", label, sessionDate)
	b.WriteString(phaseTasks[phase])
	b.WriteString("\n\n硬性约束：\n")
	b.WriteString("- 只使用下面 JSON 中提供的数据，不得编造行情、新闻或未提供的指标；\n")
	b.WriteString("- 文中出现的带小数的数值（含百分比）必须逐字来自 JSON，不得换算、四舍五入或凭记忆补充；\n")
	b.WriteString("- 每个结论后用括号标注依据的数据模块，例如（海外指数）、（行业资金）；括号内只写模块名，可加「盘中快照」「收盘口径」「推断」等限定词；\n")
	b.WriteString("- 正文用中文描述口径（如「上次最高连板」「本次最高连板」），不要出现 JSON 字段名、英文键名或下划线命名；\n")
	b.WriteString("- 某数据源 ok=false 或字段缺失时，写明缺失，不要用猜测填补；\n")
	b.WriteString("- 区分数据事实与推断；推断要写明条件；\n")
	b.WriteString("- 金额单位为元（JSON 中 net_inflow 等字段）；\n")
	b.WriteString("- 使用中文 Markdown，分小节，控制在 900 字以内；\n")
	b.WriteString("- 结尾注明：本简报仅用于信息整理与研究辅助，不构成投资建议。\n")

	if previous != nil && phase == PhaseIntraday {
		prev := map[string]any{
			"label":     previous.Label,
			"facts":     map[string]any{"emotion": previous.Facts.Emotion, "intraday": previous.Facts.Intraday, "limit_up": previous.Facts.LimitUp},
			"narrative": truncateRunes(previous.Narrative, previousNarrativeRunes),
		}
		b.WriteString("\n上一次巡查（用于对比变化）：\n")
		b.Write(encodeRounded(prev))
		b.WriteString("\n")
	}

	encoded := encodeRounded(facts)
	b.WriteString("\n本次采集的事实数据（JSON，数值已舍入到 4 位小数）：\n")
	b.Write(encoded)
	b.WriteString("\n")
	return b.String()
}

// encodeRounded 先舍入再序列化；失败时退回原始值，确保提示词始终可用。
func encodeRounded(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return raw
	}
	rounded, err := json.Marshal(roundFloats(generic))
	if err != nil {
		return raw
	}
	return rounded
}

// roundFloats 把所有浮点数舍入到 4 位小数。上游原始小数位很长（如 0.31343283…），
// 模型会原样引用，既难读也容易误导；舍入只改变显示精度，不改变数据来源。
func roundFloats(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = roundFloats(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = roundFloats(item)
		}
		return typed
	case float64:
		// 金额级别的数值（≥1 万）保留整数，小数位只会增加噪声。
		if math.Abs(typed) >= 1e4 {
			return math.Round(typed)
		}
		return math.Round(typed*1e4) / 1e4
	default:
		return value
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
