package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func temperatureFixture() ([]foundation.MarketSnapshotRow, foundation.SourceMeta, time.Time) {
	now := time.Date(2026, 10, 27, 10, 30, 0, 0, foundation.AStockLocation)
	meta := foundation.SourceMeta{Source: "test:synthetic", TradeDate: "2026-10-27", FetchedAt: now}
	rows := make([]foundation.MarketSnapshotRow, minimumTemperatureSamples)
	for i := range rows {
		rows[i] = foundation.MarketSnapshotRow{Symbol: fmt.Sprintf("%06d.SH", 600000+i), Name: "测试样本", Price: 100, PreviousClose: 100, Open: 100, Volume: 100, TradeDate: meta.TradeDate}
	}
	return rows, meta, now
}

func TestTemperatureMappingProperties(t *testing.T) {
	if summarizeTemperature(nil) != nil {
		t.Fatal("empty input must stay unavailable")
	}
	for i := -10000; i <= 10000; i++ {
		r := float64(i) / 100
		v := stockTemperature(r)
		if v < 0 || v > 100 || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v+stockTemperature(-r)-100) > 1e-10 {
			t.Fatalf("out of range or asymmetric at %v: %v", r, v)
		}
		if stockTemperature(r+0.001) < v {
			t.Fatalf("temperature is not monotonic at %v", r)
		}
	}
	if stockTemperature(0) != 50 || stockTemperature(2) != 100 || stockTemperature(-2) != 0 || stockTemperature(0.01) >= 52 || stockTemperature(-0.01) <= 48 {
		t.Fatal("neutral, saturation or zero-crossing behavior changed")
	}
	if math.Abs(summarizeTemperature([]float64{-10, -1, 0, 1, 10}).Value-50) > 1e-10 {
		t.Fatal("symmetric markets must be neutral")
	}
}

func TestTemperatureBreadthAndBoundedOutliers(t *testing.T) {
	returns := make([]float64, 5000)
	for i := range returns {
		returns[i] = -1
		if i < 100 {
			returns[i] = 10
		}
	}
	before := summarizeTemperature(returns)
	if before.Value >= 10 || before.Up != 100 || before.Down != 4900 || before.MedianChangePercent != -1 {
		t.Fatalf("a small strong group must not hide broad losses: %+v", before)
	}
	after := summarizeTemperature(append(returns, 1000))
	if math.Abs(after.Value-before.Value) > 100/float64(len(returns)+1) {
		t.Fatal("one extreme stock dominates the average")
	}
	for i, j := 0, len(returns)-1; i < j; i, j = i+1, j-1 {
		returns[i], returns[j] = returns[j], returns[i]
	}
	if math.Abs(summarizeTemperature(returns).Value-before.Value) > 1e-9 {
		t.Fatal("input order changed the reading")
	}
}

func TestTemperatureUsesPricesAndPreservesRealZero(t *testing.T) {
	rows, meta, now := temperatureFixture()
	for i := range rows {
		rows[i].Price = 98
		rows[i].ChangePercent = 99 // Source change field must not be trusted.
	}
	result, err := CalculateTemperature(rows, meta, now)
	if err != nil || result.Value != 0 || result.Down != len(rows) || result.Model != TemperatureModel || result.Stale {
		t.Fatalf("real zero must remain available: %+v, %v", result, err)
	}
	for i := range rows {
		rows[i].Price = 100
	}
	result, err = CalculateTemperature(rows, meta, now)
	if err != nil || math.Abs(result.Value-50) > 1e-9 || result.Flat != len(rows) || result.UpPercent != 0 {
		t.Fatalf("unchanged sample: %+v, %v", result, err)
	}
	data, err := json.Marshal(Facts{Temperature: result})
	if err != nil || !strings.Contains(string(data), `"temperature":`) || !strings.Contains(string(data), `"up_percent":0`) {
		t.Fatalf("real zero fields omitted: %s, %v", data, err)
	}
}

