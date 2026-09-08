package stockanalysis

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Theme attribution is intentionally deterministic. The model may explain the
// result, but it never invents the score: a theme must have both evidence and
// market confirmation before it can replace the company's main business.
type themeEvidenceBucket struct {
	name        string
	evidence    []ThemeEvidence
	confirmed   bool
	mappingOK   bool
	speculative bool
	market      foundation.ThemeOverview
	marketOK    bool
	score       int
}

type themePriceConfirmation struct {
	matched       bool
	detail        string
	stockDaily    float64
	stockFive     float64
	stockTwenty   float64
	themeDaily    float64
	themeFive     float64
	stockMomentum int
	relative      int
}

var themeKeywordGroups = []struct {
	name     string
	keywords []string
	parent   string
}{
	{name: "砷化镓", keywords: []string{"砷化镓", "GaAs"}, parent: "化合物半导体"},
	{name: "磷化铟", keywords: []string{"磷化铟", "InP"}, parent: "化合物半导体"},
	{name: "化合物半导体", keywords: []string{"化合物半导体", "砷化镓", "磷化铟", "氮化镓", "GaAs", "InP", "GaN"}, parent: "半导体"},
	{name: "光电子器件", keywords: []string{"光电子", "半导体激光器", "激光器芯片", "光芯片"}, parent: "光通信"},
	{name: "光通信/CPO", keywords: []string{"CPO", "共封装光学", "光通信", "光模块", "硅光"}, parent: "AI算力"},
	{name: "AI算力", keywords: []string{"AI算力", "算力", "AI服务器", "数据中心", "液冷服务器"}, parent: "人工智能"},
	{name: "机器人", keywords: []string{"机器人", "人形机器人", "机器视觉"}, parent: "智能制造"},
	{name: "商业航天", keywords: []string{"商业航天", "卫星互联网", "卫星通信"}, parent: "航天军工"},
	{name: "低空经济", keywords: []string{"低空经济", "无人机", "飞行汽车"}, parent: "智能交通"},
	{name: "固态电池", keywords: []string{"固态电池", "半固态电池"}, parent: "新能源"},
	{name: "储能", keywords: []string{"储能", "电化学储能"}, parent: "新能源"},
}

var themeCompanyFactKeywords = []string{
	"研发", "开发", "生产", "制造", "销售", "供应", "产品", "业务", "主营",
	"参股", "控股", "投资", "收购", "并购", "子公司", "订单", "中标", "签约",
	"合作", "建设", "投产", "扩产", "产能", "客户", "收入", "营收", "项目", "布局",
	"解决方案", "设备",
}

var themeReferenceOnlyKeywords = []string{
	"丛书", "图书", "教材", "读物", "著作", "出版基金", "出版项目", "出版物", "期刊", "论文",
	"文献", "书目", "课程", "培训材料", "研究报告", "行业报告",
}

var themeNegativeFactKeywords = []string{
	"不涉及", "未涉及", "不从事", "未从事", "无相关业务", "没有相关业务", "不存在相关业务",
	"未开展", "尚未开展", "未形成收入", "尚未形成收入", "无相关收入", "终止", "取消", "澄清",
}

var themeNonBusinessLabels = []string{
	"沪股通", "深股通", "融资融券", "转融券", "转融通", "机构重仓", "基金重仓", "社保重仓", "养老金",
	"国企改革", "央企改革", "地方国企", "央企", "国有企业", "回购", "股权激励", "高送转", "填权",
	"预盈预增", "预亏预减", "昨日涨停", "昨日连板", "破净股", "低价股", "次新股", "注册制次新股",
	"标普", "MSCI", "富时罗素", "QFII", "证金持股", "AH股", "参股新股",
}

var genericBusinessThemeLabels = []string{
	"业务", "产品", "服务", "制造", "设备", "装备", "材料", "软件", "硬件", "元器件", "专用设备", "通用设备",
}

