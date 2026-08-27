package eastmoney

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type boardListPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Total int `json:"total"`
		Diff  []struct {
			Code           string  `json:"f12"`
			Name           string  `json:"f14"`
			ChangePercent  float64 `json:"f3"`
			TotalMarketCap float64 `json:"f20"`
			FloatMarketCap float64 `json:"f21"`
			MainNetInflow  float64 `json:"f62"`
		} `json:"diff"`
	} `json:"data"`
}

type boardFundFlowPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Total int `json:"total"`
		Diff  []struct {
			Code          string  `json:"f12"`
			Name          string  `json:"f14"`
			MainNetInflow float64 `json:"f62"`
		} `json:"diff"`
	} `json:"data"`
}

type boardStocksPayload struct {
	RC   int `json:"rc"`
	Data struct {
		Total int `json:"total"`
		Diff  []struct {
			Code           string        `json:"f12"`
			Name           string        `json:"f14"`
			Price          flexibleFloat `json:"f2"`
			ChangePercent  flexibleFloat `json:"f3"`
			Change         flexibleFloat `json:"f4"`
			Volume         flexibleFloat `json:"f5"`
			Amount         flexibleFloat `json:"f6"`
			TotalMarketCap flexibleFloat `json:"f20"`
			FloatMarketCap flexibleFloat `json:"f21"`
			MainNetInflow  flexibleFloat `json:"f62"`
		} `json:"diff"`
	} `json:"data"`
}

func (c *Client) Boards(ctx context.Context, keyword string, limit int) ([]foundation.Board, error) {
	if limit <= 0 {
		limit = 200
	}
	endpoint := c.quoteBaseURL + "/api/qt/clist/get"
	params := url.Values{}
	params.Set("pn", "1")
	params.Set("pz", strconv.Itoa(limit))
	params.Set("po", "1")
	params.Set("np", "1")
	params.Set("ut", "bd1d9ddb04089700cf9c27f6f7426281")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("fid", "f3")
	params.Set("fs", "m:90+t:2+f:!50")
	params.Set("fields", "f12,f14,f3,f20,f21,f62")
	params.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload boardListPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return c.boardsFromFundFlow(ctx, keyword, limit, err)
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney board rc=%d", payload.RC)
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	keyword = strings.TrimSpace(keyword)
	boards := make([]foundation.Board, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		if keyword != "" && !strings.Contains(raw.Name, keyword) {
			continue
		}
		boards = append(boards, foundation.Board{
			Code:           raw.Code,
			Name:           raw.Name,
			ChangePercent:  raw.ChangePercent,
			TotalMarketCap: raw.TotalMarketCap,
			FloatMarketCap: raw.FloatMarketCap,
			MainNetInflow:  raw.MainNetInflow,
			Meta:           meta,
		})
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("eastmoney returned no boards")
	}
	return boards, nil
}

func (c *Client) boardsFromFundFlow(ctx context.Context, keyword string, limit int, cause error) ([]foundation.Board, error) {
	start := time.Now()
	requestURLs := make([]string, 0, 2)
	meta := foundation.SourceMeta{
		Source:    "eastmoney:bkzj",
		FetchedAt: time.Now(),
	}
	keyword = strings.TrimSpace(keyword)
	boardsByCode := map[string]foundation.Board{}
	var lastErr error
	for _, boardCode := range []string{"m:90+t:2+f:!50", "m:90+t:3+f:!50"} {
		boards, requestURL, err := c.fetchFundFlowBoards(ctx, boardCode, keyword, meta)
		requestURLs = append(requestURLs, requestURL)
		if err != nil {
			lastErr = err
			continue
		}
		for _, board := range boards {
			boardsByCode[board.Code] = board
		}
	}
	meta.SourceURL = strings.Join(requestURLs, ",")
	meta.LatencyMS = time.Since(start).Milliseconds()
	boards := make([]foundation.Board, 0, len(boardsByCode))
	for _, board := range boardsByCode {
		board.Meta = meta
		boards = append(boards, board)
	}
	if len(boards) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("eastmoney board list failed: %v; fund-flow fallback failed: %w", cause, lastErr)
		}
		return nil, fmt.Errorf("eastmoney fund-flow returned no boards")
	}
	return boards, nil
}

