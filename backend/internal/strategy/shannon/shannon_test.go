package shannon

import (
	"math"
	"testing"
)

// 本文件是 Python core/test_rules.py 的 Go 同构移植。
// 合成数据生成器与断言口径一一对应，确保双端「同参同逻辑」。

// ---------------------------------------------------------------- 合成数据

// floorDiv 复刻 Python 的 //（向下取整，Go 的 / 是向零截断）。
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// pyMod 复刻 Python 的 %（结果符号跟随除数，恒非负）。
func pyMod(a, b int) int {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// makeUptrend 构造阶梯式上升趋势（与 Python make_uptrend 逐行对应）。
//
// end_phase 控制末根落在台阶内哪个相位：
//
//	0~11  上涨段（末根 5 日线上行）
//	12~17 回踩段
//	18~19 止跌反弹 → 默认，即「回踩完成、等待突破」的入场前置形态
//
// 台阶设计：每级抬升 8%，级内上涨 12% 后回撤 ~6%。
func makeUptrend(n, endPhase int) []Bar {
	bars := make([]Bar, 0, n)
	phase := endPhase
	for i := 0; i < n; i++ {
		var step int
		if i >= phase {
			step = floorDiv(i-phase, 20)
		} else {
			step = -floorDiv(phase-i+19, 20)
		}
		base := 10.0 * math.Pow(1.08, float64(step))
		ph := pyMod(i-phase, 20)

		var o, c, h, l float64
		switch {
		case ph < 12: // 上涨段：base → base×1.12
			o = base * (1 + 0.12*float64(ph)/12)
			c = o * 1.008
			h = c * 1.003
			l = o * 0.997
		case ph < 18: // 回踩段：回撤至 base×1.055
			top := base * 1.12
			o = top - (top-base*1.055)*float64(ph-12)/6
			c = o * 0.995
			l = c * 0.995
			h = o * 1.003
		default: // 止跌反弹
			o = base * 1.06 * (1 + 0.02*float64(ph-18))
			c = o * 1.008
			h = c * 1.003
			l = o * 0.997
		}
		bars = append(bars, Bar{
			Date:   20240000 + i,
			Open:   Round2(o),
			High:   Round2(h),
			Low:    Round2(l),
			Close:  Round2(c),
			Volume: 1e6,
		})
	}
	return bars
}

// makeDowntrend 以上涨段的镜像构造下降趋势（与 Python make_downtrend 一致）。
func makeDowntrend(n, endPhase int) []Bar {
	up := makeUptrend(n, endPhase)
	out := make([]Bar, 0, len(up))
	for _, b := range up {
		out = append(out, Bar{
			Date:   b.Date,
			Open:   Round2(40 - b.Open),
			High:   Round2(40 - b.Low),
			Low:    Round2(40 - b.High),
			Close:  Round2(40 - b.Close),
			Volume: b.Volume,
		})
	}
	return out
}

// ---------------------------------------------------------------- 断言工具

func check(t *testing.T, cond bool, label string) {
	t.Helper()
	if !cond {
		t.Errorf("✗ %s", label)
	} else {
		t.Logf("✓ %s", label)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// ---------------------------------------------------------------- 测试用例

func TestLayer1Trend(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 10)
	stUp := Layer1Trend(up, cfg).State
	check(t, stUp == "UPTREND", "上升合成序列 → UPTREND（实得 "+stUp+"）")

	dn := makeDowntrend(120, 10)
	stDn := Layer1Trend(dn, cfg).State
	check(t, stDn == "DOWNTREND", "下降合成序列 → DOWNTREND（实得 "+stDn+"）")
}

func TestLayer2Polarity(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 10)
	p := Layer2Polarity(up, cfg)
	check(t, p.Polarity == "RISING", "上升序列 5 日线上行（实得 "+p.Polarity+"）")
	check(t, p.AllowLong, "上升序列允许做多")
	check(t, !p.IsFilterBlocking, "上升序列不做多禁令为假")

	dn := makeDowntrend(120, 10)
	p2 := Layer2Polarity(dn, cfg)
	check(t, p2.IsFilterBlocking, "下降序列触发做多禁令（实得 "+p2.Polarity+"）")
}

func TestLayer4Light(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 10)
	tr := Layer1Trend(up, cfg)
	p := Layer2Polarity(up, cfg)
	light := Layer4Light(p, tr)
	check(t, light.Light == "GREEN" || light.Light == "YELLOW",
		"上升序列灯为 GREEN/YELLOW（实得 "+light.Light+"）")

	dn := makeDowntrend(120, 10)
	tr2 := Layer1Trend(dn, cfg)
	p2 := Layer2Polarity(dn, cfg)
	light2 := Layer4Light(p2, tr2)
	check(t, light2.Light == "RED" || light2.Light == "YELLOW",
		"下降序列灯为 RED/YELLOW（实得 "+light2.Light+"）")
	if light2.Light == "RED" {
		check(t, light2.Posture.Scale == 0.0, "红灯仓位缩放为 0")
	}
}

