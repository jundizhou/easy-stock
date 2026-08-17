package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type marketIndexDefinition struct {
	ID       string
	SecID    string
	Name     string
	Region   string
	Market   string
	Currency string
	Core     bool
}

var marketIndexCatalog = []marketIndexDefinition{
	{ID: "sse", SecID: "1.000001", Name: "上证指数", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "szse", SecID: "0.399001", Name: "深证成指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "chinext", SecID: "0.399006", Name: "创业板指", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi300", SecID: "1.000300", Name: "沪深300", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "sse50", SecID: "1.000016", Name: "上证50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "csi1000", SecID: "1.000852", Name: "中证1000", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "star50", SecID: "1.000688", Name: "科创50", Region: "中国", Market: "CN", Currency: "CNY", Core: true},
	{ID: "hsi", SecID: "100.HSI", Name: "恒生指数", Region: "中国香港", Market: "HK", Currency: "HKD", Core: true},
	{ID: "nikkei", SecID: "100.N225", Name: "日经225", Region: "亚太", Market: "JP", Currency: "JPY"},
	{ID: "kospi", SecID: "100.KS11", Name: "韩国综合", Region: "亚太", Market: "KR", Currency: "KRW"},
	{ID: "taiwan", SecID: "100.TWII", Name: "台湾加权", Region: "亚太", Market: "TW", Currency: "TWD"},
	{ID: "dow", SecID: "100.DJIA", Name: "道琼斯", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "sp500", SecID: "100.SPX", Name: "标普500", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "nasdaq", SecID: "100.NDX", Name: "纳斯达克", Region: "美洲", Market: "US", Currency: "USD", Core: true},
	{ID: "ftse", SecID: "100.FTSE", Name: "英国富时100", Region: "欧洲", Market: "UK", Currency: "GBP"},
	{ID: "dax", SecID: "100.GDAXI", Name: "德国DAX", Region: "欧洲", Market: "DE", Currency: "EUR"},
	{ID: "cac", SecID: "100.FCHI", Name: "法国CAC40", Region: "欧洲", Market: "FR", Currency: "EUR"},
}

var eastmoneyIndustryMomentumFields = []string{
	"change_percent", "five_day_change_percent", "twenty_day_change_percent", "turnover_rate",
	"rising_count", "falling_count", "main_net_inflow", "leader_name", "leader_change_percent",
}

var eastmoneyFundFlowFields = []string{
	"price", "change_percent", "main_net_inflow", "main_net_inflow_ratio",
	"super_large_net_inflow", "super_large_net_inflow_ratio", "large_net_inflow", "large_net_inflow_ratio",
	"medium_net_inflow", "medium_net_inflow_ratio", "small_net_inflow", "small_net_inflow_ratio",
}

var eastmoneyMainNetOnlyFields = []string{"main_net_inflow"}

func (c *Client) MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	definitions := make([]marketIndexDefinition, 0, len(marketIndexCatalog))
	for _, item := range marketIndexCatalog {
		if scope == "core" && !item.Core {
			continue
		}
		definitions = append(definitions, item)
	}
	secids := make([]string, 0, len(definitions))
	for _, item := range definitions {
		secids = append(secids, item.SecID)
	}
	endpoint := c.quoteBaseURL + "/api/qt/ulist.np/get"
	params := url.Values{}
	params.Set("fltt", "2")
	params.Set("secids", strings.Join(secids, ","))
	params.Set("fields", "f12,f13,f14,f2,f3,f4,f124")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []map[string]any `json:"diff"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if payload.RC != 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney index rc=%d", payload.RC)
	}
	meta := foundation.SourceMeta{Source: "eastmoney:index", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	definitionsBySecID := make(map[string]marketIndexDefinition, len(definitions))
	for _, item := range definitions {
		definitionsBySecID[item.SecID] = item
	}
	items := make([]foundation.MarketIndexSnapshot, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		secid := fmt.Sprintf("%d.%s", int(asFloat(raw["f13"])), asString(raw["f12"]))
		definition, ok := definitionsBySecID[secid]
		if !ok {
			continue
		}
		tradeTime := time.Unix(int64(asFloat(raw["f124"])), 0)
		items = append(items, foundation.MarketIndexSnapshot{
			ID: definition.ID, SecID: definition.SecID, Code: asString(raw["f12"]), Name: firstString(asString(raw["f14"]), definition.Name),
			Region: definition.Region, Market: definition.Market, Currency: definition.Currency,
			Price: asFloat(raw["f2"]), ChangePercent: asFloat(raw["f3"]), Change: asFloat(raw["f4"]), TradeTime: tradeTime,
			Status: indexStatus(tradeTime, time.Now()), Meta: meta,
		})
	}
	order := map[string]int{}
	for index, item := range definitions {
		order[item.ID] = index
	}
	sort.SliceStable(items, func(i, j int) bool { return order[items[i].ID] < order[items[j].ID] })
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney returned no index snapshots")
	}
	return items, meta, nil
}

func (c *Client) MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error) {
	definition, ok := findMarketIndex(id)
	if !ok {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported index %q", id)
	}
	if limit <= 0 {
		limit = 120
	}
	klt := eastMoneyPeriod(period)
	if klt == "" {
		return foundation.MarketIndexSeries{}, fmt.Errorf("unsupported period %q", period)
	}
	endpoint := c.baseURL + "/api/qt/stock/kline/get"
	params := url.Values{}
	params.Set("secid", definition.SecID)
	params.Set("fields1", "f1,f2,f3,f4,f5,f6")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	params.Set("klt", klt)
	params.Set("fqt", "1")
	params.Set("end", "20500101")
	params.Set("lmt", strconv.Itoa(limit))
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return foundation.MarketIndexSeries{}, err
	}
	if payload.RC != 0 {
		return foundation.MarketIndexSeries{}, fmt.Errorf("eastmoney index kline rc=%d", payload.RC)
	}
	meta := foundation.SourceMeta{Source: "eastmoney:index-kline", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	lines := make([]foundation.KLine, 0, len(payload.Data.KLines))
	for _, raw := range payload.Data.KLines {
		line, err := parseKLine(raw, definition.ID, meta)
		if err != nil {
			return foundation.MarketIndexSeries{}, err
		}
		lines = append(lines, line)
	}
	index := foundation.MarketIndexSnapshot{ID: definition.ID, SecID: definition.SecID, Name: definition.Name, Region: definition.Region, Market: definition.Market, Currency: definition.Currency, Meta: meta}
	if len(lines) > 0 {
		latest := lines[len(lines)-1]
		index.Code = definition.SecID[strings.Index(definition.SecID, ".")+1:]
		index.Price = latest.Close
		index.ChangePercent = latest.ChangePercent
		index.TradeTime = latest.Time
	}
	return foundation.MarketIndexSeries{Index: index, Lines: lines, Meta: meta}, nil
}

func (c *Client) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	endpoint := c.quoteBaseURL + "/api/qt/clist/get"
	params := standardListParams(limit)
	params.Set("fid", "f3")
	params.Set("fs", "m:90+t:2+f:!50")
	params.Set("fields", "f12,f14,f3,f8,f62,f104,f105,f128,f136,f109,f160,f24")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []map[string]any `json:"diff"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil || payload.RC != 0 || len(payload.Data.Diff) == 0 {
		boards, fallbackErr := c.boardsFromFundFlow(ctx, "", limit, firstError(err, fmt.Errorf("eastmoney industry momentum unavailable")))
		if fallbackErr != nil {
			return nil, foundation.SourceMeta{}, fallbackErr
		}
		items := make([]foundation.MarketIndustryMomentum, 0, len(boards))
		for _, board := range boards {
			if len(items) >= limit {
				break
			}
			items = append(items, foundation.MarketIndustryMomentum{Code: board.Code, Name: board.Name, MainNetInflow: board.MainNetInflow, Score: scoreMomentum(0, 0, 0, board.MainNetInflow, 0, 0), Meta: board.Meta})
		}
		meta := boards[0].Meta
		meta.AvailableFields = eastmoneyMainNetOnlyFields
		meta.FallbackReason = "实时行业强度不可用，仅保留资金净流入"
		for index := range items {
			items[index].Meta = meta
		}
		return items, meta, nil
	}
	meta := foundation.SourceMeta{Source: "eastmoney:industry-momentum", SourceURL: requestURL, AvailableFields: eastmoneyIndustryMomentumFields, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	items := make([]foundation.MarketIndustryMomentum, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		change := asFloat(raw["f3"])
		fiveDay := asFloat(raw["f109"])
		twentyDay := asFloat(raw["f24"])
		rising := int(asFloat(raw["f104"]))
		falling := int(asFloat(raw["f105"]))
		flow := asFloat(raw["f62"])
		items = append(items, foundation.MarketIndustryMomentum{
			Code: asString(raw["f12"]), Name: asString(raw["f14"]), ChangePercent: change, FiveDayChangePercent: fiveDay,
			TwentyDayChange: twentyDay, TurnoverRate: asFloat(raw["f8"]), RisingCount: rising, FallingCount: falling,
			MainNetInflow: flow, LeaderName: asString(raw["f128"]), LeaderChangePercent: asFloat(raw["f136"]),
			Score: scoreMomentum(change, fiveDay, twentyDay, flow, rising, falling), Meta: meta,
		})
	}
	return items, meta, nil
}

func (c *Client) MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	dimension = strings.ToLower(strings.TrimSpace(dimension))
	fs := ""
	switch dimension {
	case "stock":
		fs = "m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23"
	case "industry":
		fs = "m:90+t:2+f:!50"
	case "theme":
		fs = "m:90+t:3+f:!50"
	default:
		return nil, foundation.SourceMeta{}, fmt.Errorf("unsupported fund-flow dimension %q", dimension)
	}
	fid := "f62"
	if sortKey == "change" {
		fid = "f3"
	}
	if sortKey == "ratio" {
		fid = "f184"
	}
	endpoint := c.quoteBaseURL + "/api/qt/clist/get"
	params := standardListParams(limit)
	params.Set("fid", fid)
	params.Set("fs", fs)
	params.Set("fields", "f12,f14,f2,f3,f62,f184,f66,f69,f72,f75,f78,f81,f84,f87")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []map[string]any `json:"diff"`
		} `json:"data"`
	}
	err := c.getJSONWithRetry(ctx, requestURL, &payload)
	if err != nil || payload.RC != 0 || len(payload.Data.Diff) == 0 {
		if dimension == "stock" {
			return nil, foundation.SourceMeta{}, firstError(err, fmt.Errorf("eastmoney stock fund flow returned no data"))
		}
		code := "m:90+t:2+f:!50"
		if dimension == "theme" {
			code = "m:90+t:3+f:!50"
		}
		meta := foundation.SourceMeta{Source: "eastmoney:bkzj", AvailableFields: eastmoneyMainNetOnlyFields, FetchedAt: time.Now(), FallbackReason: "详细分单资金不可用，使用板块净流入榜"}
		boards, fallbackURL, fallbackErr := c.fetchFundFlowBoards(ctx, code, "", meta)
		if fallbackErr != nil {
			return nil, foundation.SourceMeta{}, fallbackErr
		}
		meta.SourceURL = fallbackURL
		items := make([]foundation.MarketFundFlow, 0, min(limit, len(boards)))
		for _, board := range boards {
			if len(items) >= limit {
				break
			}
			items = append(items, foundation.MarketFundFlow{Dimension: dimension, Code: board.Code, Name: board.Name, MainNetInflow: board.MainNetInflow, Meta: meta})
		}
		return items, meta, nil
	}
	meta := foundation.SourceMeta{Source: "eastmoney:fund-flow", SourceURL: requestURL, AvailableFields: eastmoneyFundFlowFields, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	items := make([]foundation.MarketFundFlow, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		code := asString(raw["f12"])
		name := asString(raw["f14"])
		if dimension == "stock" && strings.Contains(strings.ToUpper(name), "ST") {
			continue
		}
		symbol := ""
		if dimension == "stock" {
			if normalized, normalizeErr := normalizeEastMoneyStockCode(code); normalizeErr == nil {
				symbol = normalized
			}
		}
		items = append(items, foundation.MarketFundFlow{
			Dimension: dimension, Code: code, Symbol: symbol, Name: name, Price: asFloat(raw["f2"]), ChangePercent: asFloat(raw["f3"]),
			MainNetInflow: asFloat(raw["f62"]), MainNetInflowRatio: asFloat(raw["f184"]), SuperLargeNet: asFloat(raw["f66"]), SuperLargeRatio: asFloat(raw["f69"]),
			LargeNet: asFloat(raw["f72"]), LargeRatio: asFloat(raw["f75"]), MediumNet: asFloat(raw["f78"]), MediumRatio: asFloat(raw["f81"]),
			SmallNet: asFloat(raw["f84"]), SmallRatio: asFloat(raw["f87"]), Meta: meta,
		})
	}
	return items, meta, nil
}

