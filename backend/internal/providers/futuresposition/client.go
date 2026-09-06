package futuresposition

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var varieties = map[string]struct{ name, index string }{
	"IF": {"沪深300股指期货", "000300.SH"},
	"IH": {"上证50股指期货", "000016.SH"},
	"IC": {"中证500股指期货", "000905.SH"},
	"IM": {"中证1000股指期货", "000852.SH"},
}

var contractPattern = regexp.MustCompile(`^(IF|IH|IC|IM)[0-9]{2}(0[1-9]|1[0-2])$`)

func ValidVariety(variety string) bool   { _, ok := varieties[variety]; return ok }
func ValidContract(contract string) bool { return contractPattern.MatchString(contract) }

type Client struct {
	http              *http.Client
	dataURL, cffexURL string
	now               func() time.Time
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 8 * time.Second}, dataURL: "https://datacenter-web.eastmoney.com", cffexURL: "http://www.cffex.com.cn", now: time.Now}
}

func (c *Client) Trend(ctx context.Context, variety string, limit int) (foundation.MarketFuturesPositionSeries, error) {
	variety = strings.ToUpper(strings.TrimSpace(variety))
	if !ValidVariety(variety) || limit < 1 || limit > 250 {
		return foundation.MarketFuturesPositionSeries{}, fmt.Errorf("invalid futures variety or limit")
	}
	start := time.Now()
	series := foundation.MarketFuturesPositionSeries{Variety: variety, VarietyName: varieties[variety].name, IndexCode: varieties[variety].index}
	// Reserve time for the independent exchange fallback; never guess a contract
	// from the calendar month (rollover can already have happened).
	primaryCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	contracts, _, err := c.report(primaryCtx, "RPT_FUTU_POSITIONCODE", fmt.Sprintf(`(TRADE_CODE="%s")(IS_MAINCODE="1")`, variety), 5)
	if err == nil {
		for _, row := range contracts {
			code := rawString(row["SECURITY_CODE"])
			if ValidContract(code) && strings.HasPrefix(code, variety) {
				series.ContractCode = code
				break
			}
		}
		if series.ContractCode == "" {
			err = fmt.Errorf("主力合约目录为空")
		}
	}
	if err == nil {
		var rows []map[string]json.RawMessage
		var sourceURL string
		rows, sourceURL, err = c.report(primaryCtx, "RPT_FUTU_NET_POSITION", fmt.Sprintf(`(SECURITY_CODE="%s")`, series.ContractCode), limit)
		if err == nil {
			series.Rows, err = parseTrend(rows)
			if err == nil {
				series.Meta = foundation.SourceMeta{Source: "eastmoney:futures-position", SourceURL: sourceURL}
			}
		}
	}
	cancel()
	if err != nil {
		// A bounded fallback supplies the latest exchange snapshot, not a made-up
		// full history. Rank changes use the exchange's published values.
		members, fallbackErr := c.latestMembers(ctx, variety, series.ContractCode)
		if fallbackErr != nil {
			return series, fmt.Errorf("东方财富期指持仓不可用：%v；中金所备用数据不可用：%w", err, fallbackErr)
		}
		series.ContractCode = members.ContractCode
		point := foundation.MarketFuturesPositionRow{TradeDate: members.TradeDate}
		var longChange, shortChange int64
		longComplete, shortComplete := true, true
		for _, member := range members.Members {
			point.LongPosition += member.LongPosition
			point.ShortPosition += member.ShortPosition
			if member.LongChange == nil {
				longComplete = false
			} else {
				longChange += *member.LongChange
			}
			if member.ShortChange == nil {
				shortComplete = false
			} else {
				shortChange += *member.ShortChange
			}
		}
		if longComplete {
			point.LongChange = &longChange
		}
		if shortComplete {
			point.ShortChange = &shortChange
		}
		point.NetPosition = point.LongPosition - point.ShortPosition
		series.Rows = []foundation.MarketFuturesPositionRow{point}
		series.Meta = members.Meta
		series.Meta.FallbackReason = "东方财富期指数据不可用，降级为中金所最近交易日快照；仅提供单日持仓，不提供历史走势、指数或基差"
	}
	series.Meta.TradeDate = series.Rows[len(series.Rows)-1].TradeDate
	series.Meta.FetchedAt = c.now()
	series.Meta.LatencyMS = time.Since(start).Milliseconds()
	return series, nil
}