func enrichTheme(input Input, short ShortTermAnalysis, base ThemeAnalysis) ThemeAnalysis {
	buckets := map[string]*themeEvidenceBucket{}
	ensure := func(name string) *themeEvidenceBucket {
		name = canonicalTheme(name)
		if name == "" {
			return nil
		}
		if bucket, ok := buckets[name]; ok {
			return bucket
		}
		bucket := &themeEvidenceBucket{name: name}
		buckets[name] = bucket
		return bucket
	}
	add := func(name string, evidence ThemeEvidence, confirmed, speculative bool) *themeEvidenceBucket {
		bucket := ensure(name)
		if bucket == nil {
			return nil
		}
		bucket.evidence = append(bucket.evidence, evidence)
		bucket.confirmed = bucket.confirmed || confirmed
		bucket.speculative = bucket.speculative || speculative
		return bucket
	}

	// Existing attribution is a strong market signal, but it remains separate
	// from hard company facts.
	if base.HotTheme != "" {
		add(base.HotTheme, ThemeEvidence{Theme: base.HotTheme, Type: "market_attribution", Source: base.Source, Title: base.EvidenceString(), Snippet: base.Description, Strength: .78, Freshness: freshnessForDate(base.AsOf)}, false, false)
	}
	for _, concept := range base.Concepts {
		if canonical := canonicalTheme(concept); canonical != "" {
			add(canonical, ThemeEvidence{Theme: canonical, Type: "catalog", Source: "eastmoney:stock-concepts", Title: "个股概念目录", Snippet: concept, Strength: .18, Freshness: .35}, false, true)
		}
	}
	for _, candidate := range businessThemeCandidates(input) {
		if snippet, ok := businessThemeFactSnippet(input, candidate); ok {
			canonical := canonicalTheme(candidate)
			add(canonical, ThemeEvidence{
				Theme: canonical, Type: "fact", Relation: "own_business", Direction: "positive",
				Source: firstNonEmpty(input.BusinessSource, "eastmoney:f10-business"), Title: "东方财富F10主营业务",
				Snippet: truncateText(snippet, 220), Strength: .90, Freshness: .70,
			}, true, false)
		}
	}
	for _, item := range input.Announcements {
		for _, group := range themeKeywordGroups {
			if announcementReducesThemeExposure(item.Title) {
				continue
			}
			if snippet, ok := companyThemeFactSnippet(item.Title+"。"+item.Category+"。"+item.Content, append([]string{group.name}, group.keywords...)); ok {
				add(group.name, ThemeEvidence{Theme: group.name, Type: "announcement", Source: firstNonEmpty(item.Meta.Source, "eastmoney:announcement"), Title: item.Title, URL: item.URL, PublishedAt: item.PublishedAt, Snippet: truncateText(snippet, 220), Strength: .95, Freshness: freshnessForTime(item.PublishedAt)}, true, false)
			}
		}
	}
	for _, item := range input.News {
		text := item.Title + " " + item.Content + " " + strings.Join(item.Tags, " ")
		stockMatch := containsAnyFold(text, input.Quote.Name, strings.Split(input.Symbol, ".")[0])
		if !stockMatch {
			continue
		}
		for _, group := range themeKeywordGroups {
			if containsAnyFold(text, group.keywords...) {
				add(group.name, ThemeEvidence{Theme: group.name, Type: "news", Source: firstNonEmpty(item.Meta.Source, "market-news"), Title: item.Title, URL: item.URL, PublishedAt: item.PublishedAt, Snippet: truncateText(item.Content, 160), Strength: .68, Freshness: freshnessForTime(item.PublishedAt)}, false, true)
			}
		}
	}
	for _, item := range input.ModelThemeEvidence {
		if strings.TrimSpace(item.Theme) == "" {
			continue
		}
		item.Theme = canonicalTheme(item.Theme)
		item.Type = normalizeThemeEvidenceType(item.Type)
		item.Relation = normalizeThemeEvidenceRelation(item.Relation)
		item.Direction = normalizeThemeEvidenceDirection(item.Direction)
		if item.Direction == "negative" || item.Relation == "disposed_asset" {
			continue
		}
		if item.Strength <= 0 {
			item.Strength = .62
		}
		confirmed, mappingOK := false, false
		if item.Type == "fact" {
			confirmed = modelRelationCanConfirmFact(item.Relation) && inputConfirmsThemeFact(input, item.Theme)
			if !confirmed {
				item.Type = "inference"
				item.Strength = math.Min(item.Strength, .55)
			}
		} else if item.Type == "market_mapping" {
			mappingOK = modelMappingSupportedByInput(input, item)
			if mappingOK {
				item.Strength = math.Min(item.Strength, .78)
			} else {
				item.Type = "inference"
				item.Strength = math.Min(item.Strength, .5)
			}
		}
		if item.Freshness <= 0 {
			item.Freshness = freshnessForTime(item.PublishedAt)
		}
		bucket := add(item.Theme, item, confirmed, item.Type == "inference" || item.Type == "market_mapping")
		if bucket != nil {
			bucket.mappingOK = bucket.mappingOK || mappingOK
		}
	}
	if base.HotTheme != "" {
		for _, group := range themeKeywordGroups {
			if themeMatches(group.name, foundation.ThemeOverview{Name: base.HotTheme}) || containsAnyFold(base.HotTheme, group.keywords...) {
				add(group.name, ThemeEvidence{Theme: group.name, Type: "market_attribution", Source: base.Source, Title: "开盘啦题材归因", Snippet: base.HotTheme, Strength: .82, Freshness: freshnessForDate(base.AsOf)}, false, false)
			}
		}
	}

	// Enrich each candidate with the live theme radar. A catalog-only concept
	// can never pass the promotion threshold on its own.
	for name, bucket := range buckets {
		bucket.market = bestThemeOverview(input.Themes, append([]string{name}, themeAliases(name)...))
		bucket.marketOK = bucket.market.Name != "" || bucket.market.Theme != ""
		bucket.score = scoreThemeCandidate(*bucket, input, short)
	}
	ordered := make([]*themeEvidenceBucket, 0, len(buckets))
	for _, bucket := range buckets {
		ordered = append(ordered, bucket)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		leftPriority, rightPriority := themePrimaryPriority(ordered[i].name), themePrimaryPriority(ordered[j].name)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return ordered[i].name < ordered[j].name
	})

	best := (*themeEvidenceBucket)(nil)
	rejected := (*themeEvidenceBucket)(nil)
	rejectedPrice := themePriceConfirmation{}
	for _, candidate := range ordered {
		explicitMarket := hasExplicitMarketAttribution(candidate.evidence)
		minimumScore := 55
		if !explicitMarket {
			// Company facts answer "what the company does". A current traded
			// theme additionally needs a live market theme; otherwise the fact is
			// retained below without being mislabeled as the current speculation.
			if !candidate.marketOK {
				continue
			}
			if !candidate.confirmed {
				if !candidate.mappingOK {
					continue
				}
				minimumScore = 62
			}
		}
		if candidate.score < minimumScore {
			continue
		}
		price := confirmThemePrice(input, short, *candidate)
		if !price.matched {
			if rejected == nil {
				rejected, rejectedPrice = candidate, price
			}
			continue
		}
		best = candidate
		break
	}
	base.ConfirmedThemes, base.SpeculativeThemes = buildThemeLayerTags(ordered, rejected, rejectedPrice)
	if best == nil {
		base.HotTheme, base.IsHot, base.HotScore = "", false, 0
		base.Primary = firstNonEmpty(base.Business, input.Industry)
		base.BusinessTheme = base.Primary
		base.Confidence = "低"
		base.Source = firstNonEmpty(input.BusinessSource, base.Source, "eastmoney-f10-business")
		base.AsOf = ""
		base.TrendScore, base.ActiveDays, base.MaxStreak = 0, 0, 0
		base.TrendStage, base.Role = "", "待确认"
		base.EvidenceItems = collectThemeLayerEvidence(ordered, 8)
		base.Evidence = themeEvidenceStrings(base.EvidenceItems, 5)
		if len(base.Evidence) == 0 && input.BusinessDetail != "" {
			base.Evidence = []string{"F10主营资料：" + truncateText(input.BusinessDetail, 120)}
		}
		if rejected != nil {
			base.Description = fmt.Sprintf("%s虽为近期热点候选，但%s；当前按公司主业%s定位", rejected.name, rejectedPrice.detail, firstNonEmpty(base.Primary, "未取得"))
			base.Resonance = ThemeResonance{Available: false, State: "价格未确认", Detail: rejectedPrice.detail, StockMomentum: rejectedPrice.stockMomentum, RelativeStrength: rejectedPrice.relative}
		} else if len(base.ConfirmedThemes) > 0 {
			names := themeTagNames(base.ConfirmedThemes, 4)
			base.Description = fmt.Sprintf("已由公司主营或公告确认%s，但尚未形成可验证的当前主炒作共振；当前按公司主业%s定位", strings.Join(names, "、"), firstNonEmpty(base.Primary, "未取得"))
			base.Resonance = ThemeResonance{Available: false, State: "事实已确认", Detail: fmt.Sprintf("已确认公司涉及%s，尚未同时满足热点强度和个股价格反馈", strings.Join(names, "、"))}
		} else if len(base.SpeculativeThemes) > 0 {
			names := themeTagNames(base.SpeculativeThemes, 4)
			base.Description = fmt.Sprintf("识别到%s等市场映射，但公司关系或盘面验证仍不足；当前按公司主业%s定位", strings.Join(names, "、"), firstNonEmpty(base.Primary, "未取得"))
			base.Resonance = ThemeResonance{Available: false, State: "映射待确认", Detail: fmt.Sprintf("%s尚未形成公司证据、市场热度与个股价格的一致验证", strings.Join(names, "、"))}
		} else {
			base.Description = fmt.Sprintf("未发现同时具备事件证据、热点强度和个股涨幅验证的有效题材，当前按公司主业%s定位", firstNonEmpty(base.Primary, "未取得"))
			base.Resonance = ThemeResonance{Available: false, State: "暂无题材", Detail: "未发现同时具备事件证据、热点强度和个股涨幅验证的有效题材"}
		}
		return base
	}

	displayHotTheme := best.name
	if best.name == "化合物半导体" {
		if specific, ok := buckets["砷化镓"]; ok && specific.confirmed && specific.score >= 55 {
			displayHotTheme += " / 砷化镓"
		}
	}
	base.HotTheme, base.Primary, base.IsHot = displayHotTheme, displayHotTheme, true
	base.HotScore = best.score
	base.Confidence = confidenceForScore(best.score)
	base.BusinessTheme = base.Business
	base.Source = firstEvidenceSource(best.evidence)
	base.AsOf = latestThemeEvidenceDate(best.evidence)
	if best.marketOK {
		base.TrendScore, base.TrendStage, base.ActiveDays, base.MaxStreak = best.market.TrendScore, best.market.TrendStage, best.market.ActiveDays, best.market.MaxStreak
	}
	base.EvidenceItems = append([]ThemeEvidence(nil), best.evidence...)
	base.Evidence = themeEvidenceStrings(best.evidence, 5)
	attributionKind := "公司事实或市场明确归因"
	if best.mappingOK && !best.confirmed && !hasExplicitMarketAttribution(best.evidence) {
		attributionKind = "经原文核验的市场映射"
	}
	base.Description = fmt.Sprintf("当前主炒作题材为%s，依据为%s，证据置信度%s；公司主业为%s", best.name, attributionKind, base.Confidence, firstNonEmpty(base.Business, "未取得"))
	if best.marketOK {
		base.Description += fmt.Sprintf("；题材趋势%d分、上涨广度%d/%d、涨停%d家", best.market.TrendScore, best.market.RisingNodes, max(best.market.MatchedNodes, best.market.TotalNodes), best.market.LimitUpCount)
	}
	base.Resonance = calculateThemeResonance(input, short, *best)
	return base
}

