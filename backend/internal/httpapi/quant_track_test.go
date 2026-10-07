package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSignalPeriodsSkipsJunk(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("signal_period_01.json", `{"period":1,"signal_date":"2026-10-08","role":"FORWARD","evidence_class":"FORWARD","status":"SIGNAL_ONLY","n_holdings":300,"recorded_at":"2026-10-08T09:31:00+08:00"}`)
	write("signal_period_00.json", `{"period":0,"signal_date":"2026-09-30","role":"T0_BUILD","evidence_class":"PRE_ANCHOR_DESCRIPTIVE","status":"SIGNAL_ONLY","n_holdings":300}`)
	write("signal_period_bad.json", `{not json}`)
	write("unrelated.json", `{"period":9}`)
	write("signal_period_02.json", `[]`) // 顶层非对象，跳过
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	periods := readSignalPeriods(dir)
	if len(periods) != 2 {
		t.Fatalf("expected 2 valid periods, got %d: %+v", len(periods), periods)
	}
	if periods[0].Period != 0 || periods[1].Period != 1 {
		t.Fatalf("periods not sorted: %d then %d", periods[0].Period, periods[1].Period)
	}
	if periods[0].EvidenceClass != "PRE_ANCHOR_DESCRIPTIVE" || periods[1].EvidenceClass != "FORWARD" {
		t.Fatalf("evidence_class mismatch: %+v", periods)
	}
	if periods[1].NHoldings != 300 {
		t.Fatalf("n_holdings parse failed: %+v", periods[1])
	}
}

func TestReadNavSeriesTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nav_series.csv")
	content := ",signal_date,evidence_class,status,n_holdings,settled,net_100000\n" +
		"0,2026-09-30,PRE_ANCHOR_DESCRIPTIVE,SIGNAL_ONLY,300,False,\n" +
		"1,2026-10-08,FORWARD,SIGNAL_ONLY,300,True,1.0042\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	nav := readNavSeries(path)
	if len(nav) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(nav))
	}
	if nav[0]["signal_date"] != "2026-09-30" {
		t.Fatalf("string cell parse: %+v", nav[0])
	}
	if nav[0]["settled"] != false {
		t.Fatalf("bool cell parse: %+v", nav[0])
	}
	if nav[1]["net_100000"] != 1.0042 {
		t.Fatalf("float cell parse: %+v", nav[1])
	}
	// 空单元格不落键
	if _, ok := nav[0]["net_100000"]; ok {
		t.Fatalf("empty cell should be omitted: %+v", nav[0])
	}
}

func TestBuildQuantTrackPayloadMissingDir(t *testing.T) {
	payload, err := buildQuantTrackPayload(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir should degrade, not error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["ok"] != true {
		t.Fatalf("ok flag missing: %+v", doc)
	}
	if doc["config"] != nil {
		t.Fatalf("config should be nil for missing dir")
	}
	if doc["oos"] == nil {
		t.Fatalf("oos block missing")
	}
	periods, _ := doc["periods"].([]any)
	if periods == nil || len(periods) != 0 {
		t.Fatalf("periods should be empty array, got %v", doc["periods"])
	}
}

// TestQuantTrackLive 读取本机真实 clip_n300 轨道（write-once 资产，只读）。
// 运行：QUANT_TRACK_LIVE=1 go test ./internal/httpapi/ -run TestQuantTrackLive
func TestQuantTrackLive(t *testing.T) {
	if os.Getenv("QUANT_TRACK_LIVE") != "1" {
		t.Skip("set QUANT_TRACK_LIVE=1 to run against real clip_n300 track")
	}
	payload, err := buildQuantTrackPayload(`C:\quant\paper_forward\shadow\clip_n300`)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		t.Fatal(err)
	}
	config, _ := doc["config"].(map[string]any)
	if config == nil {
		t.Fatalf("track_config.json missing")
	}
	if config["anchor_date"] != "2026-10-08" || config["target_periods"] != float64(24) {
		t.Fatalf("config anchor/target mismatch: %+v", config)
	}
	periods, _ := doc["periods"].([]any)
	if len(periods) < 1 {
		t.Fatalf("expected at least the T0 signal period")
	}
	first, _ := periods[0].(map[string]any)
	if first["signal_date"] != "2026-09-30" {
		t.Fatalf("T0 signal_date mismatch: %v", first["signal_date"])
	}
	nav, _ := doc["nav"].([]any)
	if len(nav) < 1 {
		t.Fatalf("nav_series.csv should have the T0 row")
	}
	t.Logf("live track OK: periods=%d nav_rows=%d", len(periods), len(nav))
}
