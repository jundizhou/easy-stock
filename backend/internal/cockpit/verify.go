package cockpit

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 只校验带小数或百分号的数值：整数多为序号、天数或条数，误报很多，不作为事实判断依据。
var narrativeNumberPattern = regexp.MustCompile(`[-+]?\d+\.\d+%?`)
var factNumberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?(?:e[-+]?\d+)?`)

// VerifyNarrative 找出叙述中出现、但在本次采集的事实 JSON 里找不到的数值。
// 返回值非空时，界面必须提示这些数字需要人工核对，不能当作已验证的结论。
func VerifyNarrative(narrative string, facts Facts) []string {
	return VerifyNarrativeAgainst(narrative, facts)
}

// VerifyNarrativeAgainst 允许同时传入多组事实。盘中巡查的提示词里含上一次巡查的
// 情绪与涨停数据，模型引用这些数值做环比是正确的；若只用本次事实校验，会把它们
// 误报成「未核实」，反而削弱这套校验的可信度。
func VerifyNarrativeAgainst(narrative string, factSets ...Facts) []string {
	if strings.TrimSpace(narrative) == "" {
		return nil
	}
	known := map[int64]bool{}
	for _, facts := range factSets {
		encoded, err := json.Marshal(facts)
		if err != nil {
			continue
		}
		for _, raw := range factNumberPattern.FindAllString(string(encoded), -1) {
			if value, err := strconv.ParseFloat(raw, 64); err == nil {
				// 比较只看绝对值：叙述里常省略涨跌符号，方向本身由叙述与事实的文字关系判断。
				known[roundKey(math.Abs(value))] = true
			}
		}
	}
	var unverified []string
	seen := map[string]bool{}
	for _, token := range narrativeNumberPattern.FindAllString(narrative, -1) {
		if seen[token] {
			continue
		}
		seen[token] = true
		value, err := strconv.ParseFloat(strings.TrimSuffix(token, "%"), 64)
		if err != nil {
			continue
		}
		if !known[roundKey(math.Abs(value))] {
			unverified = append(unverified, token)
		}
	}
	return unverified
}

// roundKey 把数值统一到两位小数，与界面展示精度一致。
func roundKey(value float64) int64 {
	return int64(math.Round(value * 100))
}