func TestLayer5Structure(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 18) // 回踩完成、等待突破
	tr := Layer1Trend(up, cfg)
	p := Layer2Polarity(up, cfg)
	light := Layer4Light(p, tr)
	e := Layer5Entry(up, tr, p, light, cfg)

	check(t, e.Stop != nil && e.PullLow != nil && e.Trigger != nil, "止损/回踩低点/触发位 均已产出")
	if e.Stop != nil && e.PullLow != nil && e.Trigger != nil {
		check(t, *e.Stop < *e.PullLow, "止损 < 回踩低点")
		check(t, *e.PullLow < *e.Trigger, "回踩低点 < 触发位")
		check(t, e.Risk != nil && *e.Risk > 0, "风险为正")
		check(t, e.RRRatio != nil && *e.RRRatio >= cfg.MinRR, "回踩完成形态 RR ≥ MinRR")
	}
	check(t, e.PulledBack, "识别为有效回踩")

	// 合成突破后应为 valid=True，且触发位不被抬高（自指陷阱回归）
	up2 := append([]Bar(nil), up...)
	if e.Trigger != nil {
		up2[len(up2)-1].Close = Round2(*e.Trigger * 1.005)
		up2[len(up2)-1].High = Round2(*e.Trigger * 1.008)
	}
	tr2 := Layer1Trend(up2, cfg)
	p2 := Layer2Polarity(up2, cfg)
	l2 := Layer4Light(p2, tr2)
	e2 := Layer5Entry(up2, tr2, p2, l2, cfg)
	if e.Trigger != nil && e2.Trigger != nil {
		check(t, near(*e2.Trigger, *e.Trigger), "突破后触发位不被抬高")
	}
	check(t, !e2.ChaseBlocked, "未超 2% 容忍度 → 不判追高")
	check(t, e2.Valid, "突破确认后 valid=True（reason="+e2.Reason+"）")
}

func TestLayer5RRNotLocked(t *testing.T) {
	cfg := DefaultConfig()
	var rrs []float64
	for _, n := range []int{80, 100, 120, 140, 160} {
		up := makeUptrend(n, 18)
		tr := Layer1Trend(up, cfg)
		p := Layer2Polarity(up, cfg)
		light := Layer4Light(p, tr)
		e := Layer5Entry(up, tr, p, light, cfg)
		if e.RRRatio != nil {
			rrs = append(rrs, *e.RRRatio)
		}
	}
	check(t, len(rrs) == 5, "5 个窗口均产出 RR")
	if len(rrs) > 0 {
		sum, mx, mn := 0.0, rrs[0], rrs[0]
		for _, v := range rrs {
			sum += v
			if v > mx {
				mx = v
			}
			if v < mn {
				mn = v
			}
		}
		m := sum / float64(len(rrs))
		check(t, m >= cfg.MinRR, "RR 均值 ≥ MinRR")
		check(t, !(m > 0.55 && m < 0.65), "RR 未被结构性锁死在 0.59")
		t.Logf("    RR 样本 = %v  spread=%.3f", rrs, mx-mn)
	}
}

