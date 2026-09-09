package stockanalysis

import (
	"fmt"
	"math"
	"time"

	"easy-stock/backend/internal/foundation"
)

// VerifyResearch checks only later, completed daily bars. It does not score a
// forecast as correct merely because a condition happened at an unrelated time.
func VerifyResearch(snapshot ResearchSnapshot, report ResearchReport, lines []foundation.KLine, now time.Time, calendars ...[]foundation.KLine) ResearchVerification {
	result := ResearchVerification{CheckedAt: now, BaselineAt: snapshot.CutoffAt, Source: "daily-kline", Checks: []ConditionCheck{}, Summary: "仅核对观察条件，不代表收益验证或预测准确率"}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	baselineDay := snapshot.CutoffAt.In(loc).Format("2006-01-02")
	usable := []foundation.KLine{}
	for _, bar := range normalizeKLines(lines) {
		date := bar.Time.In(loc).Format("2006-01-02")
		if date > now.In(loc).Format("2006-01-02") || (date == now.In(loc).Format("2006-01-02") && now.In(loc).Hour() < 15) {
			continue
		}
		usable = append(usable, bar)
	}
	byDate := map[string]int{}
	for i, bar := range usable {
		byDate[bar.Time.In(loc).Format("2006-01-02")] = i
	}
	expected := []string{}
	if len(calendars) > 0 {
		for _, bar := range normalizeKLines(calendars[0]) {
			date := bar.Time.In(loc).Format("2006-01-02")
			if date > baselineDay && date <= now.In(loc).Format("2006-01-02") && !(date == now.In(loc).Format("2006-01-02") && now.In(loc).Hour() < 15) {
				expected = append(expected, date)
			}
		}
	}
	coverageProblem := ""
	if len(calendars) == 0 || len(calendars[0]) == 0 {
		coverageProblem = "交易日基准不可用，不能把任意下一条日线当成下一交易日"
	}
	overlap := false
	for i := len(snapshot.DailyBars) - 1; i >= 0; i-- {
		old := snapshot.DailyBars[i]
		if old.Date == baselineDay && snapshot.CutoffAt.In(loc).Hour() < 15 {
			continue
		}
		if index, ok := byDate[old.Date]; ok {
			overlap = true
			if math.Abs(usable[index].Close-old.Close) > math.Max(.015, old.Close*.0002) {
				coverageProblem = "历史重叠收盘价发生变化，可能存在前复权调整或数据修订；原价格阈值不可直接核验"
			}
			break
		}
	}
	if !overlap {
		coverageProblem = "后续日线未覆盖原快照的完整交易日，无法确认连续性和复权口径"
	}
	for _, condition := range report.Conditions {
		check := ConditionCheck{ConditionID: condition.ID, Status: "pending", Detail: "尚无分析时点之后的完整交易日数据"}
		if condition.Metric != "close" && condition.Metric != "volume_ratio" {
			check.Status = "manual_review"
			check.Detail = "当前没有可自动核验的公告语义或竞价/开盘数据，需核对原始资料"
			result.Checks = append(result.Checks, check)
			continue
		}
		if condition.Threshold == nil {
			check.Status = "unavailable"
			check.Detail = "缺少可复算阈值"
			result.Checks = append(result.Checks, check)
			continue
		}
		if coverageProblem != "" {
			check.Status = "unavailable"
			check.Detail = coverageProblem
			result.Checks = append(result.Checks, check)
			continue
		}
		window := 5
		if condition.Window == "next_close" {
			window = 1
		}
		for _, date := range expected[:min(window, len(expected))] {
			index, ok := byDate[date]
			if !ok {
				check.Status = "unavailable"
				check.Detail = "缺少窗口内" + date + "个股日线，可能停牌或采集缺失，不能跳过该日"
				break
			}
			bar := usable[index]
			value := bar.Close
			if condition.Metric == "volume_ratio" {
				if index < 19 {
					check.Status = "unavailable"
					check.Detail = "成交量样本不足20日"
					continue
				}
				five, twenty := 0.0, 0.0
				for j := index - 19; j <= index; j++ {
					twenty += usable[j].Volume
					if j >= index-4 {
						five += usable[j].Volume
					}
				}
				if twenty <= 0 {
					check.Status = "unavailable"
					check.Detail = "成交量不可用"
					continue
				}
				value = (five / 5) / (twenty / 20)
			}
			check.Observed = &value
			check.AsOf = bar.Time.In(loc).Format("2006-01-02")
			matched := (condition.Operator == "gte" && value >= *condition.Threshold) || (condition.Operator == "lte" && value <= *condition.Threshold)
			if matched {
				check.Status = "met"
				check.Detail = fmt.Sprintf("窗口内观测值%.4f满足条件；不等于交易已成交", value)
				break
			}
			check.Status = "not_yet"
			check.Detail = "已取得后续数据，尚未满足条件"
		}
		if len(expected) >= window && check.Status == "not_yet" {
			check.Status = "not_met"
			check.Detail = "指定交易日窗口已结束，未满足条件"
		}
		result.Checks = append(result.Checks, check)
	}
	return result
}
