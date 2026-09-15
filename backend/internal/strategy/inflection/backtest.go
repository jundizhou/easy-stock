package inflection

import "time"

// BacktestBar is one end-of-day observation. The next bar is the earliest
// executable bar, which prevents look-ahead bias.
type BacktestBar struct {
	Time               time.Time
	Request            EvaluationRequest
	Open, Close        float64
	LimitUp, LimitDown bool
}

type BacktestTrade struct {
	Symbol     string       `json:"symbol"`
	SignalTime time.Time    `json:"signal_time"`
	EntryTime  time.Time    `json:"entry_time"`
	ExitTime   time.Time    `json:"exit_time"`
	Entry      float64      `json:"entry"`
	Exit       float64      `json:"exit"`
	Return     float64      `json:"return"`
	NetReturn  float64      `json:"net_return"`
	Status     SignalStatus `json:"status"`
}

type BacktestResult struct {
	InitialCapital float64         `json:"initial_capital"`
	FinalCapital   float64         `json:"final_capital"`
	TotalReturn    float64         `json:"total_return"`
	MaxDrawdown    float64         `json:"max_drawdown"`
	WinRate        float64         `json:"win_rate"`
	Trades         []BacktestTrade `json:"trades"`
}

// RunBacktest evaluates each bar and enters the selected carrier on the next
// bar, exiting at that bar's close. Prices are supplied by the local data
// adapter; limit rules make unavailable fills explicit.
func (e *Engine) RunBacktest(bars []BacktestBar, initialCapital, feeRate, slippage float64) BacktestResult {
	if initialCapital <= 0 {
		initialCapital = 1
	}
	capital, peak := initialCapital, initialCapital
	result := BacktestResult{InitialCapital: initialCapital}
	for i := 0; i+1 < len(bars); i++ {
		ev, err := e.Evaluate(bars[i].Request)
		if err != nil || ev.PrimarySignal == InflectionNone {
			continue
		}
		symbol := ev.Big.NewCarrierSymbol
		if symbol == "" {
			symbol = ev.Small.NewCarrierSymbol
		}
		if symbol == "" {
			continue
		}
		next := bars[i+1]
		if next.LimitUp || next.Open <= 0 || next.Close <= 0 {
			continue
		}
		entry := next.Open * (1 + slippage)
		exit := next.Close * (1 - slippage)
		gross := exit/entry - 1
		net := gross - feeRate*2
		capital *= 1 + net
		if capital > peak {
			peak = capital
		}
		dd := (peak - capital) / peak
		if dd > result.MaxDrawdown {
			result.MaxDrawdown = dd
		}
		result.Trades = append(result.Trades, BacktestTrade{Symbol: symbol, SignalTime: bars[i].Time, EntryTime: next.Time, ExitTime: next.Time, Entry: entry, Exit: exit, Return: gross, NetReturn: net, Status: ev.Big.Status})
	}
	result.FinalCapital = capital
	result.TotalReturn = capital/initialCapital - 1
	if len(result.Trades) > 0 {
		wins := 0
		for _, t := range result.Trades {
			if t.NetReturn > 0 {
				wins++
			}
		}
		result.WinRate = float64(wins) / float64(len(result.Trades))
	}
	return result
}
