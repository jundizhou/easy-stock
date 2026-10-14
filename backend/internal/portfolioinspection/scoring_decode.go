package portfolioinspection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var explanationListLimits = []struct {
	field string
	limit int
}{
	{"primary_risks", 8}, {"concentration_findings", 8},
	{"adjustment_order", 10}, {"next_checklist", 10}, {"data_limitations", 10},
}

// Only normalize narrative lists at the model boundary. Scores, holdings, facts
// and references still go through their ordinary strict decoding and validation.
func decodeScoringReport(content string) (AIReport, error) {
	var issues scoringIssues
	var raw map[string]json.RawMessage
	if err := decodeJSONObject(content, &raw); err != nil {
		return AIReport{}, err
	}
	details := map[string]ExplanationDetail{}
	if value, ok := raw["explanation_details"]; ok {
		if err := decodeStrict(value, &details); err != nil {
			issues.collect("format", "explanation_details", err)
		}
		if details == nil {
			details = map[string]ExplanationDetail{}
		}
	}
	for _, list := range explanationListLimits {
		if value, ok := raw[list.field]; ok {
			normalized, err := normalizeExplanationList(value, list.field, details)
			if err != nil {
				issues.collect("format", list.field, err)
				delete(raw, list.field)
				continue
			}
			raw[list.field] = normalized
		}
	}
	if value, ok := raw["dimensions"]; ok {
		var dimensions []map[string]json.RawMessage
		if err := json.Unmarshal(value, &dimensions); err != nil {
			dimensions = nil
			// The four canonical keys also unambiguously identify a keyed object.
			// Normalize container shape only, preserving every value and citation.
			var keyed map[string]map[string]json.RawMessage
			if json.Unmarshal(value, &keyed) != nil || len(keyed) != len(scoreRubric) {
				issues.collect("format", "dimensions", err)
			}
			for _, rubric := range scoreRubric {
				d, exists := keyed[rubric.Key]
				if !exists {
					issues.add("format", "dimensions."+rubric.Key, "评分对象缺少规范维度")
					continue
				}
				if d == nil {
					issues.add("format", "dimensions."+rubric.Key, "评分维度必须是非空对象")
					continue
				}
				if rawKey, exists := d["key"]; exists {
					var key string
					if json.Unmarshal(rawKey, &key) != nil || key != rubric.Key {
						issues.add("format", "dimensions."+rubric.Key, "评分对象key与维度名称冲突")
					}
				} else {
					d["key"], _ = json.Marshal(rubric.Key)
				}
				dimensions = append(dimensions, d)
			}
		}
		for _, d := range dimensions {
			var key string
			if err := json.Unmarshal(d["key"], &key); err != nil {
				issues.collect("format", "dimensions.key", err)
			}
			if value, ok := d["limitations"]; ok {
				normalized, err := normalizeExplanationList(value, "dimensions."+key+".limitations", details)
				if err != nil {
					issues.collect("format", "dimensions."+key+".limitations", err)
					delete(d, "limitations")
					continue
				}
				d["limitations"] = normalized
			}
		}
		raw["dimensions"], _ = json.Marshal(dimensions)
	}
	raw["explanation_details"], _ = json.Marshal(details)
	var report AIReport
	// Decode independent top-level fields even if one has the wrong shape.
	// Partial values are used only to collect diagnostics, never published.
	value := reflect.ValueOf(&report).Elem()
	for i := 0; i < value.NumField(); i++ {
		key := strings.Split(value.Type().Field(i).Tag.Get("json"), ",")[0]
		if data, ok := raw[key]; ok {
			issues.collect("format", key, json.Unmarshal(data, value.Field(i).Addr().Interface()))
		}
	}
	return report, issues.err()
}

func normalizeExplanationList(value json.RawMessage, field string, details map[string]ExplanationDetail) (json.RawMessage, error) {
	var items []json.RawMessage
	var single string
	if json.Unmarshal(value, &single) == nil && strings.TrimSpace(single) != "" {
		// A single narrative item has the same meaning as a one-item list.
		// Keep its text; numeric scores and evidence are not normalized here.
		value, _ = json.Marshal([]string{single})
	}
	if err := json.Unmarshal(value, &items); err != nil {
		return nil, fmt.Errorf("%s必须是说明列表: %w", field, err)
	}
	texts := make([]string, 0, len(items))
	for i, item := range items {
		path := fmt.Sprintf("%s[%d]", field, i)
		text, detail, err := decodeExplanation(item)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		texts = append(texts, text)
		if len(detail.EvidenceRefs) > 0 || len(detail.Symbols) > 0 {
			// Merge rather than overwrite so conflicting or invalid references
			// supplied through either form cannot escape validation.
			prior := details[path]
			prior.EvidenceRefs = append(prior.EvidenceRefs, detail.EvidenceRefs...)
			prior.Symbols = append(prior.Symbols, detail.Symbols...)
			details[path] = prior
		}
	}
	return json.Marshal(texts)
}

