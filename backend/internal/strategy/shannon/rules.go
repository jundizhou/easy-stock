package shannon

import "math"

// ---- 输出结构（字段名与 Python core/rules.py 完全一致，便于双端对拍）----

// Trend 是 L1 趋势结构结果。
type Trend struct {
	State       string    `json:"state"`
	HigherHighs bool      `json:"higher_highs"`
	HigherLows  bool      `json:"higher_lows"`
	LowerHighs  bool      `json:"lower_highs"`
	LowerLows   bool      `json:"lower_lows"`
	RecentHighs []float64 `json:"recent_highs"`
	RecentLows  []float64 `json:"recent_lows"`
	// Python 侧字段名虽叫 *_idx，实际是 find_swings 的元组 (idx, price) 切片，
	// 这里保持同构以便对拍。
	SwingHighIdx  [][2]float64 `json:"swing_highs_idx"`
	SwingLowIdx   [][2]float64 `json:"swing_lows_idx"`
	LastSwingLow  *float64     `json:"last_swing_low"`
	LastSwingHigh *float64     `json:"last_swing_high"`
	Sources       []string     `json:"sources"`
}

// Polarity 是 L2 5 日线极性结果。
type Polarity struct {
	Polarity         string   `json:"polarity"`
	MA5              *float64 `json:"ma5"`
	MA5Prev          *float64 `json:"ma5_prev"`
	SlopePct         *float64 `json:"slope_pct"`
	Price            *float64 `json:"price"`
	PriceAboveMA5    bool     `json:"price_above_ma5"`
	AllowLong        bool     `json:"allow_long"`
	AllowShort       bool     `json:"allow_short"`
	IsFilterBlocking bool     `json:"is_filter_blocking"`
	Sources          []string `json:"sources"`
}

// Alignment 是 L3 多周期对齐结果。
type Alignment struct {
	Alignment     string   `json:"alignment"`
	Score         int      `json:"score"`
	Monthly       string   `json:"monthly"`
	Weekly        string   `json:"weekly"`
	Daily         string   `json:"daily"`
	Ups           int      `json:"ups"`
	Downs         int      `json:"downs"`
	AllowFullSize bool     `json:"allow_full_size"`
	PositionScale float64  `json:"position_scale"`
	Sources       []string `json:"sources"`
}

// Posture 是红绿灯对应的应对与仓位系数。
type Posture struct {
	Action string  `json:"action"`
	MaxNew bool    `json:"max_new"`
	Scale  float64 `json:"scale"`
}

// Light 是 L4 红绿灯结果。
type Light struct {
	Light        string   `json:"light"`
	Reasons      []string `json:"reasons"`
	Posture      Posture  `json:"posture"`
	ReclaimLevel *float64 `json:"reclaim_level"`
	Sources      []string `json:"sources"`
}

// Entry 是 L5 三步入场模板结果。
type Entry struct {
	Valid            bool      `json:"valid"`
	Blocked          bool      `json:"blocked"`
	Reason           string    `json:"reason,omitempty"`
	Support          *float64  `json:"support"`
	PullLow          *float64  `json:"pull_low"`
	Trigger          *float64  `json:"trigger"`
	Stop             *float64  `json:"stop"`
	Targets          []float64 `json:"targets"`
	Risk             *float64  `json:"risk"`
	Reward           *float64  `json:"reward"`
	RRRatio          *float64  `json:"rr_ratio"`
	StructureOK      bool      `json:"structure_ok"`
	ChaseBlocked     bool      `json:"chase_blocked"`
	AwaitingBreakout bool      `json:"awaiting_breakout"`
	PulledBack       bool      `json:"pulled_back"`
	PullbackPct      *float64  `json:"pullback_pct"`
	Steps            []string  `json:"steps"`
	Prohibitions     []string  `json:"prohibitions"`
	Sources          []string  `json:"sources"`
}

