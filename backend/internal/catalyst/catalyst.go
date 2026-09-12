// Package catalyst 从财联社电报流里筛选「能实质性影响个股/板块情绪」的重大消息。
//
// 为什么需要它：电报是高频流，绝大多数是盘面播报、复盘、公司例行公告、
// 机构观点转述这类噪音。真正能牵动情绪的（如「摩根发布厄尔尼诺影响时间表」、
// 「芒果台首次播出 AI 制作电视剧」）只占极少数，且往往混在几十条里被淹掉。
//
// 筛选分两层，都是为了「宁可漏、不可凑」：
//  1. 启发式预筛（PreFilter）：纯规则、零成本，先砍掉明显的播报型噪音，
//     并给被主力资金/涨停验证过的消息优先级；
//  2. 模型精筛（Select）：交给 Hermes 按「有没有产业/公司/板块层面的新增事实」
//     逐条判断，没有合格的就返回空列表——不凑数。
package catalyst

import (
	"context"
	"sort"
	"strings"

	"easy-stock/backend/internal/foundation"
)

// MaxItems 是界面能容纳的催化条数上限。
const MaxItems = 10

// MaxCandidates 是送入模型精筛的候选上限。
//
// 电报流里真催化稀少，候选给少了会漏；给多了会让 prompt 过长、模型判断变糊。
// 40 条在实测里既能覆盖大部分场景，也把 prompt 控制在几千字量级。
const MaxCandidates = 40

