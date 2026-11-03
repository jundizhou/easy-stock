package cockpit

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 常规叙述只校验小数；温度和对应统计另按明确的单位、字段识别整数。
var narrativeNumberPattern = regexp.MustCompile(`[-+]?\d+\.\d+%?`)
var factNumberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:e[-+]?\d+)?`)
var temperatureDegreePattern = regexp.MustCompile(`([-+]?\d+(?:\.\d+)?)\s*[度°]`)
var temperatureScaleReferencePattern = regexp.MustCompile(`(?:^|[^\d.+-])0\s*[-–—~～至到]\s*(100(?:\.0+)?)\s*[度°]`)
var temperatureStatPattern = regexp.MustCompile(`(上涨占比|等权涨跌幅|中位涨跌幅)[^\d+\-\n。；;]{0,12}([-+]?\d+(?:\.\d+)?%?)`)
var temperatureNeutralReferencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|[，,。；;：:\n（(])\s*(50(?:\.0+)?)\s*[度°]\s*(?:为|是|代表)中性`),
	regexp.MustCompile(`中性(?:基准|基线|参考线|线|点|值|阈值|水平)?\s*(?:为|是|设为|[:：])\s*(50(?:\.0+)?)\s*[度°]`),
	regexp.MustCompile(`(?:若|如果|假如|一旦)[^。！？\n；;]{0,100}?(?:回落至|降至|跌至|低于|高于|跌破|升至|突破)\s*(50(?:\.0+)?)\s*[度°]\s*中性`),
}
var previousQualifierPattern = regexp.MustCompile(`[（(][^（）()\n。；;]*(?:对比上次|上次巡查|上一轮巡查)[^（）()\n。；;]*[）)]`)

// VerifyNarrative 找出叙述中出现、但在本次采集的事实 JSON 里找不到的数值。
// 返回值非空时，界面必须提示这些数字需要人工核对，不能当作已验证的结论。
func VerifyNarrative(narrative string, facts Facts) []string {
	return VerifyNarrativeAgainst(narrative, facts)
}

// VerifyNarrativeAgainst 第一组是本次事实，其余仅供明确标注的历史对比。
// 每次引用单独检查，防止旧数值在对比中通过后又被当作当前读数。
func VerifyNarrativeAgainst(narrative string, factSets ...Facts) []string {
	if strings.TrimSpace(narrative) == "" {
		return nil
	}
	current, previous := map[int64]bool{}, map[int64]bool{}
	for i, facts := range factSets {
		known := current
		if i > 0 {
			known = previous
		}
		addFactNumbers(known, facts)
	}
	// 温度与三项统计还要核对所属模块；不能借同值的指数或资金数字通过。
	metrics := map[int]string{}
	positions := map[int][2]int{}
	for _, match := range narrativeNumberPattern.FindAllStringIndex(narrative, -1) {
		positions[match[0]] = [2]int{match[0], match[1]}
	}
	for _, match := range temperatureDegreePattern.FindAllStringSubmatchIndex(narrative, -1) {
		positions[match[2]] = [2]int{match[2], match[3]}
		metrics[match[2]] = "value"
	}
	for _, match := range temperatureStatPattern.FindAllStringSubmatchIndex(narrative, -1) {
		positions[match[4]] = [2]int{match[4], match[5]}
		metrics[match[4]] = narrative[match[2]:match[3]]
	}
	ordered := make([][2]int, 0, len(positions))
	for _, position := range positions {
		ordered = append(ordered, position)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i][0] < ordered[j][0] })
	var unverified []string
	seen := map[string]bool{}
	references := temperatureReferencePositions(narrative)
	for _, position := range ordered {
		if metrics[position[0]] == "value" && references[position[0]] {
			continue
		}
		token := narrative[position[0]:position[1]]
		value, err := strconv.ParseFloat(strings.TrimSuffix(token, "%"), 64)
		if err != nil {
			continue
		}
		known := current
		historical := len(factSets) > 1 && narrativeNumberIsHistorical(narrative, position[0], position[1])
		if historical {
			known = previous
		}
		valid := known[roundKey(math.Abs(value))]
		if metric, ok := metrics[position[0]]; ok {
			valid = false
			for i, facts := range factSets {
				if (i > 0) == historical && temperatureNumberMatches(facts.Temperature, metric, value) {
					valid = true
				}
			}
		}
		if !valid && !seen[token] {
			unverified = append(unverified, token)
			seen[token] = true
		}
	}
	return unverified
}

