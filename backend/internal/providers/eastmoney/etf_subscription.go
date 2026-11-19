package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/nationalteam"
)

type subscriptionDataCache struct {
	mu        sync.Mutex
	revision  string
	files     map[string]nationalteam.SubscriptionRaw
	calendars map[int][]string
	closes    map[string]map[string]float64
}

func WithETFSubscriptionSources(snapshot, sse, szse string) Option {
	return func(c *Client) {
		c.subscriptionBaseURL = strings.TrimRight(snapshot, "/")
		c.subscriptionSSEURL = sse
		c.subscriptionSZSEURL = szse
	}
}

func subscriptionPairs(pairs [][]json.RawMessage, nonnegative bool) map[string]float64 {
	result := map[string]float64{}
	for _, pair := range pairs {
		if len(pair) != 2 {
			continue
		}
		var day string
		var value *float64
		if json.Unmarshal(pair[0], &day) != nil || json.Unmarshal(pair[1], &value) != nil {
			continue
		}
		if _, err := time.Parse(time.DateOnly, day); err != nil || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || (nonnegative && *value < 0) {
			continue
		}
		result[day] = *value
	}
	return result
}

func (c *Client) subscriptionSourceFiles(ctx context.Context, base, revision string, firstYear, lastYear int) (map[string]nationalteam.SubscriptionRaw, []string, []string) {
	cache := &c.subscriptionData
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.revision != revision {
		cache.revision = revision
		cache.files = map[string]nationalteam.SubscriptionRaw{}
		cache.calendars = map[int][]string{}
		cache.closes = map[string]map[string]float64{}
	}
	type job struct {
		key, code string
		year      int
	}
	jobs := []job{}
	for year := firstYear; year <= lastYear; year++ {
		if _, ok := cache.calendars[year]; !ok {
			var calendar struct {
				Dates []string `json:"dates"`
			}
			requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.getJSON(requestCtx, fmt.Sprintf("%s/industry/0/%d.json", base, year), &calendar)
			cancel()
			if err == nil && len(calendar.Dates) > 0 {
				cache.calendars[year] = calendar.Dates
			}
		}
		for _, item := range nationalteam.SubscriptionCatalog() {
			key := item.Code + "/" + strconv.Itoa(year)
			if _, ok := cache.files[key]; !ok {
				jobs = append(jobs, job{key: key, code: item.Code, year: year})
			}
		}
	}
	results := make([]nationalteam.SubscriptionRaw, len(jobs))
	success := make([]bool, len(jobs))
	work := make(chan int, len(jobs))
	for i := range jobs {
		work <- i
	}
	close(work)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				var payload struct {
					Shares [][]json.RawMessage `json:"shares"`
					Prices [][]json.RawMessage `json:"prices"`
				}
				requestCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
				err := c.getJSON(requestCtx, fmt.Sprintf("%s/series/%s/%d.json", base, jobs[i].code, jobs[i].year), &payload)
				cancel()
				if err == nil {
					results[i] = nationalteam.SubscriptionRaw{Shares: subscriptionPairs(payload.Shares, true), Prices: subscriptionPairs(payload.Prices, true)}
					success[i] = len(results[i].Shares) > 0
				}
			}
		}()
	}
	wg.Wait()
	for i, item := range jobs {
		if success[i] {
			cache.files[item.key] = results[i]
		}
	}
	raw := map[string]nationalteam.SubscriptionRaw{}
	dates := map[string]bool{}
	warnings := []string{}
	for year := firstYear; year <= lastYear; year++ {
		for _, day := range cache.calendars[year] {
			if _, err := time.Parse(time.DateOnly, day); err == nil {
				dates[day] = true
			}
		}
	}
	for _, item := range nationalteam.SubscriptionCatalog() {
		merged := nationalteam.SubscriptionRaw{Shares: map[string]float64{}, Prices: map[string]float64{}}
		for year := firstYear; year <= lastYear; year++ {
			key := item.Code + "/" + strconv.Itoa(year)
			values, ok := cache.files[key]
			if !ok {
				warnings = append(warnings, item.Code+" 的 "+strconv.Itoa(year)+" 年历史暂不可用")
				continue
			}
			for day, value := range values.Shares {
				merged.Shares[day] = value
				dates[day] = true
			}
			for day, value := range values.Prices {
				merged.Prices[day] = value
			}
		}
		raw[item.Code] = merged
	}
	calendar := make([]string, 0, len(dates))
	for day := range dates {
		calendar = append(calendar, day)
	}
	sort.Strings(calendar)
	return raw, calendar, warnings
}

