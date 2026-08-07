package narrative

import (
	"encoding/base64"
	"strings"
)

const trendIDPrefix = "trend:"

type Rule struct {
	Name     string
	Keywords []string
}

// Rules merge synonymous EastMoney concept labels into trading narratives.
// Stock membership still comes from the live stock catalog; these rules never
// maintain local stock lists.
var rules = []Rule{
	{Name: "AI应用", Keywords: []string{"AI应用", "AI智能体", "多模态AI", "AIGC", "CHATGPT", "DEEPSEEK", "KIMI", "SORA", "智谱AI", "大模型", "AI语料", "生成式AI", "文心一言", "豆包"}},
	{Name: "光通信/CPO", Keywords: []string{"CPO", "光通信模块", "光模块"}},
	{Name: "AI算力", Keywords: []string{"算力概念", "东数西算", "液冷服务器", "AI服务器", "AI芯片", "华为昇腾", "英伟达概念"}},
	{Name: "教育", Keywords: []string{"在线教育", "职业教育", "智慧教育", "教育信息化"}},
	{Name: "华为鸿蒙", Keywords: []string{"华为概念", "鸿蒙概念", "华为鲲鹏"}},
	{Name: "机器人", Keywords: []string{"机器人概念", "人形机器人", "工业机器人", "机器视觉"}},
	{Name: "商业航天", Keywords: []string{"商业航天", "卫星互联网", "航天航空"}},
	{Name: "低空经济", Keywords: []string{"低空经济", "飞行汽车", "无人机"}},
	{Name: "半导体芯片", Keywords: []string{"半导体概念", "国产芯片", "第三代半导体", "存储芯片", "先进封装"}},
	{Name: "创新药", Keywords: []string{"创新药", "减肥药", "AI制药", "CRO"}},
	{Name: "固态电池", Keywords: []string{"固态电池", "锂电池概念", "钠离子电池"}},
	{Name: "储能", Keywords: []string{"储能概念", "抽水蓄能", "熔盐储能"}},
	{Name: "数据要素", Keywords: []string{"数据要素", "数据确权", "数据安全"}},
	{Name: "信创", Keywords: []string{"信创", "国产软件", "操作系统"}},
}

var ignoredLabels = map[string]struct{}{
	"融资融券":    {},
	"深股通":     {},
	"沪股通":     {},
	"转融券标的":   {},
	"MSCI中国":  {},
	"富时罗素":    {},
	"标普道琼斯A股": {},
	"QFII重仓":  {},
	"基金重仓":    {},
	"机构重仓":    {},
	"昨日涨停":    {},
	"昨日连板":    {},
	"创业板综":    {},
	"上证180_":  {},
	"沪深300_":  {},
	"中证500_":  {},
	"中证1000_": {},
	"深证100R":  {},
	"深成500":   {},
}

var structuralNarratives = map[string]struct{}{
	"长江三角":   {},
	"深圳特区":   {},
	"上海自贸":   {},
	"粤港澳大湾区": {},
	"成渝特区":   {},
	"海峡西岸":   {},
	"央国企改革":  {},
	"国企改革":   {},
	"地方国企":   {},
	"央企改革":   {},
}

func Memberships(rawConcepts []string) map[string][]string {
	result := map[string][]string{}
	labels := make([]string, 0, len(rawConcepts))
	for _, raw := range rawConcepts {
		raw = strings.TrimSpace(raw)
		if raw == "" || IsIgnored(raw) {
			continue
		}
		labels = append(labels, raw)
	}

	computeLeasingLabels := computeLeasingEvidence(labels)
	hasComputeLeasing := len(computeLeasingLabels) > 0
	if hasComputeLeasing {
		result["算力租赁"] = computeLeasingLabels
	}
	aiApplicationLabels := aiApplicationEvidence(labels)
	hasDerivedAIApplication := len(aiApplicationLabels) > 0
	if hasDerivedAIApplication {
		result["AI应用"] = aiApplicationLabels
	}

	for _, raw := range labels {
		if hasComputeLeasing && isComputeLeasingBaseLabel(raw) {
			continue
		}
		if hasDerivedAIApplication && strings.Contains(strings.ToUpper(raw), "人工智能") {
			continue
		}
		name := Canonical(raw)
		if name == "" {
			continue
		}
		result[name] = appendUnique(result[name], raw)
	}
	return result
}

