// Package execution 提供 easy-stock 信号 → 券商 QMT/Ptrade 执行通道的执行网关。
//
// 设计红线（见 V0.2_Execution_Integration.md 与 2026 监管约束）：
//   - PIT 审计通过前，共振/拐点/双确认板块不进入执行；core2 是唯一被允许的执行源
//     （其信号来自 write-once 静态文件，天然无前视泄漏）。
//   - 订单只经券商官方 QMT/Ptrade 策略环境下达；禁止裸连柜台、禁止公有云跑单、
//     禁止破解客户端。
//   - P0+P1 阶段：网关默认 sim 模式，不接真实下单；live 路径仅在已启用生产开关、
//     未熔断、且适配层就绪时才放行，且本阶段适配层未接线（返回明确错误）。
package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SchemaVersion 是 OrderIntent / TargetPlan 的契约版本，前端与适配层据此消费。
const SchemaVersion = "1.0.0"

// PITWhitelist 是允许进入执行通道的策略白名单。
// 硬约束：共振/拐点/双确认板块仅作观察，PIT 审计通过前不生成实盘指令。
var PITWhitelist = map[string]bool{
	"core2": true,
}

// core2StrategyName 与 httpapi.core2Spec 对齐（展示一致性）。
const core2StrategyName = "core2"

// GatewayConfig 是执行网关的构造配置（全部经 env 注入，不进 httpapi.Config）。
type GatewayConfig struct {
	// PaperDir 是 core2 paper_forward 目录（覆盖 A_STOCK_CORE2_PAPER_DIR）。
	PaperDir string
	// ProductionEnabled 是否允许 live 执行（应来自 env A_STOCK_PRODUCTION_ENABLED）。
	ProductionEnabled bool
	// KillSwitchFile 急停文件落点（默认 <dataDir>/execution/killswitch.on）。
	KillSwitchFile string
	// AuditFile append-only 审计落点（默认 <dataDir>/execution/audit.jsonl）。
	AuditFile string
	// Logger 可选日志器；nil 时退化为 log.Default()。
	Logger *log.Logger
}

// Gateway 是执行网关的运行时实例，线程安全（读多写少，关键动作串行化）。
type Gateway struct {
	cfg      GatewayConfig
	paperDir string
	ks       *KillSwitch
	auditor  *Auditor
	mu       sync.RWMutex
	adapter  string // 已就绪的券商适配层名（"" 表示 pending，P0+P1 阶段恒为 ""）
}

// NewGateway 构造执行网关。所有路径均可经配置或环境变量解析，缺省回退到本地约定。
func NewGateway(cfg GatewayConfig) *Gateway {
	paperDir := strings.TrimSpace(cfg.PaperDir)
	if paperDir == "" {
		paperDir = strings.TrimSpace(os.Getenv("A_STOCK_CORE2_PAPER_DIR"))
	}
	if paperDir == "" {
		paperDir = `C:\quant\paper_forward`
	}
	ksFile := cfg.KillSwitchFile
	if ksFile == "" {
		ksFile = filepath.Join(defaultExecDir(), "killswitch.on")
	}
	auditFile := cfg.AuditFile
	if auditFile == "" {
		auditFile = filepath.Join(defaultExecDir(), "audit.jsonl")
	}
	return &Gateway{
		cfg:      cfg,
		paperDir: paperDir,
		ks:       NewKillSwitch(ksFile),
		auditor:  NewAuditor(auditFile),
	}
}

