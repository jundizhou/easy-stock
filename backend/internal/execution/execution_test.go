package execution

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 测试脚手架
// ---------------------------------------------------------------------------

// writeCore2Sample 构造一份最小的 core2 paper_forward 目录（periods/period_NN.json），
// 其中 period_02 是「最新期」，period_01 用于验证网关取的是期号最大者而非字典序首个。
func writeCore2Sample(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	periods := filepath.Join(dir, "periods")
	if err := os.MkdirAll(periods, 0o755); err != nil {
		t.Fatalf("mkdir periods: %v", err)
	}
	newest := map[string]any{
		"period":           2,
		"role":             "forward",
		"status":           "locked",
		"signal_date":      "2026-10-09",
		"execute_date":     "2026-10-12",
		"data_fingerprint": "fp-test-0001",
		"n_holdings":       3,
		"gross_weight":     0.23,
		"holdings": []any{
			// 8000 / 10 = 800 股（整手）
			map[string]any{"code": "600000.SH", "target_weight": 0.08, "assumed_price": 10.0, "notional_cny": 8000.0, "signal_score": 1.2},
			// 5000 / 12 = 416.7 → 整手 400 股
			map[string]any{"code": "000001.SZ", "target_weight": 0.05, "assumed_price": 12.0, "notional_cny": 5000.0, "signal_score": 0.8},
			// 1000 / 1500 = 0.67 → 不足一手，换算为 0 股并给告警
			map[string]any{"code": "000002.SZ", "target_weight": 0.10, "assumed_price": 1500.0, "notional_cny": 1000.0, "signal_score": 0.4},
		},
	}
	older := map[string]any{"period": 1, "holdings": []any{}}
	for name, doc := range map[string]map[string]any{"period_02.json": newest, "period_01.json": older} {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(periods, name), raw, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// newTestGateway 构造一个全部落点都在临时目录、且急停环境变量为空的网关，
// 避免测试写入 C:\quant\data 或受外部 A_STOCK_EXEC_KILLSWITCH 干扰。
func newTestGateway(t *testing.T, cfg GatewayConfig) *Gateway {
	t.Helper()
	t.Setenv("A_STOCK_EXEC_KILLSWITCH", "")
	dir := t.TempDir()
	if cfg.PaperDir == "" {
		cfg.PaperDir = writeCore2Sample(t)
	}
	if cfg.KillSwitchFile == "" {
		cfg.KillSwitchFile = filepath.Join(dir, "exec", "killswitch.on")
	}
	if cfg.AuditFile == "" {
		cfg.AuditFile = filepath.Join(dir, "exec", "audit.jsonl")
	}
	return NewGateway(cfg)
}

func auditTypeCounts(t *testing.T, gw *Gateway) map[string]int {
	t.Helper()
	records, err := gw.auditor.Read(0)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	counts := map[string]int{}
	for _, rec := range records {
		counts[rec.Type]++
	}
	return counts
}

// ---------------------------------------------------------------------------
// limits.go —— 整手换算与护栏
// ---------------------------------------------------------------------------

func TestRoundLotRoundsDownToWholeLots(t *testing.T) {
	cases := []struct {
		name     string
		shares   int
		lot      int
		expected int
	}{
		{"exact one lot", 100, 100, 100},
		{"two and a half lots", 250, 100, 200},
		{"below one lot", 99, 100, 0},
		{"negative shares become zero", -5, 100, 0},
		{"zero lot falls back to default", 250, 0, 200},
		{"negative lot falls back to default", 250, -10, 200},
		{"zero shares", 0, 100, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoundLot(tc.shares, tc.lot); got != tc.expected {
				t.Fatalf("RoundLot(%d, %d) = %d, want %d", tc.shares, tc.lot, got, tc.expected)
			}
		})
	}
}

func TestSharesForNotionalRequiresPricingAndWholeLots(t *testing.T) {
	cases := []struct {
		name     string
		notional float64
		price    float64
		lot      int
		expected int
	}{
		{"whole lots", 100000, 10, 100, 10000},
		{"rounds down to lot", 1500, 10, 100, 100},
		{"too small for one lot", 1000, 1500, 100, 0},
		{"zero notional", 0, 10, 100, 0},
		{"negative notional", -100, 10, 100, 0},
		{"zero price", 10000, 0, 100, 0},
		{"negative price", 10000, -3, 100, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SharesForNotional(tc.notional, tc.price, tc.lot); got != tc.expected {
				t.Fatalf("SharesForNotional(%v, %v, %d) = %d, want %d", tc.notional, tc.price, tc.lot, got, tc.expected)
			}
		})
	}
}

