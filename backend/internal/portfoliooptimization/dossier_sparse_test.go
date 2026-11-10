package portfoliooptimization

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestSparseStockFactsRoundTripPreservesUnknownZeroAndExactNumbers(t *testing.T) {
	rows := []any{}
	for i := 0; i < 16; i++ {
		values := make([]any, 64)
		values[i%2], values[3], values[8], values[14] = 0, false, json.Number("9007199254740993"), map[string]any{"source_ids": []string{"e-execution"}, "status": "unknown"}
		rows = append(rows, []any{fmt.Sprintf("600%03d.SH", i), values})
	}
	table := map[string]any{"rows": rows, "row_columns": []string{"symbol", "values"}, "shared": map[string]any{"date": "2026-11-10"}}
	before, _ := json.Marshal(table)
	compactSparseStockFacts(table)
	sets, ok := table["column_sets"].([][]int)
	if !ok {
		t.Fatal("sparse mixed table was not compressed")
	}
	after, _ := json.Marshal(table)
	if len(after) >= len(before) || !reflect.DeepEqual(table["shared"], map[string]any{"date": "2026-11-10"}) {
		t.Fatal("encoding enlarged facts or changed shared facts")
	}
	for i, item := range table["rows"].([]any) {
		row := item.([]any)
		values := make([]any, 64)
		for n, column := range sets[row[1].(int)] {
			values[column] = row[2].([]any)[n]
		}
		if !reflect.DeepEqual([]any{row[0], values}, rows[i]) {
			t.Fatal("sparse facts changed identity, value or unknown status", i)
		}
	}
}

func TestDenseStockFactsKeepSmallerRepresentation(t *testing.T) {
	table := map[string]any{"rows": []any{[]any{"600000.SH", []any{1, 2, 3}}}, "row_columns": []string{"symbol", "values"}}
	before, _ := json.Marshal(table)
	compactSparseStockFacts(table)
	after, _ := json.Marshal(table)
	if string(before) != string(after) {
		t.Fatal("dense table unnecessarily changed representation")
	}
}

func TestPackedETFJudgmentsRoundTripKeepsCitationsAndUnknownFields(t *testing.T) {
	judgments := map[string]any{
		"technical": map[string]any{"status": "mixed", "text": "短期反弹与中期下行并存", "source_ids": []string{"e-direction-technical"}},
		"news":      map[string]any{"status": "unknown", "text": "未核实催化"},
		"entry":     map[string]any{"status": "blocked", "text": "执行前提未解除", "source_ids": nil},
		"future":    map[string]any{"status": "unknown", "text": "新字段必须保留", "source_ids": []string{"e-extra"}, "extra": "不能丢弃"},
	}
	before, _ := json.Marshal(judgments)
	card := map[string]any{"etf": map[string]any{"judgments": judgments}}
	if !packETFJudgments(card) {
		t.Fatal("ETF judgments were not encoded")
	}
	restored := map[string]any{}
	for key, raw := range judgments {
		if row, ok := raw.([]any); ok {
			object := map[string]any{}
			for i, value := range row {
				object[etfJudgmentColumns[i]] = value
			}
			restored[key] = object
		} else {
			restored[key] = raw
		}
	}
	after, _ := json.Marshal(restored)
	if string(before) != string(after) {
		t.Fatal("judgment packing changed claims, citations or missing/null fields")
	}
}