// Sizing 是 L6 仓位与止损结果（仓位以占净值 % 为准）。
type Sizing struct {
	Equity              float64   `json:"equity"`
	RiskPct             float64   `json:"risk_pct"`
	RiskAmount          float64   `json:"risk_amount"`
	PerShareRisk        *float64  `json:"per_share_risk"`
	RawShares           int       `json:"raw_shares"`
	CapShares           *int      `json:"cap_shares"`
	BindingConstraint   string    `json:"binding_constraint"`
	TotalShares         int       `json:"total_shares"`
	BoardLot            int       `json:"board_lot"`
	Notional            float64   `json:"notional"`
	PositionPct         float64   `json:"position_pct"`
	RiskPctActual       float64   `json:"risk_pct_actual"`
	RawPositionPct      float64   `json:"raw_position_pct"`
	PositionCapPct      float64   `json:"position_cap_pct"`
	InsufficientCapital bool      `json:"insufficient_capital"`
	Tranches            []int     `json:"tranches"`
	TranchePcts         []float64 `json:"tranche_pcts"`
	ScaleApplied        float64   `json:"scale_applied"`
	StopRules           []string  `json:"stop_rules"`
	PartialExit         []string  `json:"partial_exit"`
	Sources             []string  `json:"sources"`
}

// Result 是单只标的的六层综合结果。
type Result struct {
	Code          string    `json:"code"`
	Name          string    `json:"name,omitempty"`
	AsOf          int       `json:"as_of"`
	Close         *float64  `json:"close"`
	Score         float64   `json:"score"`
	Trend         Trend     `json:"trend"`
	Polarity      Polarity  `json:"polarity"`
	Alignment     Alignment `json:"alignment"`
	Light         Light     `json:"light"`
	Entry         Entry     `json:"entry"`
	Sizing        Sizing    `json:"sizing"`
	Sources       []string  `json:"sources"`
	NeedsBacktest bool      `json:"needs_backtest"`
	Bucket        string    `json:"bucket,omitempty"`
}

// ============================================================ L1 趋势结构

// Layer1Trend 判定趋势结构（HH+HL / LH+LL）。
func Layer1Trend(bars []Bar, cfg Config) Trend {
	highs := HighsOf(bars)
	lows := LowsOf(bars)
	swH, swL := FindSwings(highs, lows, cfg.SwingK)
	st := ClassifyStructure(swH, swL, cfg.SwingN)

	t := Trend{
		State:        st.State,
		HigherHighs:  st.HigherHighs,
		HigherLows:   st.HigherLows,
		LowerHighs:   st.LowerHighs,
		LowerLows:    st.LowerLows,
		RecentHighs:  st.RecentHighs,
		RecentLows:   st.RecentLows,
		SwingHighIdx: tailPairs(swH, 5),
		SwingLowIdx:  tailPairs(swL, 5),
		Sources:      []string{"VIDEO-003", "VIDEO-054", "VIDEO-964", "VIDEO-226", "VIDEO-464", "VIDEO-271", "VIDEO-286", "VIDEO-1687"},
	}
	if len(swL) > 0 {
		v := swL[len(swL)-1].Price
		t.LastSwingLow = &v
	}
	if len(swH) > 0 {
		v := swH[len(swH)-1].Price
		t.LastSwingHigh = &v
	}
	return t
}

// tailPairs 取最近 n 个摆动点的 (idx, price) 对，与 Python sw_h[-5:] 同构。
func tailPairs(points []SwingPoint, n int) [][2]float64 {
	if len(points) == 0 {
		return nil
	}
	from := len(points) - n
	if from < 0 {
		from = 0
	}
	out := make([][2]float64, 0, len(points)-from)
	for _, p := range points[from:] {
		out = append(out, [2]float64{float64(p.Idx), p.Price})
	}
	return out
}

// ============================================================ L2 5 日线极性