func (g *Gateway) logf(format string, args ...any) {
	if g.cfg.Logger != nil {
		g.cfg.Logger.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

// SetAdapter 注册一个已就绪的券商适配层（P2 阶段调用）。P0+P1 阶段不调用，
// 因此 adapter 恒为 ""，网关 Mode() 恒为 sim。
func (g *Gateway) SetAdapter(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.adapter = strings.TrimSpace(name)
}

// Mode 返回网关当前执行模式：当且仅当「生产开关已开 + 未熔断 + 适配层就绪」时为 live，
// 否则为 sim。P0+P1 阶段适配层未接线，故恒为 sim。
func (g *Gateway) Mode() string {
	g.mu.RLock()
	adapterReady := g.adapter != ""
	g.mu.RUnlock()
	if g.cfg.ProductionEnabled && !g.ks.Engaged() && adapterReady {
		return "live"
	}
	return "sim"
}

// adapterReady 内部读取适配层就绪态（需调用方持有/不持有锁时各自一致）。
func (g *Gateway) adapterReady() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.adapter != ""
}

// defaultExecDir 返回执行网关默认数据目录（A_STOCK_DATA_DIR/execution 或 C:\quant\data\execution）。
func defaultExecDir() string {
	base := strings.TrimSpace(os.Getenv("A_STOCK_DATA_DIR"))
	if base == "" {
		base = `C:\quant\data`
	}
	return filepath.Join(base, "execution")
}

// ---------------------------------------------------------------------------
// 读取 core2 最新期（复用与 httpapi 一致的 period_NN.json 约定，但保持包解耦，
// 不反向依赖 httpapi 以避免循环引用）。
// ---------------------------------------------------------------------------

func (g *Gateway) readLatestCore2(dir string) (map[string]any, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "periods"))
	if err != nil {
		return nil, err
	}
	type item struct {
		period int
		doc    map[string]any
	}
	best := item{}
	found := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "period_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, "periods", name))
		if rerr != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		period, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "period_"), ".json"))
		if !found || period > best.period {
			best = item{period: period, doc: doc}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("未找到任何 period_NN.json（目录 %s）", dir)
	}
	return best.doc, nil
}

// ---------------------------------------------------------------------------
// 生成可执行目标（GenerateTarget）
// ---------------------------------------------------------------------------

// GenerateTargetRequest 是生成目标计划的请求。Strategy 缺省为 core2。
type GenerateTargetRequest struct {
	Strategy string `json:"strategy"`
}

// PlanTarget 是目标计划中的单票条目（已做整手换算与预过滤标注）。
type PlanTarget struct {
	Code         string  `json:"code"`
	TargetWeight float64 `json:"target_weight"`
	AssumedPrice float64 `json:"assumed_price"`
	SignalScore  float64 `json:"signal_score"`
	NotionalCNY  float64 `json:"notional_cny"`
	Shares       int     `json:"shares"`        // 整手股数
	EstNotional  float64 `json:"est_notional"`  // shares * assumed_price
	Action       string  `json:"action"`        // REBALANCE_TO_WEIGHT（具体买/卖由持仓比对得出，P2 处理）
	Prefilter    string  `json:"prefilter"`     // ST/停牌/涨跌停预过滤原因（空表示放行）
	Warning      string  `json:"warning,omitempty"`
}

// TargetPlan 是 GenerateTarget 的产出：一份可供复核与执行的目标权重表。
type TargetPlan struct {
	SchemaVersion   string       `json:"schema_version"`
	Strategy        string       `json:"strategy"`
	PitAuditPassed  bool         `json:"pit_audit_passed"`
	SignalID        string       `json:"signal_id"`
	SignalDate      any          `json:"signal_date"`
	ExecuteDate     any          `json:"execute_date"`
	Period          any          `json:"period"`
	Role            any          `json:"role"`
	Status          any          `json:"status"`
	GrossWeight     float64      `json:"gross_weight"`
	NHoldings       int          `json:"n_holdings"`
	Mode            string       `json:"mode"`
	PaperDir        string       `json:"paper_dir"`
	GeneratedAt     string       `json:"generated_at"`
	Targets         []PlanTarget `json:"targets"`
	Warnings        []string     `json:"warnings"`
}