func temperatureReferencePositions(narrative string) map[int]bool {
	positions := map[int]bool{}
	for _, pattern := range temperatureNeutralReferencePatterns {
		for _, match := range pattern.FindAllStringSubmatchIndex(narrative, -1) {
			positions[match[2]] = true
		}
	}
	for _, match := range temperatureScaleReferencePattern.FindAllStringSubmatchIndex(narrative, -1) {
		context := narrative[:match[2]]
		if start := strings.LastIndexAny(context, "\n。！？；;，,"); start >= 0 {
			context = context[start:]
		}
		// 只识别市场温度同一分句内的固定量程，不借此放行其他模块的读数。
		if strings.Contains(context, "市场温度") || strings.Contains(context, "温度读数") || strings.Contains(context, "温度模型") || strings.Contains(context, "温度量程") {
			positions[match[2]] = true
			// 0-100 中的连字符会被温度正则读成负号，保持引用起点一致。
			if match[2] > 0 && narrative[match[2]-1] == '-' {
				positions[match[2]-1] = true
			}
		}
	}
	return positions
}

// NarrativeNumberIsTemperatureReference identifies old flags caused solely by
// the model's fixed neutral baseline or scale. A repeated current reading remains flagged.
func NarrativeNumberIsTemperatureReference(narrative, token string) bool {
	references := temperatureReferencePositions(narrative)
	found := false
	for _, match := range temperatureDegreePattern.FindAllStringSubmatchIndex(narrative, -1) {
		if narrative[match[2]:match[3]] != token {
			continue
		}
		if !references[match[2]] {
			return false
		}
		found = true
	}
	return found
}

// 来源错误与温度告警不能反过来成为旧数值的验证依据。
func addFactNumbers(known map[int64]bool, facts Facts) {
	facts.Sources = nil
	if facts.Temperature != nil {
		temperature := *facts.Temperature
		temperature.Warning = ""
		facts.Temperature = &temperature
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return
	}
	for _, raw := range factNumberPattern.FindAllString(string(encoded), -1) {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			known[roundKey(math.Abs(value))] = true
		}
	}
}

func narrativeNumberIsHistorical(text string, start, end int) bool {
	lineStart := strings.LastIndexAny(text[:start], "\n。！？") + 1
	// LastIndexAny 返回 UTF-8 字节位置，跳过整枚中文标点。
	for lineStart < start && !utf8.RuneStart(text[lineStart]) {
		lineStart++
	}
	lineEnd := len(text)
	if offset := strings.IndexAny(text[end:], "\n。！？"); offset >= 0 {
		lineEnd = end + offset
	}
	if strings.Contains(text[lineStart:lineEnd], "数据缺口") {
		return false
	}
	prefix := text[lineStart:start]
	if offset := strings.LastIndexAny(prefix, "，,；;"); offset >= 0 {
		_, width := utf8.DecodeRuneInString(prefix[offset:])
		prefix = prefix[offset+width:]
	}
	history := lastNarrativeMarker(prefix, []string{"上次", "上一次", "上一轮", "此前", "之前", "历史"})
	current := lastNarrativeMarker(prefix, []string{"本次", "当前", "目前", "现在", "本轮", "沿用", "现使用", "升至", "降至", "增至", "回落至", "变为", "转为"})
	trimmed := strings.TrimSpace(prefix)
	if strings.HasSuffix(trimmed, "由") || strings.HasSuffix(trimmed, "从") {
		return true
	}
	// 「目前沿用上次缓存」仍在声称本次可用，不能因后出现「上次」就通过。
	if current >= 0 {
		return false
	}
	if history >= 0 {
		return true
	}
	return strings.Contains(text[lineStart:start], "较上次变化") || previousQualifierPattern.MatchString(text[end:lineEnd])
}

func lastNarrativeMarker(text string, markers []string) int {
	last := -1
	for _, marker := range markers {
		if index := strings.LastIndex(text, marker); index > last {
			last = index
		}
	}
	return last
}

func temperatureNumberMatches(temperature *Temperature, metric string, value float64) bool {
	if temperature == nil {
		return false
	}
	expected := temperature.Value
	switch metric {
	case "上涨占比":
		expected = temperature.UpPercent
	case "等权涨跌幅":
		expected = temperature.MeanChangePercent
	case "中位涨跌幅":
		expected = temperature.MedianChangePercent
	}
	return roundKey(math.Abs(value)) == roundKey(math.Abs(expected))
}

// roundKey 把数值统一到两位小数，与界面展示精度一致。
func roundKey(value float64) int64 {
	return int64(math.Round(value * 100))
}
