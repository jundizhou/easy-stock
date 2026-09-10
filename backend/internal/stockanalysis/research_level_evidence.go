package stockanalysis

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Snapshots remain complete; only the model's view is reduced for its level.
func researchSourceForLevel(source ResearchSource, snapshot ResearchSnapshot, policy researchLevelPolicy) ResearchSource {
	if source.ID == "f-business" {
		limit := 1800
		if policy.DailyBars == 100 {
			limit = 600
		} else if policy.DailyBars == 60 {
			limit = 400
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
	}
	for {
		encoded, _ = json.Marshal(value)
		if len(encoded) <= policy.MaxEvidenceBytes*3/4 || len(rows) <= 5 {
			break
		}
		rows = rows[1:]
		value["recent_bars"] = rows
		value["sample_note"] = "统计覆盖所选周期；逐日行仅保留预算内最近记录"
	}
	source.Content = string(encoded)
	return source
}
