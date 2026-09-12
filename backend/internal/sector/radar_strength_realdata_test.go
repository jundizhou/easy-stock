package sector

import (
	"testing"

	"easy-stock/backend/internal/foundation"
)

// 下面两组用例直接取自 2026-09-10 收盘后的东财概念板块真实快照，
// 用来钉住用户报告的「小板块靠少数票把平均涨幅拉高，从而排名虚高」问题。
//
//	板块          涨幅     上涨   下跌   旧口径排名   修复后
//	船舶制造      +1.25%     9      5      第 4      第 4（中性保留）
//	纳米银        +1.06%     2      5      第 21     掉出前列
//	科创板做市商   +0.87%    14      0      第 1      第 1（广度 100%，真强势）
//	金融地产风格   +0.83%    86     14      第 3      并列第 2
//	地热能        +0.55%     3      6      第 25     掉出前列

// 纳米银式：涨幅为正但七成成员在跌 —— 必须被压在低位。
func TestRealNanoSilverBoardIsNotTopRanked(t *testing.T) {
	// 7 只成分股：2 只上涨（1 只涨停带动板块涨幅），5 只下跌。
	score := scorePool([]float64{10, 3.5, -0.5, -1, -1.5, -2, -2.5})
	if score > 35 {
		t.Fatalf("纳米银式小板块得分 %d，仍然偏高", score)
	}
}

// 地热能式：9 只成分股，3 涨 6 跌。
func TestRealGeothermalBoardIsNotTopRanked(t *testing.T) {
	score := scorePool([]float64{6, 2, 1.5, -0.5, -1, -1, -1.5, -2, -2})
	if score > 40 {
		t.Fatalf("地热能式小板块得分 %d，仍然偏高", score)
	}
}

// 科创板做市商式：14 只成分股全部上涨，广度 100% —— 这是真强势，
// 修复不得把它一起误伤掉：它必须显著高于纳米银式薄板块。
// （温和普涨 1.2%、无涨停的板块本就不该冲上高位，所以这里比的是相对次序。）
func TestRealStarMarketMakerBoardStaysStrong(t *testing.T) {
	broad := scorePool(repeatFloat(1.2, 14))
	thin := scorePool([]float64{10, 3.5, -0.5, -1, -1.5, -2, -2.5})
	if broad < 50 {
		t.Fatalf("14 只全涨的板块只拿到 %d 分，低于合理区间", broad)
	}
	if broad <= thin+20 {
		t.Fatalf("14 只全涨得分 %d 与 7 只 2 涨得分 %d 差距不足，真强势被埋没", broad, thin)
	}
}

// 金融地产风格式：100 只成分股，86 涨 14 跌 —— 广度充分，应排在薄板块之上。
func TestRealBroadStyleBoardOutranksThinBoard(t *testing.T) {
	wide := scorePool(append(append(repeatFloat(1.5, 40), repeatFloat(1, 30)...), append(repeatFloat(0.5, 16), repeatFloat(-1, 14)...)...))
	thin := scorePool([]float64{10, 3.5, -0.5, -1, -1.5, -2, -2.5})
	if wide <= thin {
		t.Fatalf("100 只 86 涨得分 %d 未超过 7 只 2 涨得分 %d", wide, thin)
	}
}

// 直接以 foundation.BoardStock 构造，确认入口签名与生产调用一致。
func TestRealBoardSnapshotThroughProductionEntryPoint(t *testing.T) {
	changes := []float64{10, 3.5, -0.5, -1, -1.5, -2, -2.5}
	stocks := make([]foundation.BoardStock, 0, len(changes))
	lookup := map[string]stockStrengthChange{}
	for index, change := range changes {
		symbol := "30" + string(rune('0'+index)) + "00.SZ"
		stocks = append(stocks, foundation.BoardStock{Symbol: symbol, Name: "纳米银成分", Price: 10})
		lookup[symbol] = stockStrengthChange{daily: change, dailyValid: true}
	}
	score := calculateThemeStrength(stocks, lookup, func(change stockStrengthChange) (float64, bool) {
		return change.daily, change.dailyValid
	})
	if score > 35 {
		t.Fatalf("经生产入口计算得分 %d，仍然偏高", score)
	}
}