func businessThemeCandidates(input Input) []string {
	result := businessThemeSegments(input.Business)
	result = append(result, input.Concepts...)
	if strings.TrimSpace(input.Industry) != "" {
		result = append(result, input.Industry)
	}
	businessText := strings.Join([]string{input.Business, input.BusinessDetail}, "。")
	for _, overview := range input.Themes {
		name := firstNonEmpty(overview.Name, overview.Theme)
		if businessTextSupportsTheme(businessText, name) {
			result = append(result, name)
		}
	}
	normalized := make([]string, 0, len(result))
	seen := map[string]bool{}
	for _, candidate := range result {
		canonical := canonicalTheme(candidate)
		if canonical == "" || seen[canonical] {
			continue
		}
		seen[canonical] = true
		normalized = append(normalized, canonical)
		if len(normalized) >= 48 {
			break
		}
	}
	return normalized
}

func businessThemeSegments(value string) []string {
	segments := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		switch r {
		case '、', '，', ',', '；', ';', '。', '\n', '\r':
			return true
		default:
			return false
		}
	})
	result := make([]string, 0, len(segments)*2)
	for _, segment := range segments {
		segment = trimBusinessThemeLabel(segment)
		splitSharedSuffix := false
		for _, connector := range []string{"及", "和", "与"} {
			parts := strings.Split(segment, connector)
			if len(parts) != 2 {
				continue
			}
			left, right := trimBusinessThemeLabel(parts[0]), trimBusinessThemeLabel(parts[1])
			suffix := businessThemeSuffix(right)
			if suffix != "" && !strings.HasSuffix(left, suffix) {
				left += suffix
			}
			splitSharedSuffix = suffix != ""
			if validBusinessThemeLabel(left) {
				result = append(result, left)
			}
			if validBusinessThemeLabel(right) {
				result = append(result, right)
			}
			break
		}
		if !splitSharedSuffix && validBusinessThemeLabel(segment) {
			result = append(result, segment)
		}
	}
	return uniqueStrings(result, 16)
}

