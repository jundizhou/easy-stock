package monitor

import "testing"

// 港股基准必须切到恒生指数：A 股宽基与港股交易日/成分不同，
// 拿深证成指对照会把两个市场的系统性差异算成个股偏离。
func TestDeviationIndexIDUsesHangSengForHongKong(t *testing.T) {
	for _, symbol := range []string{"00700.HK", "hk09988", "09988"} {
		if got := deviationIndexID("", symbol); got != "hsi" {
			t.Fatalf("deviationIndexID(%q) = %q, want hsi", symbol, got)
		}
	}
	if got := deviationIndexID("BoardMain", "600519.SH"); got == "hsi" {
		t.Fatalf("A 股不应使用恒生指数作为基准: %q", got)
	}
}

// 异动明细要能显示港股基准名称，否则页面上只剩一个 hsi 代码。
func TestAbnormalBenchmarkLabelsCoverHangSeng(t *testing.T) {
	if label := abnormalBenchmarkLabels[deviationIndexID("", "00700.HK")]; label != "恒生指数" {
		t.Fatalf("hk benchmark label = %q, want 恒生指数", label)
	}
}

// 港股没有交易所异常波动认定，必须带上口径说明；A 股不应出现该提示。
func TestAbnormalThresholdNoteOnlyForHongKong(t *testing.T) {
	if note := abnormalThresholdNote("00700.HK"); note == "" {
		t.Fatal("港股异动阈值必须说明口径")
	}
	if note := abnormalThresholdNote("600519.SH"); note != "" {
		t.Fatalf("A 股不该带港股口径提示：%q", note)
	}
}
