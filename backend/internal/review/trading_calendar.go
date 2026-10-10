package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// TradingCalendar requires an explicit exchange answer, never a weekday guess.
type TradingCalendar interface {
	IsTradingDay(context.Context, time.Time) (bool, error)
}
type ExchangeCalendar struct {
	mu       sync.Mutex
	store    *Store
	client   *http.Client
	endpoint string
	now      func() time.Time
}
type calendarMonth struct {
	Days      map[string]bool `json:"days"`
	FetchedAt time.Time       `json:"fetched_at"`
}

func NewExchangeCalendar(store *Store, client *http.Client) *ExchangeCalendar {
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	return &ExchangeCalendar{store: store, client: client, endpoint: "https://www.szse.cn/api/report/exchange/onepersistenthour/monthList", now: time.Now}
}
func (c *ExchangeCalendar) IsTradingDay(ctx context.Context, date time.Time) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	date = date.In(shanghaiLocation())
	month := date.Format("2006-01")
	key := "calendar:" + month
	var cached calendarMonth
	_ = c.store.readScheduleState(ctx, key, &cached)
	now := c.now()
	if value, ok := cached.Days[date.Format("2006-01-02")]; ok && now.Sub(cached.FetchedAt) < 24*time.Hour {
		return value, nil
	}
	fresh, err := c.fetch(ctx, month)
	if err != nil {
		// A previously validated complete month remains usable briefly during outages.
		if value, ok := cached.Days[date.Format("2006-01-02")]; ok && now.Sub(cached.FetchedAt) < 7*24*time.Hour {
			return value, nil
		}
		return false, errors.New("交易日历获取失败或缓存已过期，稍后自动重试")
	}
	fresh.FetchedAt = now
	if err = c.store.writeScheduleState(ctx, key, fresh); err != nil {
		return false, err
	}
	return fresh.Days[date.Format("2006-01-02")], nil
}
func (c *ExchangeCalendar) fetch(ctx context.Context, month string) (calendarMonth, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?month="+month, nil)
	if err != nil {
		return calendarMonth{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock")
	req.Header.Set("Referer", "https://www.szse.cn/")
	resp, err := c.client.Do(req)
	if err != nil {
		return calendarMonth{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return calendarMonth{}, fmt.Errorf("calendar HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			Date string `json:"jyrq"`
			Open string `json:"jybz"`
		} `json:"data"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return calendarMonth{}, err
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return calendarMonth{}, err
	}
	first, err := time.Parse("2006-01", month)
	if err != nil {
		return calendarMonth{}, err
	}
	count := first.AddDate(0, 1, -1).Day()
	days := map[string]bool{}
	for _, item := range payload.Data {
		d, e := time.Parse("2006-01-02", item.Date)
		if e != nil || d.Format("2006-01") != month || (item.Open != "0" && item.Open != "1") {
			return calendarMonth{}, errors.New("invalid calendar day")
		}
		if _, exists := days[item.Date]; exists {
			return calendarMonth{}, errors.New("duplicate calendar day")
		}
		days[item.Date] = item.Open == "1"
	}
	if len(days) != count {
		return calendarMonth{}, errors.New("incomplete exchange calendar")
	}
	return calendarMonth{Days: days}, nil
}