func TestValidateTargetWeightsFlagsSinglePositionBreach(t *testing.T) {
	targets := []PlanTarget{
		{Code: "600000.SH", TargetWeight: 0.10}, // 恰好上限，放行
		{Code: "600519.SH", TargetWeight: 0.15}, // 超过 10%，告警
	}
	warnings := ValidateTargetWeights(targets, DefaultMaxSingleWeight)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one for the breaching target", warnings)
	}
	if defaultMaxSingle(0) != DefaultMaxSingleWeight {
		t.Fatalf("defaultMaxSingle(0) = %v, want %v", defaultMaxSingle(0), DefaultMaxSingleWeight)
	}
	if got := ValidateTargetWeights(targets, 0.2); len(got) != 0 {
		t.Fatalf("custom max single should clear warnings, got %v", got)
	}
}

func TestValidateGrossWeightEnforcesFullInvestedCap(t *testing.T) {
	if msg := ValidateGrossWeight(1.0); msg != "" {
		t.Fatalf("gross=1.0 should pass, got %q", msg)
	}
	if msg := ValidateGrossWeight(0.9); msg != "" {
		t.Fatalf("gross=0.9 should pass, got %q", msg)
	}
	if msg := ValidateGrossWeight(1.05); msg == "" {
		t.Fatal("gross=1.05 must be rejected")
	}
}