func trimBusinessThemeLabel(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"公司主要从事", "主要从事", "主营业务为", "主营业务是", "主营"} {
		value = strings.TrimPrefix(value, prefix)
	}
	for _, suffix := range []string{"相关业务", "业务"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return strings.Trim(value, " ：:、")
}

func businessThemeSuffix(value string) string {
	for _, suffix := range []string{"精密元器件", "元器件", "设备", "装备", "系统", "材料", "器件", "软件", "服务", "产品"} {
		if strings.HasSuffix(value, suffix) {
			return suffix
		}
	}
	return ""
}

func validBusinessThemeLabel(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) < 2 || len([]rune(value)) > 16 {
		return false
	}
	if containsAnyFold(value, themeNonBusinessLabels...) {
		return false
	}
	normalized := normalizeBusinessThemeText(value)
	for _, generic := range genericBusinessThemeLabels {
		if normalized == normalizeBusinessThemeText(generic) {
			return false
		}
	}
	return true
}

func businessThemeFactSnippet(input Input, theme string) (string, bool) {
	if !validBusinessThemeLabel(theme) {
		return "", false
	}
	for _, derived := range businessThemeSegments(input.Business) {
		if normalizeBusinessThemeText(derived) == normalizeBusinessThemeText(theme) {
			return firstNonEmpty(input.Business, input.BusinessDetail), true
		}
	}
	for _, text := range []string{input.Business, input.BusinessDetail} {
		if !businessTextSupportsTheme(text, theme) {
			continue
		}
		for _, clause := range strings.FieldsFunc(text, func(r rune) bool {
			switch r {
			case '。', '！', '？', '；', '\n', '\r':
				return true
			default:
				return false
			}
		}) {
			if businessTextSupportsTheme(clause, theme) && !themeMentionIsQualifiedExtension(clause, theme) && !containsAnyFold(clause, themeNegativeFactKeywords...) {
				return strings.TrimSpace(clause), true
			}
		}
	}
	return "", false
}

func businessTextSupportsTheme(text, theme string) bool {
	if !validBusinessThemeLabel(theme) {
		return false
	}
	text = normalizeBusinessThemeText(text)
	theme = normalizeBusinessThemeText(theme)
	return theme != "" && strings.Contains(text, theme)
}

func normalizeBusinessThemeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"锂离子电池装备", "锂电设备", "锂离子电池设备", "锂电设备", "锂电池装备", "锂电设备", "锂电池设备", "锂电设备", "装备", "设备",
		"概念", "", "板块", "", "产业链", "",
		" ", "", "\t", "", "\n", "", "\r", "",
		"，", "", ",", "", "。", "", "；", "", ";", "", "：", "", ":", "",
		"、", "", "/", "", "-", "", "_", "", "（", "", "）", "", "(", "", ")", "",
	)
	return replacer.Replace(value)
}

func themeMentionIsQualifiedExtension(text, theme string) bool {
	text = normalizeBusinessThemeText(text)
	theme = normalizeBusinessThemeText(theme)
	if theme == "" {
		return false
	}
	for _, suffix := range []string{"设备", "材料", "产业链", "客户", "产业客户", "行业客户", "下游客户", "供应商"} {
		if strings.Contains(text, theme+suffix) {
			return true
		}
	}
	return false
}

func buildThemeLayerTags(ordered []*themeEvidenceBucket, rejected *themeEvidenceBucket, rejectedPrice themePriceConfirmation) ([]ThemeTag, []ThemeTag) {
	confirmedCandidates := make([]ThemeTag, 0, len(ordered))
	speculativeCandidates := make([]ThemeTag, 0, len(ordered))
	for _, candidate := range ordered {
		detail := themeTagDetail(candidate)
		if candidate == rejected {
			detail = rejectedPrice.detail
		}
		tag := ThemeTag{Name: candidate.name, Score: candidate.score, EvidenceCount: len(candidate.evidence), Detail: detail}
		if candidate.confirmed {
			tag.Layer = "事实支撑"
			tag.Confidence = confidenceForEvidence(candidate.evidence)
			confirmedCandidates = append(confirmedCandidates, tag)
			continue
		}
		if candidate.score < 35 || (!candidate.mappingOK && !hasExplicitMarketAttribution(candidate.evidence) && !(candidate.marketOK && hasThemeEvidenceType(candidate.evidence, "news"))) {
			continue
		}
		tag.Layer, tag.Confidence = "市场延伸", confidenceForScore(candidate.score)
		speculativeCandidates = append(speculativeCandidates, tag)
	}
	sort.SliceStable(confirmedCandidates, func(i, j int) bool {
		leftLength := len([]rune(compactTheme(confirmedCandidates[i].Name)))
		rightLength := len([]rune(compactTheme(confirmedCandidates[j].Name)))
		if leftLength != rightLength {
			return leftLength > rightLength
		}
		if confirmedCandidates[i].Score != confirmedCandidates[j].Score {
			return confirmedCandidates[i].Score > confirmedCandidates[j].Score
		}
		return confirmedCandidates[i].Name < confirmedCandidates[j].Name
	})
	confirmed := distinctThemeTags(confirmedCandidates, 4)
	speculative := distinctThemeTags(speculativeCandidates, 4)
	return confirmed[:min(len(confirmed), 4)], speculative[:min(len(speculative), 4)]
}

