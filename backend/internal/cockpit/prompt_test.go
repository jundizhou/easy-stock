package cockpit

import (
	"strings"
	"testing"
)

func TestBuildPromptRoundsLongFloatsButKeepsStoredFacts(t *testing.T) {
	facts := Facts{
		Emotion: &Emotion{Score: 43.812345678, FinalBreakRate: 0.31343283582089554},
		Sources: []SourceResult{{Name: "情绪周期", OK: true}},
	}
	prompt := BuildPrompt(PhasePostClose, "2026-10-20", "盘后复盘", facts, nil)
	if strings.Contains(prompt, "0.31343283582089554") || strings.Contains(prompt, "43.812345678") {
		t.Fatalf("long floats leaked into prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "0.3134") || !strings.Contains(prompt, "43.8123") {
		t.Fatalf("rounded values missing from prompt: %s", prompt)
	}
	if facts.Emotion.FinalBreakRate != 0.31343283582089554 {
		t.Fatal("rounding must not mutate the stored facts")
	}
}

func TestBuildPromptUsesCompactMarketSnapshotForEveryPhase(t *testing.T) {
	for _, phase := range []Phase{PhasePreOpen, PhaseIntraday, PhasePostClose, PhaseRest} {
		t.Run(string(phase), func(t *testing.T) {
			prompt := BuildPrompt(phase, "2026-10-27", "巡查", Facts{}, nil)
			last := -1
			for _, field := range []string{"**市场状态**", "**较上次变化**", "**资金方向**", "**主要风险**", "**接下来盯什么**"} {
				index := strings.Index(prompt, field)
				if index <= last {
					t.Fatalf("snapshot field missing or out of order: %s", field)
				}
				last = index
			}
			for _, rule := range []string{"正文目标 200 字、最多 260 字", "--（无同口径对比）", "--（数据不足）", "**数据缺口**", "不得编造", "推断要写明条件", "不构成投资建议"} {
				if !strings.Contains(prompt, rule) {
					t.Fatalf("compact snapshot lost rule %q", rule)
				}
			}
			for _, obsolete := range []string{"900 字", "至少 3 条", "2–3 句话", "列出 3 条"} {
				if strings.Contains(prompt, obsolete) {
					t.Fatalf("conflicting long-form instruction remains: %s", obsolete)
				}
			}
		})
	}
}

func TestBuildPromptDistinguishesCachedTemperatureAndOmitsEmotionScores(t *testing.T) {
	facts := Facts{
		Temperature: &Temperature{Value: 0, Stale: true, Model: TemperatureModel, FetchedAt: "2026-10-27T14:00:00+08:00"},
		Emotion:     &Emotion{Score: 67.12, Phase: "发酵/主升", TradeDate: "2026-10-26"},
		Sources:     []SourceResult{{Name: "市场温度", OK: false, Error: "snapshot EOF"}},
	}
	for _, phase := range []Phase{PhasePreOpen, PhaseIntraday, PhasePostClose, PhaseRest} {
		prompt := BuildPrompt(phase, "2026-10-27", "巡查", facts, &Brief{Facts: facts, Narrative: "情绪周期 67.12 分"})
		for _, rule := range []string{"若仍有 stale=true 的有效缓存", "不得写成数据缺失", "对应字段为空时才写明缺失", "不展示总分或分项分数", "本次与上次 JSON 或叙述中的旧情绪分数", `"temperature":`, `"stale":true`, `"value":0`, facts.Temperature.FetchedAt} {
			if !strings.Contains(prompt, rule) {
				t.Fatalf("phase %s missing cache/score constraint %q", phase, rule)
			}
		}
	}
	if facts.Temperature.Value != 0 || facts.Emotion.Score != 67.12 {
		t.Fatal("narrative constraints must not alter original facts")
	}
}