func TestLayer5HardFilters(t *testing.T) {
	cfg := DefaultConfig()
	dn := makeDowntrend(120, 10)
	tr := Layer1Trend(dn, cfg)
	p := Layer2Polarity(dn, cfg)
	light := Layer4Light(p, tr)
	e := Layer5Entry(dn, tr, p, light, cfg)

	check(t, p.IsFilterBlocking, "下行 5 日线 → is_filter_blocking=True")
	check(t, e.Blocked, "L5 被硬阻断")
	check(t, !e.Valid, "被阻断时 valid=False")
	check(t, e.Trigger == nil && e.Stop == nil, "被阻断时不产出触发位/止损")
	check(t, contains(e.Reason, "5 日线") || contains(e.Reason, "红"),
		"阻断原因指向过滤器（"+e.Reason+"）")

	up := makeUptrend(120, 18)
	tr2 := Layer1Trend(up, cfg)
	p2 := Layer2Polarity(up, cfg)
	l2 := Layer4Light(p2, tr2)
	e2 := Layer5Entry(up, tr2, p2, l2, cfg)
	check(t, !e2.Blocked, "上升序列不被硬阻断")
	check(t, e2.Trigger != nil, "上升序列正常产出触发位")

	dn2 := makeDowntrend(120, 10)
	tr3 := Layer1Trend(dn2, cfg)
	p3 := Layer2Polarity(dn2, cfg)
	l3 := Layer4Light(p3, tr3)
	if l3.Light == "RED" {
		e3 := Layer5Entry(dn2, tr3, p3, l3, cfg)
		check(t, e3.Blocked, "红灯 → L5 被阻断")
		check(t, e3.Trigger == nil, "红灯不产出触发位")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLayer6Sizing(t *testing.T) {
	cfg := DefaultConfig()
	full := Light{Posture: Posture{Scale: 1.0}}
	align := Alignment{PositionScale: 1.0}
	polarity := Polarity{Polarity: "RISING"}
	_ = polarity

	entry := Entry{Trigger: fptr(11.0), Stop: fptr(10.0)} // 每股风险 1 元
	s := Layer6Sizing(entry, align, full, cfg, 100000.0, 1.0)
	check(t, s.RawShares == 1000, "原始股数 1000")
	check(t, s.TotalShares%cfg.BoardLot == 0, "股数为整手倍数")
	sumTr := 0
	for _, x := range s.Tranches {
		sumTr += x
	}
	check(t, sumTr == s.TotalShares, "分批之和等于总股数")

	// 缩放后不足半手 → 无法建仓
	s2 := Layer6Sizing(entry, Alignment{PositionScale: 0.0}, full, cfg, 100000.0, 1.0)
	check(t, s2.TotalShares == 0, "缩放为 0 → 不建仓")

	// 高价股小额账户 → 不足一手（半手边界进位）
	entry3 := Entry{Trigger: fptr(300.0), Stop: fptr(280.0)}
	s3 := Layer6Sizing(entry3, align, full, cfg, 100000.0, 1.0)
	check(t, s3.RawShares == 50, "原始股数 50")

	// ---- 百分比口径 ----
	check(t, near(s.PositionPct, 11.0), "仓位百分比 11%")
	check(t, near(s.RiskPctActual, 1.0), "实际风险 1%")
	check(t, s.BindingConstraint == "risk", "生效约束应为风险预算")
	check(t, len(s.TranchePcts) == len(s.Tranches), "分批百分比与分批股数等长")

	// 止损极窄 → 风险口径股数远超单笔仓位上限，应由 position_cap 接管
	entry4 := Entry{Trigger: fptr(100.0), Stop: fptr(99.5)}
	s4 := Layer6Sizing(entry4, align, full, cfg, 100000.0, 1.0)
	check(t, s4.BindingConstraint == "position_cap", "极窄止损应由仓位上限接管")
	check(t, s4.PositionPct <= cfg.MaxPositionPct+1e-9, "仓位不得超过单笔上限")
	check(t, s4.CapShares != nil && *s4.CapShares == 200, "上限口径股数 200")
}

func fptr(v float64) *float64 { return &v }

func TestLayer3Alignment(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 18)
	dn := makeDowntrend(120, 10)

	a := Layer3Alignment(up, up, "UPTREND", cfg)
	check(t, a.Alignment == "ALIGNED_BULL", "三周期同多 → ALIGNED_BULL（实得 "+a.Alignment+"）")
	check(t, a.PositionScale == 1.0, "ALIGNED_BULL 缩放 1.0")

	a2 := Layer3Alignment(up, dn, "UPTREND", cfg)
	check(t, a2.Alignment == "CONFLICT", "月多/周空 → CONFLICT（实得 "+a2.Alignment+"）")
	check(t, a2.PositionScale == 0.0, "CONFLICT 缩放 0")

	a3 := Layer3Alignment(up, up, "RANGE", cfg)
	check(t, a3.Alignment == "BULL_LEAN" || a3.Alignment == "NEUTRAL",
		"多/多/区间 → BULL_LEAN 或 NEUTRAL（实得 "+a3.Alignment+"）")
}

func TestAnalyzeIntegrity(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 18)
	r := Analyze("TEST", up, up, up, cfg, 100000.0)
	check(t, r.Code == "TEST", "输出含 code")
	check(t, r.Trend.State != "", "trend 非空")
	check(t, r.Polarity.Polarity != "", "polarity 非空")
	check(t, r.Alignment.Alignment != "", "alignment 非空")
	check(t, r.Light.Light != "", "light 非空")
	check(t, r.Entry.Targets != nil, "entry.targets 已初始化")
	check(t, r.Sizing.BindingConstraint != "", "sizing.binding_constraint 非空")
	check(t, len(r.Sources) > 0, "溯源编号非空")
	check(t, r.Sizing.PositionCapPct == cfg.MaxPositionPct, "单笔上限透传")
	check(t, r.Bucket != "", "bucket 已归类（实得 "+r.Bucket+"）")
}

