package eastmoney

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/nationalteam"
)

// subscriptionDailyCloses only accepts an exact security identity and an
// explicitly dated, completed-session, unadjusted daily close.
func (c *Client) subscriptionDailyCloses(ctx context.Context, code, exchange, start, end string) (map[string]float64, error) {
	prefix := "1."
	if exchange == "SZSE" {
		prefix = "0."
	}
	params := url.Values{
		"secid": {prefix + code}, "klt": {"101"}, "fqt": {"0"},
		"beg": {strings.ReplaceAll(start, "-", "")}, "end": {strings.ReplaceAll(end, "-", "")},
		"lmt": {"400"}, "fields1": {"f1,f2,f3,f4,f5,f6"}, "fields2": {"f51,f53"},
	}
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			Code   string   `json:"code"`
			Market *int     `json:"market"`
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := c.getJSON(ctx, c.baseURL+"/api/qt/stock/kline/get?"+params.Encode(), &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 || payload.Data.Code != code || payload.Data.Market == nil || fmt.Sprint(*payload.Data.Market) != strings.TrimSuffix(prefix, ".") {
		return nil, fmt.Errorf("ETF daily close identity unavailable or differs")
	}
	result := map[string]float64{}
	for _, line := range payload.Data.KLines {
		parts := strings.Split(line, ",")
		if len(parts) != 2 {
			continue
		}
		day := parts[0]
		if _, err := time.Parse(time.DateOnly, day); err != nil || day < start || day > end {
			continue
		}
		if price := etfNumber(parts[1]); price != nil && *price > 0 {
			result[day] = *price
		}
	}
	return result, nil
}

// Only missing prices with disclosed shares are fetched, in bounded batches.
// Successful completed-day closes are cached; missing/failed days are retried.
func (c *Client) subscriptionBackfillCloses(ctx context.Context, raw map[string]nationalteam.SubscriptionRaw, dates map[string]bool, start string, now time.Time) []string {
	local := now.In(nationalteam.China)
	today := local.Format(time.DateOnly)
	completed := func(day string) bool { return day < today || (day == today && local.Hour() >= 15) }
	firstDate := ""
	for day := range dates {
		if firstDate == "" || day < firstDate {
			firstDate = day
		}
	}
	type job struct {
		code, exchange string
		days           []string
	}
	jobs := []job{}
	cache := &c.subscriptionData
	cache.mu.Lock()
	for _, item := range nationalteam.SubscriptionCatalog() {
		values := raw[item.Code]
		missing := []string{}
		for day, shares := range values.Shares {
			if !dates[day] || day == firstDate || day < start || !completed(day) || shares < 0 || math.IsNaN(shares) || math.IsInf(shares, 0) {
				continue
			}
			if price := values.Prices[day]; price > 0 && !math.IsNaN(price) && !math.IsInf(price, 0) {
				continue
			}
			if price, ok := cache.closes[item.Code][day]; ok {
				values.Prices[day] = price
			} else {
				missing = append(missing, day)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			jobs = append(jobs, job{code: item.Code, exchange: item.Exchange, days: missing})
		}
	}
	cache.mu.Unlock()
	type response struct {
		prices map[string]float64
		err    error
	}
	responses := make([]response, len(jobs))
	work := make(chan int, len(jobs))
	for i := range jobs {
		work <- i
	}
	close(work)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				item := jobs[i]
				requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
				prices, err := c.subscriptionDailyCloses(requestCtx, item.code, item.exchange, item.days[0], item.days[len(item.days)-1])
				cancel()
				responses[i] = response{prices: prices, err: err}
			}
		}()
	}
	wg.Wait()
	warnings := []string{}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closes == nil {
		cache.closes = map[string]map[string]float64{}
	}
	for i, item := range jobs {
		row := responses[i]
		if row.err != nil {
			warnings = append(warnings, item.code+" 同日收盘价补抓暂不可用")
			continue
		}
		if cache.closes[item.code] == nil {
			cache.closes[item.code] = map[string]float64{}
		}
		for _, day := range item.days {
			if price, ok := row.prices[day]; ok {
				raw[item.code].Prices[day] = price
				cache.closes[item.code][day] = price
			} else {
				warnings = append(warnings, item.code+" 的 "+day+" 收盘价待补齐")
			}
		}
	}
	return warnings
}
