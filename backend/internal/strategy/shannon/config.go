// Package shannon 是 Brian Shannon 方法论（六层规则）的 Go 实现。
//
// 规则来源：Brian Shannon 公开演讲/访谈逐字稿 72 批蒸馏结果，
// 与 Python 侧 C:/quant/brian_shannon_system/core 严格同参同逻辑，
// 由 shannon_test.go 与 tools/shannon_parity.py 双端对拍保证一致。
//
// 六层自顶向下，L2 为硬性过滤器（短路）：
//
//	L1 趋势结构   HH+HL → UPTREND
//	L2 5 日线极性 必须 SMA（VIDEO-094）；下行为硬阻断
//	L3 多周期对齐  月/周/日共振 → 仓位系数
//	L4 红绿灯     GREEN / YELLOW / RED
//	L5 三步入场   回踩 → 突破 → 止损挂回踩低点下方，RR ≥ 1.5
//	L6 仓位止损   以占净值百分比为准（股数只是落地执行量）
package shannon

// ---- L1 趋势结构 -----------------------------------------------------------

const (
	// SwingK 摆动点识别：左右各 k 根 K 线极值。
	SwingK = 5
	// SwingN 结构判定取最近 n 个摆动点。
	SwingN = 3
)

// ---- L2 5 日线极性 ---------------------------------------------------------

const (
	// MA5Period 必须 SMA（VIDEO-094）。
	MA5Period = 5
	// FlatEpsPct 斜率绝对值 < 该阈值(%) 视为走平。
	FlatEpsPct = 0.05
)

// ---- L3 多周期对齐 ---------------------------------------------------------

// SizeScale 对齐档位 → 仓位系数。
var SizeScale = map[string]float64{
	"ALIGNED_BULL": 1.0,
	"BULL_LEAN":    0.75,
	"NEUTRAL":      0.5,
	"CONFLICT":     0.0,
	"BEAR_LEAN":    0.0,
	"ALIGNED_BEAR": 0.0,
}

// ---- L5 入场 --------------------------------------------------------------

const (
	// BreakoutLookback 突破位 = 最近 N 日高点（回踩前的高点）。
	BreakoutLookback = 20
	// PullbackLookback 回踩低点搜索窗口。
	PullbackLookback = 10
	// StopBufferPct 止损缓冲：结构位下方该比例。
	StopBufferPct = 0.005
	// StopBufferMin 缓冲下限（元），防止极低价股缓冲为 0。
	StopBufferMin = 0.01
	// MinRR 最小风险回报比。
	MinRR = 1.5
	// ChaseTolerance 价格超触发位该比例以上视为追高。
	ChaseTolerance = 0.02
	// PullbackMinPct 回踩最小幅度 %。
	PullbackMinPct = 2.0
	// PullbackMaxPct 回踩最大幅度 %（超过视为结构破坏）。
	PullbackMaxPct = 25.0
	// TargetExtPct 第一目标 = 触发位 + 形态高度 × 该倍数。
	TargetExtPct = 1.618
	// TargetExt2Pct 第二目标倍数。
	TargetExt2Pct = 2.618
)

// RequirePullback 是否强制要求「先回踩」（方法论第①步）。
const RequirePullback = true

// ---- L6 仓位（百分比口径）--------------------------------------------------

const (
	// DefaultEquity 默认账户净值（元）。
	DefaultEquity = 100000.0
	// RiskPct 单笔最大风险 %（INTERVIEW-001 的 $1k/$100k 示例）。
	RiskPct = 1.0
	// BoardLot A 股每手股数。
	BoardLot = 100
	// MinLotFill 缩放后 ≥ 半手即进位 1 手；低于此视为资金不足。
	MinLotFill = 0.5
	// MaxPositionPct 单笔名义仓位占净值上限 %。
	MaxPositionPct = 20.0
	// MaxOpenRiskPct 组合未平仓风险合计上限 % 净值。
	MaxOpenRiskPct = 5.0
	// MaxOpenPositions 同时持仓标的上限。
	MaxOpenPositions = 5
)

// LightScale 红绿灯 → 仓位系数。
var LightScale = map[string]float64{
	"GREEN":  1.0,
	"YELLOW": 0.5,
	"RED":    0.0,
}

// LightAction 红绿灯 → 应对说明。
var LightAction = map[string]string{
	"GREEN":  "正常仓位，可新增",
	"YELLOW": "减仓、上移止损、不新增",
	"RED":    "只做空或空仓，现金是仓位",
}

// Config 是可覆盖的规则参数（零值表示用包级默认）。
type Config struct {
	SwingK           int
	SwingN           int
	MA5Period        int
	FlatEpsPct       float64
	BreakoutLookback int
	PullbackLookback int
	StopBufferPct    float64
	StopBufferMin    float64
	MinRR            float64
	ChaseTolerance   float64
	PullbackMinPct   float64
	PullbackMaxPct   float64
	TargetExtPct     float64
	TargetExt2Pct    float64
	RiskPct          float64
	BoardLot         int
	MinLotFill       float64
	MaxPositionPct   float64
	MaxOpenRiskPct   float64
	MaxOpenPositions int
}

// DefaultConfig 返回与 Python rules_config.py 对齐的默认参数。
func DefaultConfig() Config {
	return Config{
		SwingK:           SwingK,
		SwingN:           SwingN,
		MA5Period:        MA5Period,
		FlatEpsPct:       FlatEpsPct,
		BreakoutLookback: BreakoutLookback,
		PullbackLookback: PullbackLookback,
		StopBufferPct:    StopBufferPct,
		StopBufferMin:    StopBufferMin,
		MinRR:            MinRR,
		ChaseTolerance:   ChaseTolerance,
		PullbackMinPct:   PullbackMinPct,
		PullbackMaxPct:   PullbackMaxPct,
		TargetExtPct:     TargetExtPct,
		TargetExt2Pct:    TargetExt2Pct,
		RiskPct:          RiskPct,
		BoardLot:         BoardLot,
		MinLotFill:       MinLotFill,
		MaxPositionPct:   MaxPositionPct,
		MaxOpenRiskPct:   MaxOpenRiskPct,
		MaxOpenPositions: MaxOpenPositions,
	}
}