// Candidate 是预筛后待精筛的一条电报，附带规则层给出的线索供模型参考。
type Candidate struct {
	Item   foundation.NewsItem
	Score  int      `json:"score"`
	Hints  []string `json:"hints,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// Item 是最终输出的催化条目。
type Item struct {
	foundation.NewsItem
	// Impact 是影响方向：bullish（利好）/ bearish（利空）/ neutral（方向不明）。
	Impact string `json:"impact"`
	// Strength 是影响强度 0–100，越大越可能实质性改变预期。
	Strength int `json:"strength"`
	// Sectors 是消息可能传导到的板块/领域。
	Sectors []string `json:"sectors,omitempty"`
	// Stocks 是消息直接指向的个股名称或代码。
	Stocks []string `json:"stocks,omitempty"`
	// Why 是入选理由（一句话），用于界面解释「为什么这条重要」。
	Why string `json:"why,omitempty"`
	// Horizon 是影响的时间尺度：immediate（当日）/ short（数日）/ medium（数周以上）。
	Horizon string `json:"horizon,omitempty"`
	// Score 是规则层给出的排序分。
	Score int `json:"score"`
}

// Result 是一次筛选的完整结果，含诊断信息便于前端解释「为什么是空的」。
type Result struct {
	Items []Item `json:"items"`
	Meta  Meta   `json:"meta"`
}

// Meta 描述这次筛选的口径与执行情况。
type Meta struct {
	// Scanned 是本次扫描的电报条数。
	Scanned int `json:"scanned"`
	// Candidates 是通过预筛、进入模型判断的条数。
	Candidates int `json:"candidates"`
	// Filtered 是被规则直接淘汰的条数。
	Filtered int `json:"filtered"`
	// ModelUsed 表示是否真的调用了模型精筛。
	ModelUsed bool `json:"model_used"`
	// Note 是给用户看的说明（如模型不可用时的降级提示）。
	Note string `json:"note,omitempty"`
	// SourceURL 是数据来源。
	SourceURL string `json:"source_url,omitempty"`
	// UpdatedAt 是本次筛选时间（RFC3339）。
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ── 规则层 ──────────────────────────────────────────────────────────

// 播报型噪音：整条消息几乎只是转述盘面/指数/板块涨跌，没有新增事实。
// 命中即淘汰，除非同时含强信号（见 strongSignals）。
var broadcastNoisePatterns = []string{
	"截至发稿", "截至收盘", "截至午间", "今日收盘", "今日开盘", "盘中播报",
	"涨幅居前", "跌幅居前", "涨跌互现", "板块异动", "异动拉升", "异动下跌",
	"盘中涨停", "盘中跌停", "收盘上涨", "收盘下跌", "报收",
	"三大指数", "沪指", "深证成指", "创业板指", "科创50", "两市成交额",
	"涨跌家数", "北向资金今日", "半日成交", "全天成交",
}

// 例行公事型：合规/程序性公告，通常不改变预期。
var routinePatterns = []string{
	"股东大会", "独立董事", "监事会", "股票交易异常波动公告", "关于回购注销",
	"投资者关系活动记录表", "业绩说明会", "变更签字会计师", "证券事务代表",
}

// 弱信号：机构观点转述、泛泛而谈的评论，单独出现时不足以构成催化。
var weakOpinionPatterns = []string{
	"分析师表示", "业内人士称", "机构认为", "有分析指出", "专家称",
	"预计将", "有望成为", "长期看好", "维持买入评级",
}

// 强信号：被真实资金或价格行为验证过，是资金正在投票的直接证据。
var strongSignals = []string{
	"涨停", "封板", "连板", "一字板", "跌停", "撬板",
	"中标", "签署", "签约", "合同", "订单",
	"涨价", "提价", "上调价格", "价格上调",
	"获批", "批准", "通过审批", "获得批文", "注册证",
	"量产", "投产", "下线", "试产", "开工",
	"首发", "首次", "首款", "首个", "首台", "首家",
	"突破", "研发成功", "技术突破", "专利",
	"成立", "设立", "并购", "收购", "重组", "注入", "增资",
	"合作", "战略协议", "框架协议", "独家",
	"获准", "许可", "牌照", "资质",
}

// 有明确主体/权威来源：能指名道姓就比泛泛评论更值得看。
//
// ⚠️ 这张表是**召回瓶颈**：漏一个机构名，就可能把用户真正关心的消息
//（如「美国气象预报机构 CPC 称厄尔尼诺正在增强」）挡在候选之外。
// 维护取向是宁滥勿缺——多进来的噪音由模型层淘汰，漏掉的催化无法补救。
var authoritySignals = []string{
	"摩根", "高盛", "大摩", "小摩", "摩根士丹利", "摩根大通", "美银", "花旗", "瑞银", "野村",
	"巴克莱", "德意志银行", "汇丰", "渣打", "麦格理", "中金", "中信证券", "国泰君安", "华泰",
	"国家发改委", "发改委", "工信部", "财政部", "国务院", "证监会", "央行", "国资委",
	"能源局", "科技部", "商务部", "海关总署", "市场监管总局", "医保局", "药监局",
	"美联储", "欧洲央行", "日本央行", "央行行长",
	"新华社", "人民日报", "央视",
	// 国际组织与官方统计/研究机构。这类机构发布的时间表、数据口径常直接决定
	// 大宗商品与相关板块的预期（厄尔尼诺、库存、产量预测等）。
	// ⚠️ 中英文名都要收：实测只写 IEA 会漏掉「国际能源署下调原油需求预期」。
	"世界气象组织", "气象预报机构", "气象局", "国家气候中心", "气象台",
	"CPC", "NOAA", "EIA", "IEA", "OPEC", "欧佩克",
	"国际能源署", "国际能源论坛", "欧佩克+", "能源信息署",
	"世界银行", "国际货币基金组织", "IMF", "WTO", "联合国", "世卫组织",
	"美国农业部", "USDA", "统计局", "海关",
	"交易所", "上期所", "大商所", "郑商所", "中金所", "广期所",
}

// 权威机构发布的**预测/时间表/目标**类动作词。
//
// 「机构 + 预计/预测/展望」是本功能最典型的催化形态（如「摩根发布厄尔尼诺
// 影响时间」），但这类表述天然带弱观点词，容易被降权误杀。单独识别出来给分，
// 让它稳定进入候选。
//
// ⚠️ 这里刻意不收「报告称」「提示」「评估」这类过泛的词：它们会在长正文里
// 随机命中，把国际疫情、社会新闻也拉进候选（实测「荷兰感染西尼罗病毒死亡」
// 因正文含「研究所…报告称」被误判为权威预测）。判据必须是「对市场关心的事
// 给出可交易的时间表」，而不是「出现了某个机构名」。
var forecastSignals = []string{
	"预计", "预测", "展望", "时间表", "路线图", "目标价", "上调", "下调",
	"警告", "预警", "风险提示", "研判", "测算",
}

// 政策/宏观类：这类消息天然面广，容易形成板块级联动。
var policySignals = []string{
	"印发", "发布", "出台", "通知", "征求意见", "方案", "规划", "补贴", "退税",
	"降息", "加息", "降准", "关税", "制裁", "出口管制", "反倾销",
}

// PreFilter 用纯规则从电报流里挑出值得送模型的候选，并按重要性排序。
//
// 设计取向：这一层只做「减法 + 排序」，不做最终取舍；宁可多留一些给模型看，
// 也不要因为规则写窄了把真催化漏掉。
func PreFilter(items []foundation.NewsItem, limit int) ([]Candidate, int) {
	candidates := make([]Candidate, 0, len(items))
	filtered := 0
	for _, item := range items {
		text := item.Title + " " + item.Content
		if isBroadcastNoise(text) {
			filtered++
			continue
		}
		if containsAny(text, routinePatterns) && !containsAny(text, strongSignals) {
			filtered++
			continue
		}
		score, hints := scoreCandidate(text)
		if score <= 0 {
			filtered++
			continue
		}
		candidates = append(candidates, Candidate{Item: item, Score: score, Hints: hints})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		// 同分按时间新在前，保证结果对用户是「最新最重要」的顺序。
		return candidates[i].Item.PublishedAt.After(candidates[j].Item.PublishedAt)
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, filtered
}

// isBroadcastNoise 判断是否为纯盘面播报：命中噪音词且没有强信号兜底。
func isBroadcastNoise(text string) bool {
	if !containsAny(text, broadcastNoisePatterns) {
		return false
	}
	// 例如「XX涨停，板块涨幅居前」既含噪音词也含强信号，不应被丢掉。
	return !containsAny(text, strongSignals) || !containsAny(text, authoritySignals)
}

// scoreCandidate 给一条候选打分并给出线索标签。
//
// 分值只用于排序与粗略闸门（>0 才留），不作为最终强度；最终强度由模型给定。
//
// 注意闸门的分寸：只有「弱观点」命中时给一个很小的正分而非负分。
// 理由是不能用规则替模型做最终判决——「某机构认为 X」这类表述里，
// 确实可能藏着带时间表和幅度的真催化，规则层只负责把它排到后面。
func scoreCandidate(text string) (int, []string) {
	score := 0
	var hints []string

	authorityHits := matchAll(text, authoritySignals)
	strongHits := matchAll(text, strongSignals)
	policyHits := matchAll(text, policySignals)
	forecastHits := matchAll(text, forecastSignals)

	if len(strongHits) > 0 {
		score += 30 + min(len(strongHits), 4)*5
		hints = append(hints, "事实性动作："+strings.Join(strongHits, "/"))
	}
	if len(authorityHits) > 0 {
		score += 22
		hints = append(hints, "权威来源："+strings.Join(authorityHits, "/"))
	}
	if len(policyHits) > 0 {
		score += 16
		hints = append(hints, "政策口径："+strings.Join(policyHits, "/"))
	}
	// 权威来源 + 预测/时间表：本功能最典型的催化形态，单独加权保证稳定入选。
	if len(authorityHits) > 0 && len(forecastHits) > 0 {
		score += 14
		hints = append(hints, "含时间表/预测："+strings.Join(limited(forecastHits, 3), "/"))
	}
	weakHits := matchAll(text, weakOpinionPatterns)
	if len(weakHits) > 0 {
		// 见上：降权到刚好过闸门，交模型判断，不直接淘汰。
		// 已有权威来源或预测线索时不降权——那属于「机构发布研判」，不是泛泛评论。
		if len(authorityHits) == 0 && len(forecastHits) == 0 {
			score -= len(weakHits) * 8
		}
		hints = append(hints, "含观点表述："+strings.Join(weakHits, "/"))
	}
	if score <= 0 && len(weakHits) > 0 {
		score = 1
	}
	return score, hints
}

// limited 返回前 limit 个元素，避免线索串过长挤占 prompt 与界面。
func limited(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func containsAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func matchAll(text string, patterns []string) []string {
	var hits []string
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			hits = append(hits, pattern)
		}
	}
	return hits
}

// ── 模型层接口 ──────────────────────────────────────────────────────

// Selector 是模型精筛能力。返回的 Item 由实现负责做字段清洗与上限裁剪。
type Selector interface {
	Select(ctx context.Context, candidates []Candidate) ([]Item, bool, error)
}

// Enricher 把纯规则结果与模型结果合并成最终 Result。
//
// 模型不可用时（未配置 API Key、调用失败、超时）不能把候选直接当成催化输出——
// 那样会把播报噪音重新灌回界面。此时退化为「只输出规则层高分项」，
// 并通过 Meta.Note 明确告知降级事实。
func Enrich(candidates []Candidate, filtered int, meta foundation.SourceMeta, selector Selector, ctx context.Context) Result {
	result := Result{
		Items: []Item{},
		Meta: Meta{
			Candidates: len(candidates),
			Filtered:   filtered,
			SourceURL:  meta.SourceURL,
		},
	}
	if selector != nil && len(candidates) > 0 {
		items, ok, err := selector.Select(ctx, candidates)
		if err == nil && ok {
			result.Items = items
			result.Meta.ModelUsed = true
			return result
		}
		if err != nil {
			result.Meta.Note = "AI 精筛不可用，已降级为规则筛选：" + err.Error()
		} else {
			result.Meta.Note = "AI 精筛不可用，已降级为规则筛选"
		}
	} else if len(candidates) > 0 {
		result.Meta.Note = "AI 精筛未启用，当前为规则筛选结果"
	}
	result.Items = ruleOnly(candidates)
	return result
}

// ruleOnly 是降级路径：只取规则分明显领先的少数几条，且必须带事实性动作或权威来源。
func ruleOnly(candidates []Candidate) []Item {
	items := make([]Item, 0, MaxItems)
	for _, candidate := range candidates {
		if candidate.Score < 30 {
			continue
		}
		if len(items) >= MaxItems {
			break
		}
		items = append(items, Item{
			NewsItem: candidate.Item,
			Impact:   "neutral",
			Strength: clamp(candidate.Score, 0, 100),
			Why:      strings.Join(candidate.Hints, "；"),
			Horizon:  "short",
			Score:    candidate.Score,
		})
	}
	return items
}

// ── 输出清洗（供 Selector 实现复用） ─────────────────────────────────

// NormalizeItems 对模型返回的结果做防御性清洗：
// 逐条校验、按强度排序、去重、裁到上限。任何不合法条目直接丢弃而非猜测补全。
func NormalizeItems(items []Item) []Item {
	cleaned := make([]Item, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		key := item.ID
		if key == "" {
			key = item.Title
		}
		if seen[key] {
			continue
		}
		switch item.Impact {
		case "bullish", "bearish", "neutral":
		default:
			item.Impact = "neutral"
		}
		switch item.Horizon {
		case "immediate", "short", "medium":
		default:
			item.Horizon = "short"
		}
		// 先裁剪再排序：模型偶尔会给出 500 这种越界强度，
		// 若先排序会让越界值虚假地占据首位，裁剪后顺序才是真实的。
		item.Strength = clamp(item.Strength, 0, 100)
		item.Sectors = trimList(item.Sectors, 6)
		item.Stocks = trimList(item.Stocks, 8)
		seen[key] = true
		cleaned = append(cleaned, item)
	}
	sort.SliceStable(cleaned, func(i, j int) bool {
		if cleaned[i].Strength != cleaned[j].Strength {
			return cleaned[i].Strength > cleaned[j].Strength
		}
		return cleaned[i].PublishedAt.After(cleaned[j].PublishedAt)
	})
	if len(cleaned) > MaxItems {
		cleaned = cleaned[:MaxItems]
	}
	return cleaned
}

func trimList(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