func TestTemperatureSampleFilteringAndDeduplication(t *testing.T) {
	rows, meta, now := temperatureFixture()
	invalid := []foundation.MarketSnapshotRow{}
	for i, mutate := range []func(*foundation.MarketSnapshotRow){
		func(r *foundation.MarketSnapshotRow) { r.Name = "*ST测试" },
		func(r *foundation.MarketSnapshotRow) { r.IsST = true },
		func(r *foundation.MarketSnapshotRow) { r.Suspended = true },
		func(r *foundation.MarketSnapshotRow) { r.Price = 0 },
		func(r *foundation.MarketSnapshotRow) { r.PreviousClose = math.NaN() },
		func(r *foundation.MarketSnapshotRow) { r.Open = math.Inf(1) },
		func(r *foundation.MarketSnapshotRow) { r.Volume = 0 },
		func(r *foundation.MarketSnapshotRow) { r.TradeDate = "2026-10-26" },
		func(r *foundation.MarketSnapshotRow) { r.Symbol = "920001.BJ" },
		func(r *foundation.MarketSnapshotRow) { r.Symbol = "510300.SH" },
		func(r *foundation.MarketSnapshotRow) { r.Symbol = "60000.SH" },
	} {
		r := rows[0]
		r.Symbol = fmt.Sprintf("%06d.SH", 603000+i)
		mutate(&r)
		invalid = append(invalid, r)
	}
	duplicate := rows[0]
	duplicate.Symbol, duplicate.Price = "sh600000", 102
	starST, chinextST := rows[0], rows[0]
	starST.Symbol, starST.Name = "688001.SH", "*ST测试"
	chinextST.Symbol, chinextST.Name = "300001.SZ", "ST测试"
	rows = append(rows, invalid...)
	rows = append(rows, duplicate, starST, chinextST)
	result, err := CalculateTemperature(rows, meta, now)
	if err != nil || result.SampleCount != minimumTemperatureSamples+2 || result.Up != 0 || math.Abs(result.Value-50) > 1e-9 {
		t.Fatalf("sample filter changed: %+v, %v", result, err)
	}
}

func TestTemperatureRejectsIncompleteOrUnverifiableSnapshots(t *testing.T) {
	for _, scenario := range []string{"partial", "empty", "too few", "no date", "no time", "no source", "future time", "future date"} {
		t.Run(scenario, func(t *testing.T) {
			rows, meta, now := temperatureFixture()
			switch scenario {
			case "partial":
				meta.Stale = true
			case "empty":
				rows = nil
			case "too few":
				rows = rows[:len(rows)-1]
			case "no date":
				meta.TradeDate = ""
			case "no time":
				meta.FetchedAt = time.Time{}
			case "no source":
				meta.Source = ""
			case "future time":
				meta.FetchedAt = now.Add(2 * time.Minute)
			case "future date":
				meta.TradeDate = "2026-10-28"
			}
			if result, err := CalculateTemperature(rows, meta, now); result != nil || err == nil {
				t.Fatalf("unverifiable input accepted: %+v, %v", result, err)
			}
		})
	}
}

func TestTemperatureCachedMetadata(t *testing.T) {
	rows, meta, now := temperatureFixture()
	meta.FetchedAt = now.Add(-2 * time.Minute)
	result, err := CalculateTemperature(rows, meta, now)
	if err != nil || !result.Stale || result.Warning == "" || result.FetchedAt != meta.FetchedAt.Format(time.RFC3339) {
		t.Fatalf("cached reading must retain original time: %+v, %v", result, err)
	}
	meta.FetchedAt, meta.CarryForward = now, true
	result, err = CalculateTemperature(rows, meta, now)
	if err != nil || !result.Stale {
		t.Fatalf("carry-forward must be marked cached: %+v, %v", result, err)
	}
}

func TestTemperatureFeedIsolationAndPrompt(t *testing.T) {
	for _, phase := range []Phase{PhasePreOpen, PhaseIntraday, PhasePostClose, PhaseRest} {
		facts := CollectFacts(context.Background(), phase, Feed{
			Temperature: func(context.Context) (*Temperature, error) {
				return &Temperature{Value: 0, Stale: true, Model: TemperatureModel}, errors.New("refresh failed")
			},
			Emotion: func(context.Context) (*Emotion, error) { return &Emotion{Score: 88}, nil },
		})
		if facts.Temperature == nil || facts.Temperature.Value != 0 || facts.Emotion.Score != 88 {
			t.Fatalf("independent facts lost in phase %s: %+v", phase, facts)
		}
		for _, source := range facts.Sources {
			if source.Name == "市场温度" && source.OK {
				t.Fatal("failed refresh reported as live")
			}
		}
		prompt := BuildPrompt(PhaseIntraday, "2026-10-27", "测试", facts, &Brief{Facts: facts})
		if !strings.Contains(prompt, `"temperature":`) || !strings.Contains(prompt, "禁止用情绪分代替") {
			t.Fatal("temperature facts or constraints missing from prompt")
		}
	}
	facts := CollectFacts(context.Background(), PhaseIntraday, Feed{Temperature: func(context.Context) (*Temperature, error) { return nil, errors.New("unavailable") }})
	if facts.Temperature != nil || len(facts.Sources) != 1 || facts.Sources[0].OK {
		t.Fatal("missing temperature must remain missing")
	}
	encoded, _ := json.Marshal(Facts{})
	if strings.Contains(string(encoded), `"temperature"`) {
		t.Fatal("old facts must remain compatible")
	}
}
