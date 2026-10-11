package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// resonancePITAudit 是共振看板的 PIT 审计结论（工具化判定，替代旧硬编码 passed=false）。
//
// 判定全部来自真实数据：看板 JSON 自带的 pit 块（Python 侧 report_resonance.py
// 按资讯 15:00 锚可得性 + 涨停快照 fetched_at 计算）+ 看板覆盖时效。
// 每一项检查都带可操作的 detail，前端直接渲染；未通过时绝不放行绩效/实盘语义。
type resonancePITCheck struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type resonancePITAudit struct {
	Passed    bool                `json:"passed"`
	CheckedAt string              `json:"checked_at"`
	TradeDate string              `json:"trade_date"`
	Checks    []resonancePITCheck `json:"checks"`
	Note      string              `json:"note"`
}

func mapBool(doc map[string]any, key string) bool {
	if value, ok := doc[key].(bool); ok {
		return value
	}
	return false
}

func mapNumber(doc map[string]any, key string) float64 {
	if value, ok := doc[key].(float64); ok {
		return value
	}
	return 0
}

// buildResonancePITAudit 从看板内容计算真实 PIT 审计（只读，不写任何文件）。
func buildResonancePITAudit(doc map[string]any, boardModTime time.Time) resonancePITAudit {
	tradeDate, _ := doc["trade_date"].(string)
	pitBlock, _ := doc["pit"].(map[string]any)
	newsPIT := pitBlock != nil && mapBool(pitBlock, "news_pit_ok")
	moneyPIT := pitBlock != nil && mapBool(pitBlock, "money_pit_ok")
	availableNews := 0.0
	freshNews := 0.0
	if pitBlock != nil {
		availableNews = mapNumber(pitBlock, "available_news")
		freshNews = mapNumber(pitBlock, "fresh_news")
	}

	// 覆盖时效：看板交易日不应落后当前交易日过多（节假日顺延按 7 个自然日宽限）。
	freshBoard := false
	freshDetail := "看板缺少 trade_date"
	if parsed, err := time.ParseInLocation("2006-01-02", tradeDate, time.Local); err == nil {
		ageDays := int(time.Since(parsed).Hours() / 24)
		freshBoard = ageDays <= 7
		freshDetail = fmt.Sprintf("数据日 %s（距今 %d 天）", tradeDate, ageDays)
	}

	newsDetail := "资讯须由常驻采集器在交易时段（09:15–15:05）持续抓取，事后补抓的资讯永远过不了此闸门。"
	if newsPIT {
		newsDetail = "通过。"
	}
	checks := []resonancePITCheck{
		{
			ID:     "news_pit",
			Name:   "资讯轴 PIT（15:00 锚前已可得 ≥3 条，且发布时间落在 24h 窗口内）",
			OK:     newsPIT,
			Detail: fmt.Sprintf("新鲜可得 %d 条 / 当时可得 %d 条。%s", int(freshNews), int(availableNews), newsDetail),
		},
		{
			ID:   "money_pit",
			Name: "行情轴 PIT（涨停快照 fetched_at 距当日 15:00 ≤1 小时，非事后补抓）",
			OK:   moneyPIT,
			Detail: map[bool]string{true: "快照为盘中/临近收盘抓取，通过。", false: "当前快照为事后补抓，行情轴判定不可用。"}[moneyPIT],
		},
		{
			ID:     "board_fresh",
			Name:   "看板覆盖时效（trade_date 距今 ≤7 天）",
			OK:     freshBoard,
			Detail: freshDetail,
		},
	}
	passed := newsPIT && moneyPIT && freshBoard
	note := "PIT 审计通过前，共振标签仅作观察，不得用于历史绩效评估或实盘指令"
	if passed {
		note = "PIT 审计通过（基于看板产出当时的真实数据可得性）"
	}
	return resonancePITAudit{
		Passed:    passed,
		CheckedAt: time.Now().Format(time.RFC3339),
		TradeDate: tradeDate,
		Checks:    checks,
		Note:      note,
	}
}

// resonanceBoard 返回双确认共振看板的结构化快照（只读透传）。
//
// 数据源是 Python 侧 news_ferment 的产出文件（默认 C:\quant\news_ferment\双确认看板.json），
// 由 report_resonance.py 与 HTML 看板同一次运行写出。本 handler 只读不写、不重算，
// 也不对内容做任何解读 —— 界面上的「仅供观察」约束由前端呈现。
//
// ⚠ PIT 纪律：共振标签在 PIT 审计通过前不得用于历史绩效评估，不得生成实盘交易指令。
// 本接口只回传观察名单与分值，不含任何交易建议字段。
//
//	GET /api/v1/strategy/resonance/board
//	→ { ok, board_file, updated_at, pit_audit, payload }
type resonanceBoardCache struct {
	mu        sync.Mutex
	payload   json.RawMessage
	fetchedAt time.Time
	ttl       time.Duration
}

var resonanceBoard = &resonanceBoardCache{ttl: 30 * time.Second}

func (s *Server) resonanceBoardHandler(w http.ResponseWriter, r *http.Request) {
	resonanceBoard.mu.Lock()
	defer resonanceBoard.mu.Unlock()
	if resonanceBoard.payload != nil && time.Since(resonanceBoard.fetchedAt) < resonanceBoard.ttl {
		writeRawJSON(w, http.StatusOK, resonanceBoard.payload)
		return
	}
	file := os.Getenv("A_STOCK_RESONANCE_BOARD")
	if file == "" {
		file = filepath.Join(`C:\quant\news_ferment`, "双确认看板.json")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":        false,
			"error":     err.Error(),
			"board_file": file,
			"hint":      "先在 C:\\quant\\news_ferment 运行 report_resonance.py 生成双确认看板.json",
		})
		return
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "双确认看板.json 解析失败", "board_file": file})
		return
	}
	stat, statErr := os.Stat(file)
	// PIT 审计工具化：判定来自看板真实数据（资讯/行情轴闸门 + 覆盖时效），
	// 不再硬编码 passed=false；未通过时逐项给出原因与修复路径。
	var boardModTime time.Time
	if statErr == nil && stat != nil {
		boardModTime = stat.ModTime()
	}
	pitAudit := buildResonancePITAudit(doc, boardModTime)
	payload := map[string]any{
		"ok":         true,
		"board_file": file,
		"updated_at": "",
		"pit_audit":  pitAudit,
		"payload":    doc,
	}
	if statErr == nil {
		payload["updated_at"] = stat.ModTime().Format(time.RFC3339)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resonanceBoard.payload = encoded
	resonanceBoard.fetchedAt = time.Now()
	writeRawJSON(w, http.StatusOK, encoded)
}