// Layer2Polarity 判定 5 日线极性（核心过滤器）。
// 必须 SMA（VIDEO-094）；斜率优先于位置（VIDEO-475）。
func Layer2Polarity(bars []Bar, cfg Config) Polarity {
	closes := ClosesOf(bars)
	ma5 := SMA(closes, cfg.MA5Period)

	p := Polarity{
		Polarity: "UNKNOWN",
		Sources:  []string{"VIDEO-274", "VIDEO-506", "VIDEO-810", "INTERVIEW-001", "VIDEO-094", "VIDEO-475", "VIDEO-386"},
	}

	var cur, prev *float64
	if len(ma5) > 0 && !math.IsNaN(ma5[len(ma5)-1]) {
		v := ma5[len(ma5)-1]
		cur = &v
	}
	if len(ma5) >= 2 && !math.IsNaN(ma5[len(ma5)-2]) {
		v := ma5[len(ma5)-2]
		prev = &v
	}
	p.MA5 = cur
	p.MA5Prev = prev

	if cur != nil && prev != nil {
		denom := math.Abs(*prev)
		slope := 0.0
		if denom != 0 {
			slope = (*cur - *prev) / denom * 100.0
		}
		rounded := Round4(slope)
		p.SlopePct = &rounded
		switch {
		case slope > cfg.FlatEpsPct:
			p.Polarity = "RISING"
		case slope < -cfg.FlatEpsPct:
			p.Polarity = "DECLINING"
		default:
			p.Polarity = "FLAT"
		}
	}

	if len(closes) > 0 {
		v := closes[len(closes)-1]
		p.Price = &v
	}
	p.PriceAboveMA5 = p.Price != nil && cur != nil && *p.Price > *cur
	p.AllowLong = p.Polarity == "RISING" || p.Polarity == "FLAT"
	p.AllowShort = p.Polarity == "DECLINING" || p.Polarity == "FLAT"
	p.IsFilterBlocking = p.Polarity == "DECLINING"
	return p
}

// ============================================================ L3 多周期对齐

func periodState(bars []Bar, cfg Config) string {
	if len(bars) < 2*cfg.SwingK+3 {
		return "UNKNOWN"
	}
	k := cfg.SwingK
	if maxV := len(bars) / 6; maxV < k {
		k = maxV
	}
	if k < 1 {
		k = 1
	}
	swH, swL := FindSwings(HighsOf(bars), LowsOf(bars), k)
	st := ClassifyStructure(swH, swL, 2).State
	// 周期级简化：结构性判为 RANGE 时，用 MA 方向兜底（避免长周期常年判区间）。
	if st == "RANGE" {
		n := len(bars) / 3
		if n < 3 {
			n = 3
		}
		if n > 20 {
			n = 20
		}
		ma := SMA(ClosesOf(bars), n)
		if len(ma) >= 2 && !math.IsNaN(ma[len(ma)-1]) && !math.IsNaN(ma[len(ma)-2]) {
			if ma[len(ma)-1] > ma[len(ma)-2] {
				st = "UPTREND"
			} else if ma[len(ma)-1] < ma[len(ma)-2] {
				st = "DOWNTREND"
			}
		}
	}
	return st
}

// Layer3Alignment 判定多周期对齐与仓位系数。
func Layer3Alignment(monthly, weekly []Bar, dailyState string, cfg Config) Alignment {
	mState := periodState(monthly, cfg)
	wState := periodState(weekly, cfg)
	dState := dailyState

	ups, downs := 0, 0
	for _, s := range []string{mState, wState, dState} {
		switch s {
		case "UPTREND":
			ups++
		case "DOWNTREND":
			downs++
		}
	}

	align := "NEUTRAL"
	switch {
	case ups == 3:
		align = "ALIGNED_BULL"
	case downs == 3:
		align = "ALIGNED_BEAR"
	case ups >= 2 && downs == 0:
		align = "BULL_LEAN"
	case downs >= 2 && ups == 0:
		align = "BEAR_LEAN"
	case ups >= 1 && downs >= 1:
		align = "CONFLICT"
	}

	return Alignment{
		Alignment:     align,
		Score:         ups - downs,
		Monthly:       mState,
		Weekly:        wState,
		Daily:         dState,
		Ups:           ups,
		Downs:         downs,
		AllowFullSize: align == "ALIGNED_BULL" || align == "BULL_LEAN",
		PositionScale: SizeScale[align],
		Sources:       []string{"VIDEO-018", "VIDEO-968", "ARTICLE-100", "VIDEO-680", "VIDEO-1983"},
	}
}

// ============================================================ L4 红绿灯

