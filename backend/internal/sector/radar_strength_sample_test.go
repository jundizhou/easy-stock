package sector

import (
	"testing"

	"easy-stock/backend/internal/foundation"
)

// buildStrengthPool 构造一个成分股池：changes[i] 对应第 i 只票的涨幅。
func buildStrengthPool(changes []float64) ([]foundation.BoardStock, map[string]stockStrengthChange) {
	stocks := make([]foundation.BoardStock, 0, len(changes))
	lookup := make(map[string]stockStrengthChange, len(changes))
	for index, change := range changes {
		symbol := "60" + string(rune('0'+index/10)) + string(rune('0'+index%10)) + ".SH"
		stocks = append(stocks, foundation.BoardStock{Symbol: symbol, Name: "成分股", Price: 10})
		lookup[symbol] = stockStrengthChange{daily: change, dailyValid: true}
	}
	return stocks, lookup
}

func scorePool(changes []float64) int {
	stocks, lookup := buildStrengthPool(changes)
	return calculateThemeStrength(stocks, lookup, func(change stockStrengthChange) (float64, bool) {
		return change.daily, change.dailyValid
	})
}

// 用户报告的问题：成分股少的板块靠少数票的平均涨幅排名虚高。
// 7 只票里 2 涨 5 跌（其中 1 只涨停）不应因「平均涨幅为正」而拿到高分。
func TestThinThemeWithMostMembersFallingScoresLow(t *testing.T) {
	thin := scorePool([]float64{10, 4, -1, -1.5, -1, -2, -2})
	if thin > 30 {
		t.Fatalf("7 只票 2 涨 5 跌仍得 %d 分，小样本虚高未修复", thin)
	}
}

// 相反的对照：同样 7 只票但普遍上涨，应当拿到明显更高的分数。
// 这两条一起钉住「广度」而非「平均涨幅」才是主导项。
func TestThinThemeWithBroadParticipationScoresHigh(t *testing.T) {
	broad := scorePool([]float64{10, 5, 4, 3, 2, -1, -1})
	falling := scorePool([]float64{10, 4, -1, -1.5, -1, -2, -2})
	if broad <= falling+25 {
		t.Fatalf("广度为 5/7 得分 %d 与 2/7 得分 %d 差距过小", broad, falling)
	}
}

// 相同涨幅下，成分股多的板块应比成分股少的板块更可信（得分更高）。
func TestWidePoolOutranksNarrowPoolAtEqualChange(t *testing.T) {
	narrow := scorePool([]float64{3, 3, 3, 3, 3})
	wide := scorePool(repeatFloat(3, 100))

	if wide <= narrow {
		t.Fatalf("100 只全涨 3%% 得分 %d 未超过 5 只全涨 3%% 得分 %d", wide, narrow)
	}
}

// 单只涨停不得主导整个题材：在 7 只票的池里，一个 +10% 换成的中位数抬升有限。
func TestSingleLimitUpDoesNotDominateThinPool(t *testing.T) {
	withLimit := scorePool([]float64{10, 0, 0, 0, 0, 0, 0})
	withoutLimit := scorePool([]float64{0, 0, 0, 0, 0, 0, 0})
	if withLimit-withoutLimit > 20 {
		t.Fatalf("单只涨停把 7 只票的题材抬高了 %d 分，仍然过强", withLimit-withoutLimit)
	}
}

// 中位数本身对离群值免疫：把最高的几只票换成极端值不应改变中位数。
func TestMedianChangeIgnoresExtremeTail(t *testing.T) {
	base := []float64{1, 2, 3, 4, 5, 6, 7}
	extreme := []float64{1, 2, 3, 4, 5, 6, 300}
	if medianChange(base) != medianChange(extreme) {
		t.Fatalf("中位数 %v 与 %v 不一致", medianChange(base), medianChange(extreme))
	}
}

// 偶数个样本取中间两值均值。
func TestMedianChangeAveragesMiddlePairForEvenCount(t *testing.T) {
	if got := medianChange([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Fatalf("medianChange=%v want=2.5", got)
	}
}

// 可信度系数必须随样本量单调递增，且始终落在 (0,1)。
func TestConstituentConfidenceIsMonotonicAndBounded(t *testing.T) {
	previous := 0.0
	for _, size := range []int{1, 3, 7, 12, 30, 100, 500} {
		confidence := constituentConfidence(size)
		if confidence <= previous {
			t.Fatalf("样本量 %d 的可信度 %v 未递增（前值 %v）", size, confidence, previous)
		}
		if confidence <= 0 || confidence >= 1 {
			t.Fatalf("样本量 %d 的可信度 %v 越界", size, confidence)
		}
		previous = confidence
	}
}

// 空池返回 0，不得 panic。
func TestCalculateThemeStrengthHandlesEmptyPool(t *testing.T) {
	if score := calculateThemeStrength(nil, nil, func(change stockStrengthChange) (float64, bool) {
		return change.daily, change.dailyValid
	}); score != 0 {
		t.Fatalf("空池得分 %d want=0", score)
	}
}

func repeatFloat(value float64, count int) []float64 {
	result := make([]float64, count)
	for index := range result {
		result[index] = value
	}
	return result
}
