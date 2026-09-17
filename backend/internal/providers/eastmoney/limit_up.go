package eastmoney

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"easy-stock/backend/internal/foundation"
)

type limitUpPoolPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Pool []struct {
			Code           string  `json:"c"`
			Name           string  `json:"n"`
			Price          float64 `json:"p"`
			ChangePercent  float64 `json:"zdp"`
			Amount         float64 `json:"amount"`
			FloatMarketCap float64 `json:"ltsz"`
			TurnoverRate   float64 `json:"hs"`
			Streak         int     `json:"lbc"`
			FirstLimitTime int     `json:"fbt"`
			LastLimitTime  int     `json:"lbt"`
			OpenCount      int     `json:"zbc"`
			Industry       string  `json:"hybk"`
			Statistics     struct {
				Days  int `json:"days"`
				Count int `json:"ct"`
			} `json:"zttj"`
		} `json:"pool"`
	} `json:"data"`
}

type marketLimitPoolPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Pool []struct {
			Code          string  `json:"c"`
			Name          string  `json:"n"`
			Price         float64 `json:"p"`
			ChangePercent float64 `json:"zdp"`
			Amount        float64 `json:"amount"`
			Industry      string  `json:"hybk"`
		} `json:"pool"`
	} `json:"data"`
}

func (c *Client) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	if lookbackDays <= 0 {
		lookbackDays = 12
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().In(location)
	events := make([]foundation.LimitUpEvent, 0, 200)
	successfulDays := 0
	var lastErr error
	for offset := lookbackDays - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		dayEvents, err := c.LimitUpPool(ctx, date)
		if err != nil {
			lastErr = err
			continue
		}
		successfulDays++
		events = append(events, dayEvents...)
	}
	if successfulDays == 0 {
		return nil, fmt.Errorf("eastmoney recent limit-up pool unavailable: %w", lastErr)
	}
	return events, nil
}

// ProgressiveRecentLimitUps also reports incomplete history, so comparisons
// are not calculated from a silently missing trading day.
func (c *Client) ProgressiveRecentLimitUps(ctx context.Context, days int, publish func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error) {
	return c.recentLimitUps(ctx, days, publish)
}

func (c *Client) recentLimitUps(ctx context.Context, lookbackDays int, publish func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error) {
	if lookbackDays <= 0 {
		lookbackDays = 12
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Now().In(location)
	type dayResult struct {
		offset int
		events []foundation.LimitUpEvent
		err    error
	}
	results := make(chan dayResult, lookbackDays)
	jobs := make(chan int, lookbackDays)
	for offset := 0; offset < lookbackDays; offset++ {
		jobs <- offset
	}
	close(jobs)
	for worker := 0; worker < min(3, lookbackDays); worker++ {
		go func() {
			for offset := range jobs {
				if ctx.Err() != nil {
					return
				}
				events, err := c.LimitUpPool(ctx, now.AddDate(0, 0, -offset))
				results <- dayResult{offset, events, err}
			}
		}()
	}
	days := make([][]foundation.LimitUpEvent, lookbackDays)
	collect := func() []foundation.LimitUpEvent {
		events := make([]foundation.LimitUpEvent, 0, 200)
		for offset := lookbackDays - 1; offset >= 0; offset-- {
			events = append(events, cloneLimitEvents(days[offset])...)
		}
		return events
	}
	var lastErr error
	for remaining := lookbackDays; remaining > 0; remaining-- {
		select {
		case <-ctx.Done():
			return collect(), ctx.Err()
		case result := <-results:
			if result.err != nil {
				lastErr = fmt.Errorf("%s 涨停池: %w", now.AddDate(0, 0, -result.offset).Format("2006-01-02"), result.err)
			}
			days[result.offset] = result.events
			if publish != nil && len(result.events) > 0 {
				publish(collect())
			}
		}
	}
	return collect(), lastErr
}

func (c *Client) fetchLimitUpPool(ctx context.Context, date time.Time) ([]foundation.LimitUpEvent, error) {
	endpoint := c.topicBaseURL + "/getTopicZTPool"
	params := url.Values{}
	params.Set("ut", "7eea3edcaed734bea9cbfc24409ed989")
	params.Set("dpt", "wz.ztzt")
	params.Set("Pageindex", "0")
	params.Set("pagesize", "500")
	params.Set("sort", "fbt:asc")
	params.Set("date", date.Format("20060102"))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload limitUpPoolPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney limit-up pool rc=%d", payload.RC)
	}
	meta := foundation.SourceMeta{
		Source:    "eastmoney:limit-up-pool",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	events := make([]foundation.LimitUpEvent, 0, len(payload.Data.Pool))
	for _, raw := range payload.Data.Pool {
		symbol, err := normalizeEastMoneyStockCode(raw.Code)
		if err != nil {
			continue
		}
		events = append(events, foundation.LimitUpEvent{
			Symbol:         symbol,
			Name:           raw.Name,
			Date:           date,
			Price:          raw.Price / 1000,
			ChangePercent:  raw.ChangePercent,
			Amount:         raw.Amount,
			FloatMarketCap: raw.FloatMarketCap,
			TurnoverRate:   raw.TurnoverRate,
			Streak:         raw.Streak,
			FirstLimitTime: formatTradeClock(raw.FirstLimitTime),
			LastLimitTime:  formatTradeClock(raw.LastLimitTime),
			OpenCount:      raw.OpenCount,
			Industry:       raw.Industry,
			Days:           raw.Statistics.Days,
			Count:          raw.Statistics.Count,
			Meta:           meta,
		})
	}
	return events, nil
}

// BrokenLimitUpPool returns stocks that touched their limit-up price but did
// not remain sealed. This is the numerator of the final broken-board rate and
// must not be confused with LimitUpEvent.OpenCount, which describes stocks
// that reopened but eventually sealed again.
func (c *Client) BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	return c.marketLimitPool(ctx, date, "/getTopicZBPool", "fbt:asc", "eastmoney:broken-limit-up-pool")
}

