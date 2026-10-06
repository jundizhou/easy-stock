// Package quant 聚合自研 Quant Terminal（默认 http://127.0.0.1:8766）。
//
// Quant Terminal 是自研单文件 Python 量化终端，提供行情、回测、板块归因、
// 市场状态（MER）等能力，典型端点：
//   - /api/kline?code=&kind=&days=&adj=
//   - /api/board_attrib
//   - /api/sys
//   - /api/hubstatus
//
// 本客户端提供通用的 GET 透传能力，由 httpapi 层封装成 /api/v1/quant/* 路由。
// 仅依赖标准库。
package quant

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:8766"
	defaultTimeout = 12 * time.Second
)

// Client 是 Quant Terminal 的 HTTP 聚合客户端。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option 用于构造 Client 的可选参数。
type Option func(*Client)

// WithBaseURL 设置 Quant Terminal 基址（含协议与端口）。
func WithBaseURL(value string) Option {
	return func(c *Client) {
		if strings.TrimSpace(value) != "" {
			c.baseURL = strings.TrimRight(value, "/")
		}
	}
}

// WithHTTPClient 注入自定义 *http.Client（用于测试或调优超时）。
func WithHTTPClient(value *http.Client) Option {
	return func(c *Client) {
		if value != nil {
			c.httpClient = value
		}
	}
}

// NewClient 创建 Quant Terminal 客户端。
func NewClient(options ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range options {
		opt(c)
	}
	return c
}

// Proxy 将一次 GET 透传到 Quant Terminal 的 path（形如 /api/kline），
// 转发 query 参数，返回上游状态码与原始响应体。出错时 body 为 nil。
func (c *Client) Proxy(ctx context.Context, path string, query url.Values) (status int, body []byte, err error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("quant: proxy %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("quant: read %s: %w", path, err)
	}
	return resp.StatusCode, body, nil
}
