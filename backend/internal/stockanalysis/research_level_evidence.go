package stockanalysis

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Snapshots remain complete; only the model's view is reduced for its level.
func researchSourceForLevel(source ResearchSource, snapshot ResearchSnapshot, policy researchLevelPolicy) ResearchSource {
	if source.ID == "m-quote" {
		return researchQuoteSessionContext(source, snapshot)
	}
	if source.ID == "m-sector" {
		return compactResearchMarketSource(source, policy)
	}
	if source.ID == "f-financial" {
		var value map[string]any
		if json.Unmarshal([]byte(source.Content), &value) == nil {
			if data, ok := value["data"].(map[string]any); ok {
				date, _ := data["report_date"].(string)
				limit := 8
				if policy.DailyBars == 60 {
					limit = 1
				} else if policy.DailyBars == 100 {
					limit = 4
				}
				for _, key := range []string{"history", "supplemental_history", "cross_checks"} {
					if items, ok := value[key].([]any); ok {
						value[key] = selectComparableFinancialPeriods(items, date, limit)
					}
				}
			}
			if history, ok := value["history"].([]any); ok {
				if policy.DailyBars == 60 {
					delete(value, "history")
					value["history_note"] = "仅最新累计披露，不判断连续改善"
					value["definitions"] = map[string]string{"period": "年初至报告期末累计值，不能按单季或环比解读", "operating_cash_flow_per_share": "每股经营现金流，不是现金流总额"}
				} else {
					// The latest period already appears in data. Keep comparable
					// prior periods rather than spending the budget on it twice.
					if data, ok := value["data"].(map[string]any); ok && len(history) > 0 {
						if first, ok := history[0].(map[string]any); ok && data["report_date"] != nil && data["report_date"] == first["report_date"] {
							history = history[1:]
						}
					}
					// Reserve prior-year matching quarters before generic budget
					// compression; recent three periods alone cannot establish Q2 YoY.
					value["history"] = history
					value["history_note"] = "最新期见data；history为预算内最近可比累计披露，不代表全部历史"
				}
			}
			encoded, _ := json.Marshal(value)
			source.Content = string(encoded)
		}
	}
	if source.ID == "f-business" {
		limit := 1800
		if policy.DailyBars == 100 {
			limit = 600
		} else if policy.DailyBars == 60 {
			limit = 100
		}
		source.Content = truncateExactText(source.Content, limit)
	}
	if source.ID != "m-price" {
		return source
	}
	var original map[string]any
	if json.Unmarshal([]byte(source.Content), &original) != nil {
		return source
	}
	bars := snapshot.DailyBars[max(0, len(snapshot.DailyBars)-policy.DailyBars):]
	if len(bars) == 0 {
		return source
	}
	stats := summarizeDailyKLines(bars)
	encoded, _ := json.Marshal(stats)
	var summary map[string]any
	_ = json.Unmarshal(encoded, &summary)
	if policy.DailyBars < 300 {
		keys := []string{"sample_days", "limited_sample", "start_date", "end_date", "latest_close", "period_high", "period_low", "window_returns_percent", "volume_ratio_5d_20d", "max_drawdown_percent"}
		compact := make(map[string]any, len(keys))
		for _, key := range keys {
			if value, ok := summary[key]; ok {
				compact[key] = value
			}
		}
		summary = compact
	}
	rows := make([][]any, 0, len(bars))
	for _, bar := range bars {
		date, _ := strconv.Atoi(strings.ReplaceAll(bar.Date, "-", ""))
		rows = append(rows, []any{date, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume})
	}
	value := map[string]any{
		"summary": summary, "bar_columns": []string{"date_YYYYMMDD", "open", "high", "low", "close", "volume"},
		"recent_bars": rows, "price_basis": original["price_basis"], "volume_unit": original["volume_unit"],
		"missing_fields": original["missing_fields"], "intraday_caution": original["intraday_caution"],
		"return_definition": original["return_definition"],
	}
	if policy.DailyBars == 60 {
		value["volume_unit"] = "同源单位，仅用于相对量比"
		value["return_definition"] = "N日收盘/此前第N日收盘-1，需N+1个收盘"
		value["intraday_caution"] = "15:00前当日日线未完成"
	}
	for {
		encoded, _ = json.Marshal(value)
		if len(encoded) <= policy.MaxEvidenceBytes/6 || len(rows) <= 5 {
			break
		}
		rows = rows[1:]
		value["recent_bars"] = rows
		value["sample_note"] = "统计覆盖所选周期；逐日行仅保留预算内最近记录"
	}
	source.Content = string(encoded)
	return source
}

func researchQuoteSessionContext(source ResearchSource, snapshot ResearchSnapshot) ResearchSource {
	var value map[string]any
	if json.Unmarshal([]byte(source.Content), &value) != nil {
		return source
	}
	text, _ := value["trade_time"].(string)
	tradeTime, err := time.Parse(time.RFC3339, text)
	if err != nil || tradeTime.IsZero() {
		return source
	}
	local := tradeTime.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if local.Hour() >= 15 {
		value["session_context"] = "after_close_15h"
		for _, bar := range snapshot.DailyBars {
			if bar.Date != local.Format("2006-01-02") {
				continue
			}
			value["same_date_daily_close"] = bar.Close
			value["same_date_daily_close_date"] = bar.Date
			break
		}
	} else {
		value["session_context"] = "before_close_15h"
	}
	encoded, _ := json.Marshal(value)
	source.Content = string(encoded)
	return source
}