func (c *Client) report(ctx context.Context, report, filter string, limit int) ([]map[string]json.RawMessage, string, error) {
	params := url.Values{"reportName": {report}, "columns": {"ALL"}, "pageSize": {strconv.Itoa(limit)}, "pageNumber": {"1"}, "filter": {filter}, "source": {"WEB"}, "client": {"WEB"}}
	if report == "RPT_FUTU_NET_POSITION" {
		params.Set("sortColumns", "TRADE_DATE")
		params.Set("sortTypes", "-1")
	}
	u := c.dataURL + "/api/data/v1/get?" + params.Encode()
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, u, err
	}
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Result  struct {
			Data []map[string]json.RawMessage `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, u, err
	}
	if !payload.Success || len(payload.Result.Data) == 0 {
		return nil, u, fmt.Errorf("%s: %s (no data)", report, payload.Message)
	}
	return payload.Result.Data, u, nil
}

func parseTrend(data []map[string]json.RawMessage) ([]foundation.MarketFuturesPositionRow, error) {
	rows := make([]foundation.MarketFuturesPositionRow, 0, len(data))
	seen := map[string]bool{}
	for _, raw := range data {
		date := rawString(raw["TRADE_DATE"])
		if len(date) < 10 {
			continue
		}
		date = date[:10]
		if _, err := time.Parse("2006-01-02", date); err != nil || seen[date] {
			continue
		}
		long, short := integer(raw["TOTAL_LONG_POSITION"]), integer(raw["TOTAL_SHORT_POSITION"])
		if long == nil || short == nil || *long < 0 || *short < 0 {
			continue
		}
		seen[date] = true
		rows = append(rows, foundation.MarketFuturesPositionRow{TradeDate: date, LongPosition: *long, LongChange: integer(raw["LP_CHANGE_TOTAL"]), ShortPosition: *short, ShortChange: integer(raw["SP_CHANGE_TOTAL"]), NetPosition: *long - *short, SettlePrice: number(raw["SETTLE_PRICE"]), IndexClose: number(raw["CLOSE_PRICE"]), IndexChange: number(raw["CLOSE_PRICE_CHANGE"]), Basis: number(raw["BASIS"])})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("期指持仓没有有效交易日记录")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TradeDate < rows[j].TradeDate })
	return rows, nil
}

func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

func number(raw json.RawMessage) *float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(rawString(raw), ",", ""), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func integer(raw json.RawMessage) *int64 {
	v := number(raw)
	if v == nil || *v != math.Trunc(*v) || math.Abs(*v) > 1e12 {
		return nil
	}
	i := int64(*v)
	return &i
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if strings.HasPrefix(u, c.cffexURL+"/") {
		req.Header.Set("Referer", c.cffexURL+"/")
	} else {
		req.Header.Set("Referer", "https://data.eastmoney.com/")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func (c *Client) Members(ctx context.Context, contract, tradeDate string) (foundation.MarketFuturesMembers, error) {
	if !ValidContract(contract) {
		return foundation.MarketFuturesMembers{}, fmt.Errorf("invalid futures contract")
	}
	d, err := time.Parse("2006-01-02", tradeDate)
	if err != nil {
		return foundation.MarketFuturesMembers{}, err
	}
	return c.membersAt(ctx, contract[:2], contract, d)
}

// Consensus returns the CFFEX community convention: all contracts of IF/IH/IC/IM,
// top-20 member positions, and signed net-long values (positive = long,
// negative = short). CITIC is the exact 中信期货(代客) member row, not a fuzzy
// match of other 中信 entities. Legacy net-short fields are also populated for
// compatibility, but the UI and AI use the net-long fields.
func (c *Client) Consensus(ctx context.Context, tradeDate string) (foundation.MarketFuturesConsensus, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	if strings.TrimSpace(tradeDate) != "" {
		d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(tradeDate), zone)
		if err != nil {
			return foundation.MarketFuturesConsensus{}, err
		}
		return c.consensusAt(ctx, d)
	}
	for i := 0; i < 15; i++ {
		d := c.now().In(zone).AddDate(0, 0, -i)
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		result, err := c.consensusAt(ctx, d)
		if err == nil {
			return result, nil
		}
	}
	return foundation.MarketFuturesConsensus{}, fmt.Errorf("近15个自然日未获取到完整四品种共识数据")
}

func (c *Client) consensusAt(ctx context.Context, date time.Time) (foundation.MarketFuturesConsensus, error) {
	start := time.Now()
	result := foundation.MarketFuturesConsensus{TradeDate: date.Format("2006-01-02")}
	for _, variety := range []string{"IF", "IH", "IC", "IM"} {
		requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		body, err := c.get(requestCtx, fmt.Sprintf("%s/sj/ccpm/%s/%s/%s_1.csv", c.cffexURL, date.Format("200601"), date.Format("02"), variety))
		cancel()
		if err != nil {
			return foundation.MarketFuturesConsensus{}, fmt.Errorf("%s: %w", variety, err)
		}
		item, err := parseConsensusCSV(body, variety, result.TradeDate)
		if err != nil {
			return foundation.MarketFuturesConsensus{}, fmt.Errorf("%s: %w", variety, err)
		}
		result.Varieties = append(result.Varieties, item)
		result.Top20NetShortPosition += item.NetShortPosition
		result.Top20NetLongPosition += item.NetLongPosition
		result.Top20NetShortChange += item.NetShortChange
		result.Top20NetLongChange += item.NetLongChange
		result.CITICNetShortChange += item.CITICNetShortChange
		result.CITICNetLongChange += item.CITICNetLongChange
	}
	result.Meta = foundation.SourceMeta{Source: "cffex:futures-consensus", TradeDate: result.TradeDate, FetchedAt: c.now(), LatencyMS: time.Since(start).Milliseconds()}
	return result, nil
}

func parseConsensusCSV(body []byte, variety, date string) (foundation.MarketFuturesConsensusVariety, error) {
	if !utf8.Valid(body) {
		decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body)
		if err != nil {
			return foundation.MarketFuturesConsensusVariety{}, err
		}
		body = decoded
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return foundation.MarketFuturesConsensusVariety{}, err
	}
	byContract := map[string]map[int]bool{}
	var item foundation.MarketFuturesConsensusVariety
	item.Variety, item.TradeDate = variety, date
	normalizeName := func(value string) string { return strings.Join(strings.Fields(value), "") }
	for _, rec := range records {
		if len(rec) < 12 {
			continue
		}
		rowDate := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(rec[0]), "-", ""), "/", "")
		if rowDate != strings.ReplaceAll(date, "-", "") {
			continue
		}
		contract := strings.ToUpper(strings.TrimSpace(rec[1]))
		if !ValidContract(contract) || !strings.HasPrefix(contract, variety) {
			continue
		}
		rank, rankErr := strconv.Atoi(strings.TrimSpace(rec[2]))
		long, short := integer(json.RawMessage(rec[7])), integer(json.RawMessage(rec[10]))
		longChange, shortChange := integer(json.RawMessage(rec[8])), integer(json.RawMessage(rec[11]))
		if rankErr != nil || rank < 1 || rank > 20 || long == nil || short == nil || longChange == nil || shortChange == nil || *long < 0 || *short < 0 {
			return foundation.MarketFuturesConsensusVariety{}, fmt.Errorf("%s 会员排名字段不完整", contract)
		}
		if byContract[contract] == nil {
			byContract[contract] = map[int]bool{}
		}
		if byContract[contract][rank] {
			return foundation.MarketFuturesConsensusVariety{}, fmt.Errorf("%s 会员排名重复", contract)
		}
		byContract[contract][rank] = true
		item.LongPosition += *long
		item.ShortPosition += *short
		item.LongChange += *longChange
		item.ShortChange += *shortChange
		if normalizeName(rec[6]) == "中信期货(代客)" {
			item.CITICNetShortChange -= *longChange
		}
		if normalizeName(rec[9]) == "中信期货(代客)" {
			item.CITICNetShortChange += *shortChange
		}
	}
	if len(byContract) == 0 {
		return foundation.MarketFuturesConsensusVariety{}, fmt.Errorf("未找到有效合约")
	}
	for contract, ranks := range byContract {
		if len(ranks) != 20 {
			return foundation.MarketFuturesConsensusVariety{}, fmt.Errorf("%s 会员排名不足20名", contract)
		}
		item.ContractCount++
	}
	item.NetShortPosition = item.ShortPosition - item.LongPosition
	item.NetLongPosition = item.LongPosition - item.ShortPosition
	item.NetShortChange = item.ShortChange - item.LongChange
	item.NetLongChange = item.LongChange - item.ShortChange
	item.CITICNetLongChange = -item.CITICNetShortChange
	return item, nil
}

func (c *Client) membersAt(ctx context.Context, variety, contract string, date time.Time) (foundation.MarketFuturesMembers, error) {
	u := fmt.Sprintf("%s/sj/ccpm/%s/%s/%s_1.csv", c.cffexURL, date.Format("200601"), date.Format("02"), variety)
	start := time.Now()
	body, err := c.get(ctx, u)
	if err != nil {
		return foundation.MarketFuturesMembers{}, err
	}
	if !utf8.Valid(body) {
		body, err = simplifiedchinese.GB18030.NewDecoder().Bytes(body)
		if err != nil {
			return foundation.MarketFuturesMembers{}, err
		}
	}
	ranks, err := parseRanks(string(body), variety, contract, date.Format("2006-01-02"))
	if err != nil {
		return foundation.MarketFuturesMembers{}, err
	}
	return foundation.MarketFuturesMembers{ContractCode: ranks[0].Contract, TradeDate: date.Format("2006-01-02"), Members: ranks, Meta: foundation.SourceMeta{Source: "cffex:futures-position", SourceURL: u, TradeDate: date.Format("2006-01-02"), FetchedAt: c.now(), LatencyMS: time.Since(start).Milliseconds()}}, nil
}

func parseRanks(body, variety, contract, date string) ([]foundation.MarketFuturesMemberRank, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\ufeff")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	byContract := map[string][]foundation.MarketFuturesMemberRank{}
	volumes := map[string]int64{}
	for _, rec := range records {
		if len(rec) < 12 {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(rec[1]))
		rowDate := strings.ReplaceAll(strings.TrimSpace(rec[0]), "-", "")
		rowDate = strings.ReplaceAll(rowDate, "/", "")
		if rowDate != strings.ReplaceAll(date, "-", "") || !ValidContract(code) || !strings.HasPrefix(code, variety) || (contract != "" && code != contract) {
			continue
		}
		rank, err := strconv.Atoi(strings.TrimSpace(rec[2]))
		if err != nil || rank < 1 || rank > 20 {
			continue
		}
		long, short := integer(json.RawMessage(rec[7])), integer(json.RawMessage(rec[10]))
		if long == nil || short == nil || *long < 0 || *short < 0 {
			continue
		}
		if volume := integer(json.RawMessage(rec[4])); volume != nil {
			volumes[code] += *volume
		}
		byContract[code] = append(byContract[code], foundation.MarketFuturesMemberRank{Contract: code, Rank: rank, LongName: strings.TrimSpace(rec[6]), LongPosition: *long, LongChange: integer(json.RawMessage(rec[8])), ShortName: strings.TrimSpace(rec[9]), ShortPosition: *short, ShortChange: integer(json.RawMessage(rec[11]))})
	}
	if contract == "" {
		// Only used when the primary contract directory failed. Label the actual
		// exchange contract selected by volume, never mix contracts across dates.
		for code := range byContract {
			if contract == "" || volumes[code] > volumes[contract] || (volumes[code] == volumes[contract] && code < contract) {
				contract = code
			}
		}
	}
	ranks := byContract[contract]
	if len(ranks) == 0 {
		return nil, fmt.Errorf("%s %s 无该合约会员排名", date, contract)
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i].Rank < ranks[j].Rank })
	// A truncated or duplicate ranking must not be reported as the top 20 sum.
	if len(ranks) != 20 {
		return nil, fmt.Errorf("%s %s 会员排名不完整", date, contract)
	}
	for i, rank := range ranks {
		if rank.Rank != i+1 {
			return nil, fmt.Errorf("会员排名重复或缺失")
		}
	}
	return ranks, nil
}

func (c *Client) latestMembers(ctx context.Context, variety, contract string) (foundation.MarketFuturesMembers, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	var dates []time.Time
	for i := 0; i < 14; i++ {
		d := c.now().In(zone).AddDate(0, 0, -i)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d)
		}
	}
	// Four bounded requests per batch. Consume results in date order so a fast
	// older response cannot replace the latest published session.
	for offset := 0; offset < len(dates) && ctx.Err() == nil; offset += 4 {
		n := min(4, len(dates)-offset)
		results := make([]chan foundation.MarketFuturesMembers, n)
		for i := 0; i < n; i++ {
			results[i] = make(chan foundation.MarketFuturesMembers, 1)
			go func(i int, date time.Time) {
				requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				data, _ := c.membersAt(requestCtx, variety, contract, date)
				results[i] <- data
			}(i, dates[offset+i])
		}
		for _, result := range results {
			if data := <-result; len(data.Members) > 0 {
				return data, nil
			}
		}
	}
	return foundation.MarketFuturesMembers{}, fmt.Errorf("近14个自然日未获取到完整持仓排名")
}
