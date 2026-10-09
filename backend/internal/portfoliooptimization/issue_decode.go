package portfoliooptimization

import (
	pi "easy-stock/backend/internal/portfolioinspection"
	"encoding/json"
	"errors"
	"strings"
)

type IssueDetail struct {
	Name         string           `json:"name,omitempty"`
	Detail       string           `json:"detail,omitempty"`
	Text         string           `json:"text,omitempty"`
	EvidenceRefs []pi.EvidenceRef `json:"evidence_refs,omitempty"`
}

// Models commonly wrap display-only issues as a string or cited descriptions.
// Preserve all text and citations while normalizing the public string list;
// unknown structured fields remain errors, never silently discarded.
func (p *Proposal) UnmarshalJSON(data []byte) error {
	type named Proposal
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	raw := root["issues"]
	delete(root, "issues")
	rest, err := json.Marshal(root)
	if err != nil {
		return err
	}
	var value named
	if err := json.Unmarshal(rest, &value); err != nil {
		return err
	}
	if len(raw) == 0 || string(raw) == "null" {
		*p = Proposal(value)
		return nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		value.Issues = []string{single}
		*p = Proposal(value)
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for _, item := range items {
		var text string
		if json.Unmarshal(item, &text) == nil {
			value.Issues = append(value.Issues, text)
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(item, &fields); err != nil {
			return err
		}
		for key := range fields {
			if key != "name" && key != "detail" && key != "text" && key != "evidence_refs" {
				return errors.New("问题描述含未知字段")
			}
		}
		var issue IssueDetail
		if err := json.Unmarshal(item, &issue); err != nil {
			return err
		}
		parts := []string{}
		for _, s := range []string{issue.Name, issue.Detail, issue.Text} {
			if strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return errors.New("问题描述缺少文字")
		}
		value.Issues = append(value.Issues, strings.Join(parts, "；"))
		value.IssueDetails = append(value.IssueDetails, issue)
	}
	*p = Proposal(value)
	return nil
}