// Layer4Light 判定红绿灯风控总闸。
func Layer4Light(polarity Polarity, trend Trend) Light {
	var reasons []string
	pol := polarity.Polarity
	above := polarity.PriceAboveMA5
	st := trend.State

	light := "YELLOW"
	switch {
	case pol == "RISING" && above && st == "UPTREND":
		light = "GREEN"
		reasons = append(reasons, "5日线上行且价在其上，结构 HH+HL 完整")
	case pol == "DECLINING" && !above && st == "DOWNTREND":
		light = "RED"
		reasons = append(reasons, "5日线下行且价在其下，结构 LH+LL 确认")
	case pol == "RISING" && !above:
		reasons = append(reasons, "价格跌破上行 5 日线 —— just a little caution")
	case pol == "DECLINING" && above:
		reasons = append(reasons, "价格回到下行 5 日线上方，但均线方向未改")
	default:
		pos := "在"
		if !above {
			pos = "不在"
		}
		reasons = append(reasons, "混合状态（5日线 "+pol+"、结构 "+st+"、价"+pos+"线上）")
	}

	return Light{
		Light:   light,
		Reasons: reasons,
		Posture: Posture{
			Action: LightAction[light],
			MaxNew: light == "GREEN",
			Scale:  LightScale[light],
		},
		ReclaimLevel: polarity.MA5,
		Sources:      []string{"VIDEO-746", "VIDEO-744", "VIDEO-386"},
	}
}

// ============================================================ L5 入场模板

var (
	entrySteps            = []string{"①回踩支撑", "②在回踩前高点上方买强度", "③止损放回踩低点下方"}
	entryProhibits        = []string{"禁止追高(chase)", "禁止提前买(cheat buy)", "禁止买 dip 本身"}
	entryProhibitsBlocked = []string{"禁止追高(chase)", "禁止提前买(cheat buy)", "禁止买 dip 本身", "禁止在下行 5 日线下做多"}
)

func blockedEntry(reason string, sources, prohibits []string) Entry {
	return Entry{
		Valid:        false,
		Blocked:      true,
		Reason:       reason,
		Targets:      []float64{},
		StructureOK:  false,
		Steps:        entrySteps,
		Prohibitions: prohibits,
		Sources:      sources,
	}
}