func (c *Client) MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 120
	}
	if limit > 500 {
		limit = 500
	}

	endpoint := c.datacenterBaseURL + "/api/data/v1/get"
	start := time.Now()
	pointsByDate := make(map[string]*foundation.MarketMarginPoint, limit+1)
	lastRequestURL := ""
	for page := 1; page <= 6; page++ {
		params := url.Values{}
		params.Set("reportName", "RPTA_WEB_RZRQ_LSSH")
		params.Set("columns", "DIM_DATE,SCDM,XOB_MARKET_0001,RZYE,RQYE,RZRQYE,RZMRE,RZCHE,RZJME,RQMCL,RQCHL")
		params.Set("source", "WEB")
		params.Set("client", "WEB")
		params.Set("sortColumns", "DIM_DATE,SCDM")
		params.Set("sortTypes", "-1,1")
		params.Set("pageNumber", strconv.Itoa(page))
		params.Set("pageSize", "500")
		requestURL := endpoint + "?" + params.Encode()
		lastRequestURL = requestURL
		var payload struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Result  struct {
				Pages int `json:"pages"`
				Data  []struct {
					Date                         string  `json:"DIM_DATE"`
					FinancingBalance             float64 `json:"RZYE"`
					SecuritiesLendingBalance     float64 `json:"RQYE"`
					MarginBalance                float64 `json:"RZRQYE"`
					FinancingBuyAmount           float64 `json:"RZMRE"`
					FinancingRepayAmount         float64 `json:"RZCHE"`
					FinancingNetBuyAmount        float64 `json:"RZJME"`
					SecuritiesLendingSellVolume  float64 `json:"RQMCL"`
					SecuritiesLendingRepayVolume float64 `json:"RQCHL"`
				} `json:"data"`
			} `json:"result"`
		}
		if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
			return nil, foundation.SourceMeta{}, err
		}
		if !payload.Success {
			return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney margin balance unavailable: %s", firstString(payload.Message, "unknown response"))
		}
		for _, raw := range payload.Result.Data {
			tradeDate := strings.TrimSpace(strings.SplitN(raw.Date, " ", 2)[0])
			if tradeDate == "" {
				continue
			}
			point := pointsByDate[tradeDate]
			if point == nil {
				point = &foundation.MarketMarginPoint{TradeDate: tradeDate}
				pointsByDate[tradeDate] = point
			}
			point.FinancingBalance += raw.FinancingBalance
			point.SecuritiesLendingBalance += raw.SecuritiesLendingBalance
			point.MarginBalance += raw.MarginBalance
			point.FinancingBuyAmount += raw.FinancingBuyAmount
			point.FinancingRepayAmount += raw.FinancingRepayAmount
			point.FinancingNetBuyAmount += raw.FinancingNetBuyAmount
			point.SecuritiesLendingSellVolume += raw.SecuritiesLendingSellVolume
			point.SecuritiesLendingRepayVolume += raw.SecuritiesLendingRepayVolume
		}
		if len(pointsByDate) >= limit+1 || len(payload.Result.Data) < 500 || (payload.Result.Pages > 0 && page >= payload.Result.Pages) {
			break
		}
	}
	if len(pointsByDate) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney returned no margin balance history")
	}

	tradeDates := make([]string, 0, len(pointsByDate))
	for tradeDate := range pointsByDate {
		tradeDates = append(tradeDates, tradeDate)
	}
	sort.Strings(tradeDates)
	meta := foundation.SourceMeta{
		Source: "eastmoney:margin-balance", SourceURL: lastRequestURL,
		AvailableFields: []string{"financing_balance", "securities_lending_balance", "margin_balance", "margin_balance_change", "financing_buy_amount", "financing_repay_amount", "financing_net_buy_amount", "securities_lending_sell_volume", "securities_lending_repay_volume"},
		FetchedAt:       time.Now(), LatencyMS: time.Since(start).Milliseconds(), TradeDate: tradeDates[len(tradeDates)-1],
	}
	points := make([]foundation.MarketMarginPoint, 0, len(tradeDates))
	for _, tradeDate := range tradeDates {
		point := *pointsByDate[tradeDate]
		if len(points) > 0 {
			point.MarginBalanceChange = point.MarginBalance - points[len(points)-1].MarginBalance
		}
		point.Meta = meta
		points = append(points, point)
	}
	if len(points) > limit {
		points = points[len(points)-limit:]
	}
	return points, meta, nil
}