func (c *Client) fetchFundFlowBoards(ctx context.Context, boardCode string, keyword string, meta foundation.SourceMeta) ([]foundation.Board, string, error) {
	endpoint := c.dataBaseURL + "/dataapi/bkzj/getbkzj"
	params := url.Values{}
	params.Set("key", "f62")
	params.Set("code", boardCode)
	requestURL := endpoint + "?" + params.Encode()

	var payload boardFundFlowPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, requestURL, err
	}
	if payload.RC != 0 {
		return nil, requestURL, fmt.Errorf("eastmoney fund-flow rc=%d", payload.RC)
	}

	boards := make([]foundation.Board, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		if keyword != "" && !strings.Contains(raw.Name, keyword) {
			continue
		}
		boards = append(boards, foundation.Board{
			Code:          raw.Code,
			Name:          raw.Name,
			MainNetInflow: raw.MainNetInflow,
			Meta:          meta,
		})
	}
	return boards, requestURL, nil
}

func (c *Client) BoardStocks(ctx context.Context, boardCode string, limit int) ([]foundation.BoardStock, error) {
	boardCode = strings.ToUpper(strings.TrimSpace(boardCode))
	if boardCode == "" {
		return nil, fmt.Errorf("board code is required")
	}
	if limit <= 0 {
		limit = 20
	}
	baseURLs := append([]string{c.quoteBaseURL}, c.quoteFallbackURLs...)
	var lastErr error
	for index, baseURL := range baseURLs {
		attemptCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		stocks, err := c.boardStocksFromBaseURL(attemptCtx, baseURL, boardCode, limit, index > 0)
		cancel()
		if err == nil {
			return stocks, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("eastmoney board stocks unavailable: %w", lastErr)
}

func (c *Client) boardStocksFromBaseURL(ctx context.Context, baseURL string, boardCode string, limit int, fallback bool) ([]foundation.BoardStock, error) {
	endpoint := baseURL + "/api/qt/clist/get"
	params := url.Values{}
	params.Set("pn", "1")
	params.Set("pz", strconv.Itoa(limit))
	params.Set("po", "1")
	params.Set("np", "1")
	params.Set("ut", "bd1d9ddb04089700cf9c27f6f7426281")
	params.Set("fltt", "2")
	params.Set("invt", "2")
	params.Set("fid", "f3")
	params.Set("fs", "b:"+boardCode)
	params.Set("fields", "f12,f14,f2,f3,f4,f5,f6,f20,f21,f62")
	params.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	requestURL := endpoint + "?" + params.Encode()

	start := time.Now()
	var payload boardStocksPayload
	if err := c.getJSONWithRetry(ctx, requestURL, &payload); err != nil {
		return nil, err
	}
	if payload.RC != 0 {
		return nil, fmt.Errorf("eastmoney board stocks rc=%d", payload.RC)
	}

	meta := foundation.SourceMeta{
		Source:    "eastmoney",
		SourceURL: requestURL,
		FetchedAt: time.Now(),
		LatencyMS: time.Since(start).Milliseconds(),
	}
	if fallback {
		meta.FallbackReason = "东方财富主行情节点暂不可用，已切换备用节点"
	}
	stocks := make([]foundation.BoardStock, 0, len(payload.Data.Diff))
	for _, raw := range payload.Data.Diff {
		symbol, err := normalizeEastMoneyStockCode(raw.Code)
		if err != nil {
			continue
		}
		stocks = append(stocks, foundation.BoardStock{
			Symbol:         symbol,
			Name:           raw.Name,
			Price:          float64(raw.Price),
			Change:         float64(raw.Change),
			ChangePercent:  float64(raw.ChangePercent),
			Volume:         float64(raw.Volume),
			Amount:         float64(raw.Amount),
			TotalMarketCap: float64(raw.TotalMarketCap),
			FloatMarketCap: float64(raw.FloatMarketCap),
			MainNetInflow:  float64(raw.MainNetInflow),
			Meta:           meta,
		})
	}
	if len(stocks) == 0 {
		return nil, fmt.Errorf("eastmoney returned no board stocks")
	}
	return stocks, nil
}

func normalizeEastMoneyStockCode(code string) (string, error) {
	symbol, err := foundation.NormalizeSymbol(code)
	if err != nil {
		return "", err
	}
	return symbol.Canonical, nil
}
