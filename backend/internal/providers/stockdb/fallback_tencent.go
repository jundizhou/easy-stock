package stockdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// tencentDailySource 通过腾讯 fqkline 接口取日线，返回已前复权（fq=qfq）的 OHLC，
// 与 stockdb 的前复权口径一致（参考点=最新价）。作为 stockdb 7899 宕机/降级时的
// 远程备用接口之一（多接口共用 / 负载均衡 / 故障转移）。
type tencentDailySource struct {
	baseURL    string
	httpClient *http.Client
	stats      *Stats
}

// NewTencentDailySource 构造腾讯日线备用源。baseURL 为空时使用公开端点。
func NewTencentDailySource(baseURL string) DailySource {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get"
	}
	return &tencentDailySource{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 12 * time.Second},
		stats:      newStats(),
	}
}

// marketPrefix 为 A 股代码补全市场前缀（腾讯接口需要 sh/sz/bj）。
func marketPrefix(code string) string {
	switch {
	case strings.HasPrefix(code, "6"):
		return "sh"
	case strings.HasPrefix(code, "0"), strings.HasPrefix(code, "3"):
		return "sz"
	default:
		return "bj"
	}
}

func (t *tencentDailySource) Name() string { return "tencent-fqkline" }
func (t *tencentDailySource) QFQ() bool    { return true }
func (t *tencentDailySource) Health() (bool, float64) {
	return t.stats.Healthy(), t.stats.snapshot().P50Ms
}

func (t *tencentDailySource) DailyBars(ctx context.Context, code string, count int) ([]Bar, error) {
	symbol := marketPrefix(code) + code
	param := fmt.Sprintf("%s,day,,,%d,qfq", symbol, count)
	u := t.baseURL + "?param=" + url.QueryEscape(param)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := t.httpClient.Do(req)
	if err != nil {
		t.stats.record(0, false, err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	elapsed := time.Since(started)
	if err != nil {
		t.stats.record(elapsed.Microseconds(), false, err.Error())
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		t.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("http %d", resp.StatusCode))
		return nil, fmt.Errorf("tencent daily: http %d", resp.StatusCode)
	}
	var parsed struct {
		Data map[string]struct {
			QfqDay []struct {
				Date   string  `json:"date"`
				Open   float64 `json:"open"`
				Close  float64 `json:"close"`
				High   float64 `json:"high"`
				Low    float64 `json:"low"`
				Volume float64 `json:"volume"`
			} `json:"qfqday"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.stats.record(elapsed.Microseconds(), false, fmt.Sprintf("decode: %v", err))
		return nil, fmt.Errorf("tencent daily: decode: %w", err)
	}
	entry, ok := parsed.Data[symbol]
	if !ok || len(entry.QfqDay) == 0 {
		t.stats.record(elapsed.Microseconds(), false, "no data")
		return nil, fmt.Errorf("tencent daily: no data for %s", code)
	}
	out := make([]Bar, 0, len(entry.QfqDay))
	for _, d := range entry.QfqDay {
		ts, perr := time.Parse("2006-01-02", d.Date)
		if perr != nil {
			continue
		}
		out = append(out, Bar{
			Date:   int(ts.Year())*10000 + int(ts.Month())*100 + int(ts.Day()),
			Open:   d.Open,
			High:   d.High,
			Low:    d.Low,
			Close:  d.Close,
			Volume: d.Volume,
		})
	}
	if count > 0 && len(out) > count {
		out = out[len(out)-count:]
	}
	t.stats.record(elapsed.Microseconds(), true, "")
	return out, nil
}