func (c *Client) MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	explicit := strings.TrimSpace(tradeDate) != ""
	date := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if explicit {
		parsed, err := time.ParseInLocation("2006-01-02", tradeDate, date.Location())
		if err != nil {
			return nil, foundation.SourceMeta{}, fmt.Errorf("invalid trade_date")
		}
		date = parsed
	}
	var lastMeta foundation.SourceMeta
	for attempt := 0; attempt < 8; attempt++ {
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			date = date.AddDate(0, 0, -1)
			continue
		}
		items, meta, err := c.fetchBillboardDate(ctx, date.Format("2006-01-02"), limit)
		lastMeta = meta
		if err != nil {
			return nil, foundation.SourceMeta{}, err
		}
		if len(items) > 0 || explicit {
			return items, meta, nil
		}
		date = date.AddDate(0, 0, -1)
	}
	return []foundation.MarketBillboardItem{}, lastMeta, nil
}

func (c *Client) fetchBillboardDate(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
	endpoint := c.datacenterBaseURL + "/api/data/v1/get"
	params := url.Values{}
	params.Set("reportName", "RPT_DAILYBILLBOARD_DETAILSNEW")
	params.Set("columns", "ALL")
	params.Set("filter", "(TRADE_DATE>='"+tradeDate+" 00:00:00')")
	params.Set("pageNumber", "1")
	params.Set("pageSize", strconv.Itoa(limit))
	params.Set("sortColumns", "BILLBOARD_NET_AMT")
	params.Set("sortTypes", "-1")
	params.Set("source", "WEB")
	params.Set("client", "WEB")
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Code    int    `json:"code"`
		Result  *struct {
			Data []map[string]any `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{Source: "eastmoney:billboard", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(), TradeDate: tradeDate}
	if !payload.Success {
		if isEmptyBillboardResponse(payload.Code, payload.Message) {
			return []foundation.MarketBillboardItem{}, meta, nil
		}
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney billboard: %s", payload.Message)
	}
	if payload.Result == nil {
		return []foundation.MarketBillboardItem{}, meta, nil
	}
	items := make([]foundation.MarketBillboardItem, 0, len(payload.Result.Data))
	seen := map[string]bool{}
	for _, raw := range payload.Result.Data {
		symbol := firstString(asString(raw["SECUCODE"]), asString(raw["SECURITY_CODE"]))
		key := symbol + "|" + asString(raw["EXPLANATION"])
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, foundation.MarketBillboardItem{
			TradeDate: tradeDate, Symbol: symbol, Name: asString(raw["SECURITY_NAME_ABBR"]), ClosePrice: asFloat(raw["CLOSE_PRICE"]), ChangePercent: asFloat(raw["CHANGE_RATE"]),
			TurnoverRate: asFloat(raw["TURNOVERRATE"]), Reason: asString(raw["EXPLANATION"]), Summary: asString(raw["EXPLAIN"]),
			BuyAmount: firstFloat(raw["BILLBOARD_BUY_AMT"], raw["SUM_BUY_AMT"]), SellAmount: firstFloat(raw["BILLBOARD_SELL_AMT"], raw["SUM_SELL_AMT"]),
			NetAmount: firstFloat(raw["BILLBOARD_NET_AMT"], raw["NET_BS_AMT"]), InstitutionBuyers: institutionCount(asString(raw["EXPLAIN"])),
			BuySeats: int(asFloat(raw["BUY_SEAT"])), SellSeats: int(asFloat(raw["SELL_SEAT"])), Meta: meta,
		})
	}
	return items, meta, nil
}

func isEmptyBillboardResponse(code int, message string) bool {
	if code == 9201 {
		return true
	}
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(message, "返回数据为空") || strings.Contains(message, "暂无数据") || strings.Contains(message, "no data")
}

func (c *Client) MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
	normalized, err := foundation.NormalizeSymbol(symbol)
	if err != nil {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, err
	}
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, fmt.Errorf("invalid trade_date")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, fmt.Errorf("reason is required")
	}

	start := time.Now()
	buySeats, buyURL, err := c.fetchBillboardSeats(ctx, normalized.Canonical, tradeDate, reason, "0")
	if err != nil {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, err
	}
	sellSeats, sellURL, err := c.fetchBillboardSeats(ctx, normalized.Canonical, tradeDate, reason, "1")
	if err != nil {
		return foundation.MarketBillboardDetail{}, foundation.SourceMeta{}, err
	}
	buySeats, sellSeats = c.enrichBillboardSeatLabels(ctx, normalized.Canonical, tradeDate, buySeats, sellSeats)
	meta := foundation.SourceMeta{
		Source:    "eastmoney:billboard-seats",
		SourceURL: buyURL + " | " + sellURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
		TradeDate: tradeDate,
	}
	detail := foundation.MarketBillboardDetail{
		TradeDate: tradeDate,
		Symbol:    normalized.Canonical,
		Reason:    reason,
		BuySeats:  buySeats,
		SellSeats: sellSeats,
		Meta:      meta,
	}
	return detail, meta, nil
}

func (c *Client) fetchBillboardSeats(ctx context.Context, symbol string, tradeDate string, reason string, direction string) ([]foundation.MarketBillboardSeat, string, error) {
	endpoint := c.f10BaseURL + "/api/data/v1/get"
	params := url.Values{}
	params.Set("reportName", "RPT_OPERATEDEPT_TRADE")
	params.Set("columns", "TRADE_DATE,EXPLANATION,OPERATEDEPT_NAME,BUY_AMT_REAL,BUY_RATIO,SELL_AMT_REAL,SELL_RATIO,TRADE_DIRECTION,RANK")
	params.Set("quoteColumns", "")
	params.Set("filter", fmt.Sprintf("(SECUCODE=\"%s\")(TRADE_DIRECTION=\"%s\")(TRADE_DATE='%s 00:00:00')(EXPLANATION=\"%s\")", escapeEastMoneyFilter(symbol), direction, tradeDate, escapeEastMoneyFilter(reason)))
	params.Set("pageNumber", "1")
	params.Set("pageSize", "5")
	params.Set("sortTypes", "1")
	params.Set("sortColumns", "RANK")
	params.Set("source", "HSF10")
	params.Set("client", "PC")
	requestURL := endpoint + "?" + params.Encode()
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  *struct {
			Data []map[string]any `json:"data"`
		} `json:"result"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, requestURL, err
	}
	if !payload.Success {
		return nil, requestURL, fmt.Errorf("eastmoney billboard seats: %s", payload.Message)
	}
	if payload.Result == nil {
		return []foundation.MarketBillboardSeat{}, requestURL, nil
	}
	seats := make([]foundation.MarketBillboardSeat, 0, len(payload.Result.Data))
	for _, raw := range payload.Result.Data {
		buyAmount := asFloat(raw["BUY_AMT_REAL"])
		sellAmount := asFloat(raw["SELL_AMT_REAL"])
		name := asString(raw["OPERATEDEPT_NAME"])
		seatDirection := "buy"
		if direction == "1" {
			seatDirection = "sell"
		}
		seats = append(seats, foundation.MarketBillboardSeat{
			Direction:   seatDirection,
			Rank:        int(asFloat(raw["RANK"])),
			Name:        name,
			BuyAmount:   buyAmount,
			BuyRatio:    asFloat(raw["BUY_RATIO"]),
			SellAmount:  sellAmount,
			SellRatio:   asFloat(raw["SELL_RATIO"]),
			NetAmount:   buyAmount - sellAmount,
			Institution: strings.Contains(name, "机构专用"),
		})
	}
	sort.SliceStable(seats, func(i, j int) bool { return seats[i].Rank < seats[j].Rank })
	if len(seats) > 5 {
		seats = seats[:5]
	}
	return seats, requestURL, nil
}