func distinctThemeTags(items []ThemeTag, limit int) []ThemeTag {
	result := make([]ThemeTag, 0, min(len(items), limit))
	for _, item := range items {
		name := compactTheme(item.Name)
		duplicate := false
		for _, existing := range result {
			existingName := compactTheme(existing.Name)
			if name == existingName || (len([]rune(name)) >= 2 && len([]rune(existingName)) >= 2 && (strings.Contains(name, existingName) || strings.Contains(existingName, name))) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		result = append(result, item)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func collectThemeLayerEvidence(ordered []*themeEvidenceBucket, limit int) []ThemeEvidence {
	result := make([]ThemeEvidence, 0, limit)
	seen := map[string]bool{}
	for _, candidate := range ordered {
		if !candidate.confirmed && !candidate.mappingOK && !hasExplicitMarketAttribution(candidate.evidence) {
			continue
		}
		for _, item := range candidate.evidence {
			if item.Type == "catalog" {
				continue
			}
			key := canonicalTheme(item.Theme) + "|" + item.Source + "|" + item.Title
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, item)
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func themeTagNames(items []ThemeTag, limit int) []string {
	result := make([]string, 0, min(len(items), limit))
	for _, item := range items {
		if strings.TrimSpace(item.Name) != "" {
			result = append(result, item.Name)
		}
		if len(result) >= limit {
			break
		}
	}
	return result
}

func companyThemeFactSnippet(text string, keywords []string) (string, bool) {
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '\n', '\r':
			return true
		default:
			return false
		}
	}) {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		for _, window := range themeFactWindows(clause, keywords) {
			if containsAnyFold(window, themeReferenceOnlyKeywords...) {
				continue
			}
			if containsAnyFold(window, themeNegativeFactKeywords...) {
				continue
			}
			if containsAnyFold(window, themeCompanyFactKeywords...) {
				return window, true
			}
		}
	}
	return "", false
}

func themeFactWindows(clause string, keywords []string) []string {
	runes := []rune(strings.TrimSpace(clause))
	if len(runes) == 0 {
		return nil
	}
	windows := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		keywordRunes := []rune(strings.TrimSpace(keyword))
		if len(keywordRunes) == 0 {
			continue
		}
		for start := 0; start+len(keywordRunes) <= len(runes); start++ {
			if !strings.EqualFold(string(runes[start:start+len(keywordRunes)]), string(keywordRunes)) {
				continue
			}
			entityEnd := min(start+len(keywordRunes)+16, len(runes))
			if containsAnyFold(string(runes[start:entityEnd]),
				"有限公司", "股份公司", "股份有限公司", "有限合伙", "集团公司", "研究院", "研究所",
			) {
				continue
			}
			windowStart := max(start-60, 0)
			windowEnd := min(start+len(keywordRunes)+100, len(runes))
			windows = append(windows, strings.TrimSpace(string(runes[windowStart:windowEnd])))
		}
	}
	return uniqueStrings(windows, 8)
}

func announcementReducesThemeExposure(title string) bool {
	return containsAnyFold(title,
		"拟转让", "转让所持", "转让参股公司股权", "转让子公司股权", "拟出售", "出售股权", "出售资产",
		"出售所持", "拟处置", "处置股权", "资产处置", "股权处置", "挂牌转让", "减持参股", "退出投资",
		"终止投资", "不再持有", "清算注销", "注销子公司",
	)
}

func modelRelationCanConfirmFact(relation string) bool {
	switch normalizeThemeEvidenceRelation(relation) {
	case "own_business", "subsidiary", "equity_investment":
		return true
	default:
		return false
	}
}

func modelMappingSupportedByInput(input Input, item ThemeEvidence) bool {
	relation := normalizeThemeEvidenceRelation(item.Relation)
	if relation != "market_mapping" && relation != "customer_supplier" && relation != "own_business" && relation != "subsidiary" && relation != "equity_investment" {
		return false
	}
	needle := normalizeEvidenceText(item.Snippet)
	if len([]rune(needle)) < 6 {
		return false
	}
	haystacks := []string{input.Business, input.BusinessDetail, input.Industry}
	for _, source := range input.Announcements {
		if announcementReducesThemeExposure(source.Title) {
			continue
		}
		haystacks = append(haystacks, source.Title, source.Content)
	}
	for _, source := range input.News {
		text := source.Title + " " + source.Content
		if containsAnyFold(text, input.Quote.Name, strings.Split(input.Symbol, ".")[0]) {
			haystacks = append(haystacks, text)
		}
	}
	for _, candidate := range haystacks {
		if strings.Contains(normalizeEvidenceText(candidate), needle) {
			return true
		}
	}
	return false
}

func normalizeEvidenceText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		" ", "", "\t", "", "\n", "", "\r", "",
		"，", "", ",", "", "。", "", ".", "", "；", "", ";", "", "：", "", ":", "",
		"“", "", "”", "", "‘", "", "’", "", `"`, "", "（", "", "）", "", "(", "", ")", "",
	)
	return replacer.Replace(value)
}

func inputConfirmsThemeFact(input Input, theme string) bool {
	if _, ok := businessThemeFactSnippet(input, theme); ok {
		return true
	}
	keywords := append([]string{theme}, themeAliases(theme)...)
	for _, item := range input.Announcements {
		if announcementReducesThemeExposure(item.Title) {
			continue
		}
		if _, ok := companyThemeFactSnippet(item.Title+"。"+item.Category+"。"+item.Content, keywords); ok {
			return true
		}
	}
	return false
}

func (theme ThemeAnalysis) EvidenceString() string { return firstString(theme.Evidence) }

func scoreThemeCandidate(bucket themeEvidenceBucket, input Input, short ShortTermAnalysis) int {
	evidenceScore := 0.0
	freshnessScore := 0.0
	for _, item := range bucket.evidence {
		evidenceScore = math.Max(evidenceScore, item.Strength*100)
		freshnessScore = math.Max(freshnessScore, item.Freshness*100)
	}
	marketScore := 0.0
	if bucket.marketOK {
		marketScore = float64(bucket.market.TrendScore)
		if marketScore == 0 {
			marketScore = normalizeThemeChange(bucket.market.ChangePercent)
		}
	}
	stockScore := normalizeStockMomentum(input.KLines)
	shortScore := 35.0 + math.Min(float64(short.LimitUpCount20)*12, 30) + math.Min(float64(short.MaxLimitStreak20)*8, 24)
	score := evidenceScore*.25 + freshnessScore*.20 + stockScore*.20 + shortScore*.15 + marketScore*.20
	if !bucket.confirmed && !bucket.marketOK {
		score *= .55
	}
	return int(math.Round(clamp(score, 0, 100)))
}

