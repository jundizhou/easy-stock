package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// quantTrack 返回 clip_n300 独立前向验证轨的只读快照。
//
// 数据源是 write-once 静态文件（C:\quant\paper_forward\shadow\clip_n300\），
// 本 handler 只读不写；目录可经 A_STOCK_QUANT_TRACK_DIR 覆盖。
//
//	GET /api/v1/quant/track
//	→ { ok, track_dir, config, summary, periods[], settled[], nav[], oos, fetched_at }
//
// OOS 门禁语义（governance/ADR-20261001 + oos_gate.py）：
//   - 受污染区间 2024-01-01~2026-09-18 永久失去样本外资格；
//   - 唯一合法验证 = 2026-10-08 锚点起的前向累积，目标 24 期；
//   - gate.state: "LOCKED"（forward_settled < target）| "OPEN"（达标）。
type quantTrackCache struct {
	mu        sync.Mutex
	payload   json.RawMessage
	fetchedAt time.Time
	ttl       time.Duration
}

var quantTrack = &quantTrackCache{ttl: 30 * time.Second}

func (s *Server) quantTrackHandler(w http.ResponseWriter, r *http.Request) {
	quantTrack.mu.Lock()
	defer quantTrack.mu.Unlock()
	if quantTrack.payload != nil && time.Since(quantTrack.fetchedAt) < quantTrack.ttl {
		writeRawJSON(w, http.StatusOK, quantTrack.payload)
		return
	}
	dir := strings.TrimSpace(os.Getenv("A_STOCK_QUANT_TRACK_DIR"))
	if dir == "" {
		dir = `C:\quant\paper_forward\shadow\clip_n300`
	}
	payload, err := buildQuantTrackPayload(dir)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "track_dir": dir})
		return
	}
	quantTrack.payload = payload
	quantTrack.fetchedAt = time.Now()
	writeRawJSON(w, http.StatusOK, payload)
}

func buildQuantTrackPayload(dir string) (json.RawMessage, error) {
	info := map[string]any{"ok": true, "track_dir": dir}
	if config, err := os.ReadFile(filepath.Join(dir, "track_config.json")); err == nil {
		info["config"] = json.RawMessage(config)
	} else {
		info["config"] = nil
		info["config_error"] = err.Error()
	}
	if summary, err := os.ReadFile(filepath.Join(dir, "summary.json")); err == nil {
		info["summary"] = json.RawMessage(summary)
	} else {
		info["summary"] = nil
		info["summary_error"] = err.Error()
	}
	info["periods"] = readSignalPeriods(filepath.Join(dir, "signal"))
	info["settled"] = readSettledPeriods(filepath.Join(dir, "settled"))
	info["nav"] = readNavSeries(filepath.Join(dir, "nav_series.csv"))
	info["oos"] = map[string]any{
		"contaminated_start": "2024-01-01",
		"contaminated_end":   "2026-09-18",
		"anchor_date":        "2026-10-08",
		"target_periods":     24,
		"basis":              "governance/ADR-20261001 · backtest_5strategies/oos_gate.py",
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// signalPeriodSummary 是 signal_period_NN.json 的瘦身摘要（不含 300 只权重表）。
type signalPeriodSummary struct {
	Period        int    `json:"period"`
	SignalDate    string `json:"signal_date"`
	Role          string `json:"role"`
	EvidenceClass string `json:"evidence_class"`
	Status        string `json:"status"`
	NHoldings     int    `json:"n_holdings"`
	Turnover      any    `json:"turnover,omitempty"`
	RecordedAt    string `json:"recorded_at,omitempty"`
	File          string `json:"file"`
}

func readSignalPeriods(dir string) []signalPeriodSummary {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []signalPeriodSummary{}
	}
	periods := make([]signalPeriodSummary, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "signal_period_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		period, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "signal_period_"), ".json"))
		item := signalPeriodSummary{
			Period:        period,
			SignalDate:    stringField(doc, "signal_date"),
			Role:          stringField(doc, "role"),
			EvidenceClass: stringField(doc, "evidence_class"),
			Status:        stringField(doc, "status"),
			NHoldings:     intField(doc, "n_holdings"),
			Turnover:      doc["turnover"],
			RecordedAt:    stringField(doc, "recorded_at"),
			File:          name,
		}
		periods = append(periods, item)
	}
	sort.Slice(periods, func(i, j int) bool { return periods[i].Period < periods[j].Period })
	return periods
}

func readSettledPeriods(dir string) []map[string]any {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "period_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		doc["file"] = name
		items = append(items, doc)
	}
	sort.Slice(items, func(i, j int) bool {
		return fmtAny(items[i]["period"]) < fmtAny(items[j]["period"])
	})
	return items
}

func readNavSeries(path string) []map[string]any {
	file, err := os.Open(path)
	if err != nil {
		return []map[string]any{}
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil || len(rows) < 2 {
		return []map[string]any{}
	}
	header := rows[0]
	out := make([]map[string]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		item := make(map[string]any, len(header))
		for i, cell := range row {
			if i >= len(header) {
				break
			}
			if header[i] == "" || cell == "" {
				continue
			}
			if v, err := strconv.ParseFloat(cell, 64); err == nil {
				item[header[i]] = v
			} else if v, err := strconv.ParseBool(cell); err == nil {
				item[header[i]] = v
			} else {
				item[header[i]] = cell
			}
		}
		out = append(out, item)
	}
	return out
}

func stringField(doc map[string]any, key string) string {
	if v, ok := doc[key].(string); ok {
		return v
	}
	return ""
}

func intField(doc map[string]any, key string) int {
	switch v := doc[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func fmtAny(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	default:
		return ""
	}
}

func writeRawJSON(w http.ResponseWriter, status int, payload json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}