func escapeEastMoneyFilter(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\"", "\\\"")
}

func (c *Client) MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	endpoint := c.announcementBaseURL + "/api/security/ann"
	params := url.Values{}
	params.Set("sr", "-1")
	params.Set("page_size", strconv.Itoa(max(limit*2, 30)))
	params.Set("page_index", "1")
	params.Set("ann_type", "A")
	params.Set("client_source", "web")
	if symbol != "" {
		params.Set("stock_list", strings.Split(symbol, ".")[0])
	}
	if query != "" {
		params.Set("searchkey", query)
	}
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		Success int `json:"success"`
		Data    struct {
			List []struct {
				ID         string `json:"art_code"`
				Title      string `json:"title"`
				NoticeDate string `json:"notice_date"`
				Codes      []struct {
					Code string `json:"stock_code"`
					Name string `json:"short_name"`
				} `json:"codes"`
				Columns []struct {
					Name string `json:"column_name"`
				} `json:"columns"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if payload.Success != 1 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("eastmoney announcement request failed")
	}
	meta := foundation.SourceMeta{Source: "eastmoney:announcement", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	items := make([]foundation.MarketResearchItem, 0, min(limit, len(payload.Data.List)))
	for _, raw := range payload.Data.List {
		itemCategory := "其他"
		if len(raw.Columns) > 0 {
			itemCategory = raw.Columns[0].Name
		}
		if category != "" && category != "all" && !strings.Contains(itemCategory, category) {
			continue
		}
		stockCode, stockName := "", ""
		if len(raw.Codes) > 0 {
			stockCode, stockName = raw.Codes[0].Code, raw.Codes[0].Name
		}
		publishedAt := parseEastMoneyTime(raw.NoticeDate)
		items = append(items, foundation.MarketResearchItem{Kind: "announcement", ID: raw.ID, Symbol: stockCode, StockName: stockName, Title: raw.Title, Category: itemCategory, PublishedAt: publishedAt, URL: fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s/%s.html", stockCode, raw.ID), Meta: meta})
		if len(items) >= limit {
			break
		}
	}
	// The list endpoint only exposes titles. Load the announcement body as
	// well, otherwise event-driven theme attribution cannot see the target
	// company, ownership ratio or product terms hidden in the notice.
	var bodyWG sync.WaitGroup
	for index := range items {
		bodyWG.Add(1)
		go func(index int) {
			defer bodyWG.Done()
			content, contentErr := c.announcementContent(ctx, items[index].ID)
			if contentErr == nil {
				items[index].Content = truncateAnnouncementContent(content, 8_000)
			}
		}(index)
	}
	bodyWG.Wait()
	return items, meta, nil
}

func (c *Client) announcementContent(ctx context.Context, artCode string) (string, error) {
	base := c.announcementBaseURL
	if strings.Contains(base, "np-anotice-stock") {
		base = strings.Replace(base, "np-anotice-stock", "np-cnotice-stock", 1)
	}
	params := url.Values{}
	params.Set("art_code", strings.TrimSpace(artCode))
	params.Set("client_source", "web")
	params.Set("page_index", "1")
	requestURL := strings.TrimRight(base, "/") + "/api/content/ann?" + params.Encode()
	var payload struct {
		Data struct {
			NoticeContent string `json:"notice_content"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Data.NoticeContent), nil
}