// LimitDownPool returns the final daily limit-down pool. The endpoint does not
// support the limit-up pool's fbt sort field; fund:asc is intentionally used so
// a non-empty pool is not silently returned as an empty response.
func (c *Client) LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	return c.marketLimitPool(ctx, date, "/getTopicDTPool", "fund:asc", "eastmoney:limit-down-pool")
}

func (c *Client) marketLimitPool(
	ctx context.Context,
	date time.Time,
	path string,
	sortValue string,
	source string,
) ([]foundation.MarketLimitEvent, error) {
	endpoint := c.topicBaseURL + path
	params := url.Values{}
	params.Set("ut", "7eea3edcaed734bea9cbfc24409ed989")
	params.Set("dpt", "wz.ztzt")
	params.Set("Pageindex", "0")
	params.Set("pagesize", "500")
	params.Set("sort", sortValue)
	params.Set("date", date.Format("20060102"))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload marketLimitPoolPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney market limit pool rc=%d", payload.RC)
	}
	meta := foundation.SourceMeta{
		Source:    source,
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	events := make([]foundation.MarketLimitEvent, 0, len(payload.Data.Pool))
	for _, raw := range payload.Data.Pool {
		symbol, err := normalizeEastMoneyStockCode(raw.Code)
		if err != nil {
			continue
		}
		events = append(events, foundation.MarketLimitEvent{
			Symbol:        symbol,
			Name:          raw.Name,
			Date:          date,
			Price:         raw.Price / 1000,
			ChangePercent: raw.ChangePercent,
			Amount:        raw.Amount,
			Industry:      raw.Industry,
			Meta:          meta,
		})
	}
	return events, nil
}

func formatTradeClock(value int) string {
	if value <= 0 {
		return ""
	}
	raw := fmt.Sprintf("%06d", value)
	return raw[0:2] + ":" + raw[2:4] + ":" + raw[4:6]
}

type limitUpDayFlight struct {
	done    chan struct{}
	events  []foundation.LimitUpEvent
	err     error
	expires time.Time
}

func (c *Client) LimitUpPool(ctx context.Context, date time.Time) ([]foundation.LimitUpEvent, error) {
	key := date.Format("2006-01-02")
	c.poolMu.Lock()
	if f := c.limitUpDays[key]; f != nil && (f.expires.IsZero() || time.Now().Before(f.expires)) {
		c.poolMu.Unlock()
		select {
		case <-f.done:
			return cloneLimitEvents(f.events), f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if c.limitUpDays == nil {
		c.limitUpDays = make(map[string]*limitUpDayFlight)
	}
	for day, flight := range c.limitUpDays {
		if !flight.expires.IsZero() && time.Now().After(flight.expires) {
			delete(c.limitUpDays, day)
		}
	}
	f := &limitUpDayFlight{done: make(chan struct{})}
	c.limitUpDays[key] = f
	c.poolMu.Unlock()
	start := time.Now()
	f.events, f.err = c.fetchLimitUpPool(ctx, date)
	ttl := 30 * time.Second
	if key < time.Now().In(date.Location()).Format("2006-01-02") && len(f.events) > 0 && f.err == nil {
		ttl = 6 * time.Hour
	}
	if f.err != nil {
		ttl = 5 * time.Second
	}
	c.poolMu.Lock()
	f.expires = time.Now().Add(ttl)
	close(f.done)
	c.poolMu.Unlock()
	log.Printf("event=limit_up_stage stage=eastmoney_day date=%s duration_ms=%d failed=%t", key, time.Since(start).Milliseconds(), f.err != nil)
	return cloneLimitEvents(f.events), f.err
}

func cloneLimitEvents(events []foundation.LimitUpEvent) []foundation.LimitUpEvent {
	result := append([]foundation.LimitUpEvent(nil), events...)
	for i := range result {
		result[i].Concepts = append([]string(nil), result[i].Concepts...)
	}
	return result
}
