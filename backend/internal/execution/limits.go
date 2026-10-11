// Package execution 提供 easy-stock 信号 → 券商 QMT/Ptrade 执行通道的执行网关。
//
// 本文件（limits.go）定义执行护栏的纯函数：整手换算、单票/合计权重校验、
// ST / 停牌 / 涨跌停预过滤。这些函数无状态，可被网关与 P2 适配层共用。
package execution

import (
	"fmt"
	"math"
)

const (
	// DefaultMaxSingleWeight 单票目标权重上限（与 core2 规格一致：单票 ≤ 10%）。
	DefaultMaxSingleWeight = 0.10
	// DefaultMaxDailyTurnover 单日换手率上限（护栏：日换手 ≤ 30%）。
	DefaultMaxDailyTurnover = 0.30
	// DefaultRoundLot A 股最小交易单位（100 股/手）。
	DefaultRoundLot = 100
)

// RoundLot 将任意股数向下取整到整手（lot 的正整数倍）。lot<=0 时回退到默认 100。
// 负数股数按 0 处理（禁止裸卖空，本系统不加杠杆不做空）。
func RoundLot(shares, lot int) int {
	if lot <= 0 {
		lot = DefaultRoundLot
	}
	if shares < 0 {
		return 0
	}
	return (shares / lot) * lot
}

// SharesForNotional 根据目标金额与假定价格，换算为整手股数。
// notional<=0 或 price<=0 时返回 0（无法定价的标的直接剔除）。
func SharesForNotional(notional, price float64, lot int) int {
	if notional <= 0 || price <= 0 {
		return 0
	}
	raw := int(math.Floor(notional / price))
	return RoundLot(raw, lot)
}

// ValidateTargetWeights 检查每个目标权重是否超过单票上限，返回告警列表（空表示通过）。
func ValidateTargetWeights(targets []PlanTarget, maxSingle float64) []string {
	if maxSingle <= 0 {
		maxSingle = DefaultMaxSingleWeight
	}
	var warnings []string
	for _, t := range targets {
		if t.TargetWeight > maxSingle+1e-9 {
			warnings = append(warnings, fmt.Sprintf("%s 目标权重 %.4f 超过单票上限 %.4f", t.Code, t.TargetWeight, maxSingle))
		}
	}
	return warnings
}

// ValidateGrossWeight 检查合计目标权重是否超过 1（满仓），返回告警（空表示通过）。
func ValidateGrossWeight(gross float64) string {
	if gross > 1.0+1e-9 {
		return fmt.Sprintf("合计目标权重 %.4f 超过 1.0（满仓上限）", gross)
	}
	return ""
}

// QuoteSnapshot 是网关在缺乏实时行情时仍可接收的可选快照，由 P2 适配层在 live
// 模式下填充。P0+P1（sim）下通常为 nil，预过滤仅作声明性占位。
type QuoteSnapshot struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`       // 用于 *ST/ST/退 字面识别
	Suspended bool    `json:"suspended"`  // 停牌
	IsST      bool    `json:"is_st"`      // ST / *ST / 退市整理
	LimitUp   bool    `json:"limit_up"`   // 涨停（买不进）
	LimitDown bool    `json:"limit_down"` // 跌停（卖不出）
	LastPrice float64 `json:"last_price"`
}

// Prefilter 对单票做 ST / 停牌 / 涨跌停预过滤，返回预过滤原因（空表示放行）。
//
// 当 quote 为 nil 时（sim 模式无行情）无法做真实过滤，仅做名称/代码层面的 ST
// 字面粗判，并明确标注「待行情复核」，避免误下真实订单。
func Prefilter(code string, quote *QuoteSnapshot) string {
	if quote == nil {
		if IsSTName(code) {
			return "ST 字面命中：待行情复核，预拦截"
		}
		return "sim 无行情：待 P2 适配层行情复核"
	}
	if quote.Suspended {
		return "停牌：禁止交易"
	}
	if quote.IsST || IsSTName(quote.Name) {
		return "ST/*ST：禁止交易"
	}
	if quote.LimitUp {
		return "涨停：买盘无法成交，延后"
	}
	if quote.LimitDown {
		return "跌停：卖盘无法成交，延后"
	}
	return ""
}

// IsSTName 按名称字面粗判 ST（含 "ST" / "*ST" / "退"）。无行情时的兜底识别，
// 不依赖代码段（代码段无法可靠区分）。
func IsSTName(name string) bool {
	for i := 0; i+2 <= len(name); i++ {
		seg := name[i : i+2]
		if seg == "ST" || seg == "*S" {
			return true
		}
	}
	return false
}