func truncateAnnouncementContent(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}

func (c *Client) MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	qType := "0"
	if kind == "industry" {
		qType = "1"
	}
	now := time.Now()
	endpoint := c.reportBaseURL + "/report/list"
	params := url.Values{}
	params.Set("pageSize", strconv.Itoa(max(limit*3, 50)))
	params.Set("pageNo", "1")
	params.Set("qType", qType)
	params.Set("industryCode", "*")
	params.Set("code", "*")
	params.Set("beginTime", now.AddDate(0, 0, -45).Format("2006-01-02"))
	params.Set("endTime", now.Format("2006-01-02"))
	if symbol != "" {
		params.Set("code", strings.Split(symbol, ".")[0])
	}
	requestURL := endpoint + "?" + params.Encode()
	start := time.Now()
	var payload struct {
		Data []struct {
			Title          string `json:"title"`
			StockName      string `json:"stockName"`
			StockCode      string `json:"stockCode"`
			Organization   string `json:"orgSName"`
			PublishDate    string `json:"publishDate"`
			InfoCode       string `json:"infoCode"`
			IndustryCode   string `json:"industryCode"`
			IndustryName   string `json:"industryName"`
			Rating         string `json:"emRatingName"`
			PreviousRating string `json:"lastEmRatingName"`
			RatingChange   any    `json:"ratingChange"`
			Researcher     string `json:"researcher"`
			TargetHigh     any    `json:"indvAimPriceT"`
			TargetLow      any    `json:"indvAimPriceL"`
			EPS            any    `json:"predictThisYearEps"`
			PE             any    `json:"predictThisYearPe"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{Source: "eastmoney:report", SourceURL: requestURL, FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	items := make([]foundation.MarketResearchItem, 0, min(limit, len(payload.Data)))
	query = strings.ToLower(strings.TrimSpace(query))
	industry = strings.ToLower(strings.TrimSpace(industry))
	for _, raw := range payload.Data {
		haystack := strings.ToLower(strings.Join([]string{raw.Title, raw.StockName, raw.StockCode, raw.Organization, raw.IndustryName, raw.Researcher}, " "))
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		if industry != "" && !strings.Contains(strings.ToLower(raw.IndustryName), industry) && !strings.Contains(strings.ToLower(raw.IndustryCode), industry) {
			continue
		}
		urlValue := fmt.Sprintf("https://data.eastmoney.com/report/info/%s.html", raw.InfoCode)
		if kind == "industry" {
			urlValue = fmt.Sprintf("https://data.eastmoney.com/report/zw_industry.jshtml?infocode=%s", raw.InfoCode)
		}
		items = append(items, foundation.MarketResearchItem{
			Kind: firstString(kind, "stock"), ID: raw.InfoCode, Symbol: raw.StockCode, StockName: raw.StockName, IndustryCode: raw.IndustryCode, IndustryName: raw.IndustryName,
			Title: raw.Title, Organization: raw.Organization, Researchers: raw.Researcher, Rating: raw.Rating, PreviousRating: raw.PreviousRating,
			RatingChange: asString(raw.RatingChange), TargetLow: asFloat(raw.TargetLow), TargetHigh: asFloat(raw.TargetHigh), EPS: asFloat(raw.EPS), PE: asFloat(raw.PE),
			PublishedAt: parseEastMoneyTime(raw.PublishDate), URL: urlValue, Meta: meta,
		})
		if len(items) >= limit {
			break
		}
	}
	return items, meta, nil
}

func standardListParams(limit int) url.Values {
	params := url.Values{}
	params.Set("pn", "1")
	params.Set("pz", strconv.Itoa(limit))
	params.Set("po", "1")
	params.Set("np", "1")
	params.Set("ut", "bd1d9ddb04089700cf9c27f6f7426281")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	return params
}

func findMarketIndex(id string) (marketIndexDefinition, bool) {
	for _, item := range marketIndexCatalog {
		if item.ID == strings.ToLower(strings.TrimSpace(id)) {
			return item, true
		}
	}
	return marketIndexDefinition{}, false
}

func indexStatus(tradeTime, now time.Time) string {
	if tradeTime.IsZero() {
		return "unknown"
	}
	if now.Sub(tradeTime) >= 0 && now.Sub(tradeTime) <= 20*time.Minute {
		return "open"
	}
	return "closed"
}

func scoreMomentum(change, fiveDay, twentyDay, flow float64, rising, falling int) float64 {
	breadth := 0.0
	if rising+falling > 0 {
		breadth = float64(rising-falling) / float64(rising+falling) * 20
	}
	flowScore := 0.0
	if flow > 0 {
		flowScore = min(20, flow/100_000_000)
	} else {
		flowScore = max(-20, flow/100_000_000)
	}
	score := 50 + change*4 + fiveDay*1.5 + twentyDay*.5 + breadth + flowScore
	return max(0, min(100, score))
}

func asFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	case fmt.Stringer:
		parsed, _ := strconv.ParseFloat(typed.String(), 64)
		return parsed
	default:
		return 0
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstFloat(values ...any) float64 {
	for _, value := range values {
		if parsed := asFloat(value); parsed != 0 {
			return parsed
		}
	}
	return 0
}

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return fmt.Errorf("upstream request failed")
}

func institutionCount(summary string) int {
	index := strings.Index(summary, "家机构")
	if index <= 0 {
		return 0
	}
	start := index - 1
	for start > 0 && summary[start-1] >= '0' && summary[start-1] <= '9' {
		start--
	}
	count, _ := strconv.Atoi(summary[start:index])
	return count
}

func parseEastMoneyTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
