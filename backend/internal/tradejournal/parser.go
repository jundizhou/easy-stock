package tradejournal

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// parseCSV 解析券商交割单 CSV。自动识别中文表头的同义列（业务日期/成交日期、证券代码、业务名称/操作、成交价格、成交数量、成交金额、手续费）。
func parseCSV(content string) (trades []RawTrade, skipped int, err error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimPrefix(content, "\ufeff")
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	var headers []string
	first := true
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("CSV 格式错误: %w", readErr)
		}
		if len(record) < 3 {
			continue
		}
		if first {
			headers = normalizeHeaders(record)
			first = false
			continue
		}
		trade, parseErr := parseRecord(record, headers)
		if parseErr != nil {
			skipped++
			continue
		}
		if trade.Operation == "skip" {
			skipped++
			continue
		}
		trades = append(trades, trade)
		if len(trades) > MaxTrades {
			return nil, 0, fmt.Errorf("成交记录超过上限 %d 行", MaxTrades)
		}
	}
	if len(headers) == 0 {
		return nil, 0, fmt.Errorf("未识别到 CSV 表头行")
	}
	return trades, skipped, nil
}

func normalizeHeaders(record []string) []string {
	headers := make([]string, len(record))
	for i, value := range record {
		headers[i] = normalizeHeader(strings.TrimSpace(value))
	}
	return headers
}

func normalizeHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "（", "(", "）", ")", "证券", "", "成交", "", "发生", "", "交易", "", "委托", "", "费", "")
	value = replacer.Replace(value)
	return value
}

func parseRecord(record []string, headers []string) (RawTrade, error) {
	trade := RawTrade{}
	for i, header := range headers {
		if i >= len(record) {
			break
		}
		value := strings.TrimSpace(record[i])
		switch header {
		case "日期", "occur_date", "date", "业务日期", "时间":
			trade.OccurDate = value
		case "代码", "symbol", "股票代码":
			trade.Symbol = value
		case "名称", "name", "证券名称", "证券简称":
			trade.Name = value
		case "操作", "operation", "业务名称", "业务类型", "方向", "买卖":
			trade.Operation = value
		case "价格", "price", "单价":
			trade.Price = parseNumber(value)
		case "数量", "volume", "股数", "股":
			trade.Volume = parseNumber(value)
		case "金额", "amount", "发生金额", "成交金额":
			trade.Amount = parseNumber(value)
		case "手续费", "fee", "佣金", "费用", "印花税", "过户":
			trade.Fee += parseNumber(value)
		}
	}
	if trade.Symbol == "" || trade.OccurDate == "" {
		return trade, fmt.Errorf("缺少代码或日期")
	}
	operation := classifyOperation(trade.Operation)
	if operation == "skip" {
		return trade, nil
	}
	trade.Operation = operation
	if trade.Volume <= 0 {
		return trade, fmt.Errorf("数量无效")
	}
	if trade.Price <= 0 {
		if trade.Amount > 0 {
			trade.Price = trade.Amount / trade.Volume
		} else {
			return trade, fmt.Errorf("价格无效")
		}
	}
	if trade.Amount <= 0 {
		trade.Amount = trade.Price * trade.Volume
	}
	return trade, nil
}

// classifyOperation 把各种券商表述归一为 buy / sell / skip。
func classifyOperation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "买"), strings.Contains(value, "buy"), strings.Contains(value, "开仓"), strings.Contains(value, "证券买入"):
		return "buy"
	case strings.Contains(value, "卖"), strings.Contains(value, "sell"), strings.Contains(value, "平仓"), strings.Contains(value, "证券卖出"):
		return "sell"
	default:
		return "skip"
	}
}

func parseNumber(value string) float64 {
	value = strings.ReplaceAll(value, ",", "")
	value = strings.ReplaceAll(value, "，", "")
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return number
}

func parseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{"2006-01-02", "2006/01/02", "20060102", "2006-01-02 15:04:05", "2006/01/02 15:04"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析日期 %q", value)
}

// matchFIFO 把流水按代码分组、日期排序后做 FIFO 配对，产出完整回合与未平仓头寸。
func matchFIFO(trades []RawTrade) (roundTrips []RoundTrip, openPositions []OpenPosition, skipped int) {
	bySymbol := map[string][]RawTrade{}
	order := []string{}
	for _, trade := range trades {
		if _, ok := bySymbol[trade.Symbol]; !ok {
			order = append(order, trade.Symbol)
		}
		bySymbol[trade.Symbol] = append(bySymbol[trade.Symbol], trade)
	}
	type lot struct {
		date    time.Time
		dateStr string
		price   float64
		volume  float64
		fee     float64
	}
	for _, symbol := range order {
		items := bySymbol[symbol]
		var pending []lot
		var name string
		for _, item := range items {
			if name == "" {
				name = item.Name
			}
			itemDate, err := parseDate(item.OccurDate)
			if err != nil {
				skipped++
				continue
			}
			remaining := item.Volume
			if item.Operation == "buy" {
				pending = append(pending, lot{date: itemDate, dateStr: item.OccurDate, price: item.Price, volume: remaining, fee: item.Fee})
				continue
			}
			// sell：与最早的开仓 lot 配对。
			for remaining > 1e-6 && len(pending) > 0 {
				head := pending[0]
				matched := head.volume
				if matched > remaining {
					matched = remaining
				}
				openFee := head.fee * (matched / maxFloat(head.volume, 1e-9))
				sellFee := item.Fee * (matched / maxFloat(item.Volume, 1e-9))
				netProfit := (item.Price-head.price)*matched - openFee - sellFee
				cost := head.price * matched
				profitRate := 0.0
				if cost > 0 {
					profitRate = netProfit / cost * 100
				}
				roundTrips = append(roundTrips, RoundTrip{
					Symbol: symbol, Name: name, OpenDate: head.dateStr, CloseDate: item.OccurDate,
					HoldingDays: itemDate.Sub(head.date).Hours() / 24, Direction: "long",
					Volume: matched, OpenPrice: head.price, ClosePrice: item.Price,
					NetProfit: netProfit, ProfitRate: profitRate, Fees: openFee + sellFee,
					Outcome: outcomeFor(netProfit),
				})
				remaining -= matched
				head.volume -= matched
				head.fee -= openFee
				if head.volume <= 1e-6 {
					pending = pending[1:]
				} else {
					pending[0] = head
				}
			}
			if remaining > 1e-6 {
				// 卖出数量超过已有持仓：忽略超额部分（数据不完整）。
				skipped++
			}
		}
		for _, rest := range pending {
			if rest.volume > 1e-6 {
				openPositions = append(openPositions, OpenPosition{Symbol: symbol, Name: name, Volume: rest.volume, OpenPrice: rest.price, OpenDate: rest.dateStr})
			}
		}
	}
	return roundTrips, openPositions, skipped
}

func outcomeFor(netProfit float64) string {
	if netProfit > 0 {
		return "win"
	}
	if netProfit < 0 {
		return "loss"
	}
	return "flat"
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