// ---------------------------------------------------------------- Go 侧扩展用例

func TestSMAAndSwings(t *testing.T) {
	// SMA：前 n-1 位为 NaN，第 n 位起为均值
	got := SMA([]float64{1, 2, 3, 4, 5, 6}, 5)
	check(t, math.IsNaN(got[3]), "SMA 前 n-1 位为 NaN")
	check(t, near(got[4], 3.0), "SMA 第 5 位 = (1+2+3+4+5)/5 = 3")
	check(t, near(got[5], 4.0), "SMA 第 6 位 = (2+3+4+5+6)/5 = 4")

	// 遇 NaN 清空窗口
	got2 := SMA([]float64{1, 2, math.NaN(), 4, 5, 6, 7, 8, 9, 10}, 3)
	check(t, math.IsNaN(got2[4]), "SMA 窗口遇 NaN 清空")
	check(t, near(got2[9], 9.0), "SMA 重建窗口后 = (8+9+10)/3 = 9")

	// FindSwings：双峰序列，k=2 时应在 idx=3 与 idx=9 各识别一个摆动高点
	highs := []float64{1, 2, 3, 9, 3, 2, 1, 2, 3, 4, 3, 2, 1}
	lows := []float64{0.5, 1, 2, 8, 2, 1, 0.5, 1, 2, 3, 2, 1, 0.5}
	swH, swL := FindSwings(highs, lows, 2)
	check(t, len(swH) == 2 && swH[0].Idx == 3 && swH[1].Idx == 9,
		"摆动高点定位在 idx=3 与 idx=9")
	check(t, len(swL) == 1 && swL[0].Idx == 6, "摆动低点定位在 idx=6")
	check(t, swH[0].Price == 9 && swH[1].Price == 4, "摆动高点价格正确")

	// 长度不足 → 空
	swH2, swL2 := FindSwings([]float64{1, 2}, []float64{1, 2}, 5)
	check(t, len(swH2) == 0 && len(swL2) == 0, "样本不足 → 无摆动点")
}