func decodeExplanation(value json.RawMessage) (string, ExplanationDetail, error) {
	var text string
	if err := json.Unmarshal(value, &text); err == nil && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), ExplanationDetail{}, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil || object == nil {
		return "", ExplanationDetail{}, errors.New("说明须为非空文字或含明确文字字段的对象")
	}
	// Keep a finite vocabulary. Unknown fields must be repaired by the model,
	// especially unrecognized evidence shapes, rather than silently discarded.
	textFields := []string{"title", "name", "risk", "finding", "text", "description", "reason", "detail", "impact", "action", "condition", "limitation", "recommendation"}
	allowed := map[string]bool{"evidence_refs": true, "symbols": true, "risk_id": true, "priority": true, "severity": true}
	for _, key := range textFields {
		allowed[key] = true
	}
	for key := range object {
		if !allowed[key] {
			return "", ExplanationDetail{}, fmt.Errorf("不支持说明字段%s，请使用文字并将引用写入evidence_refs", key)
		}
	}
	parts := []string{}
	for _, key := range textFields {
		if raw, ok := object[key]; ok {
			var part string
			if err := json.Unmarshal(raw, &part); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return "", ExplanationDetail{}, fmt.Errorf("%s必须是文字", key)
			}
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
	}
	if len(parts) == 0 {
		return "", ExplanationDetail{}, errors.New("说明对象缺少有效文字")
	}
	for _, key := range []string{"risk_id", "priority", "severity"} {
		if raw, ok := object[key]; ok {
			var label string
			if err := json.Unmarshal(raw, &label); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return "", ExplanationDetail{}, fmt.Errorf("%s必须是文字", key)
			}
			if label = strings.TrimSpace(label); label != "" && key != "risk_id" {
				prefix := map[string]string{"priority": "优先级", "severity": "风险程度"}[key]
				parts = append(parts, prefix+"："+label)
			}
		}
	}
	detail := ExplanationDetail{}
	if raw, ok := object["evidence_refs"]; ok {
		if err := decodeStrict(raw, &detail.EvidenceRefs); err != nil {
			return "", ExplanationDetail{}, fmt.Errorf("evidence_refs: %w", err)
		}
	}
	if raw, ok := object["symbols"]; ok {
		if err := json.Unmarshal(raw, &detail.Symbols); err != nil {
			return "", ExplanationDetail{}, fmt.Errorf("symbols: %w", err)
		}
	}
	return strings.Join(parts, "；"), detail, nil
}

func decodeStrict(value []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func validateExplanationDetails(report *AIReport, weights map[string]int, checkRefs func([]EvidenceRef) error) error {
	var issues scoringIssues
	paths := map[string]bool{}
	addPaths := func(field string, items []string, limit int) {
		if len(items) > limit {
			issues.add("shape", field, fmt.Sprintf("%s最多%d条说明", field, limit))
		}
		for i, text := range items {
			if strings.TrimSpace(text) == "" {
				issues.add("shape", fmt.Sprintf("%s[%d]", field, i), "说明不能为空")
			}
			paths[fmt.Sprintf("%s[%d]", field, i)] = true
		}
	}
	lists := [][]string{report.PrimaryRisks, report.ConcentrationFinding, report.AdjustmentOrder, report.NextChecklist, report.DataLimitations}
	for i, list := range explanationListLimits {
		addPaths(list.field, lists[i], list.limit)
	}
	for _, d := range report.Dimensions {
		addPaths("dimensions."+d.Key+".limitations", d.Limitations, 10)
	}
	keys := make([]string, 0, len(report.ExplanationDetails))
	for path := range report.ExplanationDetails {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		detail := report.ExplanationDetails[path]
		if !paths[path] {
			issues.add("shape", path, fmt.Sprintf("说明引用位置%s不存在", path))
		}
		if len(detail.EvidenceRefs) > 0 {
			if err := checkRefs(detail.EvidenceRefs); err != nil {
				issues.collect("evidence", path, err)
			}
		}
		for _, symbol := range detail.Symbols {
			if _, ok := weights[symbol]; !ok {
				issues.add("holding", path, fmt.Sprintf("%s涉及的股票%s不在持仓中", path, symbol))
			}
		}
	}
	return issues.err()
}