// Layer5Entry 三步入场模板。
//
// 两个易错点（与 Python 同口径，均有回归测试）：
//  1. 突破位必须用 highs[:len-1] 计算（排除当前 K 线），否则今日新高会立刻
//     成为突破位本身 → 触发位自我抬升、永远无法被突破（自指陷阱）。
//  2. 止损挂回踩低点（相关更高低点）下方，不是更早的深摆动低点。
func Layer5Entry(bars []Bar, trend Trend, polarity Polarity, light Light, cfg Config) Entry {
	// 硬过滤器 1：红灯
	if light.Light == "RED" {
		return blockedEntry("红灯状态，禁止做多",
			[]string{"VIDEO-746", "VIDEO-1331", "VIDEO-386"}, entryProhibitsBlocked)
	}
	// 硬过滤器 2：L2 下行 5 日线（核心过滤器，独立于红绿灯）
	if polarity.IsFilterBlocking {
		return blockedEntry("5 日线下行 → 按纪律禁止做多（核心过滤器）",
			[]string{"VIDEO-386", "VIDEO-274", "VIDEO-506", "VIDEO-094"}, entryProhibitsBlocked)
	}

	if len(bars) == 0 {
		return blockedEntry("无行情数据", []string{}, entryProhibits)
	}

	closes := ClosesOf(bars)
	highs := HighsOf(bars)
	lows := LowsOf(bars)
	price := closes[len(closes)-1]

	// ① 突破位 = 回踩前的摆动高点（排除当前 K 线）
	lookback := cfg.BreakoutLookback
	if len(highs)-1 < lookback {
		lookback = len(highs) - 1
	}
	if lookback < 0 {
		lookback = 0
	}
	var breakoutLevel *float64
	boPos := -1
	if lookback > 0 {
		from := len(highs) - lookback - 1
		if from < 0 {
			from = 0
		}
		best := math.Inf(-1)
		for i := from; i < len(highs)-1; i++ {
			if !math.IsNaN(highs[i]) && highs[i] > best {
				best = highs[i]
			}
		}
		if !math.IsInf(best, -1) {
			breakoutLevel = &best
		}
		if breakoutLevel != nil {
			limit := len(highs) - lookback - 2
			if limit < -1 {
				limit = -1
			}
			for i := len(highs) - 2; i > limit; i-- {
				if highs[i] == *breakoutLevel {
					boPos = i
					break
				}
			}
		}
	}

	// ② 回踩低点
	pbl := cfg.PullbackLookback
	if len(lows) < pbl {
		pbl = len(lows)
	}
	segFrom := len(lows) - pbl
	if boPos >= 0 && boPos < len(lows)-1 {
		segFrom = boPos + 1
	}
	if segFrom < 0 {
		segFrom = 0
	}
	var pullLow *float64
	pullLowPos := -1
	best := math.Inf(1)
	for i := segFrom; i < len(lows); i++ {
		if math.IsNaN(lows[i]) {
			continue
		}
		if lows[i] < best {
			best = lows[i]
			pullLowPos = i
		}
	}
	if !math.IsInf(best, 1) {
		pullLow = &best
	}
	_ = pullLowPos

	// 支撑聚合（展示用）：回踩低点 / 5日线 / 10日线 取最高有效位
	var ma5, ma10 *float64
	if len(closes) >= cfg.MA5Period {
		series := SMA(closes, cfg.MA5Period)
		last := series[len(series)-1]
		if !math.IsNaN(last) {
			ma5 = &last
		}
	}
	if len(closes) >= 10 {
		series := SMA(closes, 10)
		last := series[len(series)-1]
		if !math.IsNaN(last) {
			ma10 = &last
		}
	}
	var support *float64
	candidates := []*float64{pullLow, ma5, ma10}
	for _, c := range candidates {
		if c != nil && *c <= price {
			if support == nil || *c > *support {
				v := *c
				support = &v
			}
		}
	}
	if support == nil {
		support = pullLow
	}

	// 止损挂回踩低点下方
	var stop *float64
	if pullLow != nil {
		buf := *pullLow * cfg.StopBufferPct
		if buf < cfg.StopBufferMin {
			buf = cfg.StopBufferMin
		}
		v := *pullLow - buf
		stop = &v
	}

	trigger := breakoutLevel

	// 回踩判定
	var pullbackPct *float64
	pulledBack := false
	if breakoutLevel != nil && pullLow != nil && *breakoutLevel > 0 {
		v := (*breakoutLevel - *pullLow) / *breakoutLevel * 100.0
		pullbackPct = &v
		pulledBack = v >= cfg.PullbackMinPct && v <= cfg.PullbackMaxPct
	}

	structureOK := trigger != nil && stop != nil && *trigger > *stop && pullLow != nil

	// ③ 目标：形态高度自突破位向上投射
	var targets []float64
	if trigger != nil && pullLow != nil && *trigger > *pullLow {
		ext := *trigger - *pullLow
		t1 := *trigger + ext*cfg.TargetExtPct
		t2 := *trigger + ext*cfg.TargetExt2Pct
		if t1 > *trigger {
			targets = append(targets, Round2(t1))
		}
		if t2 > *trigger {
			targets = append(targets, Round2(t2))
		}
	}
	if targets == nil {
		targets = []float64{}
	}

	var risk, reward, rr *float64
	if structureOK {
		v := Round2(*trigger - *stop)
		risk = &v
		if len(targets) > 0 {
			v2 := Round2(targets[0] - *trigger)
			reward = &v2
			if *risk != 0 {
				v3 := Round2(*reward / *risk)
				rr = &v3
			}
		}
	}

	chaseBlocked := trigger != nil && price > *trigger*(1+cfg.ChaseTolerance)
	awaitingBreakout := trigger != nil && price <= *trigger
	pullbackBlock := RequirePullback && !pulledBack && !chaseBlocked
	valid := structureOK && rr != nil && *rr >= cfg.MinRR && !chaseBlocked && !awaitingBreakout && !pullbackBlock

	reason := ""
	switch {
	case !structureOK:
		reason = "无有效做多结构（触发位与止损位关系不成立）"
	case chaseBlocked:
		// Python: f"当前价已高于触发位 {CHASE_TOLERANCE:.0%} 以上，属追高"
		reason = "当前价已高于触发位 " + fmtRaw(roundTo(cfg.ChaseTolerance*100, 0)) + "% 以上，属追高"
	case awaitingBreakout:
		// Python 的 trigger / price 用裸 repr（f"{trigger}"），pull_low / stop 用 round(x, 2)。
		// 两端文本必须逐字一致，否则对拍会把「格式化差异」误报成「规则差异」。
		if pulledBack && pullLow != nil && trigger != nil && stop != nil {
			reason = "已回踩 " + fmtFloat(*pullbackPct, 1) + "%（低点 " + fmtRound(*pullLow, 2) +
				"），等待突破 " + fmtRaw(*trigger) + " 确认强度后入场；止损 " + fmtRound(*stop, 2)
		} else if trigger != nil {
			reason = "价格 " + fmtRaw(price) + " 未突破触发位 " + fmtRaw(*trigger) + "，且未形成有效回踩"
		}
	case pullbackBlock:
		if pullbackPct != nil {
			reason = "未出现有效回踩（当前距高点 " + fmtFloat(*pullbackPct, 1) + "%，要求 " +
				fmtFloat(cfg.PullbackMinPct, 1) + "~" + fmtFloat(cfg.PullbackMaxPct, 1) + "%）"
		} else {
			reason = "未出现有效回踩"
		}
	case rr != nil && *rr < cfg.MinRR:
		// Python: f"风险回报比 {rr} 低于门槛 {MIN_RR}"（rr 已 round 到 2 位）
		reason = "风险回报比 " + fmtRaw(*rr) + " 低于门槛 " + fmtRaw(cfg.MinRR)
	}

	e := Entry{
		Valid:            valid,
		Blocked:          chaseBlocked,
		Reason:           reason,
		Support:          roundPtr(support, 2),
		PullLow:          roundPtr(pullLow, 2),
		Trigger:          roundPtr(trigger, 2),
		Stop:             roundPtr(stop, 2),
		Targets:          targets,
		Risk:             risk,
		Reward:           reward,
		RRRatio:          rr,
		StructureOK:      structureOK,
		ChaseBlocked:     chaseBlocked,
		AwaitingBreakout: awaitingBreakout,
		PulledBack:       pulledBack,
		PullbackPct:      roundPtr(pullbackPct, 2),
		Steps:            entrySteps,
		Prohibitions:     entryProhibits,
		Sources:          []string{"VIDEO-1968", "VIDEO-1975", "VIDEO-1979", "VIDEO-1985", "VIDEO-1987", "VIDEO-130", "VIDEO-839"},
	}
	return e
}

