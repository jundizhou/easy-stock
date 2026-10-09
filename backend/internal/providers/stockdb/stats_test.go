package stockdb

import (
	"testing"
)

func TestStatsRecordAndSnapshot(t *testing.T) {
	s := newStats()
	for i := 0; i < 10; i++ {
		s.record(int64((i+1)*1000), true, "") // 1ms..10ms
	}
	snap := s.snapshot()
	if snap.Total != 10 || snap.OK != 10 || snap.Errors != 0 {
		t.Fatalf("unexpected counts: %+v", snap)
	}
	if snap.P50Ms <= 0 || snap.P95Ms <= 0 {
		t.Fatalf("expected positive latency stats, got %+v", snap)
	}
	if !snap.Healthy {
		t.Fatalf("expected healthy after successes")
	}
}

func TestStatsDegraded(t *testing.T) {
	s := newStats()
	s.record(1000, true, "")
	// 首次失败未达阈值：仍健康
	s.record(1000, false, "boom")
	if !s.Healthy() {
		t.Fatalf("expected healthy before threshold (consecutive=1)")
	}
	// 连续第二次失败：进入降级
	s.record(1000, false, "boom")
	if s.Healthy() {
		t.Fatalf("expected degraded after threshold (consecutive=2)")
	}
	// 成功一次：自动恢复
	s.record(1000, true, "")
	if !s.Healthy() {
		t.Fatalf("expected healthy after a success")
	}
}

func TestStatsSnapshotPercentiles(t *testing.T) {
	s := newStats()
	// 1,2,...,100 us -> p50~50, p95~95
	for i := 1; i <= 100; i++ {
		s.record(int64(i), true, "")
	}
	snap := s.snapshot()
	if snap.P50Ms < 0.045 || snap.P50Ms > 0.055 {
		t.Fatalf("p50 out of range: %v", snap.P50Ms)
	}
	if snap.P95Ms < 0.090 || snap.P95Ms > 0.100 {
		t.Fatalf("p95 out of range: %v", snap.P95Ms)
	}
}
