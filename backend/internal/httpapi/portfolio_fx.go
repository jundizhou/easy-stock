package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Frankfurter v1 publishes ECB daily reference rates, quoted per unit of the base currency.
const portfolioFXURL = "https://api.frankfurter.dev/v1/latest?base=CNY&symbols=HKD,USD"

type portfolioFXRates struct {
	mu       sync.Mutex
	nextRead time.Time
	data     referenceRates
	url      string
}

type referenceRates struct {
	Base  string
	Date  string
	Rates map[string]float64
}

func (p *portfolioFXRates) apply(ctx context.Context, quotes []foundation.Quote) {
	var rates referenceRates
	for i := range quotes {
		currency := quotes[i].Currency
		if currency != "HKD" && currency != "USD" {
			continue
		}
		if rates.Rates == nil {
			rates = p.latest(ctx, time.Now())
			if rates.Rates == nil {
				return
			}
		}
		rate := rates.Rates[currency]
		if rate > 0 && !math.IsInf(1/rate, 0) {
			quotes[i].CNYConversion = &foundation.CurrencyConversion{Rate: 1 / rate, Date: rates.Date}
		}
	}
}

func (p *portfolioFXRates) latest(ctx context.Context, now time.Time) referenceRates {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !now.Before(p.nextRead) && ctx.Err() == nil {
		address := p.url
		if address == "" {
			address = portfolioFXURL
		}
		rates, ok := fetchReferenceRates(ctx, address)
		if ok && rates.valid(now) {
			p.data = rates
			p.nextRead = now.Add(time.Hour)
		} else if ctx.Err() == nil {
			p.nextRead = now.Add(time.Minute)
		}
	}
	if p.data.valid(now) {
		return p.data
	}
	return referenceRates{}
}

func (r referenceRates) valid(now time.Time) bool {
	day, err := time.Parse("2006-01-02", r.Date)
	return r.Base == "CNY" && err == nil && !day.After(now) && now.Sub(day) < 7*24*time.Hour && len(r.Rates) > 0
}

func fetchReferenceRates(ctx context.Context, address string) (referenceRates, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return referenceRates{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return referenceRates{}, false
	}
	defer resp.Body.Close()
	var data referenceRates
	ok := resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&data) == nil
	return data, ok
}
