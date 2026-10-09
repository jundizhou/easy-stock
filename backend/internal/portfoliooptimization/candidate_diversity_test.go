package portfoliooptimization

import "testing"

func TestCandidateDiversityChecksBothClassifications(t *testing.T) {
	cs := []Candidate{}
	for i, pair := range [][2]string{{"小家电", "机器人"}, {"小家电", "消费电子"}, {"机器人", "机器人"}, {"银行", "银行"}, {"电力", "电力"}, {"食品", "食品"}, {"通信", "通信"}, {"机械", "机械"}} {
		cs = append(cs, Candidate{Symbol: string(rune('a' + i)), Screening: &CandidateScreening{Qualified: true, Score: float64(100 - i), Industry: IndustrySignal{Qualified: true, Name: pair[0]}}})
		CandidateIndustry(&cs[i], pair[1])
	}
	indices := DiverseCandidates(cs, MaxCandidateResearch)
	if len(indices) != 6 {
		t.Fatal("greedy conflict prevented six industries", indices)
	}
	a, b := map[string]bool{}, map[string]bool{}
	for _, i := range indices {
		c := cs[i]
		if a[c.IndustryGroup] || b[c.CatalogIndustryGroup] {
			t.Fatal("duplicate industry", c)
		}
		a[c.IndustryGroup] = true
		b[c.CatalogIndustryGroup] = true
	}
	if indices[0] != 1 || indices[1] != 2 {
		t.Fatal("did not reroute conflicting pair", indices)
	}
}

func TestQualifiedPoolReservesSixDistinctIndustries(t *testing.T) {
	cs := []Candidate{}
	for i := 0; i < 18; i++ {
		group := "同一行业"
		if i >= 12 {
			group = string(rune('a' + i))
		}
		cs = append(cs, Candidate{Symbol: string(rune('a' + i)), IndustryGroup: group, CatalogIndustryGroup: group, Screening: &CandidateScreening{Qualified: true, Score: float64(100 - i)}})
	}
	pool := QualifiedCandidatePool(cs)
	if len(pool) != 8 || len(DiverseCandidates(pool, 6)) != 6 {
		t.Fatal("top same-industry stocks crowded out diversity", pool)
	}
}
