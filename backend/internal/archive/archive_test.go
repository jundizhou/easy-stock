package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sample struct {
	Symbol string `json:"symbol"`
	Price  float64 `json:"price"`
}

func TestAppendCreatesDailyJSONLAndLatest(t *testing.T) {
	root := t.TempDir()
	w, err := New[sample](root, "realtime")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := w.Append(sample{Symbol: "600519.SH", Price: 1275}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := w.Append(sample{Symbol: "000001.SZ", Price: 11.68}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 当日 JSONL 应含 2 行
	daily := filepath.Join(root, "realtime", time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(daily)
	if err != nil {
		t.Fatalf("read daily: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "600519.SH") {
		t.Errorf("first line missing symbol: %s", lines[0])
	}

	// latest.json 应为最后一次记录
	latest, err := os.ReadFile(filepath.Join(root, "realtime", "latest.json"))
	if err != nil {
		t.Fatalf("read latest: %v", err)
	}
	if !strings.Contains(string(latest), "000001.SZ") {
		t.Errorf("latest should be last record, got %s", latest)
	}
}

func TestAppendBatchAndReport(t *testing.T) {
	root := t.TempDir()
	w, err := New[sample](root, "news")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n, err := w.AppendBatch([]sample{{Symbol: "a"}, {Symbol: "b"}, {Symbol: "c"}})
	if err != nil || n != 3 {
		t.Fatalf("AppendBatch = %d, %v", n, err)
	}
	st := w.Report()
	if st.Files != 1 {
		t.Errorf("files = %d, want 1", st.Files)
	}
	if st.Lines != 3 {
		t.Errorf("lines = %d, want 3", st.Lines)
	}
	if st.LastWrite.IsZero() {
		t.Errorf("last write should be set")
	}
	if st.LastError != "" {
		t.Errorf("unexpected last error: %s", st.LastError)
	}
}

func TestNewEmptyRootFails(t *testing.T) {
	if _, err := New[sample]("", "realtime"); err == nil {
		t.Fatalf("expected error for empty root")
	}
}
