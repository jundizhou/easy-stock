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
依据「海外指数」与「快讯」概括外围状态，明确 A 股尚未开盘；可能传导只写条件，不作涨跌预测。观察项只保留开盘后最关键的一项验证条件。`,
	PhaseIntraday: `任务：盘中巡查。
综合指数、涨停结构说明当前市场强弱与分歧；「市场温度」说明全市场涨跌分布，「盘中情绪」与「情绪周期」说明短线强弱与风险，两种指标分别解释。若提供上一次巡查，只写最关键的一项变化。行业与概念资金只保留主要流入、流出方向，注明主力净流入口径。`,
	PhasePostClose: `任务：盘后复盘。
综合今日指数、情绪与连板结构概括收盘状态；资金只保留主要方向，消息仅在影响结论时提及。观察项只保留下一交易日最关键的一项验证条件。`,
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
	b.WriteString("- 某数据源 ok=false 时先检查对应事实：若仍有 stale=true 的有效缓存，说明刷新失败并引用缓存及原始抓取时间，不得写成数据缺失；对应字段为空时才写明缺失，不要用猜测填补；\n")
	b.WriteString("- 区分数据事实与推断；推断要写明条件；\n")
	b.WriteString("- 市场温度是沪深有效样本当日涨跌幅的本地平滑等权统计（0–100 度，50 度为中性），不是旧情绪分、预测或交易信号；上涨占比、等权与中位涨跌幅是参考统计，不是分项权重；\n")
	b.WriteString("- 市场温度缺失时如实说明，禁止用情绪分代替；stale=true 时注明缓存及 fetched_at 抓取时间，trade_date 为来源标记日期，不能把抓取时间当成交时间；跨日期或不同模型的温度不作同口径比较；\n")
	b.WriteString("- 情绪周期只可引用阶段与日期，不展示总分或分项分数，也不展开与盘中情绪的分数对比；本次与上次 JSON 或叙述中的旧情绪分数均遵守此规则；\n")
	b.WriteString("- 金额单位为元（JSON 中 net_inflow 等字段）；\n")
	b.WriteString("- 使用中文 Markdown，正文目标 200 字、最多 260 字（不含来源括号与末尾说明）；不写总标题、长段落、表格或重复图表读数；\n")
	b.WriteString("- 严格按以下五行输出，每行一个字段、一句短话；字段名与顺序固定，市场状态必须放在第一行：\n")
	b.WriteString("- **市场状态**：短状态词；一项最关键依据（来源模块）\n")
	b.WriteString("- **较上次变化**：一项已核实变化（来源模块）；没有上一次巡查时写「--（无同口径对比）」\n")
	b.WriteString("- **资金方向**：主要流入与流出方向，最多各两个（来源模块）\n")
	b.WriteString("- **主要风险**：当前最需要警惕的一项风险（来源模块；推断须注明条件）\n")
	b.WriteString("- **接下来盯什么**：一个可观察条件及其含义，不给买卖指令（来源模块）\n")
	b.WriteString("- 任一字段依据不足就写「--（数据不足）」，不得为了凑齐五项推断或补造状态、方向、风险；\n")
	b.WriteString("- 仅在数据源失败或缺失时追加一行「**数据缺口**：缺失模块及影响」；不要把缺失写成正常或无风险；\n")
	b.WriteString("- 结尾注明：本简报仅用于信息整理与研究辅助，不构成投资建议。\n")

	if previous != nil && phase == PhaseIntraday {
		prev := map[string]any{
			"label":     previous.Label,
			"facts":     map[string]any{"emotion": previous.Facts.Emotion, "temperature": previous.Facts.Temperature, "intraday": previous.Facts.Intraday, "limit_up": previous.Facts.LimitUp},
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