func TestIsSTNameAndPrefilter(t *testing.T) {
	if !IsSTName("*ST海航") || !IsSTName("ST泰禾") {
		t.Fatal("ST names must be detected")
	}
	if IsSTName("贵州茅台") || IsSTName("600000.SH") {
		t.Fatal("non-ST names must not be flagged")
	}

	// 无行情（sim）：普通标的返回「待复核」提示，ST 字面命中则预拦截。
	if got := Prefilter("600000.SH", nil); got == "" {
		t.Fatal("a nil quote must still surface a review note rather than silently pass")
	}
	if got := Prefilter("*ST退市", nil); got == "" {
		t.Fatal("ST literal hit must produce a prefilter reason")
	}

	cases := []struct {
		name  string
		quote QuoteSnapshot
		block bool
	}{
		{"suspended", QuoteSnapshot{Code: "600000.SH", Suspended: true}, true},
		{"st flag", QuoteSnapshot{Code: "600000.SH", IsST: true}, true},
		{"st in name", QuoteSnapshot{Code: "600000.SH", Name: "ST测试"}, true},
		{"limit up", QuoteSnapshot{Code: "600000.SH", LimitUp: true}, true},
		{"limit down", QuoteSnapshot{Code: "600000.SH", LimitDown: true}, true},
		{"clean", QuoteSnapshot{Code: "600000.SH", Name: "浦发银行"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Prefilter(tc.quote.Code, &tc.quote)
			if tc.block && got == "" {
				t.Fatalf("expected prefilter to block %q", tc.name)
			}
			if !tc.block && got != "" {
				t.Fatalf("expected clean quote to pass, got %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// killswitch.go
// ---------------------------------------------------------------------------

func TestKillSwitchEngagesViaEnvironment(t *testing.T) {
	ks := NewKillSwitch("") // 无落点文件，仅看环境变量
	for _, engaged := range []string{"1", "true", "TRUE", "on", "yes", "engaged"} {
		t.Setenv("A_STOCK_EXEC_KILLSWITCH", engaged)
		if !ks.Engaged() {
			t.Fatalf("value %q must engage the kill switch", engaged)
		}
	}
	for _, idle := range []string{"", "0", "false", "no", "off"} {
		t.Setenv("A_STOCK_EXEC_KILLSWITCH", idle)
		if ks.Engaged() {
			t.Fatalf("value %q must not engage the kill switch", idle)
		}
	}
}

func TestKillSwitchEngagesViaFileAndIsIdempotent(t *testing.T) {
	t.Setenv("A_STOCK_EXEC_KILLSWITCH", "")
	file := filepath.Join(t.TempDir(), "nested", "killswitch.on")
	ks := NewKillSwitch(file)
	if ks.Engaged() {
		t.Fatal("switch must start disengaged")
	}
	if err := ks.Engage(); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if !ks.Engaged() {
		t.Fatal("switch must be engaged after writing the file")
	}
	if err := ks.Engage(); err != nil {
		t.Fatalf("re-engage must be idempotent: %v", err)
	}
	if err := ks.Disengage(); err != nil {
		t.Fatalf("disengage: %v", err)
	}
	if ks.Engaged() {
		t.Fatal("switch must be disengaged after removing the file")
	}
	if err := ks.Disengage(); err != nil {
		t.Fatalf("re-disengage must be idempotent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// audit.go —— append-only JSONL
// ---------------------------------------------------------------------------

func TestAuditorAppendsAndReadsAppendOnlyJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "audit.jsonl")
	auditor := NewAuditor(path)

	// 文件缺失时读取返回空切片而非错误。
	records, err := auditor.Read(0)
	if err != nil || len(records) != 0 {
		t.Fatalf("read on missing file: records=%v err=%v", records, err)
	}
	if count, err := auditor.Count(); err != nil || count != 0 {
		t.Fatalf("count on missing file: count=%d err=%v", count, err)
	}

	for i := 0; i < 3; i++ {
		if err := auditor.Append(AuditRecord{Type: "fill_recorded", Mode: "sim", FillID: string(rune('a' + i))}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if count, err := auditor.Count(); err != nil || count != 3 {
		t.Fatalf("count = %d err=%v, want 3", count, err)
	}
	// limit 只取最近 N 条。
	tail, err := auditor.Read(2)
	if err != nil || len(tail) != 2 || tail[1].FillID != "c" {
		t.Fatalf("read(2) = %+v err=%v, want the last two records", tail, err)
	}
	if tail[0].TS == "" {
		t.Fatal("Append must auto-stamp a timestamp")
	}
}

func TestAuditorWithEmptyPathIsNoop(t *testing.T) {
	auditor := NewAuditor("")
	if err := auditor.Append(AuditRecord{Type: "target_generated"}); err != nil {
		t.Fatalf("noop append: %v", err)
	}
	records, err := auditor.Read(0)
	if err != nil || len(records) != 0 {
		t.Fatalf("noop read: records=%v err=%v", records, err)
	}
	if count, err := auditor.Count(); err != nil || count != 0 {
		t.Fatalf("noop count: count=%d err=%v", count, err)
	}
}

// ---------------------------------------------------------------------------
// gateway.go —— 模式、白名单、目标生成、意图受理
// ---------------------------------------------------------------------------

// TestPITWhitelistOnlyAllowsCore2 锁死硬约束：PIT 审计通过前，只有 core2 能进执行通道。
func TestPITWhitelistOnlyAllowsCore2(t *testing.T) {
	if len(PITWhitelist) != 1 || !PITWhitelist["core2"] {
		t.Fatalf("PIT whitelist must contain exactly core2, got %v", PITWhitelist)
	}
}

func TestGatewayModeRequiresProductionKillswitchAndAdapter(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{ProductionEnabled: true})
	if gw.Mode() != "sim" {
		t.Fatalf("adapter not wired must keep mode=sim, got %q", gw.Mode())
	}
	gw.SetAdapter("qmt")
	if gw.Mode() != "live" {
		t.Fatalf("production + adapter ready must yield live, got %q", gw.Mode())
	}
	if err := gw.EngageKillSwitch(); err != nil {
		t.Fatalf("engage kill switch: %v", err)
	}
	if gw.Mode() != "sim" {
		t.Fatalf("engaged kill switch must force sim, got %q", gw.Mode())
	}
	if err := gw.DisengageKillSwitch(); err != nil {
		t.Fatalf("disengage kill switch: %v", err)
	}
	if gw.Mode() != "live" {
		t.Fatalf("disengaging must restore live, got %q", gw.Mode())
	}
}

func TestGatewayStatusReportsGuardsAndLatestSignal(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	status, err := gw.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Mode != "sim" || status.ProductionEnabled || status.KillSwitchEngaged {
		t.Fatalf("unexpected guard state: %+v", status)
	}
	if len(status.PITWhitelist) != 1 || status.PITWhitelist[0] != "core2" || status.StrategyAllowed != "core2" {
		t.Fatalf("whitelist must be [core2], got %+v", status.PITWhitelist)
	}
	if status.AdapterReady || status.Adapter != "" {
		t.Fatalf("adapter must be pending: %+v", status)
	}
	if status.LatestSignal == nil || status.LatestSignal.Period != float64(2) {
		t.Fatalf("latest signal must be period 2, got %+v", status.LatestSignal)
	}
	var adapterWarning, killWarning bool
	for _, w := range status.Warnings {
		if w == "券商适配层未就绪（P2 待实现）：live 执行不可用" {
			adapterWarning = true
		}
		if w == "急停开关已拉闸：拒绝一切下单动作" {
			killWarning = true
		}
	}
	if !adapterWarning {
		t.Fatalf("status must warn about the pending adapter: %v", status.Warnings)
	}
	if killWarning {
		t.Fatal("status must not warn about the kill switch while it is released")
	}
}

func TestGatewayStatusWarnsWhenSignalUnreadable(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{PaperDir: t.TempDir()}) // 空目录，读不到任何期
	status, err := gw.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.LatestSignal != nil {
		t.Fatalf("expected no latest signal, got %+v", status.LatestSignal)
	}
	found := false
	for _, w := range status.Warnings {
		if strings.HasPrefix(w, "无法读取最新信号：") {
			found = true
		}
	}
	if !found {
		t.Fatalf("status must surface an unreadable signal warning: %v", status.Warnings)
	}
}

func TestGatewayGenerateTargetBuildsWholeLotPlanFromCore2(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	plan, err := gw.GenerateTarget(context.Background(), GenerateTargetRequest{Strategy: "core2"})
	if err != nil {
		t.Fatalf("generate target: %v", err)
	}
	if !plan.PitAuditPassed || plan.Strategy != "core2" || plan.Mode != "sim" {
		t.Fatalf("plan header wrong: %+v", plan)
	}
	if plan.SignalID != "fp-test-0001" || plan.NHoldings != 3 {
		t.Fatalf("plan identity wrong: id=%q n=%d", plan.SignalID, plan.NHoldings)
	}
	if math.Abs(plan.GrossWeight-0.23) > 1e-9 {
		t.Fatalf("gross weight = %v, want 0.23", plan.GrossWeight)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("no guardrail warnings expected for this sample: %v", plan.Warnings)
	}
	expected := map[string]struct {
		shares int
		est    float64
	}{
		"600000.SH": {800, 8000},
		"000001.SZ": {400, 4800},
		"000002.SZ": {0, 0},
	}
	if len(plan.Targets) != len(expected) {
		t.Fatalf("targets = %d, want %d", len(plan.Targets), len(expected))
	}
	for _, target := range plan.Targets {
		want, ok := expected[target.Code]
		if !ok {
			t.Fatalf("unexpected target %q", target.Code)
		}
		if target.Shares != want.shares {
			t.Fatalf("%s shares = %d, want %d", target.Code, target.Shares, want.shares)
		}
		if math.Abs(target.EstNotional-want.est) > 1e-6 {
			t.Fatalf("%s est notional = %v, want %v", target.Code, target.EstNotional, want.est)
		}
		if target.Action != "REBALANCE_TO_WEIGHT" {
			t.Fatalf("%s action = %q", target.Code, target.Action)
		}
		if target.Prefilter == "" {
			t.Fatalf("%s must carry a prefilter note in sim mode", target.Code)
		}
		if target.Code == "000002.SZ" && target.Warning == "" {
			t.Fatal("a zero-share target must be flagged with a warning")
		}
	}
	if counts := auditTypeCounts(t, gw); counts["target_generated"] != 1 {
		t.Fatalf("target generation must be audited once: %v", counts)
	}
}

func TestGatewayGenerateTargetRejectsNonWhitelistStrategy(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	for _, strategy := range []string{"resonance", "inflection", "regime"} {
		if _, err := gw.GenerateTarget(context.Background(), GenerateTargetRequest{Strategy: strategy}); err == nil {
			t.Fatalf("strategy %q must be rejected by the PIT whitelist", strategy)
		}
	}
}

func TestGatewayGenerateTargetDefaultsToCore2(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	plan, err := gw.GenerateTarget(context.Background(), GenerateTargetRequest{})
	if err != nil {
		t.Fatalf("default strategy must resolve to core2: %v", err)
	}
	if plan.Strategy != "core2" {
		t.Fatalf("default strategy = %q, want core2", plan.Strategy)
	}
}

func TestGatewayAcceptIntentSimulatesFillsAndAudits(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	intent := OrderIntent{
		SchemaVersion:  SchemaVersion,
		Strategy:       "core2",
		PitAuditPassed: true,
		SignalID:       "fp-test-0001",
		ExecuteOn:      "sim",
		Targets: []IntentTarget{
			{Code: "600000.SH", TargetWeight: 0.08, AssumedPrice: 10, Action: "BUY"},
			{Code: "000001.SZ", TargetWeight: 0.05, AssumedPrice: 12, Action: "BUY"},
		},
	}
	ack, err := gw.AcceptIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("accept intent: %v", err)
	}
	if !ack.Accepted || ack.Mode != "sim" || ack.Strategy != "core2" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	// 10 万假定账户：0.08*10w/10 = 800 股；0.05*10w/12 = 416.7 → 400 股。
	if len(ack.Fills) != 2 {
		t.Fatalf("fills = %d, want 2", len(ack.Fills))
	}
	shares := map[string]int{}
	for _, fill := range ack.Fills {
		shares[fill.Code] = fill.Shares
		if fill.Source != "sim" || fill.OrderID == "" || fill.FillID == "" {
			t.Fatalf("sim fill incomplete: %+v", fill)
		}
	}
	if shares["600000.SH"] != 800 || shares["000001.SZ"] != 400 {
		t.Fatalf("unexpected simulated share counts: %v", shares)
	}
	counts := auditTypeCounts(t, gw)
	if counts["intent_accepted"] != 1 || counts["fill_recorded"] != 2 {
		t.Fatalf("accept must audit 1 intent + 2 fills: %v", counts)
	}
}

func TestGatewayAcceptIntentRejectsInvalidContracts(t *testing.T) {
	valid := OrderIntent{
		SchemaVersion:  SchemaVersion,
		Strategy:       "core2",
		PitAuditPassed: true,
		ExecuteOn:      "sim",
		Targets:        []IntentTarget{{Code: "600000.SH", TargetWeight: 0.05, AssumedPrice: 10}},
	}
	cases := []struct {
		name   string
		mutate func(*OrderIntent)
	}{
		{"schema mismatch", func(i *OrderIntent) { i.SchemaVersion = "9.9.9" }},
		{"non-whitelist strategy", func(i *OrderIntent) { i.Strategy = "resonance" }},
		{"pit audit failed", func(i *OrderIntent) { i.PitAuditPassed = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw := newTestGateway(t, GatewayConfig{})
			intent := valid
			tc.mutate(&intent)
			if _, err := gw.AcceptIntent(context.Background(), intent); err == nil {
				t.Fatalf("%s must be rejected", tc.name)
			}
			if counts := auditTypeCounts(t, gw); counts["intent_accepted"] != 0 {
				t.Fatalf("%s must not be audited as accepted: %v", tc.name, counts)
			}
		})
	}
}

func TestGatewayAcceptIntentBlocksLiveUntilWired(t *testing.T) {
	live := OrderIntent{
		SchemaVersion:  SchemaVersion,
		Strategy:       "core2",
		PitAuditPassed: true,
		ExecuteOn:      "live",
		Targets:        []IntentTarget{{Code: "600000.SH", TargetWeight: 0.05, AssumedPrice: 10}},
	}

	// 未启用生产开关：拒绝。
	gw := newTestGateway(t, GatewayConfig{})
	if _, err := gw.AcceptIntent(context.Background(), live); err == nil {
		t.Fatal("live must be rejected while production is disabled")
	}

	// 已启用生产开关但适配层未接线：仍拒绝。
	gwWiredDisabled := newTestGateway(t, GatewayConfig{ProductionEnabled: true})
	if _, err := gwWiredDisabled.AcceptIntent(context.Background(), live); err == nil {
		t.Fatal("live must be rejected while the broker adapter is not ready")
	}

	// 生产开关 + 适配层就绪：P0+P1 阶段仍明确拒绝（绝不触达券商）。
	gwReady := newTestGateway(t, GatewayConfig{ProductionEnabled: true})
	gwReady.SetAdapter("qmt")
	if _, err := gwReady.AcceptIntent(context.Background(), live); err == nil {
		t.Fatal("P0+P1 must refuse live execution even with an adapter registered")
	}
}

func TestGatewayAcceptIntentRejectedWhenKillSwitchEngaged(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	if err := gw.EngageKillSwitch(); err != nil {
		t.Fatalf("engage: %v", err)
	}
	intent := OrderIntent{
		SchemaVersion:  SchemaVersion,
		Strategy:       "core2",
		PitAuditPassed: true,
		ExecuteOn:      "sim",
		Targets:        []IntentTarget{{Code: "600000.SH", TargetWeight: 0.05, AssumedPrice: 10}},
	}
	if _, err := gw.AcceptIntent(context.Background(), intent); err == nil {
		t.Fatal("an engaged kill switch must block every order, including sim")
	}
	counts := auditTypeCounts(t, gw)
	if counts["killswitch_engaged"] != 1 || counts["intent_accepted"] != 0 {
		t.Fatalf("kill switch action must be audited and the intent refused: %v", counts)
	}
}

func TestGatewayRecordFillWithoutIDAssignsOne(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	ack, err := gw.RecordFill(context.Background(), Fill{Code: "600000.SH", Side: "BUY", Price: 10, Shares: 800})
	if err != nil {
		t.Fatalf("record fill: %v", err)
	}
	if !ack.Recorded || ack.FillID == "" || ack.RecordedAt == "" {
		t.Fatalf("unexpected fill ack: %+v", ack)
	}
	if counts := auditTypeCounts(t, gw); counts["fill_recorded"] != 1 {
		t.Fatalf("fill must be audited once: %v", counts)
	}
}

func TestGatewayKillSwitchToggleIsAudited(t *testing.T) {
	gw := newTestGateway(t, GatewayConfig{})
	if err := gw.EngageKillSwitch(); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if err := gw.DisengageKillSwitch(); err != nil {
		t.Fatalf("disengage: %v", err)
	}
	counts := auditTypeCounts(t, gw)
	if counts["killswitch_engaged"] != 1 || counts["killswitch_disengaged"] != 1 {
		t.Fatalf("both toggle actions must be audited: %v", counts)
	}
}

func TestNewIDProducesUniquePrefixedIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newID("intent")
		if len(id) < len("intent_") || id[:len("intent_")] != "intent_" {
			t.Fatalf("id %q missing prefix", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = true
	}
}
