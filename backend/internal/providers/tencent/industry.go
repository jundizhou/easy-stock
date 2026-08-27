package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"easy-stock/backend/internal/foundation"
)

var industryMomentumFields = []string{
	"change_percent", "five_day_change_percent", "twenty_day_change_percent",
	"leader_name", "leader_change_percent",
}

func (c *Client) IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 150 {
		limit = 150
	}
	values := url.Values{}
	values.Set("l", strconv.Itoa(limit))
	values.Set("p", "1")
	values.Set("t", "01/averatio")
	values.Set("ordertype", "")
	values.Set("o", "0")
	requestURL := c.industryBaseURL + "?" + values.Encode()
	start := time.Now()
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Name          string `json:"bd_name"`
			Code          string `json:"bd_code"`
			Price         string `json:"bd_zxj"`
			ChangePercent string `json:"bd_zdf"`
			FiveDay       string `json:"bd_zdf5"`
			TwentyDay     string `json:"bd_zdf20"`
			LeaderCode    string `json:"nzg_code"`
			LeaderName    string `json:"nzg_name"`
			LeaderPrice   string `json:"nzg_zxj"`
			LeaderChange  string `json:"nzg_zdf"`
		} `json:"data"`
	}
	if err := c.getIndustryJSON(ctx, requestURL, &payload); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	if payload.Code != 0 || len(payload.Data) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("tencent industry rank code=%d: %s", payload.Code, payload.Msg)
	}
	meta := foundation.SourceMeta{
		Source: "tencent:industry-rank", SourceURL: requestURL, AvailableFields: industryMomentumFields,
		FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.MarketIndustryMomentum, 0, len(payload.Data))
	for _, raw := range payload.Data {
		change := parseFloat(raw.ChangePercent)
		fiveDay := parseFloat(raw.FiveDay)
		twentyDay := parseFloat(raw.TwentyDay)
		leaderSymbol := ""
		if normalized, err := foundation.NormalizeSymbol(raw.LeaderCode); err == nil {
			leaderSymbol = normalized.Canonical
		}
		items = append(items, foundation.MarketIndustryMomentum{
			Code: raw.Code, Name: raw.Name, ChangePercent: change, FiveDayChangePercent: fiveDay,
			TwentyDayChange: twentyDay, LeaderSymbol: leaderSymbol, LeaderName: raw.LeaderName, LeaderChangePercent: parseFloat(raw.LeaderChange),
			Score: tencentIndustryScore(change, fiveDay, twentyDay), Meta: meta,
		})
	}
	return items, meta, nil
}

func (c *Client) getIndustryJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tencent industry http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func tencentIndustryScore(change, fiveDay, twentyDay float64) float64 {
	score := 50 + change*6 + fiveDay*2 + twentyDay*.8
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
