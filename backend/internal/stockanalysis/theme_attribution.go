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
	speculative bool
	market      foundation.ThemeOverview
	marketOK    bool
	score       int
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
	add := func(name string, evidence ThemeEvidence, confirmed, speculative bool) {
		bucket := ensure(name)
		if bucket == nil {
			return
		}
		bucket.evidence = append(bucket.evidence, evidence)
		bucket.confirmed = bucket.confirmed || confirmed
		bucket.speculative = bucket.speculative || speculative
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
	for _, item := range input.Announcements {
		text := strings.TrimSpace(item.Title + " " + item.Category + " " + item.Content)
		for _, group := range themeKeywordGroups {
			if containsAnyFold(text, group.keywords...) {
				add(group.name, ThemeEvidence{Theme: group.name, Type: "announcement", Source: firstNonEmpty(item.Meta.Source, "eastmoney:announcement"), Title: item.Title, URL: item.URL, PublishedAt: item.PublishedAt, Snippet: truncateText(firstNonEmpty(item.Content, item.Category), 220), Strength: .95, Freshness: freshnessForTime(item.PublishedAt)}, true, false)
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
		if item.Strength <= 0 {
			item.Strength = .62
		}
		if item.Freshness <= 0 {
			item.Freshness = freshnessForTime(item.PublishedAt)
		}
		add(item.Theme, item, item.Type == "announcement" || item.Type == "fact", item.Type == "inference" || item.Type == "market_mapping")
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
		return themePrimaryPriority(ordered[i].name) > themePrimaryPriority(ordered[j].name)
	})

	best := (*themeEvidenceBucket)(nil)
	for _, candidate := range ordered {
		if candidate.score < 55 || (!candidate.confirmed && !candidate.marketOK && !hasExplicitMarketAttribution(candidate.evidence)) {
			continue
		}
		if best == nil || candidate.score > best.score {
			best = candidate
		}
	}
	if best == nil {
		base.HotTheme, base.IsHot, base.HotScore = "", false, 0
		base.Primary = firstNonEmpty(base.Business, input.Industry)
		base.BusinessTheme = base.Primary
		base.Confidence = "低"
		base.Resonance = ThemeResonance{Available: false, State: "暂无题材", Detail: "未发现同时具备事件证据和盘面验证的有效热点题材"}
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
	base.ConfirmedThemes = make([]ThemeTag, 0, 4)
	base.SpeculativeThemes = make([]ThemeTag, 0, 4)
	for _, candidate := range ordered {
		if candidate.name == best.name {
			continue
		}
		if candidate.score < 35 {
			continue
		}
		tag := ThemeTag{Name: candidate.name, Score: candidate.score, EvidenceCount: len(candidate.evidence), Detail: themeTagDetail(candidate)}
		if candidate.confirmed && candidate.score >= 55 {
			tag.Layer, tag.Confidence = "事实支撑", confidenceForScore(candidate.score)
			base.ConfirmedThemes = append(base.ConfirmedThemes, tag)
		} else {
			tag.Layer, tag.Confidence = "市场延伸", confidenceForScore(candidate.score)
			base.SpeculativeThemes = append(base.SpeculativeThemes, tag)
		}
	}
	base.ConfirmedThemes = base.ConfirmedThemes[:min(len(base.ConfirmedThemes), 4)]
	base.SpeculativeThemes = base.SpeculativeThemes[:min(len(base.SpeculativeThemes), 4)]
	base.Description = fmt.Sprintf("当前主炒作题材为%s，证据置信度%s；公司主业为%s", best.name, base.Confidence, firstNonEmpty(base.Business, "未取得"))
	if best.marketOK {
		base.Description += fmt.Sprintf("；题材趋势%d分、上涨广度%d/%d、涨停%d家", best.market.TrendScore, best.market.RisingNodes, max(best.market.MatchedNodes, best.market.TotalNodes), best.market.LimitUpCount)
	}
	base.Resonance = calculateThemeResonance(input, short, *best)
	return base
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
	dailyExcess := stockDaily - trimmedMean(dailyValues)
	fiveExcess := stockFive - trimmedMean(fiveValues)
	relative := int(math.Round(clamp((50+dailyExcess*8)*.4+(50+fiveExcess*3)*.6, 0, 100)))
	breadth := int(math.Round(clamp(float64(rising)/float64(len(members))*70+float64(strong)/float64(len(members))*30, 0, 100)))
	leader := percentileScore(stockFive, fiveValues)
	capital := breadth
	if totalAmount > 0 {
		capital = int(math.Round(clamp(risingAmount/totalAmount*100, 0, 100)))
	}
	return themeConstituentStats{available: true, relativeStrength: relative, breadth: breadth, nearLimitRatio: float64(nearLimit) / float64(len(members)), leaderPosition: leader, capitalDiffusion: capital}
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
	for _, item := range items {
		if item.Source != "" {
			return item.Source
		}
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
	if bucket.marketOK {
		return fmt.Sprintf("题材趋势%d分，证据%d条", bucket.market.TrendScore, len(bucket.evidence))
	}
	return fmt.Sprintf("证据%d条，等待盘面确认", len(bucket.evidence))
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
