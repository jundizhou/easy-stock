package shannon

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// BoardOf 按代码前缀判定所属板块（与 Python core/stockdb.py board_of 同口径）。
func BoardOf(code string) string {
	switch {
	case strings.HasPrefix(code, "688"):
		return "科创板"
	case strings.HasPrefix(code, "30"):
		return "创业板"
	case strings.HasPrefix(code, "8"), strings.HasPrefix(code, "4"), strings.HasPrefix(code, "9"):
		return "北交所"
	case strings.HasPrefix(code, "60"):
		return "沪主板"
	case strings.HasPrefix(code, "002"):
		return "中小板"
	case strings.HasPrefix(code, "000"), strings.HasPrefix(code, "001"):
		return "深主板"
	}
	return "其他"
}

// ScanRequest 是扫描请求。
type ScanRequest struct {
	// Codes 显式指定代码（优先于 Universe / Board）。
	Codes []string
	// Universe 股票池：all（全市场，取自 stockdb 股票代码 索引）。
	// 注：Python 侧的规模指数预设（hs300/zz500 等）依赖 SDK get_index_stocks，
	// Free-StockDB 的 HTTP 接口未暴露指数成分表，Go 侧改为支持板块名扫描。
	Universe string
	// Board 板块名（取自 stockdb 板块* 表，如「3D打印」「半导体」）。
	Board string
	// IncludeBoards 只保留指定板块（按代码前缀判定）。
	IncludeBoards []string
	// ExcludeBoards 排除指定板块（按代码前缀判定）。
	ExcludeBoards []string
	// Limit 最多扫描多少只。
	Limit int
	// Workers 并发数。
	Workers int
	// Equity 账户净值（元），决定所有百分比仓位。
	Equity float64
}

// ScanError 是单只标的的取数/计算异常。
type ScanError struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

// ScanResponse 是扫描结果。
type ScanResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Universe    string        `json:"universe"`
	Count       int           `json:"count"`
	Results     []Result      `json:"results"`
	Plan        PortfolioPlan `json:"plan"`
	Errors      []ScanError   `json:"errors,omitempty"`
}

// ResolveUniverse 解析股票池。
func (f *Feed) ResolveUniverse(ctx context.Context, req ScanRequest) ([]string, string, error) {
	var codes []string
	label := ""
	switch {
	case len(req.Codes) > 0:
		codes = append(codes, req.Codes...)
		label = "指定 " + itoa(len(codes)) + " 只"
	case strings.TrimSpace(req.Board) != "":
		got, err := f.BoardSymbols(ctx, strings.TrimSpace(req.Board))
		if err != nil {
			return nil, "", err
		}
		codes = got
		label = "板块「" + strings.TrimSpace(req.Board) + "」（" + itoa(len(codes)) + " 只）"
	default:
		got, err := f.Codes(ctx)
		if err != nil {
			return nil, "", err
		}
		codes = got
		label = "全市场（" + itoa(len(codes)) + " 只）"
	}

	if len(req.IncludeBoards) > 0 {
		want := map[string]bool{}
		for _, b := range req.IncludeBoards {
			want[strings.TrimSpace(b)] = true
		}
		before := len(codes)
		kept := codes[:0]
		for _, c := range codes {
			if want[BoardOf(c)] {
				kept = append(kept, c)
			}
		}
		codes = kept
		label += " → 板块过滤 " + strings.Join(req.IncludeBoards, ",") + "（" + itoa(before) + "→" + itoa(len(codes)) + "）"
	}
	if len(req.ExcludeBoards) > 0 {
		skip := map[string]bool{}
		for _, b := range req.ExcludeBoards {
			skip[strings.TrimSpace(b)] = true
		}
		before := len(codes)
		kept := codes[:0]
		for _, c := range codes {
			if !skip[BoardOf(c)] {
				kept = append(kept, c)
			}
		}
		codes = kept
		label += " → 排除 " + strings.Join(req.ExcludeBoards, ",") + "（" + itoa(before) + "→" + itoa(len(codes)) + "）"
	}
	if req.Limit > 0 && len(codes) > req.Limit {
		codes = codes[:req.Limit]
	}
	return codes, label, nil
}

// ScanOne 对单只标的取数 + 六层分析。任何异常都降级为 error，不中断整体扫描。
func (f *Feed) ScanOne(ctx context.Context, code string, cfg Config, equity float64) (Result, string) {
	d, err := f.Daily(ctx, code)
	if err != nil {
		return Result{Code: code}, err.Error()
	}
	if len(d) < 2*cfg.SwingK+25 {
		return Result{Code: code}, "样本不足（需 " + itoa(2*cfg.SwingK+25) + " 根日线，实得 " + itoa(len(d)) + "）"
	}
	w := f.WeeklyFrom(d)
	m := f.MonthlyFrom(d)
	return Analyze(code, d, w, m, cfg, equity), ""
}

