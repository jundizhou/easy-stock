package cockpit

import (
	"reflect"
	"testing"
)

func TestVerifyNarrativeAcceptsOnlyNumbersPresentInFacts(t *testing.T) {
	facts := Facts{
		CNIndexes: []IndexQuote{{Name: "上证指数", ChangePercent: -1.3, Price: 3730.65}},
		Emotion:   &Emotion{Score: 44.5, LimitUp: 46},
	}
	narrative := "上证指数下跌 1.3%（A股指数），情绪分 44.5（情绪周期）。另有 9.9% 的虚构涨幅。"
	got := VerifyNarrative(narrative, facts)
	if !reflect.DeepEqual(got, []string{"9.9%"}) {
		t.Fatalf("unverified = %v, want only 9.9%%", got)
	}
}

func TestVerifyNarrativeIgnoresIntegersAndEmptyText(t *testing.T) {
	facts := Facts{}
	if got := VerifyNarrative("共 3 条预案，第 2 步", facts); len(got) != 0 {
		t.Fatalf("integers should not be checked, got %v", got)
	}
	if got := VerifyNarrative("  ", facts); got != nil {
		t.Fatalf("empty narrative returned %v", got)
	}
}

func TestVerifyNarrativeSeparatesNeutralTemperatureReferencesFromReadings(t *testing.T) {
	facts := Facts{Temperature: &Temperature{Value: 76.0109}}
	for _, tc := range []struct {
		name, narrative string
		want            []string
	}{
		{name: "reported definition", narrative: "温度读数是本地平滑等权统计，50 度为中性，本次 76.0109 度说明当日有效样本整体偏强。"},
		{name: "conditional neutral threshold", narrative: "若温度读数回落至 50 度中性以下且上涨占比跌破半数，则延续假设失效。"},
		{name: "named neutral baseline", narrative: "市场温度的中性基准为 50°，本次读数 76.0109 度。"},
		{name: "decimal definition", narrative: "50.0 度为中性，本次市场温度 76.0109 度。"},
		{name: "actual unsupported neutral reading", narrative: "当前市场温度 50 度，处于中性。", want: []string{"50"}},
		{name: "definition cannot validate another reading", narrative: "50 度为中性，当前市场温度 50 度。", want: []string{"50"}},
		{name: "threshold cannot validate current reading", narrative: "若温度降至 50 度中性线以下则转弱。当前温度 50 度。", want: []string{"50"}},
		{name: "untrusted adjacent definition", narrative: "当前市场温度 50 度为中性。", want: []string{"50"}},
		{name: "different invented threshold", narrative: "51 度为中性，当前温度 51 度。", want: []string{"51"}},
		{name: "unrelated unsupported decimal", narrative: "50 度为中性，当前市场温度 99.99 度。", want: []string{"99.99"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyNarrative(tc.narrative, facts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unverified = %v, want %v", got, tc.want)
			}
		})
	}
	if got := VerifyNarrative("50 度为中性。当前温度 50 度。", Facts{}); !reflect.DeepEqual(got, []string{"50"}) {
		t.Fatalf("missing temperature cannot borrow a reference value: %v", got)
	}
}

