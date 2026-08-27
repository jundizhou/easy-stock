package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

var industryStockFields = []string{
	"price", "change", "change_percent", "volume", "amount", "total_market_cap", "float_market_cap",
}

const maxIndustryStockPageSize = 200

type industryStockResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total    int `json:"total"`
		RankList []struct {
			Code           string `json:"code"`
			Name           string `json:"name"`
			Price          string `json:"zxj"`
			Change         string `json:"zd"`
			ChangePercent  string `json:"zdf"`
			Turnover       string `json:"turnover"`
			Amount         string `json:"volume"`
			TotalMarketCap string `json:"zsz"`
			FloatMarketCap string `json:"ltsz"`
		} `json:"rank_list"`
	} `json:"data"`
}

func (c *Client) IndustryStocks(ctx context.Context, industryCode string, limit int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	industryCode = strings.TrimSpace(industryCode)
	if industryCode == "" {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry code is required")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	start := time.Now()
	stocks := make([]foundation.BoardStock, 0, limit)
	requestURL := ""
	offset := 0
	for offset < limit {
		count := limit - offset
		if count > maxIndustryStockPageSize {
			count = maxIndustryStockPageSize
		}
		values := url.Values{}
		values.Set("_appver", "11.17.0")
		values.Set("board_code", industryCode)
		values.Set("sort_type", "priceRatio")
		values.Set("direct", "down")
		values.Set("offset", strconv.Itoa(offset))
		values.Set("count", strconv.Itoa(count))
		pageURL := c.industryStocksBaseURL + "?" + values.Encode()
		if requestURL == "" {
			requestURL = pageURL
		}
		var payload industryStockResponse
		if err := c.getIndustryJSON(ctx, pageURL, &payload); err != nil {
			return nil, foundation.SourceMeta{}, err
		}
		if payload.Code != 0 {
			return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry stocks code=%d: %s", payload.Code, payload.Msg)
		}
		if len(payload.Data.RankList) == 0 {
			break
		}
		for _, raw := range payload.Data.RankList {
			symbol, err := foundation.NormalizeSymbol(raw.Code)
			if err != nil {
				continue
			}
			stocks = append(stocks, foundation.BoardStock{
				Symbol:         symbol.Canonical,
				Name:           strings.TrimSpace(raw.Name),
				Price:          parseFloat(raw.Price),
				Change:         parseFloat(raw.Change),
				ChangePercent:  parseFloat(raw.ChangePercent),
				Volume:         parseFloat(raw.Turnover) * 100,
				Amount:         parseFloat(raw.Amount) * 10_000,
				TotalMarketCap: parseFloat(raw.TotalMarketCap) * 100_000_000,
				FloatMarketCap: parseFloat(raw.FloatMarketCap) * 100_000_000,
			})
		}
		offset += len(payload.Data.RankList)
		if (payload.Data.Total > 0 && offset >= payload.Data.Total) || len(payload.Data.RankList) < count {
			break
		}
	}
	if len(stocks) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry stocks returned no supported A-share symbols")
	}
	meta := foundation.SourceMeta{
		Source: "tencent:industry-constituents", SourceURL: requestURL, AvailableFields: industryStockFields,
		FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
	}
	for i := range stocks {
		stocks[i].Meta = meta
	}
	return stocks, meta, nil
}