func calculateThemeResonance(input Input, short ShortTermAnalysis, bucket themeEvidenceBucket) ThemeResonance {
	if !bucket.marketOK {
		return ThemeResonance{Available: false, State: "暂无盘面验证", Detail: "题材事实尚未匹配到可用的成分股强度数据"}
	}
	stats := themeConstituentStatsFor(input, bucket.name)
	stockMomentum := int(math.Round(normalizeStockMomentum(input.KLines)))
	relative := stockMomentum
	breadth := 0
	leader := 35
	capitalDiffusion := 0
	if stats.available {
		relative = stats.relativeStrength
		breadth = stats.breadth
		leader = stats.leaderPosition
		capitalDiffusion = stats.capitalDiffusion
	} else if matched := max(bucket.market.MatchedNodes, bucket.market.TotalNodes); matched > 0 {
		breadth = int(math.Round(clamp(float64(bucket.market.RisingNodes)/float64(matched)*100, 0, 100)))
	}
	if leader < 72 && (bucket.market.TopNode != "" || len(bucket.market.Leaders) > 0) {
		leader = 72
	}
	if stockIsNamedLeader(input, bucket.market) {
		leader = max(leader, 92)
	}
	if strings.Contains(strings.ToLower(firstEvidenceSource(bucket.evidence)), "kaipanla") {
		leader = max(leader, 82)
	}
	limitEnergy := themeLimitEnergy(bucket.market, stats)
	persistence := int(clamp(float64(bucket.market.ActiveDays*7)+float64(bucket.market.FiveDayStrengthScore)*.45+float64(bucket.market.TrendScore)*.20, 0, 100))
	evidenceQuality := int(clamp(float64(maxEvidenceStrength(bucket.evidence))*100, 0, 100))
	if capitalDiffusion == 0 {
		capitalDiffusion = int(clamp(float64(bucket.market.RisingNodes*2+bucket.market.LimitUpCount*8)+clamp(bucket.market.MainNetInflow/100_000_000*3, -20, 35), 0, 100))
	}
	// Exact, independently inspectable resonance formula:
	// relative strength 25%, breadth 20%, limit-up energy 15%, persistence
	// 15%, leader position 10%, evidence quality 10%, capital diffusion 5%.
	score := float64(relative)*.25 + float64(breadth)*.20 + float64(limitEnergy)*.15 + float64(persistence)*.15 + float64(leader)*.10 + float64(evidenceQuality)*.10 + float64(capitalDiffusion)*.05
	final := int(math.Round(clamp(score, 0, 100)))
	state := "弱共振"
	if final >= 72 {
		state = "强共振"
	} else if final >= 55 {
		state = "中等共振"
	}
	return ThemeResonance{Available: true, Score: final, State: state, Detail: fmt.Sprintf("个股动能%d、相对题材%d、上涨广度%d、涨停能量%d、持续性%d、题材地位%d、证据质量%d", stockMomentum, relative, breadth, limitEnergy, persistence, leader, evidenceQuality), StockMomentum: stockMomentum, RelativeStrength: relative, Breadth: breadth, LimitUpEnergy: limitEnergy, Persistence: persistence, LeaderPosition: int(clamp(float64(leader), 0, 100)), EvidenceQuality: evidenceQuality, CapitalDiffusion: capitalDiffusion}
}

type themeConstituentStats struct {
	available        bool
	relativeStrength int
	breadth          int
	nearLimitRatio   float64
	leaderPosition   int
	capitalDiffusion int
	stockDaily       float64
	stockFive        float64
	themeDaily       float64
	themeFive        float64
}

func themeConstituentStatsFor(input Input, theme string) themeConstituentStats {
	aliases := themeAliases(theme)
	members := make([]foundation.StockCatalogEntry, 0, 64)
	for _, entry := range input.Catalog {
		labels := append(append([]string(nil), entry.Concepts...), entry.Industry)
		matched := false
		for _, label := range labels {
			if containsAnyFold(label, aliases...) || canonicalTheme(label) == theme {
				matched = true
				break
			}
		}
		if matched {
			members = append(members, entry)
		}
	}
	if len(members) < 3 {
		return themeConstituentStats{}
	}
	dailyValues, fiveValues := make([]float64, 0, len(members)), make([]float64, 0, len(members))
	rising, strong, nearLimit := 0, 0, 0
	totalAmount, risingAmount := 0.0, 0.0
	for _, entry := range members {
		daily := clamp(entry.ChangePercent, -30, 30)
		five := clamp(entry.FiveDayChangePercent, -60, 80)
		dailyValues = append(dailyValues, daily)
		fiveValues = append(fiveValues, five)
		if daily > 0 {
			rising++
			risingAmount += math.Max(entry.Amount, 0)
		}
		if daily >= 3 {
			strong++
		}
		if daily >= nearLimitThresholdForSymbol(entry.Symbol, entry.Name) {
			nearLimit++
		}
		totalAmount += math.Max(entry.Amount, 0)
	}
	stockDaily, stockFive := stockRecentReturns(input.KLines)
	themeDaily, themeFive := trimmedMean(dailyValues), trimmedMean(fiveValues)
	dailyExcess := stockDaily - themeDaily
	fiveExcess := stockFive - themeFive
	relative := int(math.Round(clamp((50+dailyExcess*8)*.4+(50+fiveExcess*3)*.6, 0, 100)))
	breadth := int(math.Round(clamp(float64(rising)/float64(len(members))*70+float64(strong)/float64(len(members))*30, 0, 100)))
	leader := percentileScore(stockFive, fiveValues)
	capital := breadth
	if totalAmount > 0 {
		capital = int(math.Round(clamp(risingAmount/totalAmount*100, 0, 100)))
	}
	return themeConstituentStats{available: true, relativeStrength: relative, breadth: breadth, nearLimitRatio: float64(nearLimit) / float64(len(members)), leaderPosition: leader, capitalDiffusion: capital, stockDaily: stockDaily, stockFive: stockFive, themeDaily: themeDaily, themeFive: themeFive}
}