// ============================================================ L6 仓位止损

// Layer6Sizing 计算仓位与止损阶梯。
//
// 仓位主口径为「占净值百分比」，股数只是落地执行量。三层上限取最小：
//
//	① 风险上限   shares × 每股风险 ≤ 净值 × RiskPct
//	② 单笔上限   shares × 股价   ≤ 净值 × MaxPositionPct
//	③ 整手约束   向下取整到 100 股
func Layer6Sizing(entry Entry, alignment Alignment, light Light, cfg Config, equity, riskPct float64) Sizing {
	if equity <= 0 {
		equity = DefaultEquity
	}
	if riskPct <= 0 {
		riskPct = cfg.RiskPct
	}

	trigger := entry.Trigger
	stop := entry.Stop
	riskAmount := equity * riskPct / 100.0
	rawShares := 0
	capShares := 0
	hasCap := false
	binding := "risk"
	var perShare *float64

	if trigger != nil && stop != nil && *trigger > *stop {
		v := *trigger - *stop
		perShare = &v
		rawShares = int(riskAmount / v)
		capShares = int(equity * cfg.MaxPositionPct / 100.0 / *trigger)
		hasCap = true
		if rawShares > capShares {
			binding = "position_cap"
		}
	}

	scale := alignment.PositionScale * light.Posture.Scale
	scaled := int(float64(rawShares) * scale)
	if hasCap {
		if c := int(float64(capShares) * scale); c < scaled {
			scaled = c
		}
	}

	lot := cfg.BoardLot
	shares := 0
	switch {
	case scaled >= lot:
		shares = (scaled / lot) * lot
	case scaled >= int(float64(lot)*cfg.MinLotFill):
		shares = lot
	}
	if shares == 0 && scaled > 0 {
		binding = "lot"
	}
	insufficient := scaled > 0 && shares == 0

	notional := Round2(float64(shares) * ptrOr(trigger, 0))
	positionPct := 0.0
	riskPctActual := 0.0
	rawPct := 0.0
	if equity > 0 {
		positionPct = Round2(notional / equity * 100.0)
		rawPct = Round2(float64(scaled) * ptrOr(trigger, 0) / equity * 100.0)
		// 与 Python round(risk_used / equity * 100.0, 3) 同口径。
		if perShare != nil {
			riskPctActual = roundTo(float64(shares)**perShare/equity*100.0, 3)
		}
	}

	tranches := make([]int, 0, 3)
	if shares >= lot*2 {
		t := shares / 3
		a := (t / lot) * lot
		b := (t / lot) * lot
		c := shares - a - b
		for _, x := range []int{a, b, c} {
			if x > 0 {
				tranches = append(tranches, x)
			}
		}
	} else if shares >= lot {
		tranches = []int{shares}
	}
	tranchePcts := make([]float64, 0, len(tranches))
	for _, t := range tranches {
		if equity > 0 {
			tranchePcts = append(tranchePcts, Round2(float64(t)*ptrOr(trigger, 0)/equity*100.0))
		}
	}

	capPtr := (*int)(nil)
	if hasCap {
		capPtr = &capShares
	}

	return Sizing{
		Equity:              equity,
		RiskPct:             riskPct,
		RiskAmount:          Round2(riskAmount),
		PerShareRisk:        roundPtr(perShare, 3),
		RawShares:           rawShares,
		CapShares:           capPtr,
		BindingConstraint:   binding,
		TotalShares:         shares,
		BoardLot:            lot,
		Notional:            notional,
		PositionPct:         positionPct,
		RiskPctActual:       riskPctActual,
		RawPositionPct:      rawPct,
		PositionCapPct:      cfg.MaxPositionPct,
		InsufficientCapital: insufficient,
		Tranches:            tranches,
		TranchePcts:         tranchePcts,
		ScaleApplied:        Round2(scale),
		StopRules: []string{
			"止损放最近相关更高低点下方",
			"每形成新的更高低点，止损上移",
			"绝不让盈利单回落到 5 日线",
			"不在开盘前设止损",
			// 与 Python f"缓冲 结构位下方 {STOP_BUFFER_PCT:.1%}（下限 {STOP_BUFFER_MIN} 元）" 同文本。
			"缓冲 结构位下方 " + fmtFloat(cfg.StopBufferPct*100, 1) + "%（下限 " + fmtFloat(cfg.StopBufferMin, -1) + " 元）",
		},
		PartialExit: []string{"先出 1/3 降风险", "在更高低点下方止损另 1/3", "剩余移动止损跟随"},
		Sources:     []string{"INTERVIEW-001", "VIDEO-023", "VIDEO-504", "VIDEO-642", "VIDEO-087", "VIDEO-2184", "VIDEO-2186", "VIDEO-376"},
	}
}

