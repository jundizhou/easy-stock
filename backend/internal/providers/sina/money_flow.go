package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

type stockMoneyFlowItem struct {
	Symbol      any `json:"symbol"`
	Name        any `json:"name"`
	Trade       any `json:"trade"`
	ChangeRatio any `json:"changeratio"`
	Inflow      any `json:"inamount"`
	Outflow     any `json:"outamount"`
	NetInflow   any `json:"netamount"`
	NetRatio    any `json:"ratioamount"`
	MainInflow  any `json:"r0_in"`
	MainOutflow any `json:"r0_out"`
	MainNet     any `json:"r0_net"`
	MainRatio   any `json:"r0_ratio"`
	RetailIn    any `json:"r3_in"`
	RetailOut   any `json:"r3_out"`
	RetailNet   any `json:"r3_net"`
	RetailRatio any `json:"r3_ratio"`
}

type sectorMoneyFlowItem struct {
	Category       any `json:"category"`
	Name           any `json:"name"`
	AveragePrice   any `json:"avg_price"`
	AverageChange  any `json:"avg_changeratio"`
	Inflow         any `json:"inamount"`
	Outflow        any `json:"outamount"`
	NetInflow      any `json:"netamount"`
	NetRatio       any `json:"ratioamount"`
	LeaderSymbol   any `json:"ts_symbol"`
	LeaderName     any `json:"ts_name"`
	LeaderPrice    any `json:"ts_trade"`
	LeaderChange   any `json:"ts_changeratio"`
	LeaderNetRatio any `json:"ts_ratioamount"`
}

var stockMoneyFlowFields = []string{
	"price", "change_percent", "inflow", "outflow", "net_inflow", "net_inflow_ratio",
	"main_inflow", "main_outflow", "main_net_inflow", "main_net_inflow_ratio",
	"retail_inflow", "retail_outflow", "retail_net_inflow", "retail_net_inflow_ratio",
}

var sectorMoneyFlowFields = []string{
	"price", "change_percent", "inflow", "outflow", "net_inflow", "net_inflow_ratio",
	"leader_symbol", "leader_name", "leader_price", "leader_change_percent", "leader_net_inflow_ratio",
}

func (c *Client) MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "stock":
		return c.StockFundFlows(ctx, sortKey, limit)
	case "industry", "theme":
		return c.sectorFundFlows(ctx, dimension, sortKey, limit)
	default:
		return nil, foundation.SourceMeta{}, fmt.Errorf("unsupported fund-flow dimension %q", dimension)
	}
}

func (c *Client) StockFundFlows(ctx context.Context, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	sortField := "r0_net"
	if sortKey == "ratio" {
		sortField = "r0_ratio"
	} else if sortKey == "change" {
		sortField = "changeratio"
	}
	values := url.Values{}
	values.Set("page", "1")
	values.Set("num", strconv.Itoa(min(400, max(100, limit*2))))
	values.Set("sort", sortField)
	values.Set("asc", "0")
	values.Set("bankuai", "")
	values.Set("shichang", "")
	requestURL := c.moneyFlowBaseURL + "?" + values.Encode()
	start := time.Now()
	var rawItems []stockMoneyFlowItem
	if err := c.getMoneyFlowJSON(ctx, requestURL, &rawItems); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	meta := foundation.SourceMeta{
		Source: "sina:stock-money-flow", SourceURL: requestURL, AvailableFields: stockMoneyFlowFields,
		FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.MarketFundFlow, 0, min(limit, len(rawItems)))
	for _, raw := range rawItems {
		symbol := sinaString(raw.Symbol)
		name := sinaString(raw.Name)
		if !isAStockMoneyFlowSymbol(symbol) || name == "" || strings.Contains(strings.ToUpper(name), "ST") {
			continue
		}
		normalized, normalizeErr := foundation.NormalizeSymbol(symbol)
		if normalizeErr != nil {
			continue
		}
		items = append(items, foundation.MarketFundFlow{
			Dimension: "stock", Code: normalized.RawCode, Symbol: normalized.Canonical, Name: name,
			Price: parseSinaFloat(raw.Trade), ChangePercent: parseSinaFloat(raw.ChangeRatio) * 100,
			Inflow: parseSinaFloat(raw.Inflow), Outflow: parseSinaFloat(raw.Outflow),
			NetInflow: parseSinaFloat(raw.NetInflow), NetInflowRatio: parseSinaFloat(raw.NetRatio) * 100,
			MainInflow: parseSinaFloat(raw.MainInflow), MainOutflow: parseSinaFloat(raw.MainOutflow),
			MainNetInflow: parseSinaFloat(raw.MainNet), MainNetInflowRatio: parseSinaFloat(raw.MainRatio) * 100,
			RetailInflow: parseSinaFloat(raw.RetailIn), RetailOutflow: parseSinaFloat(raw.RetailOut),
			RetailNetInflow: parseSinaFloat(raw.RetailNet), RetailNetRatio: parseSinaFloat(raw.RetailRatio) * 100,
			Meta: meta,
		})
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("sina returned no stock money-flow rows")
	}
	return items, meta, nil
}