func confirmThemePrice(input Input, short ShortTermAnalysis, bucket themeEvidenceBucket) themePriceConfirmation {
	lines := normalizeKLines(input.KLines)
	result := themePriceConfirmation{relative: 35}
	if len(lines) == 0 {
		result.detail = "缺少个股价格数据，无法验证近期热点涨幅"
		return result
	}
	result.stockDaily, result.stockFive = stockRecentReturns(lines)
	result.stockTwenty = windowReturn(closesOf(lines), min(len(lines), 20))
	if len(lines) == 1 {
		result.stockFive, result.stockTwenty = result.stockDaily, result.stockDaily
	}
	result.stockMomentum = int(math.Round(normalizeStockMomentum(lines)))

	stats := themeConstituentStatsFor(input, bucket.name)
	if stats.available {
		result.themeDaily, result.themeFive = stats.themeDaily, stats.themeFive
		result.relative = stats.relativeStrength
	} else {
		result.themeDaily = bucket.market.ChangePercent
	}

	if stockIsNamedLeader(input, bucket.market) || hasRecentThemeLimitUp(input, bucket.name, lines[len(lines)-1].Time) {
		result.matched = true
		result.detail = themePriceDetail(bucket.name, result, stats.available, "已由近期领涨或涨停事件确认")
		return result
	}

	if len(lines) < 5 {
		result.matched = result.stockDaily > 0 || short.LimitUpCount20 > 0
		reason := "上市期涨幅已形成正向反馈"
		if !result.matched {
			reason = "上市期涨幅未形成正向反馈"
		}
		result.detail = themePriceDetail(bucket.name, result, stats.available, reason)
		return result
	}

	reason := "个股近期涨幅与热点方向相符"
	result.matched = true
	if result.stockMomentum < 25 {
		result.matched = false
		reason = "个股近期动能过弱"
	} else if stats.available && result.themeFive >= 1 {
		minimumFive := math.Max(.5, result.themeFive-8)
		if result.stockFive < minimumFive {
			result.matched = false
			reason = "个股5日涨幅未跟随热点成分股"
		}
	} else if stats.available && result.relative < 25 {
		result.matched = false
		reason = "个股相对热点成分股明显落后"
	} else if result.themeDaily >= 1.5 && result.stockDaily <= -1.5 && result.stockFive <= 0 {
		result.matched = false
		reason = "热点上涨时个股反向下跌"
	} else if !stats.available && result.stockFive < -3 && result.stockTwenty <= 0 {
		result.matched = false
		reason = "个股5日和20日涨幅均未验证热点"
	}
	result.detail = themePriceDetail(bucket.name, result, stats.available, reason)
	return result
}

func hasRecentThemeLimitUp(input Input, theme string, latest time.Time) bool {
	for _, event := range input.LimitUps {
		if event.Symbol != input.Symbol || event.Date.IsZero() {
			continue
		}
		if canonicalTheme(event.PrimaryTheme) != canonicalTheme(theme) && !containsAnyFold(event.PrimaryTheme, themeAliases(theme)...) {
			continue
		}
		days := latest.Sub(event.Date).Hours() / 24
		if days >= -1 && days <= 10 {
			return true
		}
	}
	return false
}

func themePriceDetail(theme string, result themePriceConfirmation, hasThemeReturns bool, reason string) string {
	if hasThemeReturns {
		return fmt.Sprintf("%s：%s；个股当日%+.1f%%、5日%+.1f%%、20日%+.1f%%，题材成分当日均值%+.1f%%、5日均值%+.1f%%，相对题材%d分", theme, reason, result.stockDaily, result.stockFive, result.stockTwenty, result.themeDaily, result.themeFive, result.relative)
	}
	return fmt.Sprintf("%s：%s；个股当日%+.1f%%、5日%+.1f%%、20日%+.1f%%，近期动能%d分", theme, reason, result.stockDaily, result.stockFive, result.stockTwenty, result.stockMomentum)
}

func themeLimitEnergy(overview foundation.ThemeOverview, stats themeConstituentStats) int {
	score := float64(overview.LimitUpCount*14 + overview.BoardCount*8 + overview.MaxStreak*7)
	if stats.available {
		score = math.Max(score, math.Min(stats.nearLimitRatio/.08, 1)*100)
	}
	return int(math.Round(clamp(score, 0, 100)))
}

func stockRecentReturns(lines []foundation.KLine) (float64, float64) {
	lines = normalizeKLines(lines)
	if len(lines) < 2 {
		return 0, 0
	}
	latest := lines[len(lines)-1]
	daily := latest.ChangePercent
	if daily == 0 && lines[len(lines)-2].Close > 0 {
		daily = percentChange(lines[len(lines)-2].Close, latest.Close)
	}
	return daily, windowReturn(closesOf(lines), 5)
}

func trimmedMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	trim := 0
	if len(copyValues) >= 10 {
		trim = len(copyValues) / 10
	}
	copyValues = copyValues[trim : len(copyValues)-trim]
	return average(copyValues)
}

func percentileScore(value float64, values []float64) int {
	if len(values) == 0 {
		return 35
	}
	below := 0
	for _, candidate := range values {
		if candidate <= value {
			below++
		}
	}
	return int(math.Round(clamp(float64(below)/float64(len(values))*100, 0, 100)))
}

func nearLimitThresholdForSymbol(symbol, name string) float64 {
	upperName := strings.ToUpper(strings.TrimSpace(name))
	if strings.HasPrefix(upperName, "ST") || strings.HasPrefix(upperName, "*ST") {
		return 4.5
	}
	code := strings.Split(symbol, ".")[0]
	if strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301") || strings.HasPrefix(code, "688") {
		return 18
	}
	if strings.HasSuffix(symbol, ".BJ") || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "92") {
		return 27
	}
	return 9
}

