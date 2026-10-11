package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
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

// newsFerment 调用 news_ferment/ferment.py，返回题材发酵（催化）排行榜。
//
//	GET /api/v1/news/ferment?window=48&days=2&top=20
//
// 与 newsVerdict 共用同一引擎目录（NEWS_FERMENT_DIR）与 Python（PYTHON_CMD）。
// ferment.py 使用与 verdict.py --json 相同的 load_merged(days) 记录源，因此题材
// 样本的 uid 必为研判集子集，前端可用 uid 下钻 join 到逐条研判详情。
// 未配置时返回 503，不阻断其他功能。排行仅供研究观察，不生成交易指令。
func (s *Server) newsFerment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	window := 48.0
	if v := strings.TrimSpace(q.Get("window")); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 6 && n <= 168 {
			window = n
		}
	}
	days := 2
	if v := strings.TrimSpace(q.Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 30 {
			days = n
		}
	}
	top := 20
	if v := strings.TrimSpace(q.Get("top")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			top = n
		}
	}

	dir := strings.TrimSpace(os.Getenv("NEWS_FERMENT_DIR"))
	if dir == "" {
		dir = `C:\quant\news_ferment`
	}
	script := filepath.Join(dir, "ferment.py")
	if _, err := os.Stat(script); err != nil {
		writeError(w, http.StatusServiceUnavailable,
			"题材发酵引擎未配置：找不到 "+script+"（设置环境变量 NEWS_FERMENT_DIR 指向 news_ferment 目录）")
		return
	}
	py := strings.TrimSpace(os.Getenv("PYTHON_CMD"))
	if py == "" {
		py = "python"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, py, script, "--json",
		"--window", strconv.FormatFloat(window, 'f', -1, 64),
		"--days", strconv.Itoa(days),
		"--top", strconv.Itoa(top))
	cmd.Dir = dir
	// 同 newsVerdict：强制子进程 UTF-8 输出，避免 Windows 管道 cp936 乱码。
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	out, err := cmd.Output()
	if err != nil {
		writeError(w, http.StatusBadGateway, "题材发酵引擎执行失败："+err.Error())
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		writeError(w, http.StatusBadGateway, "题材发酵引擎输出解析失败："+err.Error())
		return
	}
	payload["engine"] = map[string]any{
		"configured": true,
		"script":     script,
		"window":     window,
		"days":       days,
		"top":        top,
	}
	writeJSON(w, http.StatusOK, payload)
}

// newsThemeStocks 把「资讯题材名」映射到行情侧题材，返回该题材的代表个股（龙头/涨停池）。
//
//	GET /api/v1/news/theme/stocks?theme=军工&limit=20
//
// 复用已有的行情侧题材提供方（s.themeOverview），按名称/别名做双向包含匹配，
// 命中后返回其 LeaderStocks（按连板数与涨幅排序）。这是「题材热 → 哪些票」的下钻，
// 位于资讯研判板块内。未配置或未命中时返回 matched=false（不报错），便于前端降级。
// 代表个股仅供研究观察，不是成分全集，不构成交易建议。
func (s *Server) newsThemeStocks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	theme := strings.TrimSpace(q.Get("theme"))
	if theme == "" {
		writeError(w, http.StatusBadRequest, "缺少 theme 参数")
		return
	}
	limit := 20
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	if s.themeOverview == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"theme": theme, "matched": false, "stocks": []any{},
			"reason": "行情侧题材提供方不可用（未配置）",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	overviews, meta, err := s.themeOverview.Overviews(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "题材成分查询失败："+err.Error())
		return
	}
	match, ok := matchThemeOverview(overviews, theme)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"theme": theme, "matched": false, "stocks": []any{},
			"reason": "行情侧未找到匹配题材（可能名称口径不同）",
			"meta":   meta,
		})
		return
	}

	stocks := append([]foundation.BoardStock(nil), match.LeaderStocks...)
	sort.SliceStable(stocks, func(i, j int) bool {
		if stocks[i].LimitUpStreak != stocks[j].LimitUpStreak {
			return stocks[i].LimitUpStreak > stocks[j].LimitUpStreak
		}
		return stocks[i].ChangePercent > stocks[j].ChangePercent
	})
	if len(stocks) > limit {
		stocks = stocks[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"theme":          theme,
		"matched":        true,
		"matched_id":     match.Theme,
		"matched_name":   match.Name,
		"change_percent": match.ChangePercent,
		"limit_up_count": match.LimitUpCount,
		"max_streak":     match.MaxStreak,
		"leaders":        match.Leaders,
		"stocks":         stocks,
		"meta":           meta,
		"note":           "代表个股来自行情侧题材（龙头/涨停池），仅供研究观察，非成分全集，不构成交易建议",
	})
}

// matchThemeOverview 按名称/别名把资讯题材名匹配到行情侧题材（精确优先，再双向包含）。
func matchThemeOverview(overviews []foundation.ThemeOverview, theme string) (foundation.ThemeOverview, bool) {
	replacer := strings.NewReplacer(" ", "", "　", "", "-", "", "_", "", "/", "")
	norm := func(s string) string {
		return strings.ToLower(replacer.Replace(strings.TrimSpace(s)))
	}
	t := norm(theme)
	if t == "" {
		return foundation.ThemeOverview{}, false
	}
	// 1) 精确匹配 name / theme / alias
	for _, o := range overviews {
		if norm(o.Name) == t || norm(o.Theme) == t {
			return o, true
		}
		for _, a := range o.Aliases {
			if norm(a) == t {
				return o, true
			}
		}
	}
	// 2) 双向包含（如「有色金属」↔「有色金属板块」）
	for _, o := range overviews {
		if n := norm(o.Name); n != "" && (strings.Contains(n, t) || strings.Contains(t, n)) {
			return o, true
		}
		for _, a := range o.Aliases {
			an := norm(a)
			if an != "" && (strings.Contains(an, t) || strings.Contains(t, an)) {
				return o, true
			}
		}
	}
	return foundation.ThemeOverview{}, false
}
