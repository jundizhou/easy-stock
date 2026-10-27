package cockpit

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const TemperatureModel = "local-breadth-v1"

// A market-wide reading must not silently become a watchlist-sized reading.
// Completeness also requires the snapshot provider's non-stale metadata.
const minimumTemperatureSamples = 1000

type Temperature struct {
	Value               float64 `json:"value"`
	Model               string  `json:"model"`
	SampleCount         int     `json:"sample_count"`
	Up                  int     `json:"up"`
	Down                int     `json:"down"`
	Flat                int     `json:"flat"`
	UpPercent           float64 `json:"up_percent"`
	MeanChangePercent   float64 `json:"mean_change_percent"`
	MedianChangePercent float64 `json:"median_change_percent"`
	TradeDate           string  `json:"trade_date"`
	FetchedAt           string  `json:"fetched_at"`
	Source              string  `json:"source"`
	SessionStatus       string  `json:"session_status"`
	Stale               bool    `json:"stale"`
	Warning             string  `json:"warning,omitempty"`
}

// CalculateTemperature uses actual price/previous-close returns, not the shared
// short-term emotion score. Each eligible stock has equal, bounded influence.
// The 0.5 percentage-point smoothing scale is fixed, not fitted to a vendor score.
func CalculateTemperature(rows []foundation.MarketSnapshotRow, meta foundation.SourceMeta, now time.Time) (*Temperature, error) {
	if meta.Stale {
		return nil, fmt.Errorf("市场温度快照不完整或来源已标记过期：%s", meta.FallbackReason)
	}
	tradeDate, err := time.ParseInLocation("2006-01-02", meta.TradeDate, foundation.AStockLocation)
	if err != nil || meta.FetchedAt.IsZero() || strings.TrimSpace(meta.Source) == "" {
		return nil, errors.New("市场温度快照缺少日期、抓取时间或来源")
	}
	if meta.FetchedAt.After(now.Add(time.Minute)) || tradeDate.Format("2006-01-02") > now.In(foundation.AStockLocation).Format("2006-01-02") {
		return nil, errors.New("市场温度快照时间异常")
	}
	returns := make([]float64, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		symbol, err := foundation.NormalizeSymbol(row.Symbol)
		if err != nil || seen[symbol.Canonical] {
			continue
		}
		code := symbol.RawCode
		if len(code) != 6 {
			continue
		}
		if !((symbol.Market == "SH" && strings.HasPrefix(code, "6")) ||
			(symbol.Market == "SZ" && (strings.HasPrefix(code, "0") || strings.HasPrefix(code, "3")))) {
			continue
		}
		mainBoard := strings.HasPrefix(code, "60") || strings.HasPrefix(code, "00")
		if mainBoard && (row.IsST || foundation.IsST(row.Name)) {
			continue
		}
		if row.Suspended || (row.TradeDate != "" && row.TradeDate != meta.TradeDate) ||
			!positiveFinite(row.Price) || !positiveFinite(row.PreviousClose) || !positiveFinite(row.Open) || !positiveFinite(row.Volume) {
			continue
		}
		change := (row.Price/row.PreviousClose - 1) * 100
		if math.IsNaN(change) || math.IsInf(change, 0) {
			continue
		}
		seen[symbol.Canonical] = true
		returns = append(returns, change)
	}
	if len(returns) < minimumTemperatureSamples {
		return nil, fmt.Errorf("市场温度样本不足：有效 %d 只，至少需要 %d 只", len(returns), minimumTemperatureSamples)
	}
	temperature := summarizeTemperature(returns)
	temperature.Model = TemperatureModel
	temperature.TradeDate = meta.TradeDate
	temperature.FetchedAt = meta.FetchedAt.In(foundation.AStockLocation).Format(time.RFC3339)
	temperature.Source = meta.Source
	switch PhaseAt(now).Phase {
	case PhasePreOpen:
		temperature.SessionStatus = "盘前快照"
	case PhaseIntraday:
		temperature.SessionStatus = "盘中快照"
	case PhasePostClose:
		temperature.SessionStatus = "盘后快照"
	default:
		temperature.SessionStatus = "休市快照"
	}
	temperature.Stale = meta.CarryForward || now.Sub(meta.FetchedAt) > time.Minute || meta.TradeDate != now.In(foundation.AStockLocation).Format("2006-01-02")
	if temperature.Stale {
		temperature.Warning = "使用缓存快照，非当前实时读数"
	}
	return temperature, nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func stockTemperature(change float64) float64 {
	return 50 + 50*math.Max(-1, math.Min(1, math.Tanh(change/0.5)/math.Tanh(4)))
}

func summarizeTemperature(returns []float64) *Temperature {
	result := &Temperature{SampleCount: len(returns)}
	if len(returns) == 0 {
		return nil
	}
	for _, change := range returns {
		result.Value += stockTemperature(change) / float64(len(returns))
		result.MeanChangePercent += change / float64(len(returns))
		switch {
		case change > 0:
			result.Up++
		case change < 0:
			result.Down++
		default:
			result.Flat++
		}
	}
	result.Value = math.Max(0, math.Min(100, result.Value))
	result.UpPercent = 100 * float64(result.Up) / float64(result.SampleCount)
	sorted := append([]float64(nil), returns...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	result.MedianChangePercent = sorted[middle]
	if len(sorted)%2 == 0 {
		result.MedianChangePercent = sorted[middle-1]/2 + sorted[middle]/2
	}
	return result
}
