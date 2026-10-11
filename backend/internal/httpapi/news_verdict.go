package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// newsVerdict 调用 news_ferment/verdict.py 判定引擎，返回每条资讯的利好/利空研判。
//
//	GET /api/v1/news/verdict?days=2&limit=100
//
// 研判引擎（verdict.py）位于独立仓库 C:\quant\news_ferment，需要本机同时具备：
//   - news_ferment 目录（环境变量 NEWS_FERMENT_DIR 可覆盖，默认 C:\quant\news_ferment）
//   - Python 解释器（环境变量 PYTHON_CMD 可覆盖，默认 python）
//
// 未配置时返回 503，不阻断其他功能。研判仅供研究观察，不生成交易指令。
func (s *Server) newsVerdict(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days := 2
	if v := strings.TrimSpace(q.Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 30 {
			days = n
		}
	}
	limit := 100
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	dir := strings.TrimSpace(os.Getenv("NEWS_FERMENT_DIR"))
	if dir == "" {
		dir = `C:\quant\news_ferment`
	}
	script := filepath.Join(dir, "verdict.py")
	if _, err := os.Stat(script); err != nil {
		writeError(w, http.StatusServiceUnavailable,
			"资讯研判引擎未配置：找不到 "+script+"（设置环境变量 NEWS_FERMENT_DIR 指向 news_ferment 目录）")
		return
	}
	py := strings.TrimSpace(os.Getenv("PYTHON_CMD"))
	if py == "" {
		py = "python"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, py, script, "--json", "--days", strconv.Itoa(days))
	cmd.Dir = dir
	// 强制子进程 Python 以 UTF-8 输出：Windows 下管道 stdout 默认走系统
	// locale（cp936），按 UTF-8 解码会全盘乱码（2026-10-11 实测）。
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	out, err := cmd.Output()
	if err != nil {
		writeError(w, http.StatusBadGateway, "研判引擎执行失败："+err.Error())
		return
	}
	var items []map[string]any
	if err := json.Unmarshal(out, &items); err != nil {
		writeError(w, http.StatusBadGateway, "研判引擎输出解析失败："+err.Error())
		return
	}
	if len(items) > limit {
		items = items[:limit]
	}

	// 汇总批次是否启用真实 LLM 抽取（首条即代表整批）
	llmUsed := false
	if len(items) > 0 {
		if ex, ok := items[0]["extraction"].(map[string]any); ok {
			if v, ok := ex["llm_used"].(bool); ok {
				llmUsed = v
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"engine": map[string]any{
			"configured": true,
			"script":     script,
			"days":       days,
			"limit":      limit,
			"llm_used":   llmUsed,
		},
	})
}
