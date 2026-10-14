package portfolioinspection

import (
	"fmt"
	"regexp"
	"strings"
)

const etfTradeWords = `(?:买入|卖出|加仓|减仓|增持|减持|清仓|建仓|调仓)`

var immediateETFTrade = regexp.MustCompile(`(?:立即|立刻|马上|直接|现在|今天|今日|当下|即时|执行|应当|应该|建议|务必).{0,12}?` + etfTradeWords)
var unconditionalETFAllocation = regexp.MustCompile(etfTradeWords + `.{0,8}?(?:[0-9]+\s*[%％份]|全部|一半)`)
var etfTradeWord = regexp.MustCompile(etfTradeWords)
var conditionalETFTrade = regexp.MustCompile(`(?:跌破|失守|突破|收复|站稳|触发).{0,24}?(?:则|即|后|时|再|才)(?:应|可|需|按纪律|条件化|考虑|择机|立即|严格执行|执行|建议|优先)*?` + etfTradeWords)

// Keep the negation attached to one trade verb. Arbitrary text here would let
// "不执行调仓但立即买入" negate the independent buy directive as well.
var prohibitedETFTrade = regexp.MustCompile(`(?:不得|禁止|不应该|不应当|不建议|不允许|不要|不能|不应|不必|无需|无须|不用|暂不|不再|不)(?:立即|立刻|马上|直接|现在|今天|今日|当下|即时|执行|应当|应该|建议|务必|任何|所有|ETF|etf|该|本|此|持仓|产品|进行|再|继续|计划|主动|实际|\s)*` + etfTradeWords)
var negatedETFProhibition = regexp.MustCompile(`(?:不是|并非|没有|不能|不得|不可|不应该|不应当|不应|不)(?:再|仅|只)?\s*$`)

const etfExecutionVerificationObjects = `(?:交易规则|执行条件|执行前提|交易限制|停复牌|申赎|IOPV|折溢价|盘口|价差)`

var allETFTradesRequireVerification = regexp.MustCompile(`(?:(?:任何|所有|每次)(?:交易|操作|调仓|买卖|执行)前|(?:交易|调仓|操作|执行)前(?:须|必须|需|应|先))(?:须|必须|应|先|需|重新|完成)*核(?:验|实)[^。；;\n]{0,100}?` + etfExecutionVerificationObjects)
var verifiedETFTrade = regexp.MustCompile(`(?:核(?:验|实)[^。；;，,\n]{0,80}?` + etfExecutionVerificationObjects + `[^。；;，,\n]{0,24}?(?:通过|完成|确认)?(?:之)?后|` + etfExecutionVerificationObjects + `(?:的)?核(?:验|实)(?:通过|完成|确认)(?:之)?后).{0,12}?` + etfTradeWords)
var skippedETFVerification = regexp.MustCompile(`(?:无需|无须|不必|不用|不需要|免于|跳过|省略|放弃).{0,12}?(?:核验|核实|交易前)`)

func prohibitedETFTradeMatches(text string) [][]int {
	var prohibited [][]int
	for _, match := range prohibitedETFTrade.FindAllStringIndex(text, -1) {
		// "不能不执行调仓" and "不是不建议买入" are not prohibitions.
		if !negatedETFProhibition.MatchString(text[:match[0]]) {
			prohibited = append(prohibited, match)
		}
	}
	return prohibited
}

func etfMatchEndsAt(matches [][]int, end int) bool {
	for _, match := range matches {
		if match[1] == end {
			return true
		}
	}
	return false
}

func requiredETFVerificationMatches(text string, pattern *regexp.Regexp) [][]int {
	negated := skippedETFVerification.FindAllStringIndex(text, -1)
	var required [][]int
	for _, match := range pattern.FindAllStringIndex(text, -1) {
		skipped := false
		for _, span := range negated {
			if span[0] < match[1] && span[1] > match[0] {
				skipped = true
				break
			}
		}
		if !skipped {
			required = append(required, match)
		}
	}
	return required
}

func etfActionApplies(text string, r HoldingResult) bool {
	for _, identifier := range []string{r.Holding.Symbol, strings.Split(r.Holding.Symbol, ".")[0], r.Holding.Name, r.Analysis.Name, "ETF", "etf", "所有持仓", "全部持仓"} {
		if identifier != "" && strings.Contains(text, identifier) {
			return true
		}
	}
	return false
}

// Inspect each action clause so an unrelated 'wait' cannot excuse a later
// immediate directive. Conditions can request verification, never grant a
// permission that is missing from the frozen ETF research.
func validateETFActionText(text string, r HoldingResult) error {
	if !isETF(r.Analysis) || len(r.Analysis.ETF.ExecutionBlockers) == 0 {
		return nil
	}
	// An explicit prerequisite for every trade can follow the price plan in a
	// separate clause. It only qualifies price-triggered directives, never an
	// independent instruction to trade immediately.
	verificationRequired := len(requiredETFVerificationMatches(text, allETFTradesRequireVerification)) > 0
	for _, clause := range strings.FieldsFunc(text, func(ch rune) bool {
		return ch == '。' || ch == '；' || ch == ';' || ch == '\n' || ch == '，' || ch == ','
	}) {
		directives := immediateETFTrade.FindAllStringIndex(clause, -1)
		for _, allocation := range unconditionalETFAllocation.FindAllStringIndex(clause, -1) {
			words := etfTradeWord.FindAllStringIndex(clause[allocation[0]:allocation[1]], -1)
			word := words[len(words)-1]
			directives = append(directives, []int{allocation[0] + word[0], allocation[0] + word[1]})
		}
		if len(directives) == 0 {
			continue
		}
		prohibited := prohibitedETFTradeMatches(clause)
		verified := requiredETFVerificationMatches(clause, verifiedETFTrade)
		triggers := conditionalETFTrade.FindAllStringIndex(clause, -1)
		for _, directive := range directives {
			if etfMatchEndsAt(prohibited, directive[1]) || etfMatchEndsAt(verified, directive[1]) {
				continue
			}
			if !verificationRequired || !etfMatchEndsAt(triggers, directive[1]) {
				return fmt.Errorf("%s ETF执行条件尚有阻断，组合不能直接给出可执行调仓指令（命中指令%q，分句%q）；请明确所有买卖须先核验执行前提，或仅保留观察与核验事项", r.Holding.Symbol, clause[directive[0]:directive[1]], clip(clause, 160))
			}
		}
	}
	return nil
}
