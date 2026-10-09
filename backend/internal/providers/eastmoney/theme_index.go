package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"easy-stock/backend/internal/foundation"
)

func (c *Client) ThemeIndexBoards(ctx context.Context) ([]foundation.Board, error) {
	var boards []foundation.Board
	for page := 1; page <= 10; page++ {
		params := standardListParams(200)
		params.Set("pn", strconv.Itoa(page))
		params.Set("fs", "m:90+t:2,m:90+t:3")
		params.Set("fid", "f12")
		params.Set("fields", "f12,f14")
		requestURL := c.quoteBaseURL + "/api/qt/clist/get?" + params.Encode()
		var payload boardListPayload
		if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
			return nil, err
		}
		if payload.RC != 0 {
			return nil, fmt.Errorf("eastmoney index catalog rc=%d", payload.RC)
		}
		for _, board := range payload.Data.Diff {
			boards = append(boards, foundation.Board{Code: board.Code, Name: board.Name})
		}
		if len(boards) >= payload.Data.Total || len(payload.Data.Diff) == 0 {
			break
		}
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("eastmoney returned no index boards")
	}
	return boards, nil
}

var indexBoardCode = regexp.MustCompile(`^BK[0-9]{4,6}$`)

// AdjustedKLine keeps the theme basket on the explicit fqt=1 stock price basis.
func (c *Client) AdjustedKLine(ctx context.Context, symbol string, limit int) ([]foundation.KLine, error) {
	return c.KLine(ctx, symbol, "day", limit)
}

func (c *Client) BoardKLine(ctx context.Context, code string, limit int) ([]foundation.KLine, error) {
	if !indexBoardCode.MatchString(code) {
		return nil, fmt.Errorf("invalid index board code")
	}
	params := url.Values{"secid": {"90." + code}, "fields1": {"f1,f2,f3,f4,f5,f6"}, "fields2": {"f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61"}, "klt": {"101"}, "fqt": {"0"}, "end": {"20500101"}, "lmt": {strconv.Itoa(limit)}}
	requestURL := c.baseURL + "/api/qt/stock/kline/get?" + params.Encode()
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 || len(payload.Data.KLines) == 0 {
		return nil, fmt.Errorf("eastmoney board index unavailable")
	}
	meta := foundation.SourceMeta{Source: "eastmoney:board-index", SourceURL: requestURL, FetchedAt: time.Now()}
	lines := make([]foundation.KLine, 0, len(payload.Data.KLines))
	for _, raw := range payload.Data.KLines {
		line, err := parseKLine(raw, code, meta)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}