func stockIsNamedLeader(input Input, overview foundation.ThemeOverview) bool {
	for _, leader := range overview.Leaders {
		if containsAnyFold(leader, input.Quote.Name, strings.Split(input.Symbol, ".")[0]) {
			return true
		}
	}
	return containsAnyFold(overview.TopNode, input.Quote.Name, strings.Split(input.Symbol, ".")[0])
}

func canonicalTheme(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.NewReplacer(
		"锂离子电池装备", "锂电设备", "锂离子电池设备", "锂电设备", "锂电池装备", "锂电设备", "锂电池设备", "锂电设备", "装备", "设备",
	).Replace(value)
	for _, group := range themeKeywordGroups {
		if strings.EqualFold(value, group.name) {
			return group.name
		}
	}
	for _, group := range themeKeywordGroups {
		if containsAnyFold(value, append([]string{group.name}, group.keywords...)...) {
			return group.name
		}
	}
	value = strings.TrimSuffix(value, "概念")
	return value
}

func themePrimaryPriority(name string) int {
	switch name {
	case "化合物半导体":
		return 100
	case "光电子器件":
		return 95
	case "砷化镓", "磷化铟":
		return 90
	default:
		return 50
	}
}

func latestThemeEvidenceDate(items []ThemeEvidence) string {
	latest := time.Time{}
	for _, item := range items {
		if item.PublishedAt.After(latest) {
			latest = item.PublishedAt
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.Format("2006-01-02")
}

func themeAliases(name string) []string {
	for _, group := range themeKeywordGroups {
		if group.name == name {
			aliases := append([]string{group.name}, group.keywords...)
			if group.parent != "" {
				aliases = append(aliases, group.parent)
			}
			if name == "化合物半导体" {
				aliases = append(aliases, "半导体芯片", "半导体材料", "第三代半导体")
			}
			return aliases
		}
	}
	return []string{name}
}
func containsAnyFold(text string, terms ...string) bool {
	text = strings.ToLower(text)
	for _, term := range terms {
		if term != "" && strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}
func freshnessForDate(value string) float64 {
	if value == "" {
		return .25
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return .25
	}
	return freshnessForTime(parsed)
}
func freshnessForTime(value time.Time) float64 {
	if value.IsZero() {
		return .25
	}
	days := math.Abs(time.Since(value).Hours() / 24)
	switch {
	case days <= 1:
		return 1
	case days <= 3:
		return .8
	case days <= 5:
		return .6
	case days <= 10:
		return .35
	default:
		return .15
	}
}
func normalizeThemeChange(value float64) float64 { return clamp((value+3)/8*100, 0, 100) }
func normalizeStockMomentum(lines []foundation.KLine) float64 {
	if len(lines) < 20 {
		return 35
	}
	five := windowReturn(closesOf(lines), 5)
	twenty := windowReturn(closesOf(lines), 20)
	return clamp(50+five*2.2+twenty*.7, 0, 100)
}
func maxEvidenceStrength(items []ThemeEvidence) float64 {
	best := 0.0
	for _, item := range items {
		best = math.Max(best, item.Strength)
	}
	return best
}
func hasExplicitMarketAttribution(items []ThemeEvidence) bool {
	for _, item := range items {
		if item.Type == "market_attribution" && strings.Contains(strings.ToLower(item.Source), "kaipanla") {
			return true
		}
	}
	return false
}
func firstEvidenceSource(items []ThemeEvidence) string {
	fallback := ""
	for _, item := range items {
		if item.Source == "" {
			continue
		}
		if fallback == "" {
			fallback = item.Source
		}
		if item.Type != "catalog" {
			return item.Source
		}
	}
	if fallback != "" {
		return fallback
	}
	return "theme-attribution"
}
func themeEvidenceStrings(items []ThemeEvidence, limit int) []string {
	out := []string{}
	for _, item := range items {
		value := item.Title
		if value == "" {
			value = item.Snippet
		}
		if value != "" {
			out = append(out, value)
		}
	}
	return uniqueStrings(out, limit)
}
func themeTagDetail(bucket *themeEvidenceBucket) string {
	if hasF10BusinessEvidence(bucket.evidence) {
		if bucket.marketOK {
			return fmt.Sprintf("F10主营已确认，题材趋势%d分，炒作相关性%d", bucket.market.TrendScore, bucket.score)
		}
		return fmt.Sprintf("F10主营已确认，尚未匹配到当前市场热点，炒作相关性%d", bucket.score)
	}
	if bucket.mappingOK && !bucket.confirmed {
		if bucket.marketOK {
			return fmt.Sprintf("原文映射已核验，题材趋势%d分，证据%d条", bucket.market.TrendScore, len(bucket.evidence))
		}
		return fmt.Sprintf("原文映射已核验，证据%d条，等待盘面确认", len(bucket.evidence))
	}
	if bucket.marketOK {
		return fmt.Sprintf("题材趋势%d分，证据%d条", bucket.market.TrendScore, len(bucket.evidence))
	}
	return fmt.Sprintf("证据%d条，等待盘面确认", len(bucket.evidence))
}
func hasF10BusinessEvidence(items []ThemeEvidence) bool {
	for _, item := range items {
		if item.Type == "fact" && item.Relation == "own_business" && strings.Contains(strings.ToLower(item.Source), "f10-business") {
			return true
		}
	}
	return false
}
func hasThemeEvidenceType(items []ThemeEvidence, evidenceType string) bool {
	for _, item := range items {
		if item.Type == evidenceType {
			return true
		}
	}
	return false
}
func confidenceForEvidence(items []ThemeEvidence) string {
	strength := maxEvidenceStrength(items)
	if strength >= .85 {
		return "高"
	}
	if strength >= .60 {
		return "中"
	}
	return "低"
}
func confidenceForScore(score int) string {
	if score >= 75 {
		return "高"
	}
	if score >= 55 {
		return "中"
	}
	return "低"
}
