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
// 复用已有的行情侧题材提供方（s.themeOverview），先按名称/别名做字面匹配，
// 再经「同义词桥」（newsThemeSynonyms）把资讯题材词桥接到行情侧口径，按分择优。
// 命中后返回其 LeaderStocks（按连板数与涨幅排序）。这是「题材热 → 哪些票」的下钻，
// 位于资讯研判板块内。未配置或未命中时返回 matched=false + hints（相近题材，不报错），
// 命中但无龙头数据时回带 reason，便于前端降级说明。
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
	match, ok, score := bestThemeMatch(overviews, theme)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"theme": theme, "matched": false, "stocks": []any{},
			"reason": "行情侧未找到匹配题材（资讯题材与行情板块口径不同）",
			"hints":  themeMatchHints(overviews, theme, 5),
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
	resp := map[string]any{
		"theme":          theme,
		"matched":        true,
		"matched_id":     match.Theme,
		"matched_name":   match.Name,
		"match_score":    score,
		"change_percent": match.ChangePercent,
		"limit_up_count": match.LimitUpCount,
		"max_streak":     match.MaxStreak,
		"leaders":        match.Leaders,
		"stocks":         stocks,
		"meta":           meta,
		"note":           "代表个股来自行情侧题材（龙头/涨停池），仅供研究观察，非成分全集，不构成交易建议",
	}
	if len(stocks) == 0 {
		resp["reason"] = "已匹配到行情题材「" + match.Name + "」，但该题材当前没有龙头/涨停池个股数据（行情快照可能不含该题材或已过期）"
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// 资讯题材名 → 行情侧题材 的匹配
//
// 资讯题材来自 news_ferment 的行业/主题簇（分类器口径），与行情侧题材
// （短线侠/开盘啦 + 行业动量板块）名称口径不同。单靠字面包含命中率极低：
// 2026-10-11 实测「传媒游戏/新能源汽车/AI算力」对不上行情侧「文化传媒/锂电池/
// AI应用」，多数题材 matched=false → 前端个股池空白。故在字面匹配之外，
// 显式加一层「同义词桥」，并按得分择优（同分优先有龙头股数据的题材）。
// ---------------------------------------------------------------------------

// newsThemeSynonyms 资讯题材词 → 行情侧题材关键词（显式同义词桥，按需增补）。
var newsThemeSynonyms = map[string][]string{
	"军工":     {"国防军工", "军工", "航空装备", "航天装备", "船舶制造"},
	"航天":     {"航天装备", "商业航天", "卫星导航"},
	"航空":     {"航空装备", "通用航空", "航空运输", "机场"},
	"低空":     {"通用航空", "航空装备", "无人机", "低空经济"},
	"低空经济":   {"通用航空", "无人机", "航空装备"},
	"半导体":    {"半导体", "芯片", "半导体设备", "半导体材料", "分立器件", "元件"},
	"芯片":     {"半导体", "芯片", "半导体设备"},
	"AI算力":   {"算力", "AI应用", "数据中心", "CPO", "光通信", "通信设备", "半导体", "软件开发"},
	"人工智能":   {"AI应用", "算力", "软件开发", "计算机设备"},
	"算力":     {"算力", "数据中心", "CPO", "光通信"},
	"通信":     {"通信设备", "通信服务", "光通信", "通信"},
	"新能源汽车":  {"锂电池", "汽车零部件", "汽车整车", "电池", "能源金属", "新能源"},
	"新能源":    {"锂电池", "电池", "光伏设备", "风电设备", "电网设备"},
	"锂电池":    {"锂电池", "电池化学品", "电池"},
	"锂电":     {"锂电池", "电池化学品"},
	"光伏":     {"光伏设备", "电池", "电源设备"},
	"储能":     {"电池", "电网设备", "电源设备"},
	"电力公用":   {"电力", "电力设备", "公用事业", "电网设备"},
	"电力":     {"电力", "电力设备", "电网设备", "公用事业"},
	"电网":     {"电网设备", "电力设备", "特高压"},
	"有色金属":   {"有色金属", "贵金属", "小金属", "工业金属", "能源金属"},
	"黄金":     {"贵金属", "有色金属"},
	"稀土":     {"小金属", "有色金属", "稀土永磁"},
	"化工":     {"化工", "基础化工", "氟化工", "磷化工", "煤化工", "化学制品"},
	"原油市场动态": {"石油石化", "油气开采", "炼化及贸易"},
	"石油":     {"石油石化", "油气开采"},
	"煤炭":     {"煤炭", "焦炭", "焦煤"},
	"钢铁":     {"钢铁", "普钢", "特钢"},
	"地产建筑":   {"房地产开发", "建筑装饰", "房地产", "水泥", "工程建设"},
	"地产":     {"房地产开发", "房地产", "房地产服务"},
	"交通运输":   {"交通运输", "航运港口", "物流", "航空运输", "铁路公路"},
	"航运期货":   {"航运港口", "港口", "物流"},
	"传媒游戏":   {"文化传媒", "游戏", "影视院线", "出版", "数字媒体", "广告营销"},
	"传媒":     {"文化传媒", "数字媒体", "广告营销", "出版", "影视院线"},
	"游戏":     {"游戏", "文化传媒", "数字媒体"},
	"金融":     {"证券", "银行", "保险", "多元金融"},
	"券商":     {"证券", "多元金融"},
	"银行":     {"银行"},
	"保险":     {"保险"},
	"医药":     {"医药", "化学制药", "中药", "生物制品", "医疗器械", "医疗服务"},
	"医疗":     {"医疗器械", "医疗服务", "医药", "生物制品"},
	"消费":     {"食品饮料", "白酒", "零售", "家电", "商业百货"},
	"白酒":     {"白酒", "食品饮料"},
	"食品":     {"食品饮料", "农产品加工"},
	"农业":     {"农业", "种植业", "农产品加工", "养殖业", "渔业"},
	"机器人":    {"机器人", "自动化设备", "通用设备", "专用设备"},
	"消费电子":   {"消费电子", "元件", "光学光电子", "电子化学品"},
	"汽车":     {"汽车零部件", "汽车整车", "乘用车", "商用车"},
	"环保":     {"环保", "环境治理", "公用事业"},
	"旅游":     {"旅游酒店", "旅游及景区", "航空机场", "酒店餐饮"},
	"零售":     {"零售", "商业百货", "贸易"},
	"教育":     {"教育", "教育服务"},
}

// themeMatchMinScore 低于此分视为未匹配（避免把"财政/制裁/天气"等宏观词硬塞给某个板块）。
const themeMatchMinScore = 260

// normalizeThemeName 归一化题材名：去空白/分隔符/全角括号并转小写。
func normalizeThemeName(s string) string {
	replacer := strings.NewReplacer(
		" ", "", "　", "", "-", "", "_", "", "/", "", "\\", "",
		"（", "", "）", "", "(", "", ")", "", "·", "", "、", "",
	)
	return strings.ToLower(strings.TrimSpace(replacer.Replace(s)))
}

// themeMatchKeys 返回用于打分的键：原题材名 + 同义词桥命中的行情侧关键词。
func themeMatchKeys(theme string) []string {
	keys := []string{theme}
	t := normalizeThemeName(theme)
	if t == "" {
		return keys
	}
	for k, values := range newsThemeSynonyms {
		nk := normalizeThemeName(k)
		if nk == "" {
			continue
		}
		if nk == t || strings.Contains(t, nk) || strings.Contains(nk, t) {
			keys = append(keys, values...)
		}
	}
	return keys
}

func runeOverlapRatio(a, b string) float64 {
	ra, rb := len([]rune(a)), len([]rune(b))
	if ra == 0 || rb == 0 {
		return 0
	}
	shorter, longer := ra, rb
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	return float64(shorter) / float64(longer)
}

// scoreThemeOverview 给单个行情侧题材打分：原词精确 1000、同义词精确 600，
// 包含匹配按重叠比例给 400/300 底座的分。
func scoreThemeOverview(o foundation.ThemeOverview, theme string, keys []string) int {
	names := make([]string, 0, 2+len(o.Aliases))
	names = append(names, o.Name, o.Theme)
	names = append(names, o.Aliases...)
	t := normalizeThemeName(theme)
	best := 0
	consider := func(key string, exact, contains int) {
		k := normalizeThemeName(key)
		if k == "" {
			return
		}
		for _, raw := range names {
			n := normalizeThemeName(raw)
			if n == "" {
				continue
			}
			switch {
			case n == k:
				if exact > best {
					best = exact
				}
			case strings.Contains(n, k) || strings.Contains(k, n):
				if s := contains + int(float64(contains)*runeOverlapRatio(n, k)); s > best {
					best = s
				}
			}
		}
	}
	consider(theme, 1000, 400)
	for _, k := range keys {
		if normalizeThemeName(k) == t {
			continue
		}
		consider(k, 600, 300)
	}
	return best
}

// bestThemeMatch 全量打分取最优（同分优先有龙头股数据的题材）；低于阈值不算命中。
func bestThemeMatch(overviews []foundation.ThemeOverview, theme string) (foundation.ThemeOverview, bool, int) {
	if normalizeThemeName(theme) == "" {
		return foundation.ThemeOverview{}, false, 0
	}
	keys := themeMatchKeys(theme)
	var best foundation.ThemeOverview
	bestScore := 0
	found := false
	for _, o := range overviews {
		s := scoreThemeOverview(o, theme, keys)
		if s <= 0 {
			continue
		}
		if !found || s > bestScore ||
			(s == bestScore && len(o.LeaderStocks) > 0 && len(best.LeaderStocks) == 0) {
			best, bestScore, found = o, s, true
		}
	}
	if !found || bestScore < themeMatchMinScore {
		return foundation.ThemeOverview{}, false, bestScore
	}
	return best, true, bestScore
}

// themeMatchHints 未命中时给出行情侧题材参考：先列相似度>0 的，不足则按提供方
// 原有顺序补足，供前端提示「行情侧当前有哪些题材可看」（不宣称相似）。
func themeMatchHints(overviews []foundation.ThemeOverview, theme string, n int) []string {
	if n <= 0 {
		return nil
	}
	keys := themeMatchKeys(theme)
	type scored struct {
		name  string
		score int
	}
	similar := make([]scored, 0, len(overviews))
	rest := make([]string, 0, len(overviews))
	seen := make(map[string]bool, len(overviews))
	for _, o := range overviews {
		name := strings.TrimSpace(o.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if s := scoreThemeOverview(o, theme, keys); s > 0 {
			similar = append(similar, scored{name, s})
		} else {
			rest = append(rest, name)
		}
	}
	sort.SliceStable(similar, func(i, j int) bool { return similar[i].score > similar[j].score })
	out := make([]string, 0, n)
	for _, s := range similar {
		if len(out) >= n {
			break
		}
		out = append(out, s.name)
	}
	for _, name := range rest {
		if len(out) >= n {
			break
		}
		out = append(out, name)
	}
	return out
}