func TestRoundParity(t *testing.T) {
	// 与 Python round 同语义（二进制精确值 round-half-even）
	check(t, near(Round2(2.675), 2.67), "Round2(2.675) = 2.67（非 math.Round 的 2.68）")
	check(t, near(Round2(2.685), 2.69), "Round2(2.685) = 2.69")
	check(t, near(Round4(0.12345), 0.1235), "Round4(0.12345) = 0.1235")
	check(t, math.IsNaN(Round2(math.NaN())), "Round2(NaN) 保持 NaN")
}

func TestBoardOf(t *testing.T) {
	cases := map[string]string{
		"600519": "沪主板",
		"688001": "科创板",
		"300750": "创业板",
		"000001": "深主板",
		"002415": "中小板",
		"830001": "北交所",
		"123456": "其他",
	}
	for code, want := range cases {
		if got := BoardOf(code); got != want {
			t.Errorf("✗ BoardOf(%s) = %s，期望 %s", code, got, want)
		} else {
			t.Logf("✓ BoardOf(%s) = %s", code, got)
		}
	}
}

func TestBucketOf(t *testing.T) {
	base := func() Result {
		return Result{
			Light:     Light{Light: "GREEN", Posture: Posture{Scale: 1.0}},
			Alignment: Alignment{PositionScale: 1.0},
		}
	}
	r1 := base()
	r1.Entry = Entry{Valid: true}
	check(t, BucketOf(r1) == "ENTRY", "valid + 满仓系数 → ENTRY")

	r2 := base()
	r2.Entry = Entry{Valid: true}
	r2.Alignment.PositionScale = 0.0
	check(t, BucketOf(r2) == "SIZE_ZERO", "valid 但对齐系数为 0 → SIZE_ZERO")

	r3 := base()
	r3.Entry = Entry{Valid: false, AwaitingBreakout: true, PulledBack: true}
	check(t, BucketOf(r3) == "WATCH", "回踩完成等待突破 → WATCH")

	r4 := base()
	r4.Entry = Entry{Valid: false, ChaseBlocked: true}
	check(t, BucketOf(r4) == "CHASE", "追高 → CHASE（blocked 由 chase 置位时同 Python 落 L2_BLOCK）")

	r5 := base()
	r5.Light.Light = "RED"
	r5.Entry = Entry{Blocked: true}
	check(t, BucketOf(r5) == "RED", "红灯 → RED")

	r6 := base()
	r6.Light.Light = "YELLOW"
	r6.Entry = Entry{Blocked: true}
	check(t, BucketOf(r6) == "L2_BLOCK", "黄灯但 L2 硬阻断 → L2_BLOCK")

	r7 := base()
	r7.Entry = Entry{Valid: false, StructureOK: false}
	check(t, BucketOf(r7) == "NO_STRUCT", "无结构 → NO_STRUCT")

	r8 := base()
	r8.Entry = Entry{Valid: false, StructureOK: true, RRRatio: fptr(1.0)}
	check(t, BucketOf(r8) == "LOW_RR", "RR 不足 → LOW_RR")
}

