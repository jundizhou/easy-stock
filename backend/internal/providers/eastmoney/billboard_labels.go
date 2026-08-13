package eastmoney

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	thsStockBlockPattern = regexp.MustCompile(`(?is)<div\s+class=["']stockcont["'][^>]*stockcode=["']?([0-9]{6})["']?[^>]*>`)
	thsSeatCellPattern   = regexp.MustCompile(`(?is)<td\s+class=["']tl\s+rel["'][^>]*>(.*?)</td>`)
	thsSeatNamePattern   = regexp.MustCompile(`(?is)<a[^>]*\stitle=["']([^"']+)["'][^>]*>`)
	thsLabelPattern      = regexp.MustCompile(`(?is)<label[^>]*class=["'][^"']*label[^"']*["'][^>]*>([^<]+)</label>`)
)

type thsSeatLabel struct {
	Name  string
	Label string
}

type thsBillboardPage struct {
	document string
	expires  time.Time
}

func (c *Client) enrichBillboardSeatLabels(ctx context.Context, symbol, tradeDate string, buySeats, sellSeats []foundation.MarketBillboardSeat) ([]foundation.MarketBillboardSeat, []foundation.MarketBillboardSeat) {
	labels, err := c.fetchTHSBillboardSeatLabels(ctx, symbol, tradeDate)
	if err != nil || len(labels) == 0 {
		return buySeats, sellSeats
	}
	apply := func(seats []foundation.MarketBillboardSeat) []foundation.MarketBillboardSeat {
		for index := range seats {
			label, ok := labels[normalizeBillboardSeatName(seats[index].Name)]
			if !ok || strings.TrimSpace(label) == "" {
				continue
			}
			seats[index].SourceLabel = label
			seats[index].Source = "ths"
			seats[index].LabelConfidence = "high"
			seats[index].LabelNote = "同花顺当日龙虎榜公开页面标签；属于平台分类口径，不代表监管机构确认资金身份"
			if label == "机构" || strings.Contains(label, "机构专用") {
				seats[index].Institution = true
			}
		}
		return seats
	}
	return apply(buySeats), apply(sellSeats)
}

func (c *Client) fetchTHSBillboardSeatLabels(ctx context.Context, symbol, tradeDate string) (map[string]string, error) {
	code := strings.Split(strings.TrimSpace(symbol), ".")[0]
	if len(code) != 6 {
		return nil, fmt.Errorf("invalid billboard symbol %q", symbol)
	}
	document, err := c.fetchTHSBillboardPage(ctx, tradeDate)
	if err != nil {
		return nil, err
	}
	return parseTHSBillboardSeatLabels(document, code), nil
}

func (c *Client) fetchTHSBillboardPage(ctx context.Context, tradeDate string) (string, error) {
	c.thsBillboardMu.Lock()
	defer c.thsBillboardMu.Unlock()
	if c.thsBillboardPages == nil {
		c.thsBillboardPages = make(map[string]thsBillboardPage)
	}
	if cached, ok := c.thsBillboardPages[tradeDate]; ok && time.Now().Before(cached.expires) {
		return cached.document, nil
	}
	requestURL := fmt.Sprintf("%s/ifmarket/lhbggxq/report/%s/", c.thsBaseURL, tradeDate)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", "https://data.10jqka.com.cn/market/longhu/")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ths billboard labels http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return "", err
	}
	decoded := bytes.Clone(body)
	if !utf8.Valid(body) {
		decoded, _, err = transform.Bytes(simplifiedchinese.GBK.NewDecoder(), body)
		if err != nil {
			return "", err
		}
	}
	document := string(decoded)
	c.thsBillboardPages[tradeDate] = thsBillboardPage{document: document, expires: time.Now().Add(12 * time.Hour)}
	return document, nil
}

func parseTHSBillboardSeatLabels(document, code string) map[string]string {
	result := map[string]string{}
	blocks := thsStockBlockPattern.FindAllStringSubmatchIndex(document, -1)
	for index, blockMatch := range blocks {
		if len(blockMatch) < 4 || document[blockMatch[2]:blockMatch[3]] != code {
			continue
		}
		start := blockMatch[1]
		end := len(document)
		if index+1 < len(blocks) {
			end = blocks[index+1][0]
		}
		for _, cellMatch := range thsSeatCellPattern.FindAllStringSubmatch(document[start:end], -1) {
			if len(cellMatch) < 2 {
				continue
			}
			nameMatch := thsSeatNamePattern.FindStringSubmatch(cellMatch[1])
			labelMatch := thsLabelPattern.FindStringSubmatch(cellMatch[1])
			if len(nameMatch) < 2 {
				continue
			}
			name := normalizeBillboardSeatName(html.UnescapeString(strings.TrimSpace(nameMatch[1])))
			label := ""
			if len(labelMatch) >= 2 {
				label = strings.TrimSpace(html.UnescapeString(labelMatch[1]))
			}
			if strings.Contains(name, "机构专用") {
				label = "机构"
			}
			if name != "" && label != "" {
				result[name] = label
			}
		}
	}
	return result
}

func normalizeBillboardSeatName(value string) string {
	replacer := strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"（", "(", "）", ")",
		"有限责任公司", "", "股份有限公司", "", "有限公司", "",
	)
	return replacer.Replace(strings.TrimSpace(value))
}
