package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// 桌面端会话桥接（Electron 隐藏窗口现场取 nid18）的地址与令牌环境变量，
	// 由 desktop/main.cjs 注入。网页版后端没有这两个变量。
	eastmoneySessionBridgeEnvURL   = "A_STOCK_EASTMONEY_SESSION_BRIDGE_URL"
	eastmoneySessionBridgeEnvToken = "A_STOCK_EASTMONEY_SESSION_BRIDGE_TOKEN"

	// 会话最长沿用 15 分钟；桥接返回的过期时间更早时以过期时间为准。
	eastmoneySessionTTL = 15 * time.Minute
	// 取会话要等隐藏窗口加载并等待指纹 SDK 下发 Cookie，超时放宽一些。
	eastmoneySessionTimeout = 30 * time.Second
)

// SessionProvider 提供东财行情接口所需的浏览器会话 Cookie。
//
// push2 系的资金流分时接口（api/qt/stock/fflow/kline/get）带会话门禁：缺少浏览器
// 下发的 nid18 时上游会直接断开连接（Go EOF / curl 56），补上即可正常返回分钟序列。
// nid18 由东财页面里的指纹 SDK 生成，后端不伪造也不持久化，只按需向桌面端索取。
type SessionProvider interface {
	// SessionCookie 返回可直接放入 Cookie 请求头的值；refresh 为 true 时跳过本地缓存强制重新获取。
	SessionCookie(ctx context.Context, refresh bool) (string, error)
}

// BrowserSessionProvider 通过桌面端会话桥接获取 nid18。
type BrowserSessionProvider struct {
	baseURL string
	token   string
	client  *http.Client

	mu        sync.Mutex
	cookie    string
	expiresAt time.Time
}

// NewBrowserSessionProvider 使用桥接地址构造会话来源；地址为空时返回 nil，表示没有可用会话。
func NewBrowserSessionProvider(baseURL string, token string) *BrowserSessionProvider {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &BrowserSessionProvider{
		baseURL: baseURL,
		token:   strings.TrimSpace(token),
		client:  &http.Client{Timeout: eastmoneySessionTimeout},
	}
}

// NewBrowserSessionProviderFromEnv 读取桌面端注入的桥接配置。
//
// 未配置时返回 nil：此时分时资金流按“无会话”请求上游，保持原有的降级行为
// （网页版与命令行运行都走这条路径）。
func NewBrowserSessionProviderFromEnv() *BrowserSessionProvider {
	return NewBrowserSessionProvider(
		os.Getenv(eastmoneySessionBridgeEnvURL),
		os.Getenv(eastmoneySessionBridgeEnvToken),
	)
}

func (p *BrowserSessionProvider) SessionCookie(ctx context.Context, refresh bool) (string, error) {
	if !refresh {
		p.mu.Lock()
		cookie, expiresAt := p.cookie, p.expiresAt
		p.mu.Unlock()
		if cookie != "" && time.Now().Before(expiresAt) {
			return cookie, nil
		}
	}

	endpoint := p.baseURL + "/v1/eastmoney/session"
	if refresh {
		endpoint += "?refresh=1"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if p.token != "" {
		request.Header.Set("X-A-Stock-Browser-Token", p.token)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("东方财富会话桥接请求失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("读取东方财富会话桥接响应失败: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("东方财富会话桥接返回 %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		OK   bool `json:"ok"`
		Data struct {
			Cookie    string `json:"cookie"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("东方财富会话桥接响应格式无效: %w", err)
	}
	cookie := strings.TrimSpace(payload.Data.Cookie)
	if !payload.OK || cookie == "" {
		return "", fmt.Errorf("东方财富会话桥接未返回会话 Cookie")
	}

	expiresAt := time.Now().Add(eastmoneySessionTTL)
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.Data.ExpiresAt)); err == nil && parsed.Before(expiresAt) {
		expiresAt = parsed
	}
	p.mu.Lock()
	p.cookie, p.expiresAt = cookie, expiresAt
	p.mu.Unlock()
	return cookie, nil
}