func Canonical(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	for _, rule := range rules {
		for _, keyword := range rule.Keywords {
			if strings.Contains(upper, strings.ToUpper(keyword)) {
				return rule.Name
			}
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(raw), "概念")
}

func Aliases(name string) []string {
	if name == "算力租赁" {
		return []string{"算力租赁", "算力概念", "云计算", "数据中心"}
	}
	for _, rule := range rules {
		if rule.Name == name {
			return append([]string(nil), rule.Keywords...)
		}
	}
	return []string{name}
}

func IsIgnored(label string) bool {
	_, ignored := ignoredLabels[strings.TrimSpace(label)]
	return ignored
}

func IsStructural(name string) bool {
	_, structural := structuralNarratives[strings.TrimSpace(name)]
	return structural
}

// EvidenceBonus rewards a more specific concept combination over a broad but
// merely overlapping label. For example, 算力概念 + 云计算 is stronger causal
// evidence for 算力租赁 than the same stock's generic 国产芯片 tag.
func EvidenceBonus(name string, rawLabels []string) float64 {
	if name == "算力租赁" && len(rawLabels) >= 2 {
		return 110
	}
	if name == "AI应用" {
		for _, label := range rawLabels {
			if strings.Contains(strings.ToUpper(label), "AI应用") {
				return 120
			}
		}
		if len(rawLabels) >= 2 {
			return 95
		}
		return 45
	}
	return 0
}

func BreadthFactor(name string) float64 {
	if IsStructural(name) {
		return 0.28
	}
	switch name {
	case "人工智能":
		return 0.58
	case "华为鸿蒙":
		return 0.68
	case "5G", "新能源车", "一带一路":
		return 0.8
	default:
		return 1
	}
}

func ThemeID(name string) string {
	return trendIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(name)))
}

func ThemeName(id string) (string, bool) {
	if !strings.HasPrefix(id, trendIDPrefix) {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, trendIDPrefix))
	if err != nil || strings.TrimSpace(string(decoded)) == "" {
		return "", false
	}
	return strings.TrimSpace(string(decoded)), true
}

func computeLeasingEvidence(labels []string) []string {
	hasCompute := false
	hasCloudOperation := false
	evidence := []string{}
	for _, label := range labels {
		upper := strings.ToUpper(label)
		switch {
		case strings.Contains(upper, "算力租赁"):
			return []string{label}
		case strings.Contains(upper, "算力概念"):
			hasCompute = true
			evidence = appendUnique(evidence, label)
		case strings.Contains(upper, "云计算"), strings.Contains(upper, "数据中心"):
			hasCloudOperation = true
			evidence = appendUnique(evidence, label)
		}
	}
	if hasCompute && hasCloudOperation {
		return evidence
	}
	return nil
}

func isComputeLeasingBaseLabel(label string) bool {
	upper := strings.ToUpper(label)
	return strings.Contains(upper, "算力概念") || strings.Contains(upper, "云计算") || strings.Contains(upper, "数据中心")
}

func aiApplicationEvidence(labels []string) []string {
	aiLabel := ""
	applicationLabel := ""
	applicationKeywords := []string{
		"网络游戏", "影视", "短剧", "传媒", "虚拟数字人", "在线教育",
		"智慧教育", "办公", "互联网服务", "数字营销", "元宇宙", "虚拟现实",
	}
	for _, label := range labels {
		upper := strings.ToUpper(label)
		if aiLabel == "" && strings.Contains(upper, "人工智能") {
			aiLabel = label
		}
		if applicationLabel == "" {
			for _, keyword := range applicationKeywords {
				if strings.Contains(upper, strings.ToUpper(keyword)) {
					applicationLabel = label
					break
				}
			}
		}
	}
	if aiLabel == "" || applicationLabel == "" {
		return nil
	}
	return []string{aiLabel, applicationLabel}
}

func appendUnique(values []string, target string) []string {
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}
