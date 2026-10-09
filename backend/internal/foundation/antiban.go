package foundation

import (
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 本文件集中"降低被封概率"的获取策略：统一浏览器化请求头 + 每个主机的最小请求间隔限速。
// 目的是把 easy-stock 对上游（新浪/腾讯/东财/财联社等）的访问伪装成正常的浏览器流量，
// 并避免突发高频请求触发风控。

var browserUserAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edg/126.0.0.0 Safari/537.36",
}

var (
	uaRand   = rand.New(rand.NewSource(time.Now().UnixNano()))
	uaRandMu sync.Mutex
)

// RandomUserAgent 返回一个随机浏览器 UA。
func RandomUserAgent() string {
	uaRandMu.Lock()
	defer uaRandMu.Unlock()
	return browserUserAgents[uaRand.Intn(len(browserUserAgents))]
}

// ApplyBrowserHeaders 给请求套上浏览器化头（UA + Accept + Accept-Language），
// 只填补调用方未显式设置的字段，不覆盖已有的 Referer/Host 等。
func ApplyBrowserHeaders(req *http.Request) {
	if req == nil {
		return
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", RandomUserAgent())
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
	if req.Header.Get("Connection") == "" {
		req.Header.Set("Connection", "keep-alive")
	}
}

// ----------------------------------------------------------------------------
// 每主机最小请求间隔限速（防止突发高频触发风控）
// ----------------------------------------------------------------------------

var (
	hostLimiterMu sync.Mutex
	hostNext      = map[string]time.Time{}
)

// Throttle 保证对同一 host 的两次请求间隔不小于 minInterval，超时则等待或返回取消。
// 返回 true 表示已获准继续（等待完成），false 表示 ctx 在等待期间被取消。
func Throttle(ctx interface{ Done() <-chan struct{} }, host string, minInterval time.Duration) bool {
	if minInterval <= 0 {
		return true
	}
	host = strings.ToLower(strings.TrimSpace(host))

	hostLimiterMu.Lock()
	now := time.Now()
	readyAt := hostNext[host]
	if readyAt.Before(now) {
		readyAt = now
	}
	next := readyAt.Add(minInterval)
	hostNext[host] = next
	hostLimiterMu.Unlock()

	wait := time.Until(readyAt)
	if wait <= 0 {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// HostOf 从完整 URL 提取主机名（不含端口）。
func HostOf(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	idx := strings.Index(rawURL, "://")
	if idx >= 0 {
		rawURL = rawURL[idx+3:]
	}
	if slash := strings.IndexByte(rawURL, '/'); slash >= 0 {
		rawURL = rawURL[:slash]
	}
	if colon := strings.IndexByte(rawURL, ':'); colon >= 0 {
		rawURL = rawURL[:colon]
	}
	return strings.ToLower(rawURL)
}
