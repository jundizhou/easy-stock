package foundation

// Futures positions are ranked CFFEX member positions, not individual stocks or
// a census of institutional directional exposure. Consensus net-long values use
// positive = long and negative = short. Missing fields remain null.
type MarketFuturesPositionRow struct {
	TradeDate     string   `json:"trade_date"`
	LongPosition  int64    `json:"long_position"`
	LongChange    *int64   `json:"long_change"`
	ShortPosition int64    `json:"short_position"`
	ShortChange   *int64   `json:"short_change"`
	NetPosition   int64    `json:"net_position"`
	SettlePrice   *float64 `json:"settle_price"`
	IndexClose    *float64 `json:"index_close"`
	IndexChange   *float64 `json:"index_change"`
	Basis         *float64 `json:"basis"`
}

type MarketFuturesPositionSeries struct {
	Variety      string                     `json:"variety"`
	VarietyName  string                     `json:"variety_name"`
	ContractCode string                     `json:"contract_code"`
	IndexCode    string                     `json:"index_code"`
	Rows         []MarketFuturesPositionRow `json:"rows"`
	Meta         SourceMeta                 `json:"meta"`
}

type MarketFuturesMemberRank struct {
	Contract      string `json:"contract"`
	Rank          int    `json:"rank"`
	LongName      string `json:"long_name"`
	LongPosition  int64  `json:"long_position"`
	LongChange    *int64 `json:"long_change"`
	ShortName     string `json:"short_name"`
	ShortPosition int64  `json:"short_position"`
	ShortChange   *int64 `json:"short_change"`
}

type MarketFuturesMembers struct {
	ContractCode string                    `json:"contract_code"`
	TradeDate    string                    `json:"trade_date"`
	Members      []MarketFuturesMemberRank `json:"members"`
	Meta         SourceMeta                `json:"meta"`
}

type MarketFuturesConsensusVariety struct {
	Variety             string `json:"variety"`
	TradeDate           string `json:"trade_date"`
	ContractCount       int    `json:"contract_count"`
	LongPosition        int64  `json:"long_position"`
	ShortPosition       int64  `json:"short_position"`
	NetShortPosition    int64  `json:"net_short_position"`
	NetLongPosition     int64  `json:"net_long_position"`
	LongChange          int64  `json:"long_change"`
	ShortChange         int64  `json:"short_change"`
	NetShortChange      int64  `json:"net_short_change"`
	NetLongChange       int64  `json:"net_long_change"`
	CITICNetShortChange int64  `json:"citic_net_short_change"`
	CITICNetLongChange  int64  `json:"citic_net_long_change"`
}

type MarketFuturesConsensus struct {
	TradeDate             string                          `json:"trade_date"`
	Varieties             []MarketFuturesConsensusVariety `json:"varieties"`
	Top20NetShortPosition int64                           `json:"top20_net_short_position"`
	Top20NetLongPosition  int64                           `json:"top20_net_long_position"`
	Top20NetShortChange   int64                           `json:"top20_net_short_change"`
	Top20NetLongChange    int64                           `json:"top20_net_long_change"`
	CITICNetShortChange   int64                           `json:"citic_net_short_change"`
	CITICNetLongChange    int64                           `json:"citic_net_long_change"`
	Meta                  SourceMeta                      `json:"meta"`
}
