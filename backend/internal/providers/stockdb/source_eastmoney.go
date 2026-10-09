package stockdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// eastmoneyDailySource 通过东方财富 push2his 接口取日线，使用 fqt=1 直接返回前复权
// 价，与 stockdb / 腾讯 的前复权口径一致。作为 stockdb 7899 的另一远程备用源，与
// 腾讯 fqkline 互为冗余：主源降级时两者按健康 + 轮询分摊流量，避免单接口被限流/封禁。
//
// 该实现自包含（仅标准库），不依赖 providers/eastmoney 包，保持 stockdb 包的轻量性。
type eastmoneyDailySource struct {
	baseURL    string
	httpClient *http.Client
	stats      *Stats
}

// NewEastmoneyDailySource 构造东方财富日线备用源。baseURL 为空时使用公开端点。
func NewEastmoneyDailySource(baseURL string) DailySource {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://push2his.eastmoney.com"
	}
	return &eastmoneyDailySource{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 15 * time.Second},
		stats:      newStats(),
	}
}

// eastmoneySecID 将 A 股代码转为东财 secid（市场.代码）：沪市 1，深/京市 0。
func eastmoneySecID(code string) string {
	switch {
	case strings.HasPrefix(code, "6"):
		return "1." + code
	default:
		return "0." + code
	}
}

func (e *eastmoneyDailySource) Name() string { return "eastmoney-push2his" }
func (e *eastmoneyDailySource) QFQ() bool    { return true }
func (e *eastmoneyDailySource) Health() (bool, float64) {
	return e.stats.Healthy(), e.stats.snapshot().P50Ms
}

func (e *eastmoneyDailySource) DailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	if count <= 0 {
		count = 2000
	}
	params := url.Values{}
	params.Set("secid", eastmoneySecID(code))
	params.Set("fields1", "f1,f2,f3,f4,f5,f6")
	params.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58")
	params.Set("klt", "101") // 日线
	params.Set("fqt", "1")   // 前复权
	params.Set("end", "20500101")
	params.Set("lmt", strconv.Itoa(count))
	u := e.baseURL + "/api/qt/stock/kline/get?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")

	started := time.Now()
	resp, err := e.httpClient.Do(req)
	if err != nil {
		e.stats.record(0, false, err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(started)
	if err != nil {
		e.stats.record(elapsed.Microseconds(), false, err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		e.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("http %d", resp.StatusCode))
		return nil, fmt.Errorf("eastmoney daily: http %d", resp.StatusCode)
	}
	var payload struct {
		RC   int `json:"rc"`
		Data struct {
			KLines []string `json:"klines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		e.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("decode: %v", err))
		return nil, fmt.Errorf("eastmoney daily: decode: %w", err)
	}
	if payload.RC != 0 {
		e.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("rc=%d", payload.RC))
		return nil, fmt.Errorf("eastmoney daily: rc=%d", payload.RC)
	}
	if len(payload.Data.KLines) == 0 {
		e.stats.record(elapsed.Microseconds(), false, "empty")
		return nil, fmt.Errorf("eastmoney daily: no data for %s", code)
	}
	out := make([]Bar, 0, len(payload.Data.KLines))
	for _, raw := range payload.Data.KLines {
		fields := strings.Split(raw, ",")
		if len(fields) < 6 {
			continue
		}
		day, perr := time.Parse("2006-01-02", strings.TrimSpace(fields[0]))
		if perr != nil {
			continue
		}
		open, _ := strconv.ParseFloat(fields[1], 64)
		closeP, _ := strconv.ParseFloat(fields[2], 64)
		high, _ := strconv.ParseFloat(fields[3], 64)
		low, _ := strconv.ParseFloat(fields[4], 64)
		vol, _ := strconv.ParseFloat(fields[5], 64)
		if open <= 0 || closeP <= 0 {
			continue
		}
		out = append(out, Bar{
			Date:   day.Year()*10000 + int(day.Month())*100 + day.Day(),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closeP,
			Volume: vol,
		})
	}
	if count > 0 && len(out) > count {
		out = out[len(out)-count:]
	}
	e.stats.record(elapsed.Microseconds(), true, "")
	return out, nil
}
