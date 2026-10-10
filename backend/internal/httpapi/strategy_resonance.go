package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

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
	payload := map[string]any{
		"ok":         true,
		"board_file": file,
		"updated_at": "",
		"pit_audit": map[string]any{
			"passed":  false,
			"note":    "PIT 审计通过前，共振标签仅作观察，不得用于历史绩效评估或实盘指令",
		},
		"payload": doc,
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