// GenerateTarget 读取 core2 最新期，产出一份可执行的目标权重计划。
//
// 该动作只读、仅预览，不要求生产开关；sim 与 live 下都可调用（live 下用于复核）。
// 不满足 PIT 白名单的策略直接拒绝；core2 因数据来自 write-once 静态文件，
// 天然 PIT 合规，pit_audit_passed 置 true。
func (g *Gateway) GenerateTarget(_ context.Context, req GenerateTargetRequest) (*TargetPlan, error) {
	strategy := strings.TrimSpace(req.Strategy)
	if strategy == "" {
		strategy = core2StrategyName
	}
	if !PITWhitelist[strategy] {
		return nil, fmt.Errorf("策略 %q 不在 PIT 执行白名单（仅允许 core2），拒绝生成可执行目标", strategy)
	}

	doc, err := g.readLatestCore2(g.paperDir)
	if err != nil {
		return nil, fmt.Errorf("读取 core2 最新期失败: %w", err)
	}

	rawHoldings, _ := doc["holdings"].([]any)
	targets := make([]PlanTarget, 0, len(rawHoldings))
	var gross float64
	for _, rh := range rawHoldings {
		hm, ok := rh.(map[string]any)
		if !ok {
			continue
		}
		code := asString(hm, "code")
		if code == "" {
			continue
		}
		tw := asFloat(hm, "target_weight")
		price := asFloat(hm, "assumed_price")
		notional := asFloat(hm, "notional_cny")
		score := asFloat(hm, "signal_score")
		shares := SharesForNotional(notional, price, DefaultRoundLot)
		pt := PlanTarget{
			Code:         code,
			TargetWeight: tw,
			AssumedPrice: price,
			SignalScore:  score,
			NotionalCNY:  notional,
			Shares:       shares,
			EstNotional:  float64(shares) * price,
			Action:       "REBALANCE_TO_WEIGHT",
			Prefilter:    Prefilter(code, nil),
		}
		if shares == 0 && notional > 0 {
			pt.Warning = "整手换算为 0 股（金额/价格过小），已剔除"
		}
		targets = append(targets, pt)
		gross += tw
	}

	warnings := ValidateTargetWeights(targets, DefaultMaxSingleWeight)
	if w := ValidateGrossWeight(gross); w != "" {
		warnings = append(warnings, w)
	}

	signalID := asString(doc, "data_fingerprint")
	if signalID == "" {
		signalID = fmt.Sprintf("core2-period-%v", doc["period"])
	}

	plan := &TargetPlan{
		SchemaVersion:  SchemaVersion,
		Strategy:       strategy,
		PitAuditPassed: true,
		SignalID:       signalID,
		SignalDate:     doc["signal_date"],
		ExecuteDate:    doc["execute_date"],
		Period:         doc["period"],
		Role:           doc["role"],
		Status:         doc["status"],
		GrossWeight:    gross,
		NHoldings:      len(targets),
		Mode:           g.Mode(),
		PaperDir:       g.paperDir,
		GeneratedAt:    time.Now().Format(time.RFC3339),
		Targets:        targets,
		Warnings:       warnings,
	}

	_ = g.auditor.Append(AuditRecord{
		Type:     "target_generated",
		Mode:     plan.Mode,
		Strategy: strategy,
		SignalID: signalID,
		Detail:   mustJSON(map[string]any{"n_targets": len(targets), "gross_weight": gross}),
	})
	return plan, nil
}

// ---------------------------------------------------------------------------
// 网关状态（Status）
// ---------------------------------------------------------------------------

// SignalMeta 是状态里的最新信号摘要。
type SignalMeta struct {
	Period      any `json:"period"`
	Role        any `json:"role"`
	SignalDate  any `json:"signal_date"`
	ExecuteDate any `json:"execute_date"`
	Status      any `json:"status"`
	NHoldings   any `json:"n_holdings"`
	GrossWeight any `json:"gross_weight"`
}

// StatusResponse 是网关状态的只读快照。
type StatusResponse struct {
	Mode              string     `json:"mode"`
	ProductionEnabled bool       `json:"production_enabled"`
	KillSwitchEngaged bool       `json:"kill_switch_engaged"`
	PITWhitelist      []string   `json:"pit_whitelist"`
	StrategyAllowed   string     `json:"strategy_allowed"`
	PaperDir          string     `json:"paper_dir"`
	Adapter           string     `json:"adapter"` // "" 表示 pending
	AdapterReady      bool       `json:"adapter_ready"`
	LatestSignal      *SignalMeta `json:"latest_signal,omitempty"`
	AuditPath         string     `json:"audit_path"`
	AuditCount        int        `json:"audit_count"`
	Warnings          []string   `json:"warnings"`
}

