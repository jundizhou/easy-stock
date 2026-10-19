package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// 桌面端 push2 转发桥（Electron 用 Chromium 网络栈代发请求）的地址与令牌，
	// 由 desktop/main.cjs 注入。网页版后端没有这两个变量。
	push2BridgeEnvURL   = "A_STOCK_EASTMONEY_PUSH2_BRIDGE_URL"
	push2BridgeEnvToken = "A_STOCK_EASTMONEY_PUSH2_BRIDGE_TOKEN"

	// 桥接内部已经有上游超时，这里略放宽，避免后端先取消。
	push2BridgeTimeout = 20 * time.Second
	// push2 的报价响应都是小体积 JSON，超过上限按异常处理。
	push2BridgeMaxBody = 2 << 20
)

// Push2Proxy 通过桌面端把 push2 报价请求交给 Chromium 发出。
//
// push2 系列（push2 / push2delay / push2his / 编号镜像）对非浏览器客户端有
// 指纹门禁：Go 的 TLS 握手会被降级到 HTTP/1.1 后直接断连（EOF），而同一时刻、
// 同一出口 IP 的浏览器请求正常返回。桌面端因此提供本机桥接，后端只与它通信。
// 没有桥接（网页版、命令行）时返回 nil，保持原有直连行为。
type Push2Proxy struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewPush2Proxy 构造桥接客户端；地址为空时返回 nil，表示没有可用的转发通道。
func NewPush2Proxy(baseURL string, token string) *Push2Proxy {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Push2Proxy{
		baseURL: baseURL,
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: push2BridgeTimeout},
	}
}

// NewPush2ProxyFromEnv 读取桌面端注入的桥接配置。
func NewPush2ProxyFromEnv() *Push2Proxy {
	return NewPush2Proxy(os.Getenv(push2BridgeEnvURL), os.Getenv(push2BridgeEnvToken))
}

// accepts 判断请求是否属于需要走桥接的 push2 报价接口。
func (p *Push2Proxy) accepts(rawURL string) bool {
	if p == nil {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return false
	}
	if !isPush2QuoteHost(parsed.Hostname()) {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/api/")
}

// isPush2QuoteHost 覆盖 push2 的三个固定入口与 90.push2 这类编号镜像。
func isPush2QuoteHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "push2.eastmoney.com", "push2delay.eastmoney.com", "push2his.eastmoney.com":
		return true
	}
	suffix := ".push2.eastmoney.com"
	if !strings.HasSuffix(host, suffix) {
		return false
	}
	prefix := strings.TrimSuffix(host, suffix)
	if prefix == "" {
		return false
	}
	for _, char := range prefix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// get 让桥接代发一次 GET，并返回上游的响应体。
func (p *Push2Proxy) get(ctx context.Context, rawURL string) ([]byte, error) {
	endpoint := p.baseURL + "/v1/eastmoney/push2?url=" + url.QueryEscape(rawURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		request.Header.Set("X-A-Stock-Browser-Token", p.token)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("东方财富 push2 桥接请求失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, push2BridgeMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("读取东方财富 push2 桥接响应失败: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("东方财富 push2 桥接返回 %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("东方财富 push2 桥接响应格式无效: %w", err)
	}
	if !payload.OK {
		return nil, fmt.Errorf("东方财富 push2 桥接失败: %s", strings.TrimSpace(payload.Error))
	}
	if payload.Data.Status != http.StatusOK {
		return nil, fmt.Errorf("东方财富 push2 上游返回 %d", payload.Data.Status)
	}
	if strings.TrimSpace(payload.Data.Body) == "" {
		return nil, fmt.Errorf("东方财富 push2 桥接返回空响应")
	}
	return []byte(payload.Data.Body), nil
}
