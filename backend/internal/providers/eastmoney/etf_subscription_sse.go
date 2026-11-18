package eastmoney

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

func etfNumber(value any) *float64 {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	result, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return &result
}

func (c *Client) nationalTeamShareDay(ctx context.Context, endpoint, day string) (map[string]float64, error) {
	params := url.Values{"isPagination": {"true"}, "pageHelp.pageSize": {"10000"}, "pageHelp.pageNo": {"1"}, "sqlId": {"COMMON_SSE_ZQPZ_ETFZL_XXPL_ETFGM_SEARCH_L"}, "STAT_DATE": {day}}
	var payload struct {
		Result []map[string]any `json:"result"`
		Page   struct {
			Data []map[string]any `json:"data"`
		} `json:"pageHelp"`
	}
	if err := c.getJSONWithHeaders(ctx, endpoint+"?"+params.Encode(), map[string]string{"Referer": "https://www.sse.com.cn/"}, &payload); err != nil {
		return nil, err
	}
	rows := payload.Result
	if len(rows) == 0 {
		rows = payload.Page.Data
	}
	shares := map[string]float64{}
	for _, row := range rows {
		stamp := asString(row["STAT_DATE"])
		if len(stamp) < 10 || stamp[:10] != day {
			continue
		}
		amount := etfNumber(row["TOT_VOL"])
		if amount != nil && *amount >= 0 {
			shares[asString(row["SEC_CODE"])] = *amount
		}
	}
	return shares, nil
}
