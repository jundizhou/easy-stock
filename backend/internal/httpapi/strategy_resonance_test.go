package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestBuildResonancePITAuditFailsHonest(t *testing.T) {
	doc := map[string]any{
		"trade_date": "2026-10-09",
		"pit": map[string]any{
			"news_pit_ok":     false,
			"money_pit_ok":    false,
			"available_news":  float64(44),
			"fresh_news":      float64(0),
		},
	}
	audit := buildResonancePITAudit(doc, time.Now())
	if audit.Passed {
		t.Fatalf("资讯/行情轴均未通过时 passed 必须为 false")
	}
	if len(audit.Checks) != 3 {
		t.Fatalf("应有 3 项检查，实际 %d", len(audit.Checks))
	}
	if audit.TradeDate != "2026-10-09" {
		t.Fatalf("trade_date 应透传，实际 %q", audit.TradeDate)
	}
	var newsCheck *resonancePITCheck
	for i := range audit.Checks {
		if audit.Checks[i].ID == "news_pit" {
			newsCheck = &audit.Checks[i]
		}
	}
	if newsCheck == nil || newsCheck.OK {
		t.Fatalf("news_pit 检查应存在且未通过")
	}
	// 未通过的检查必须给出可操作的修复路径，而不是一句空话。
	if !strings.Contains(newsCheck.Detail, "交易时段") {
		t.Fatalf("未通过详情应包含修复路径，实际 %q", newsCheck.Detail)
	}
}

func TestBuildResonancePITAuditPassesWhenAxesPass(t *testing.T) {
	doc := map[string]any{
		"trade_date": time.Now().Format("2006-01-02"),
		"pit": map[string]any{
			"news_pit_ok":  true,
			"money_pit_ok": true,
			"available_news": float64(30),
			"fresh_news":     float64(12),
		},
	}
	audit := buildResonancePITAudit(doc, time.Now())
	if !audit.Passed {
		t.Fatalf("两轴通过 + 看板新鲜时 passed 必须为 true；checks=%+v", audit.Checks)
	}
	if !strings.Contains(audit.Note, "通过") {
		t.Fatalf("通过时 note 应说明通过，实际 %q", audit.Note)
	}
}

func TestBuildResonancePITAuditStaleBoardFails(t *testing.T) {
	doc := map[string]any{
		"trade_date": "2026-01-01",
		"pit": map[string]any{
			"news_pit_ok":  true,
			"money_pit_ok": true,
		},
	}
	audit := buildResonancePITAudit(doc, time.Now())
	if audit.Passed {
		t.Fatalf("看板覆盖过期（trade_date 距今 >7 天）时不得通过")
	}
	for _, check := range audit.Checks {
		if check.ID == "board_fresh" && check.OK {
			t.Fatalf("board_fresh 检查不应通过")
		}
	}
}
