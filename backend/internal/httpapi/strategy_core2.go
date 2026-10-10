package httpapi

import (
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

// core2Paper 返回 core2 中线量化策略的 paper 前向状态（只读）。
//
// 数据源是 paper_forward 的 write-once 静态文件：
//   - periods\period_NN.json —— 每期信号/持仓快照（含 signal_score 与目标权重）
//   - state\paper_state.json —— 前向轨推进状态
//   - state\params_lock.json —— 冻结参数锁（core2_v1.yaml v1.2.0）
//
// 本 handler 只读不写，不触发任何选股计算（计算由 Python 侧 paper_forward 负责），
// 因此不会出现「界面看到的就是刚算出来的」这类 PIT 污染。
// 目录可经 A_STOCK_CORE2_PAPER_DIR 覆盖。
//
//	GET /api/v1/strategy/core2/paper
//	→ { ok, paper_dir, spec, params_lock, state, latest, periods[], fetched_at }
type core2PaperCache struct {
	mu        sync.Mutex
	payload   json.RawMessage
	fetchedAt time.Time
	ttl       time.Duration
}

var core2Paper = &core2PaperCache{ttl: 30 * time.Second}

// core2Spec 是 core2_v1.yaml 中与展示相关的冻结口径（只读提示，不参与计算）。
var core2Spec = map[string]any{
	"name":        "core2 中线量化",
	"spec_file":   `C:\quant\specs\core2_v1.yaml`,
	"version":     "v1.2.0",
	"status":      "FROZEN",
	"score":       "rank(rev20) + rank(amihud)",
	"rev20":       "close_T / close_{T-20} - 1 · direction=LOW_GOOD",
	"amihud":      "mean(|ret| / amount) 取 log · direction=HIGH_GOOD",
	"universe":    "沪深主板 PIT 股池 · 非 ST · 非停牌",
	"pre_screen":  "资产负债率 > 60% 剔除",
	"top_n":       10,
	"min_candidates": 100,
	"rebalance":   "20 个交易日 · 定期换仓 · 不重叠",
	"execution":   "T 日收盘信号 → T+1 开盘价成交",
	"exit_rules":  map[string]any{"stop_loss": false, "take_profit": false, "early_exit": false},
	"position":    "10 只等权 · 单票 ≤ 10% · 不加杠杆不做空",
}

func (s *Server) core2PaperHandler(w http.ResponseWriter, r *http.Request) {
	core2Paper.mu.Lock()
	defer core2Paper.mu.Unlock()
	if core2Paper.payload != nil && time.Since(core2Paper.fetchedAt) < core2Paper.ttl {
		writeRawJSON(w, http.StatusOK, core2Paper.payload)
		return
	}
	dir := strings.TrimSpace(os.Getenv("A_STOCK_CORE2_PAPER_DIR"))
	if dir == "" {
		dir = `C:\quant\paper_forward`
	}
	payload, err := buildCore2PaperPayload(dir)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "paper_dir": dir})
		return
	}
	core2Paper.payload = payload
	core2Paper.fetchedAt = time.Now()
	writeRawJSON(w, http.StatusOK, payload)
}

func buildCore2PaperPayload(dir string) (json.RawMessage, error) {
	info := map[string]any{"ok": true, "paper_dir": dir, "spec": core2Spec}
	if raw, err := os.ReadFile(filepath.Join(dir, "state", "params_lock.json")); err == nil {
		info["params_lock"] = json.RawMessage(raw)
	} else {
		info["params_lock"] = nil
		info["params_lock_error"] = err.Error()
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "state", "paper_state.json")); err == nil {
		info["state"] = json.RawMessage(raw)
	} else {
		info["state"] = nil
		info["state_error"] = err.Error()
	}
	periods, latest := readCore2Periods(filepath.Join(dir, "periods"))
	info["periods"] = periods
	info["latest"] = latest
	info["fetched_at"] = time.Now().Format(time.RFC3339)
	raw, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// readCore2Periods 读取 period_NN.json（全量，按 period 升序），并把最后一期作为 latest 返回。
// latest 保留完整 holdings，历史期只保留摘要，避免一次返回过多权重表。
func readCore2Periods(dir string) ([]map[string]any, map[string]any) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}, nil
	}
	type item struct {
		period int
		doc    map[string]any
		name   string
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "period_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		period, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "period_"), ".json"))
		items = append(items, item{period: period, doc: doc, name: name})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].period < items[j].period })

	summaries := make([]map[string]any, 0, len(items))
	for _, entry := range items {
		summaries = append(summaries, map[string]any{
			"period":        entry.doc["period"],
			"role":          entry.doc["role"],
			"signal_date":   entry.doc["signal_date"],
			"execute_date":  entry.doc["execute_date"],
			"status":        entry.doc["status"],
			"n_holdings":    entry.doc["n_holdings"],
			"gross_weight":  entry.doc["gross_weight"],
			"recorded_at":   entry.doc["recorded_at"],
			"file":          entry.name,
		})
	}
	var latest map[string]any
	if len(items) > 0 {
		latest = items[len(items)-1].doc
		latest["file"] = items[len(items)-1].name
	}
	return summaries, latest
}