func TestVerifyNarrativeSeparatesTemperatureScaleReferencesFromReadings(t *testing.T) {
	facts := Facts{Temperature: &Temperature{Value: 84.53577054667922}}
	for _, tc := range []struct {
		name, narrative string
		want            []string
	}{
		{name: "reported scale", narrative: "市场温度本次采集成功且未标记过期：读数 84.5358（0–100 度，50 度为中性）。"},
		{name: "compact scale", narrative: "市场温度0–100度，本次读数 84.5358 度。"},
		{name: "ASCII separator", narrative: "市场温度采用 0-100 度量程，本次读数 84.5358 度。"},
		{name: "spaced separator", narrative: "温度读数采用 0 — 100 度量程，本次 84.5358 度。"},
		{name: "tilde separator", narrative: "温度模型量程 0～100 度，本次 84.5358 度。"},
		{name: "Chinese separator", narrative: "温度量程为 0 至 100 度，本次 84.5358 度。"},
		{name: "decimal scale endpoint", narrative: "市场温度采用 0–100.0° 量程，本次 84.5358 度。"},
		{name: "actual unsupported reading", narrative: "当前市场温度 100 度。", want: []string{"100"}},
		{name: "scale cannot validate a later reading", narrative: "市场温度采用 0–100 度量程，当前读数 100 度。", want: []string{"100"}},
		{name: "scale cannot validate an earlier reading", narrative: "当前市场温度 100 度，市场温度采用 0–100 度量程。", want: []string{"100"}},
		{name: "scale cannot validate a decimal reading", narrative: "市场温度采用 0–100.0 度量程，当前读数 100.0 度。", want: []string{"100.0"}},
		{name: "scale cannot validate a negative reading", narrative: "市场温度采用 0-100 度量程，当前读数 -100 度。", want: []string{"-100"}},
		{name: "different lower bound", narrative: "市场温度采用 1–100 度量程。", want: []string{"100"}},
		{name: "lower bound is not a suffix match", narrative: "市场温度采用 10–100 度量程。", want: []string{"100"}},
		{name: "different upper bound", narrative: "市场温度采用 0–101 度量程。", want: []string{"101"}},
		{name: "missing market temperature context", narrative: "设备温度为 0–100 度。", want: []string{"100"}},
		{name: "another sentence cannot borrow context", narrative: "市场温度 84.5358 度。设备温度为 0–100 度。", want: []string{"100"}},
		{name: "another clause cannot borrow context", narrative: "市场温度 84.5358 度，设备温度为 0–100 度。", want: []string{"100"}},
		{name: "unrelated number stays checked", narrative: "市场温度采用 0–100 度量程，另有 99.99% 的未证实涨幅。", want: []string{"99.99%"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyNarrative(tc.narrative, facts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unverified = %v, want %v", got, tc.want)
			}
		})
	}
	if got := VerifyNarrative("市场温度量程为 0–100 度，50 度为中性。", Facts{}); len(got) != 0 {
		t.Fatalf("a fixed scale is independent of missing readings: %v", got)
	}
}

func TestNarrativeNumberIsTemperatureReferenceRequiresEveryOccurrenceToBeReference(t *testing.T) {
	for _, tc := range []struct {
		name, narrative, token string
		want                   bool
	}{
		{name: "scale only", narrative: "市场温度读数 84.5358（0–100 度，50 度为中性）。", token: "100", want: true},
		{name: "ASCII scale", narrative: "市场温度量程 0-100 度。", token: "-100", want: true},
		{name: "decimal scale", narrative: "市场温度量程 0–100.0 度。", token: "100.0", want: true},
		{name: "neutral reference stays supported", narrative: "50 度为中性。", token: "50", want: true},
		{name: "same later reading", narrative: "市场温度量程 0–100 度，当前读数 100 度。", token: "100"},
		{name: "same earlier reading", narrative: "当前市场温度 100 度，市场温度量程 0–100 度。", token: "100"},
		{name: "same unrelated reading", narrative: "市场温度量程 0–100 度。设备温度 100 度。", token: "100"},
		{name: "different scale", narrative: "市场温度量程 1–100 度。", token: "100"},
		{name: "token not present", narrative: "市场温度量程 0–100 度。", token: "99.99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NarrativeNumberIsTemperatureReference(tc.narrative, tc.token); got != tc.want {
				t.Fatalf("reference = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBriefWithUnverifiedNumberIsFlaggedNotHidden(t *testing.T) {
	narrator := &fakeNarrator{available: true, text: "情绪分 88.8 为本次结论依据（情绪周期）"}
	service, store := newTestService(t, narrator, at(20, 15, 40))
	if _, err := service.RunManual(PhasePostClose); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return store.List("", 0)[0].Status == "ready" })
	brief := store.List("", 0)[0]
	if brief.NarrativeStatus != "ready" || !reflect.DeepEqual(brief.UnverifiedNumbers, []string{"88.8"}) {
		t.Fatalf("flagging failed: %+v", brief)
	}
}