func TestPortfolioPlan(t *testing.T) {
	cfg := DefaultConfig()
	// 构造 6 只 ENTRY 候选（每只仓位 10%、风险 1%），上限 5 只 / 风险 5%
	var rs []Result
	for i := 0; i < 6; i++ {
		r := Result{
			Code:      "C" + itoa(i),
			Score:     float64(100 - i),
			Bucket:    "ENTRY",
			Alignment: Alignment{PositionScale: 1.0},
			Light:     Light{Light: "GREEN", Posture: Posture{Scale: 1.0}},
			Sizing: Sizing{
				PositionPct:   10.0,
				RiskPctActual: 1.0,
				Notional:      10000.0,
				TotalShares:   1000,
			},
		}
		rs = append(rs, r)
	}
	plan := PortfolioPlanOf(rs, cfg, 100000.0)
	check(t, plan.Candidates == 6, "候选 6 只")
	check(t, len(plan.Picked) == cfg.MaxOpenPositions, "只取前 5 只（持仓数上限）")
	check(t, near(plan.TotalPositionPct, 50.0), "合计仓位 50%")
	check(t, near(plan.TotalRiskPct, 5.0), "合计风险 5%")
	check(t, near(plan.CashPct, 50.0), "现金 50%")
	check(t, plan.StoppedBy != "", "给出了停止加仓原因")

	// 风险上限先触发：每只风险 3% → 第 2 只就超限
	var rs2 []Result
	for i := 0; i < 4; i++ {
		rs2 = append(rs2, Result{
			Code:   "D" + itoa(i),
			Score:  float64(100 - i),
			Bucket: "ENTRY",
			Sizing: Sizing{PositionPct: 10.0, RiskPctActual: 3.0, Notional: 10000.0},
		})
	}
	plan2 := PortfolioPlanOf(rs2, cfg, 100000.0)
	check(t, len(plan2.Picked) == 1, "风险上限先触发 → 只建 1 只")
	check(t, contains(plan2.StoppedBy, "风险"), "停止原因是风险上限")
}

func TestMergeSourcesAndBucketLabel(t *testing.T) {
	got := mergeSources([]string{"VIDEO-003", "VIDEO-274"}, []string{"VIDEO-274", "VIDEO-018"})
	check(t, len(got) == 3, "溯源去重后 3 条")
	check(t, got[0] == "VIDEO-003" && got[1] == "VIDEO-018" && got[2] == "VIDEO-274", "溯源按字典序排序")

	for _, b := range BucketOrder {
		_, ok := BucketLabel[b]
		check(t, ok, "分组 "+b+" 有中文标签")
	}
}

// TestEndToEndUptrend 端到端：合成上升序列应走到「可入场」而非被误杀。
func TestEndToEndUptrend(t *testing.T) {
	cfg := DefaultConfig()
	up := makeUptrend(120, 18)
	// 手动制造突破：末根收在触发位上方（在触发位计算出来之后）
	tr := Layer1Trend(up, cfg)
	p := Layer2Polarity(up, cfg)
	l := Layer4Light(p, tr)
	e0 := Layer5Entry(up, tr, p, l, cfg)
	if e0.Trigger == nil {
		t.Fatalf("✗ 合成上升序列未产出触发位（reason=%s）", e0.Reason)
	}
	up[len(up)-1].Close = Round2(*e0.Trigger * 1.005)
	up[len(up)-1].High = Round2(*e0.Trigger * 1.008)

	r := Analyze("TEST", up, up, up, cfg, 100000.0)
	check(t, r.Bucket == "ENTRY", "突破后归组 ENTRY（实得 "+r.Bucket+"）")
	check(t, r.Sizing.TotalShares > 0, "建仓股数 > 0")
	check(t, r.Sizing.PositionPct > 0 && r.Sizing.PositionPct <= cfg.MaxPositionPct,
		"仓位百分比落在 (0, 20%]")
	check(t, r.Sizing.RiskPctActual <= cfg.RiskPct+1e-9, "单笔风险不超过 1%")
}