// Scan 批量扫描（有界并发 + deadline 兜底）。
//
// 并发模型镜像 quant-stock 的 ThreadPoolExecutor(max_workers) + as_completed(
// timeout=deadline)：用 foundation.FanOut 以 workers 个 goroutine 处理全市场代码，
// scanCtx 的 deadline 保证源异常时扫描不会无限拖延。单只标的的取数/计算异常已在
// ScanOne 内降级为 errs，不中断整体扫描。
func (f *Feed) Scan(ctx context.Context, req ScanRequest, cfg Config) (ScanResponse, error) {
	codes, label, err := f.ResolveUniverse(ctx, req)
	if err != nil {
		return ScanResponse{}, err
	}
	workers := req.Workers
	if workers <= 0 {
		workers = 8
	}
	equity := req.Equity
	if equity <= 0 {
		equity = DefaultEquity
	}

	// deadline 兜底：本地源为回环，余量充足；仅为防止源异常时无限拖延。
	scanCtx, cancel := context.WithTimeout(ctx, scanDeadline(len(codes)))
	defer cancel()

	results := make([]Result, 0, len(codes))
	var errs []ScanError
	var mu sync.Mutex
	collect := func(res Result, msg string) {
		mu.Lock()
		defer mu.Unlock()
		if msg != "" {
			errs = append(errs, ScanError{Code: res.Code, Error: msg})
		} else {
			results = append(results, res)
		}
	}
	foundation.FanOut(scanCtx, codes, workers, func(ctx context.Context, code string) error {
		res, msg := f.ScanOne(ctx, code, cfg, equity)
		collect(res, msg)
		return nil // 单只异常已降级为 errs，不中断整体
	})

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	return ScanResponse{
		Universe: label,
		Count:    len(results),
		Results:  results,
		Plan:     PortfolioPlanOf(results, cfg, equity),
		Errors:   errs,
	}, nil
}

// scanDeadline 计算一次扫描的合理上限：本地源回环约 10ms/只，给足 4 倍余量 +
// 基础 20s，封顶 120s（镜像 quant-stock 的 deadline 兜底）。
func scanDeadline(n int) time.Duration {
	d := 20*time.Second + time.Duration(n)*40*time.Millisecond
	if d > 120*time.Second {
		return 120 * time.Second
	}
	return d
}

// PlanPick 是组合预算中的一只标的。
type PlanPick struct {
	Code     string  `json:"code"`
	Name     string  `json:"name,omitempty"`
	Score    float64 `json:"score"`
	PosPct   float64 `json:"pos_pct"`
	Shares   int     `json:"shares"`
	Notional float64 `json:"notional"`
	RiskPct  float64 `json:"risk_pct"`
}

// PortfolioPlan 是组合层面的资金预算（仓位以占净值 % 为准）。
type PortfolioPlan struct {
	Equity           float64    `json:"equity"`
	Candidates       int        `json:"candidates"`
	Picked           []PlanPick `json:"picked"`
	TotalPositionPct float64    `json:"total_position_pct"`
	TotalRiskPct     float64    `json:"total_risk_pct"`
	TotalNotional    float64    `json:"total_notional"`
	CashPct          float64    `json:"cash_pct"`
	MaxPositions     int        `json:"max_positions"`
	MaxRiskPct       float64    `json:"max_risk_pct"`
	MaxSinglePct     float64    `json:"max_single_pct"`
	StoppedBy        string     `json:"stopped_by,omitempty"`
}

// PortfolioPlanOf 把「可入场」清单折算成一个组合层面的资金预算。
//
// 按分数取前 MaxOpenPositions 只，逐只累加名义仓位 % 与风险 %，
// 任一触及上限即停止加仓（现金是仓位）。
func PortfolioPlanOf(results []Result, cfg Config, equity float64) PortfolioPlan {
	if equity <= 0 {
		equity = DefaultEquity
	}
	cands := make([]Result, 0, len(results))
	for _, r := range results {
		if r.Bucket == "ENTRY" {
			cands = append(cands, r)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })

	plan := PortfolioPlan{
		Equity:       equity,
		Candidates:   len(cands),
		Picked:       []PlanPick{},
		MaxPositions: cfg.MaxOpenPositions,
		MaxRiskPct:   cfg.MaxOpenRiskPct,
		MaxSinglePct: cfg.MaxPositionPct,
	}
	var posPct, riskPct, notional float64
	for _, r := range cands {
		if len(plan.Picked) >= cfg.MaxOpenPositions {
			plan.StoppedBy = "持仓数已达上限 " + itoa(cfg.MaxOpenPositions) + " 只"
			break
		}
		p := r.Sizing.PositionPct
		rk := r.Sizing.RiskPctActual
		if riskPct+rk > cfg.MaxOpenRiskPct+1e-9 {
			plan.StoppedBy = "再加仓会使未平仓风险 " + fmtFloat(riskPct+rk, 2) + "% 超过上限 " + fmtFloat(cfg.MaxOpenRiskPct, 1) + "%"
			break
		}
		plan.Picked = append(plan.Picked, PlanPick{
			Code:     r.Code,
			Name:     r.Name,
			Score:    r.Score,
			PosPct:   p,
			Shares:   r.Sizing.TotalShares,
			Notional: r.Sizing.Notional,
			RiskPct:  rk,
		})
		posPct += p
		riskPct += rk
		notional += r.Sizing.Notional
	}
	plan.TotalPositionPct = Round2(posPct)
	plan.TotalRiskPct = roundTo(riskPct, 3) // Python 用 round(x, 3)
	plan.TotalNotional = Round2(notional)
	plan.CashPct = Round2(100.0 - posPct)
	return plan
}
