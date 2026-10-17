package portfolioinspection

import (
	"errors"
	"fmt"
	"strings"
)

type ScoringIssue struct {
	Code          string   `json:"code"`
	Path          string   `json:"path"`
	Message       string   `json:"message"`
	AllowedValues []string `json:"allowed_values,omitempty"`
}

type scoringIssues []ScoringIssue

func (issues *scoringIssues) add(code, path, message string) {
	// Bound malformed model output while retaining independent field failures.
	if len(*issues) < 64 {
		*issues = append(*issues, ScoringIssue{Code: code, Path: path, Message: message})
	}
}

func (issues *scoringIssues) collect(code, path string, err error) {
	if err == nil {
		return
	}
	var nested scoringIssues
	if errors.As(err, &nested) {
		for _, issue := range nested {
			p := issue.Path
			if path != "" {
				if strings.HasPrefix(p, "[") {
					p = path + p
				} else {
					p = strings.TrimSuffix(path+"."+p, ".")
				}
			}
			issue.Path = p
			if len(*issues) < 64 {
				*issues = append(*issues, issue)
			}
		}
		return
	}
	issues.add(code, path, err.Error())
}

func (issues scoringIssues) Error() string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s [%s]: %s", issue.Path, issue.Code, issue.Message))
	}
	return strings.Join(parts, "\n")
}

func (issues scoringIssues) err() error {
	if len(issues) == 0 {
		return nil
	}
	return issues
}
