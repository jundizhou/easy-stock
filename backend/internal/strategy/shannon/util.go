package shannon

import (
	"math"
	"sort"
	"strconv"
)

// itoa 整数转字符串。
func itoa(v int) string {
	return strconv.Itoa(v)
}

// fmtFloat 按固定小数位格式化（与 Python f"{v:.Nf}" 同口径）。
func fmtFloat(v float64, n int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', n, 64)
	}
	return strconv.FormatFloat(v, 'f', n, 64)
}

// roundPtr 包装指针值的四舍五入（nil 安全）。
func roundPtr(v *float64, n int) *float64 {
	if v == nil || math.IsNaN(*v) {
		return v
	}
	out := roundTo(*v, n)
	return &out
}

// fmtRaw 输出浮点的最短往返表示，等价于 Python 的 f"{x}"（repr）。
//
// Python 的 f-string 默认用 repr，会去掉尾随零（12.30 → "12.3"），
// 而固定小数位格式化会保留（"12.30"）。双端对拍文本时必须区分这两种口径。
func fmtRaw(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// fmtRound 先四舍五入到 n 位，再按最短表示输出，等价于 Python 的 f"{round(x, n)}"。
func fmtRound(v float64, n int) string {
	return fmtRaw(roundTo(v, n))
}

// ptrOr 取值或默认值。
func ptrOr(v *float64, fallback float64) float64 {
	if v == nil || math.IsNaN(*v) {
		return fallback
	}
	return *v
}

// mergeSources 合并并去重排序（与 Python sorted(set(...)) 同口径）。
func mergeSources(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		for _, s := range g {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// BucketLabel 分组中文标签。
var BucketLabel = map[string]string{
	"ENTRY":     "✅ 可入场（已突破 + 风险回报达标）",
	"WATCH":     "👀 观察名单（回踩完成，等待突破确认）",
	"LOW_RR":    "⚠️ 结构成立但风险回报不足",
	"CHASE":     "🚫 追高禁区（已高于触发位 2%+）",
	"NO_STRUCT": "— 无有效做多结构",
	"SIZE_ZERO": "⛔ 信号成立但仓位被对齐系数归零",
	"L2_BLOCK":  "🚷 5 日线下行（核心过滤器拦截）",
	"RED":       "🛑 红灯（禁止做多）",
	"OTHER":     "· 其他",
	"ERROR":     "❗ 取数/计算异常",
}

// BucketOrder 分组展示顺序。
var BucketOrder = []string{"ENTRY", "WATCH", "LOW_RR", "CHASE", "SIZE_ZERO", "NO_STRUCT", "OTHER", "L2_BLOCK", "RED", "ERROR"}

// BucketOf 把结果归入可执行分组。
//
// 优先级：硬阻断 > 可入场 > 观察 > 追高 > 结构/RR。
// blocked=True 既可能来自红灯，也可能来自 L2 下行 5 日线（灯为 YELLOW），
// 两者都属「按纪律不得做多」，但需分开归组以便区分原因。
//
// 注意：Python 侧 layer5_entry 在非阻断路径把 blocked 置为 chase_blocked，
// 因此追高标的在此也会落入 L2_BLOCK（与 Python 行为一致，属已知口径）。
func BucketOf(res Result) string {
	e := res.Entry
	light := res.Light.Light

	if e.Blocked {
		if light == "RED" {
			return "RED"
		}
		return "L2_BLOCK"
	}
	if light == "RED" {
		return "RED"
	}
	if e.Valid {
		if res.Alignment.PositionScale == 0.0 {
			return "SIZE_ZERO"
		}
		return "ENTRY"
	}
	if e.AwaitingBreakout && e.PulledBack {
		return "WATCH"
	}
	if e.ChaseBlocked {
		return "CHASE"
	}
	if !e.StructureOK {
		return "NO_STRUCT"
	}
	if e.RRRatio != nil && *e.RRRatio < MinRR {
		return "LOW_RR"
	}
	return "OTHER"
}
