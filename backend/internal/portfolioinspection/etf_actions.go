package portfolioinspection

import (
	"fmt"
	"regexp"
	"strings"
)

var immediateETFTrade = regexp.MustCompile(`(?:立即|立刻|马上|直接|现在|今天|今日|当下|即时|执行|应当|应该|建议|务必).{0,12}(?:买入|卖出|加仓|减仓|增持|减持|清仓|建仓|调仓)`)
var unconditionalETFAllocation = regexp.MustCompile(`(?:买入|卖出|加仓|减仓|增持|减持|清仓|建仓|调仓).{0,8}(?:[0-9]+\s*[%％份]|全部|一半)`)

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
	for _, clause := range strings.FieldsFunc(text, func(ch rune) bool {
		return ch == '。' || ch == '；' || ch == ';' || ch == '\n' || ch == '，' || ch == ','
	}) {
		if !immediateETFTrade.MatchString(clause) && !unconditionalETFAllocation.MatchString(clause) {
			continue
		}
		if strings.Contains(clause, "不建议") || strings.Contains(clause, "不得") || strings.Contains(clause, "禁止") || strings.Contains(clause, "不应") || strings.Contains(clause, "不要") || strings.Contains(clause, "不能") || strings.Contains(clause, "不立即") || strings.Contains(clause, "暂不") {
			continue
		}
		// Only explicit verification requirements make a future action conditional.
		if (strings.Contains(clause, "核验") || strings.Contains(clause, "核实")) && (strings.Contains(clause, "后") || strings.Contains(clause, "通过") || strings.Contains(clause, "之前")) {
			continue
		}
		return fmt.Errorf("%s ETF执行条件尚有阻断，组合不能直接给出可执行调仓指令", r.Holding.Symbol)
	}
	return nil
}