// ============================================================ 综合分析

// Analyze 对单只标的做六层综合分析。
func Analyze(code string, daily, weekly, monthly []Bar, cfg Config, equity float64) Result {
	trend := Layer1Trend(daily, cfg)
	polarity := Layer2Polarity(daily, cfg)
	alignment := Layer3Alignment(monthly, weekly, trend.State, cfg)
	light := Layer4Light(polarity, trend)
	entry := Layer5Entry(daily, trend, polarity, light, cfg)
	sizing := Layer6Sizing(entry, alignment, light, cfg, equity, cfg.RiskPct)

	score := 0.0
	switch light.Light {
	case "GREEN":
		score += 40
	case "YELLOW":
		score += 15
	case "RED":
		score -= 40
	}
	score += float64(alignment.Score) * 8
	switch trend.State {
	case "UPTREND":
		score += 20
	case "TRANSITION":
		score -= 10
	case "RANGE":
		score -= 5
	case "DOWNTREND":
		score -= 20
	}
	if entry.RRRatio != nil {
		v := *entry.RRRatio * 8
		if v > 20 {
			v = 20
		}
		if v < -20 {
			v = -20
		}
		score += v
	}
	if entry.ChaseBlocked {
		score -= 15
	}
	if !entry.StructureOK {
		score -= 15
	}
	if entry.Valid {
		score += 25
	} else if entry.AwaitingBreakout && entry.PulledBack {
		score += 12
	}
	switch polarity.Polarity {
	case "RISING":
		score += 12
	case "DECLINING":
		score -= 12
	}

	res := Result{
		Code:          code,
		AsOf:          0,
		Score:         score,
		Trend:         trend,
		Polarity:      polarity,
		Alignment:     alignment,
		Light:         light,
		Entry:         entry,
		Sizing:        sizing,
		NeedsBacktest: true,
	}
	if len(daily) > 0 {
		last := daily[len(daily)-1]
		res.AsOf = last.Date
		c := last.Close
		res.Close = &c
		res.Name = last.Name
	}
	res.Sources = mergeSources(trend.Sources, polarity.Sources, alignment.Sources, light.Sources, entry.Sources, sizing.Sources)
	res.Bucket = BucketOf(res)
	return res
}