func (c *Client) sectorFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	fenlei := "0"
	if dimension == "theme" {
		fenlei = "1"
	}
	sortField := "netamount"
	if sortKey == "ratio" {
		sortField = "ratioamount"
	} else if sortKey == "change" {
		sortField = "avg_changeratio"
	}
	values := url.Values{}
	values.Set("page", "1")
	values.Set("num", strconv.Itoa(limit))
	values.Set("sort", sortField)
	values.Set("asc", "0")
	values.Set("fenlei", fenlei)
	requestURL := c.sectorMoneyFlowBaseURL + "?" + values.Encode()
	start := time.Now()
	var rawItems []sectorMoneyFlowItem
	if err := c.getMoneyFlowJSON(ctx, requestURL, &rawItems); err != nil {
		return nil, foundation.SourceMeta{}, err
	}
	source := "sina:industry-money-flow"
	if dimension == "theme" {
		source = "sina:theme-money-flow"
	}
	meta := foundation.SourceMeta{
		Source: source, SourceURL: requestURL, AvailableFields: sectorMoneyFlowFields,
		FetchedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds(),
	}
	items := make([]foundation.MarketFundFlow, 0, min(limit, len(rawItems)))
	for _, raw := range rawItems {
		name := sinaString(raw.Name)
		if name == "" {
			continue
		}
		leaderSymbol := sinaString(raw.LeaderSymbol)
		if normalized, err := foundation.NormalizeSymbol(leaderSymbol); err == nil {
			leaderSymbol = normalized.Canonical
		}
		items = append(items, foundation.MarketFundFlow{
			Dimension: dimension, Code: sinaString(raw.Category), Name: name,
			Price: parseSinaFloat(raw.AveragePrice), ChangePercent: parseSinaFloat(raw.AverageChange) * 100,
			Inflow: parseSinaFloat(raw.Inflow), Outflow: parseSinaFloat(raw.Outflow),
			NetInflow: parseSinaFloat(raw.NetInflow), NetInflowRatio: parseSinaFloat(raw.NetRatio) * 100,
			LeaderSymbol: leaderSymbol, LeaderName: sinaString(raw.LeaderName), LeaderPrice: parseSinaFloat(raw.LeaderPrice),
			LeaderChange: parseSinaFloat(raw.LeaderChange) * 100, LeaderNetRatio: parseSinaFloat(raw.LeaderNetRatio) * 100,
			Meta: meta,
		})
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return nil, foundation.SourceMeta{}, fmt.Errorf("sina returned no %s money-flow rows", dimension)
	}
	return items, meta, nil
}

func (c *Client) getMoneyFlowJSON(ctx context.Context, requestURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125.0.0.0 Safari/537.36")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sina money flow http status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func isAStockMoneyFlowSymbol(symbol string) bool {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if len(symbol) != 8 {
		return false
	}
	code := symbol[2:]
	if strings.HasPrefix(symbol, "bj") {
		return code[0] == '4' || code[0] == '8' || code[0] == '9'
	}
	if strings.HasPrefix(symbol, "sh") {
		return strings.HasPrefix(code, "600") || strings.HasPrefix(code, "601") || strings.HasPrefix(code, "603") || strings.HasPrefix(code, "605") || strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689")
	}
	if strings.HasPrefix(symbol, "sz") {
		return strings.HasPrefix(code, "000") || strings.HasPrefix(code, "001") || strings.HasPrefix(code, "002") || strings.HasPrefix(code, "003") || strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301")
	}
	return false
}

func parseSinaFloat(value any) float64 {
	parsed, _ := strconv.ParseFloat(sinaString(value), 64)
	return parsed
}

func sinaString(value any) string {
	if value == nil {
		return ""
	}
	if boolean, ok := value.(bool); ok && !boolean {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