func (c *Client) subscriptionQuotes(ctx context.Context) (map[string]foundation.ETFSubscriptionSeries, error) {
	ids := []string{}
	for _, item := range nationalteam.SubscriptionCatalog() {
		prefix := "1."
		if item.Exchange == "SZSE" {
			prefix = "0."
		}
		ids = append(ids, prefix+item.Code)
	}
	params := url.Values{"secids": {strings.Join(ids, ",")}, "fields": {"f12,f2,f3,f124"}, "fltt": {"2"}}
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Diff []map[string]any `json:"diff"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, c.quoteBaseURL+"/api/qt/ulist.np/get?"+params.Encode(), &payload); err != nil {
		return nil, err
	}
	result := map[string]foundation.ETFSubscriptionSeries{}
	now := time.Now()
	for _, row := range payload.Data.Diff {
		code := asString(row["f12"])
		item := foundation.ETFSubscriptionSeries{Code: code, LatestPrice: etfNumber(row["f2"]), ChangePercent: etfNumber(row["f3"])}
		if item.LatestPrice != nil && *item.LatestPrice <= 0 {
			item.LatestPrice = nil
		}
		if stamp := etfNumber(row["f124"]); stamp != nil && *stamp > 946684800 && *stamp <= float64(now.Add(time.Minute).Unix()) {
			t := time.Unix(int64(*stamp), 0).In(nationalteam.China)
			item.QuoteTime = &t
		}
		result[code] = item
	}
	if payload.RC != 0 || len(result) == 0 {
		return nil, fmt.Errorf("ETF quotes unavailable")
	}
	return result, nil
}

func (c *Client) subscriptionSZSEShares(ctx context.Context, endpoint, code, start, end string) (map[string]float64, error) {
	params := url.Values{"SHOWTYPE": {"JSON"}, "CATALOGID": {"fund_jjgm"}, "TABKEY": {"tab1"}, "txtDm": {code}, "txtStart": {start}, "txtEnd": {end}, "PAGENO": {"1"}}
	var payload []struct {
		Meta struct {
			PageCount int `json:"pagecount"`
		} `json:"metadata"`
		Data []struct {
			Date string `json:"size_date"`
			Code string `json:"fund_code"`
			Size string `json:"current_size"`
		} `json:"data"`
	}
	if err := c.getJSONWithHeaders(ctx, endpoint+"?"+params.Encode(), map[string]string{"Referer": "https://fund.szse.cn/"}, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty SZSE report")
	}
	if payload[0].Meta.PageCount > 1 {
		return nil, fmt.Errorf("SZSE recent window unexpectedly paginated")
	}
	result := map[string]float64{}
	for _, row := range payload[0].Data {
		if row.Code != code || row.Date < start || row.Date > end {
			continue
		}
		if _, err := time.Parse(time.DateOnly, row.Date); err != nil {
			continue
		}
		if amount := etfNumber(strings.ReplaceAll(row.Size, ",", "")); amount != nil && *amount >= 0 {
			result[row.Date] = *amount * 10000
		}
	}
	return result, nil
}

func (c *Client) ETFSubscriptions(ctx context.Context) (foundation.ETFSubscriptionDashboard, foundation.SourceMeta, error) {
	return c.etfSubscriptionsAt(ctx, time.Now())
}

func (c *Client) etfSubscriptionsAt(ctx context.Context, now time.Time) (foundation.ETFSubscriptionDashboard, foundation.SourceMeta, error) {
	started := time.Now()
	local := now.In(nationalteam.China)
	today := local.Format(time.DateOnly)
	base := c.subscriptionBaseURL
	if base == "" {
		base = "https://h-jett.github.io/etf-radar/data"
	}
	sse := c.subscriptionSSEURL
	if sse == "" {
		sse = "https://query.sse.com.cn/commonQuery.do"
	}
	szse := c.subscriptionSZSEURL
	if szse == "" {
		szse = "https://fund.szse.cn/api/report/ShowReport/data"
	}
	var source struct {
		Date       string `json:"trade_date"`
		Generated  string `json:"generated_at_iso"`
		ReportDate string `json:"report_date"`
	}
	requestCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	err := c.getJSON(requestCtx, base+"/meta.json", &source)
	cancel()
	if err != nil {
		return foundation.ETFSubscriptionDashboard{}, foundation.SourceMeta{}, err
	}
	if _, err := time.Parse(time.DateOnly, source.Date); err != nil || source.Date > today {
		return foundation.ETFSubscriptionDashboard{}, foundation.SourceMeta{}, fmt.Errorf("invalid public ETF snapshot date")
	}
	start := nationalteam.SubscriptionYearStart(now)
	if source.Date < start {
		return foundation.ETFSubscriptionDashboard{}, foundation.SourceMeta{}, fmt.Errorf("public ETF snapshot is older than the requested year")
	}
	from, _ := time.Parse(time.DateOnly, start)
	to, _ := time.Parse(time.DateOnly, source.Date)
	raw, calendar, warnings := c.subscriptionSourceFiles(ctx, base, source.Date+"|"+source.Generated, from.AddDate(0, 0, -14).Year(), to.Year())
	if len(calendar) < 2 {
		return foundation.ETFSubscriptionDashboard{}, foundation.SourceMeta{}, fmt.Errorf("ETF share calendar unavailable")
	}
	previous := ""
	for _, day := range calendar {
		if day < source.Date {
			previous = day
		}
	}
	// Verify every date since the snapshot, including yesterday when the snapshot
	// publication lags. A weekday is only a query candidate, never a trading-day
	// assumption; only a dated exchange response adds it to the calendar.
	sseDates := []string{source.Date}
	if previous != "" {
		sseDates = append(sseDates, previous)
	}
	for day := to.AddDate(0, 0, 1); day.Format(time.DateOnly) <= today; day = day.AddDate(0, 0, 1) {
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			sseDates = append(sseDates, day.Format(time.DateOnly))
		}
	}
	type officialResult struct {
		code, date string
		values     map[string]float64
		err        error
	}
	official := make([]officialResult, len(sseDates))
	quotes := map[string]foundation.ETFSubscriptionSeries{}
	var quoteErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		requestCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		quotes, quoteErr = c.subscriptionQuotes(requestCtx)
	}()
	sseWork := make(chan int, len(sseDates))
	for i := range sseDates {
		sseWork <- i
	}
	close(sseWork)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range sseWork {
				day := sseDates[i]
				requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
				values, err := c.nationalTeamShareDay(requestCtx, sse, day)
				cancel()
				converted := map[string]float64{}
				for code, value := range values {
					converted[code] = value * 10000
				}
				official[i] = officialResult{date: day, values: converted, err: err}
			}
		}()
	}
	szItems := []foundation.ETFSubscriptionSeries{}
	for _, item := range nationalteam.SubscriptionCatalog() {
		if item.Exchange == "SZSE" {
			szItems = append(szItems, item)
		}
	}
	szResults := make([]officialResult, len(szItems))
	work := make(chan int, len(szItems))
	for i := range szItems {
		work <- i
	}
	close(work)
	recent := source.Date
	if previous != "" {
		recent = previous
	}
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
				values, err := c.subscriptionSZSEShares(requestCtx, szse, szItems[i].Code, recent, today)
				cancel()
				szResults[i] = officialResult{code: szItems[i].Code, values: values, err: err}
			}
		}()
	}
	wg.Wait()
	checks := []foundation.ETFSubscriptionCheck{}
	dateSet := map[string]bool{}
	for _, day := range calendar {
		if day <= source.Date {
			dateSet[day] = true
		}
	}
	apply := func(code, day string, shares float64) {
		values, ok := raw[code]
		if !ok {
			return
		}
		shares = math.Round(shares)
		check := foundation.ETFSubscriptionCheck{Code: code, Date: day, OfficialShares: &shares, Status: "交易所已核验"}
		if snapshot, exists := values.Shares[day]; exists {
			snapshot = math.Round(snapshot)
			check.SnapshotShares = &snapshot
			if math.Abs(snapshot-shares) > 1 {
				check.Status = "快照差异，已按交易所修订"
			}
		} else {
			check.Status = "交易所新增披露"
		}
		values.Shares[day] = shares
		raw[code] = values
		dateSet[day] = true
		checks = append(checks, check)
	}
	for _, row := range official {
		if row.err != nil {
			warnings = append(warnings, "上交所 "+row.date+" 直接核验暂不可用")
			continue
		}
		for _, item := range nationalteam.SubscriptionCatalog() {
			if item.Exchange == "SSE" {
				if shares, ok := row.values[item.Code]; ok {
					apply(item.Code, row.date, shares)
				}
			}
		}
	}
	for _, row := range szResults {
		if row.err != nil {
			warnings = append(warnings, row.code+" 深交所直接核验暂不可用")
			continue
		}
		for day, shares := range row.values {
			apply(row.code, day, shares)
		}
	}
	if quoteErr != nil {
		warnings = append(warnings, "盘中价格暂不可用；净申购估算仍使用对应日期的历史收盘价")
	}
	// Independently retrieve matching-date unadjusted daily closes. Today's latest
	// quote cannot fill yesterday's price, even when yesterday's shares are known.
	warnings = append(warnings, c.subscriptionBackfillCloses(ctx, raw, dateSet, start, now)...)
	calendar = calendar[:0]
	for day := range dateSet {
		if day <= today {
			calendar = append(calendar, day)
		}
	}
	sort.Strings(calendar)
	end := source.Date
	for _, day := range calendar {
		if day > end {
			end = day
		}
	}
	result := nationalteam.BuildSubscriptions(raw, calendar, start, end, checks)
	trial := nationalteam.BuildSubscriptionTrial(raw, calendar, quotes, now)
	result.TodayTrial = &trial
	result.SourceDate = source.Date
	result.ReportDate = source.ReportDate
	result.Warnings = warnings
	if stamp, err := time.Parse(time.RFC3339Nano, source.Generated); err == nil {
		result.SourceGeneratedAt = &stamp
	}
	for i, item := range result.Series {
		if quote, ok := quotes[item.Code]; ok {
			result.Series[i].LatestPrice = quote.LatestPrice
			result.Series[i].ChangePercent = quote.ChangePercent
			result.Series[i].QuoteTime = quote.QuoteTime
		}
	}
	if result.LatestCompleteDate == "" {
		result.Warnings = append(result.Warnings, "监测池尚无 22 只全部齐备的估算日期，可查看单只 ETF；缺失不计为零")
	}
	meta := foundation.SourceMeta{Source: "etf-radar + SSE/SZSE: ETF份额与对应日收盘价", SourceURL: base + "/meta.json", FetchedAt: time.Now(), TradeDate: result.LatestCompleteDate, LatencyMS: time.Since(started).Milliseconds()}
	if len(warnings) > 0 {
		meta.FallbackReason = strings.Join(warnings, "；")
	}
	return result, meta, nil
}
