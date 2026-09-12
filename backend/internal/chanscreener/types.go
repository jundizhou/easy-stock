// Package chanscreener wraps the local chan.py (Vespa314) engine service so
// the HTTP API can expose 缠论买卖点分析 and 缠论选股 for a batch of stocks.
//
// 与 chananalysis（czsc 引擎）一样，重活在一个进程外的 Python 脚本
// (chanpy-service/chanpy_service.py) 里完成，Go 侧只负责取参、调用与缓存。
// 脚本通过回调 easy-stock 后端的 /api/v1/quotes/kline 拉K线，再用 vendored 的
// chan.py 计算笔/线段/中枢/买卖点，因此任何 Python ≥3.11 都能跑，无第三方依赖。
package chanscreener

import "time"

// Stroke 是一笔（或线段的简化载荷）。
type Stroke struct {
	Direction string `json:"direction"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	// StartPrice / EndPrice 取笔两端KLU的收盘价。
	StartPrice float64 `json:"start_price"`
	EndPrice   float64 `json:"end_price"`
	// ChangePercent 是两端收盘价的涨跌幅。
	ChangePercent float64 `json:"change_percent"`
	Bars          int     `json:"bars"`
	Amp           float64 `json:"amp"`
	IsSure        bool    `json:"is_sure"`
}

// Pivot 是一个中枢。ZD/ZG 是中枢区间，GG/DD 是区间内最高/最低点。
type Pivot struct {
	Begin   string  `json:"begin"`
	End     string  `json:"end"`
	ZD      float64 `json:"zd"`
	ZG      float64 `json:"zg"`
	GG      float64 `json:"gg"`
	DD      float64 `json:"dd"`
	BiCount int     `json:"bi_count"`
	IsSure  bool    `json:"is_sure"`
}

// BSPoint 是一个形态学买卖点。Types 为 chan.py 的类型值（1/1p/2/2s/3a/3b），
// Labels 是带方向的中文标签（如「二买」「三a卖」）。
type BSPoint struct {
	IsBuy    bool     `json:"is_buy"`
	Types    []string `json:"types"`
	Labels   []string `json:"labels"`
	TypeStr  string   `json:"type_str"`
	Time     string   `json:"time"`
	Price    float64  `json:"price"`
	BarIndex int      `json:"bar_index"`
	IsSure   bool     `json:"is_sure"`
	IsSegBSP bool     `json:"is_segbsp"`
	// RelateBSP1Time 是二/三类买卖点关联的一类买卖点时间（可为空）。
	RelateBSP1Time string `json:"relate_bsp1_time"`
	BiDirection    string `json:"bi_direction"`
}

// ZsPosition 描述现价与最近中枢的位置关系，State 为 above/inside/below。
type ZsPosition struct {
	State string `json:"state"`
	Zone  Pivot  `json:"zone"`
}

// Structure 是一次分析的结构快照。
type Structure struct {
	Bars      int         `json:"bars"`
	BiCount   int         `json:"bi_count"`
	ZsCount   int         `json:"zs_count"`
	SegCount  int         `json:"seg_count"`
	BspCount  int         `json:"bsp_count"`
	LastClose float64     `json:"last_close"`
	Bi        []Stroke    `json:"bi"`
	Zs        []Pivot     `json:"zs"`
	Bsp       []BSPoint   `json:"bsp"`
	LastBi    *Stroke     `json:"last_bi"`
	ZsPos     *ZsPosition `json:"zs_position"`
}

// Summary 是确定性评分结果，Score 0-100 越高越偏多。
type Summary struct {
	Score      float64  `json:"score"`
	Stance     string   `json:"stance"`
	Tone       string   `json:"tone"`
	Reasons    []string `json:"reasons"`
	Conclusion string   `json:"conclusion"`
}

// BarRange 描述参与计算的K线区间。
type BarRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Bars  int    `json:"bars"`
}

// AnalyzeResult 是单只股票的 chan.py 分析结果。
type AnalyzeResult struct {
	OK          bool      `json:"ok"`
	Error       string    `json:"error,omitempty"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name,omitempty"`
	Period      string    `json:"period"`
	Range       BarRange  `json:"range"`
	Structure   Structure `json:"structure"`
	Summary     Summary   `json:"summary"`
	ElapsedMS   int       `json:"elapsed_ms"`
	GeneratedAt time.Time `json:"generated_at"`
	Engine      string    `json:"engine"`
}

// ScreenItem 是选股结果里的一只股票。未命中的股票也会带评分返回，
// 前端可切换「只看命中」查看完整候选池。
type ScreenItem struct {
	Symbol       string   `json:"symbol"`
	Name         string   `json:"name,omitempty"`
	LastClose    float64  `json:"last_close"`
	Matched      bool     `json:"matched"`
	MatchReasons []string `json:"match_reasons"`
	Score        float64  `json:"score"`
	Stance       string   `json:"stance"`
	LastBSP      *BSPoint `json:"last_bsp"`
	ZsState      string   `json:"zs_state"`
	// BiDirection 是当前笔方向 up/down。
	BiDirection string   `json:"bi_direction"`
	BiCount     int      `json:"bi_count"`
	ZsCount     int      `json:"zs_count"`
	Reasons     []string `json:"reasons"`
}

// ScreenError 是扫描过程中单只股票的失败记录。
type ScreenError struct {
	Symbol string `json:"symbol"`
	Error  string `json:"error"`
}

// ScreenResult 是一次批量选股的结果。
type ScreenResult struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	Period      string         `json:"period"`
	Filters     map[string]any `json:"filters"`
	Scanned     int            `json:"scanned"`
	Matched     int            `json:"matched"`
	Failed      int            `json:"failed"`
	Errors      []ScreenError  `json:"errors"`
	Results     []ScreenItem   `json:"results"`
	ElapsedMS   int            `json:"elapsed_ms"`
	GeneratedAt time.Time      `json:"generated_at"`
	Engine      string         `json:"engine"`
}