// Status 返回网关当前状态（只读，不触发任何动作）。
func (g *Gateway) Status() (*StatusResponse, error) {
	resp := &StatusResponse{
		Mode:              g.Mode(),
		ProductionEnabled: g.cfg.ProductionEnabled,
		KillSwitchEngaged: g.ks.Engaged(),
		StrategyAllowed:   core2StrategyName,
		PaperDir:          g.paperDir,
		AdapterReady:      g.adapterReady(),
		AuditPath:         g.auditor.path,
	}
	for k := range PITWhitelist {
		resp.PITWhitelist = append(resp.PITWhitelist, k)
	}
	sort.Strings(resp.PITWhitelist)
	resp.Adapter = strings.TrimSpace(g.adapter)

	if doc, err := g.readLatestCore2(g.paperDir); err == nil {
		resp.LatestSignal = &SignalMeta{
			Period:      doc["period"],
			Role:        doc["role"],
			SignalDate:  doc["signal_date"],
			ExecuteDate: doc["execute_date"],
			Status:      doc["status"],
			NHoldings:   doc["n_holdings"],
			GrossWeight: doc["gross_weight"],
		}
	} else {
		resp.Warnings = append(resp.Warnings, "无法读取最新信号："+err.Error())
	}
	if cnt, err := g.auditor.Count(); err == nil {
		resp.AuditCount = cnt
	}
	if g.ks.Engaged() {
		resp.Warnings = append(resp.Warnings, "急停开关已拉闸：拒绝一切下单动作")
	}
	if !g.adapterReady() {
		resp.Warnings = append(resp.Warnings, "券商适配层未就绪（P2 待实现）：live 执行不可用")
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// 受理执行意图（AcceptIntent）
// ---------------------------------------------------------------------------

// IntentTarget 是 OrderIntent 中的单票目标。
type IntentTarget struct {
	Code         string  `json:"code"`
	TargetWeight float64 `json:"target_weight"`
	AssumedPrice float64 `json:"assumed_price"`
	Action       string  `json:"action,omitempty"` // BUY | SELL | HOLD
}

// IntentConstraints 是执行约束（与 limits 护栏对齐）。
type IntentConstraints struct {
	MaxSingleWeight  float64 `json:"max_single_weight"`
	MaxDailyTurnover float64 `json:"max_daily_turnover"`
	RoundLot         int     `json:"round_lot"`
	BlockST          bool    `json:"block_st"`
	BlockSuspended   bool    `json:"block_suspended"`
	BlockLimit       bool    `json:"block_limit"`
}

// OrderIntent 是「easy-stock 信号 → 券商执行」的统一契约（前端/适配层消费）。
// schema_version 必须匹配，strategy 必须在 PIT 白名单，pit_audit_passed 必须为真。
type OrderIntent struct {
	SchemaVersion   string            `json:"schema_version"`
	Strategy        string            `json:"strategy"`
	PitAuditPassed  bool              `json:"pit_audit_passed"`
	SignalID        string            `json:"signal_id"`
	ExecuteOn       string            `json:"execute_on"` // sim | live
	Account         string            `json:"account"`
	Targets         []IntentTarget    `json:"targets"`
	Constraints     IntentConstraints `json:"constraints"`
	GeneratedAt     string            `json:"generated_at,omitempty"`
	Note            string            `json:"note,omitempty"`
}

// IntentAck 是 AcceptIntent 的回执。sim 模式下直接附带模拟成交（不落真实订单）。
type IntentAck struct {
	Accepted    bool     `json:"accepted"`
	Mode        string   `json:"mode"`
	IntentID    string   `json:"intent_id"`
	Strategy    string   `json:"strategy"`
	SignalID    string   `json:"signal_id"`
	ExecuteOn   string   `json:"execute_on"`
	ReviewedAt  string   `json:"reviewed_at"`
	Warnings    []string `json:"warnings"`
	Fills       []Fill   `json:"fills,omitempty"` // sim 模式直接给出模拟成交
	Message     string   `json:"message,omitempty"`
}

// AcceptIntent 受理一份执行意图：校验契约与护栏，sim 模式生成模拟成交，
// live 模式在 P0+P1 阶段明确拒绝（适配层未接线）。
func (g *Gateway) AcceptIntent(_ context.Context, intent OrderIntent) (*IntentAck, error) {
	now := time.Now().Format(time.RFC3339)
	intentID := newID("intent")
	ack := &IntentAck{
		IntentID:   intentID,
		Strategy:   intent.Strategy,
		SignalID:   intent.SignalID,
		ExecuteOn:  strings.ToLower(strings.TrimSpace(intent.ExecuteOn)),
		ReviewedAt: now,
	}

	if intent.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("契约版本不匹配：收到 %q，期望 %q", intent.SchemaVersion, SchemaVersion)
	}
	if !PITWhitelist[strings.TrimSpace(intent.Strategy)] {
		return nil, fmt.Errorf("策略 %q 不在 PIT 执行白名单，拒绝受理", intent.Strategy)
	}
	if !intent.PitAuditPassed {
		return nil, fmt.Errorf("PIT 审计未通过，拒绝生成实盘指令（硬约束）")
	}
	if g.ks.Engaged() {
		return nil, fmt.Errorf("急停开关已拉闸，拒绝一切下单动作")
	}
	if ack.ExecuteOn == "" {
		ack.ExecuteOn = "sim"
	}

	// 转为 PlanTarget 做护栏校验
	plans := make([]PlanTarget, 0, len(intent.Targets))
	for _, t := range intent.Targets {
		lot := intent.Constraints.RoundLot
		if lot <= 0 {
			lot = DefaultRoundLot
		}
		shares := SharesForNotional(t.TargetWeight*100000, t.AssumedPrice, lot) // 以假定账户 10w 估股数
		plans = append(plans, PlanTarget{
			Code:         t.Code,
			TargetWeight: t.TargetWeight,
			AssumedPrice: t.AssumedPrice,
			Shares:       shares,
			EstNotional:  float64(shares) * t.AssumedPrice,
			Action:       strings.ToUpper(t.Action),
			Prefilter:    Prefilter(t.Code, nil),
		})
	}
	ack.Warnings = ValidateTargetWeights(plans, defaultMaxSingle(intent.Constraints.MaxSingleWeight))

	// live 放行条件
	if ack.ExecuteOn == "live" {
		if !g.cfg.ProductionEnabled {
			return nil, fmt.Errorf("实盘未启用（需 A_STOCK_PRODUCTION_ENABLED=true），拒绝 live 执行")
		}
		if !g.adapterReady() {
			return nil, fmt.Errorf("券商适配层未就绪（P2 待实现），P0+P1 阶段不接真实下单，拒绝 live 执行")
		}
		// 适配层就绪时此处应把意图投递给适配层并拿回 broker order id；
		// P0+P1 不接线，故永不走到这里。
		return nil, fmt.Errorf("P0+P1：live 执行未接线")
	}

	// sim 模式：生成模拟成交并登记审计（不触达任何券商）
	fills := make([]Fill, 0, len(plans))
	for _, p := range plans {
		if p.Shares <= 0 {
			continue
		}
		side := "BUY"
		if p.Action == "SELL" {
			side = "SELL"
		}
		fill := Fill{
			Code:      p.Code,
			Side:      side,
			Price:     p.AssumedPrice,
			Shares:    p.Shares,
			Notional:  p.EstNotional,
			OrderID:   newID("order"),
			FillID:    newID("fill"),
			FilledAt:  now,
			Source:    "sim",
			Note:      "模拟成交（非真实订单）",
		}
		fills = append(fills, fill)
		if _, err := g.RecordFill(context.Background(), fill); err != nil {
			g.logf("level=warn event=sim_fill_audit_err fill=%s err=%q", fill.FillID, err.Error())
		}
	}
	ack.Accepted = true
	ack.Mode = g.Mode()
	ack.Fills = fills
	ack.Message = fmt.Sprintf("sim 受理成功，生成 %d 笔模拟成交", len(fills))

	_ = g.auditor.Append(AuditRecord{
		Type:     "intent_accepted",
		Mode:     ack.Mode,
		Strategy: intent.Strategy,
		SignalID: intent.SignalID,
		IntentID: intentID,
		Detail:   mustJSON(map[string]any{"execute_on": ack.ExecuteOn, "n_fills": len(fills)}),
	})
	return ack, nil
}

// ---------------------------------------------------------------------------
// 成交登记（RecordFill）
// ---------------------------------------------------------------------------

// Fill 是一笔成交记录（sim 由网关生成；live 由 P2 适配层回灌）。
type Fill struct {
	Code      string  `json:"code"`
	Side      string  `json:"side"` // BUY | SELL
	Price     float64 `json:"price"`
	Shares    int     `json:"shares"`
	Notional  float64 `json:"notional"`
	OrderID   string  `json:"order_id"`
	FillID    string  `json:"fill_id"`
	FilledAt  string  `json:"filled_at"`
	Source    string  `json:"source"` // sim | qmt | ptrade
	Note      string  `json:"note,omitempty"`
}

// FillAck 是 RecordFill 的回执。
type FillAck struct {
	Recorded   bool   `json:"recorded"`
	FillID     string `json:"fill_id"`
	RecordedAt string `json:"recorded_at"`
}

// RecordFill 登记一笔成交到 append-only 审计（底层原语，P2 适配层回灌真实成交时调用）。
func (g *Gateway) RecordFill(_ context.Context, fill Fill) (*FillAck, error) {
	if strings.TrimSpace(fill.FillID) == "" {
		fill.FillID = newID("fill")
	}
	if strings.TrimSpace(fill.FilledAt) == "" {
		fill.FilledAt = time.Now().Format(time.RFC3339)
	}
	if err := g.auditor.Append(AuditRecord{
		Type:     "fill_recorded",
		Mode:     g.Mode(),
		Strategy: core2StrategyName,
		FillID:   fill.FillID,
		Detail:   mustJSON(fill),
	}); err != nil {
		return nil, err
	}
	return &FillAck{
		Recorded:   true,
		FillID:     fill.FillID,
		RecordedAt: fill.FilledAt,
	}, nil
}

// EngageKillSwitch / DisengageKillSwitch 暴露急停拉闸/解除（供运维与前端操作）。
func (g *Gateway) EngageKillSwitch() error {
	err := g.ks.Engage()
	if err == nil {
		_ = g.auditor.Append(AuditRecord{Type: "killswitch_engaged", Mode: g.Mode()})
	}
	return err
}

func (g *Gateway) DisengageKillSwitch() error {
	err := g.ks.Disengage()
	if err == nil {
		_ = g.auditor.Append(AuditRecord{Type: "killswitch_disengaged", Mode: g.Mode()})
	}
	return err
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func defaultMaxSingle(v float64) float64 {
	if v <= 0 {
		return DefaultMaxSingleWeight
	}
	return v
}

func asFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	}
	return 0
}

func asString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// idSeq 保证同一纳秒内生成的多个 ID 仍互不相同。Windows 上 time.Now() 的时钟
// 粒度可能粗于纳秒，仅靠时间戳不足以保证唯一性（订单/成交编号必须唯一可追溯）。
var idSeq atomic.Uint64

// newID 生成带前缀的时间排序唯一 ID（用于意图/订单/成交编号）。
func newID(prefix string) string {
	return fmt.Sprintf("%s_%d_%05d", prefix, time.Now().UnixNano(), idSeq.Add(1)%100000)
}
