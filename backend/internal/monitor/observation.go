package monitor

import "easy-stock/backend/internal/foundation"

// Observation 是规则评估的全部输入。engine 在每种数据源上把它装出来，
// rules.go 只认识这个结构——换数据源时不碰判定逻辑。
type Observation struct {
	SnapshotRow   foundation.MarketSnapshotRow
	Price         float64
	ChangePercent float64
	VolumeRatio   float64
	TurnoverRate  float64
	Amount        float64

	// 成本来自持仓批次：cost_distance_pct 依赖它，没有成本的观测不算这个字段。
	HaveCost  bool
	CostPrice float64

	// 60 日高低来自候选池日线缓存；HaveHighLow=false 表示没算过，
	// 此时 high_60d/low_60d 条件保持沉默而不是按 false 触发。
	HaveHighLow bool
	NewHigh60   bool
	NewLow60    bool

	// 偏离值同上：算过才有资格成立。
	HaveDeviation bool
	// 部分历史也可以评估已有窗口；缺失窗口仍保持未知。
	DeviationAvailable [3]bool
	Deviation3         float64
	Deviation10        float64
	Deviation30        float64

	// 技术信号：日线缓存算出的"今天发生了哪些事件"（signals.go）。
	// HaveSignals=false 表示没算过，signal 条件保持沉默而不是按 false 触发。
	HaveSignals bool
	Signals     map[string]bool

	Market MarketStats

	// 板块观测（sector.go）：规则对象是 BK 代码，字段见 SectorStats。
	Sector SectorStats

	HaveRemind   bool
	DaysToRemind int
}

// MarketStats 是大盘规则的观测。它没有标的概念，scope 恒为 all。
type MarketStats struct {
	HaveIndexes    bool
	IndexChangePct float64
	// Indexes 是主要宽基指数的当日涨跌，供监控台「今日市场」并列展示。
	// IndexChangePct 仍是上证指数，大盘规则沿用它，语义不变。
	Indexes      []IndexQuote
	HaveBreadth  bool
	UpCount      int
	DownCount    int
	LimitUpCount int
}

// IndexQuote 是一条指数当日涨跌，ID 与行情服务的指数目录一致。
type IndexQuote struct {
	ID        string
	Name      string
	ChangePct float64
}

// FromSnapshotRow 把全市场快照的一行装成观测，剩余维度留给后续步骤填充。
func FromSnapshotRow(row foundation.MarketSnapshotRow) Observation {
	return Observation{
		SnapshotRow:   row,
		Price:         row.Price,
		ChangePercent: row.ChangePercent,
		VolumeRatio:   row.VolumeRatio,
		TurnoverRate:  row.TurnoverRate,
		Amount:        row.Amount,
	}
}
