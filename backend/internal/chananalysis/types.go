// Package chananalysis wraps the local czsc (缠中说禅) analysis service so the
// HTTP API can expose 缠论 structure, signals, offline charts and weight
// backtests for a single stock.
//
// The heavy lifting happens in an out-of-process Python script
// (czsc-service/analyze.py) invoked with the interpreter of the
// a-stock-data virtualenv. Keeping it out of process isolates the Rust-backed
// czsc wheel from the Go build and lets the service be upgraded independently.
//
// 结构体字段与 analyze.py 的 JSON 输出一一对应。Python 侧刻意保留了 czsc 原生
// 命名（zg/zd/gg/dd）以减少翻译层，Go 侧原样透传，由前端负责展示层映射。
package chananalysis

import "time"

// Result 是一次完整缠论分析的返回值。
type Result struct {
	Symbol      string    `json:"symbol"`
	Freq        string    `json:"freq"`
	Source      string    `json:"source,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
	ElapsedMS   int       `json:"elapsed_ms"`
	Range       BarRange  `json:"range"`
	Structure   Structure `json:"structure"`
	Summary     Summary   `json:"summary"`
	Signals     []Signal  `json:"signals"`
	Backtest    *Backtest `json:"backtest,omitempty"`
	Chart       *Chart    `json:"chart,omitempty"`
}

// BarRange 描述实际参与计算的 K 线区间。
type BarRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Bars  int    `json:"bars"`
}

// Structure 是缠论结构快照。
type Structure struct {
	Counts     Counts       `json:"counts"`
	LastClose  float64      `json:"last_close"`
	FX         []Fractal    `json:"fx"`
	Bi         []Stroke     `json:"bi"`
	Zs         []Pivot      `json:"zs"`
	CurrentBi  *CurrentBi   `json:"current_bi"`
	ZsPosition *ZsPosition  `json:"zs_position"`
	Divergence []Divergence `json:"divergence"`
}

// Counts 是分型 / 笔 / 中枢的数量统计。
type Counts struct {
	Bars int `json:"bars"`
	FX   int `json:"fx"`
	Bi   int `json:"bi"`
	Zs   int `json:"zs"`
}

// Fractal 是一个分型（fx_list 中的一项）。
type Fractal struct {
	Time  string  `json:"time"`
	Price float64 `json:"price"`
	Mark  string  `json:"mark"`
	Kind  string  `json:"kind"`
}

// Point 是笔的端点（起止时间与价格）。
type Point struct {
	Time  string  `json:"time"`
	Price float64 `json:"price"`
}

// Stroke 是一笔。power 为力度、slope 为斜率（力度/持续K线数），用于背驰比较。
type Stroke struct {
	Direction string  `json:"direction"`
	Start     Point   `json:"start"`
	End       Point   `json:"end"`
	Bars      int     `json:"bars"`
	Power     float64 `json:"power"`
	Slope     float64 `json:"slope"`
	IsSure    bool    `json:"is_sure"`
}

// CurrentBi 是当前笔及其推进位置。
type CurrentBi struct {
	Stroke
	// Progress 是现价在该笔区间内的推进比例（0 为起点，1 为终点）。
	Progress float64 `json:"progress"`
	// Sure 表示该笔是否已被 czsc 确认。
	Sure bool `json:"sure"`
}

// Pivot 是一个中枢。
type Pivot struct {
	Start string `json:"start"`
	End   string `json:"end"`
	// ZG / ZD 是中枢区间上下沿；GG / DD 是中枢内最高/最低点。
	ZG float64 `json:"zg"`
	ZD float64 `json:"zd"`
	GG float64 `json:"gg"`
	DD float64 `json:"dd"`
	// Amplitude 是中枢振幅，czsc 未提供时为 0。
	Amplitude float64 `json:"amplitude"`
}

// ZsPosition 描述现价与最近中枢的位置关系。
type ZsPosition struct {
	// State 取值为 中枢上方 / 中枢内部 / 中枢下方。
	State string `json:"state"`
	Zone  Pivot  `json:"zone"`
	Note  string `json:"note"`
}

// Divergence 是一个背驰点。
type Divergence struct {
	Direction string `json:"direction"`
	// Kind 取值为 顶背驰 / 底背驰。
	Kind string `json:"kind"`
	Time string `json:"time"`
	// Price 是背驰发生时的价格。
	Price float64 `json:"price"`
	// PrevSlope / Slope 是前一同向笔与本笔的斜率。
	PrevSlope float64 `json:"prev_slope"`
	Slope     float64 `json:"slope"`
	// Decay 是斜率衰减比例（0.35 表示动能衰减 35%）。
	Decay float64 `json:"decay"`
}

// Signal 是单个缠论信号在当前 K 线上的取值。
type Signal struct {
	// Name 是 czsc 信号函数名，例如 cxt_bi_status_V230101。
	Name string `json:"name"`
	// Label 是中文短标签，便于前端展示。
	Label string `json:"label"`
	// Category 是信号分类（czsc 侧给出）。
	Category string `json:"category,omitempty"`
	// Value 是信号的原始字符串取值，例如 向上_顶分_任意_0。
	Value string `json:"value,omitempty"`
	// Glossary 是该取值的中文解释（可能为空）。
	Glossary string `json:"glossary,omitempty"`
	// Params 是本次调用使用的参数。
	Params map[string]any `json:"params,omitempty"`
	// OK 为 false 表示该信号调用失败，此时 Error 给出原因；其余信号不受影响。
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Bias 把信号取值映射为多空倾向，供前端着色。czsc 的多数形态/时序信号约定
// 以「向上」/「向下」开头表达方向。
func (s Signal) Bias() string {
	if !s.OK {
		return "unknown"
	}
	switch {
	case hasPrefix(s.Value, "向上"):
		return "bullish"
	case hasPrefix(s.Value, "向下"):
		return "bearish"
	default:
		return "neutral"
	}
}

// Summary 是综合结构与信号得到的倾向判断。
type Summary struct {
	// Score 为 0-100，越高越偏多。
	Score float64 `json:"score"`
	// Stance 取值为 偏多 / 偏空 / 中性。
	Stance string `json:"stance"`
	// Tone 取值为 up / down / flat，供前端选色。
	Tone string `json:"tone"`
	// Reasons 是逐条打分依据，最多 6 条。
	Reasons []string `json:"reasons"`
	// Conclusion 是取前三条依据合成的单句结论。
	Conclusion string `json:"conclusion"`
}

// Backtest 是单个信号的权重回测结果。
type Backtest struct {
	Signal      string `json:"signal"`
	SignalValue string `json:"signal_value,omitempty"`
	// Stats 是 wbt 返回的绩效字段。键名可能是中文（年化收益）也可能是英文
	// （annual_return），取决于 wbt 版本，因此保持为原始映射并由前端做展示映射。
	Stats map[string]any `json:"stats"`
	// AllKeys 是完整统计字段名清单，便于排查缺失指标。
	AllKeys []string `json:"all_keys,omitempty"`
	// Error 在回测失败时给出原因（例如信号在区间内未产生多头）。
	Error string `json:"error,omitempty"`
	OK    bool   `json:"ok"`
}

// Chart 是离线缠论图信息。
type Chart struct {
	OK bool `json:"ok"`
	// Path 是生成的 HTML 文件绝对路径（指定了输出目录时）。
	Path string `json:"path,omitempty"`
	// HTML 是内联的 HTML 内容（未指定输出目录时，体积较大）。
	HTML string `json:"html,omitempty"`
	// Size 是 HTML 字节数。
	Size int `json:"size"`
	// Error 在绘图失败时给出原因。
	Error string `json:"error,omitempty"`
}

// SignalMeta 是 czsc 信号目录中的一条元数据（list_all_signals 的一项）。
// 实测返回 246 条，字段固定为 name / param_template / category / namespace，
// 其中 param_template 是形如 `{freq}_D{di}N{n}M{m}TH{th}_ADTMV230603` 的模板串，
// ParamKeys 从中解析出 {di}{n}{m}{th} 等参数占位符供前端生成表单。
type SignalMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Category  string `json:"category,omitempty"`
	// ParamTemplate 是 czsc 原始参数模板串。
	ParamTemplate string `json:"param_template,omitempty"`
	// ParamKeys 是从模板中解析出的参数占位符，例如 ["di","n","m","th"]。
	ParamKeys []string `json:"param_keys,omitempty"`
}

func hasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
