package narrative

import "testing"

func TestMembershipsPreferComputeLeasingFromConceptCombination(t *testing.T) {
	items := Memberships([]string{"国产芯片", "云计算", "英伟达概念", "算力概念", "人工智能"})
	if len(items["算力租赁"]) != 2 {
		t.Fatalf("compute leasing evidence = %+v", items["算力租赁"])
	}
	if _, exists := items["云计算"]; exists {
		t.Fatalf("cloud computing should be folded into compute leasing: %+v", items)
	}
	if _, exists := items["半导体芯片"]; !exists {
		t.Fatalf("expected secondary semiconductor narrative: %+v", items)
	}
}

func TestTrendIDRoundTrip(t *testing.T) {
	id := ThemeID("算力租赁")
	name, ok := ThemeName(id)
	if !ok || name != "算力租赁" {
		t.Fatalf("round trip failed: id=%q name=%q ok=%v", id, name, ok)
	}
}

func TestMembershipsDeriveAIApplicationFromAIAndApplicationDomain(t *testing.T) {
	items := Memberships([]string{"人工智能", "网络游戏", "影视概念", "创投"})
	if len(items["AI应用"]) != 2 {
		t.Fatalf("AI application evidence = %+v", items["AI应用"])
	}
	if _, exists := items["人工智能"]; exists {
		t.Fatalf("generic AI label should be folded into AI应用: %+v", items)
	}
	if EvidenceBonus("AI应用", items["AI应用"]) < 90 {
		t.Fatalf("derived AI application evidence was not prioritized: %+v", items)
	}
}

func TestEvidenceBonusPrioritizesExplicitAIApplication(t *testing.T) {
	items := Memberships([]string{"国产软件", "AI应用", "信创"})
	if EvidenceBonus("AI应用", items["AI应用"]) < 100 {
		t.Fatalf("explicit AI应用 should be authoritative evidence: %+v", items)
	}
}
