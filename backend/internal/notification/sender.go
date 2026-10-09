package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"easy-stock/backend/internal/appsettings"
)

const Timeout = 10 * time.Second

type Message struct {
	Title string
	Text  string
}

type Sender struct {
	client *http.Client
}

func NewSender() *Sender {
	return &Sender{client: &http.Client{Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func channelName(channel string) string {
	if channel == "feishu" {
		return "飞书"
	}
	return "钉钉"
}

// ValidateWebhook accepts only the official custom-bot endpoints. Never echo
// the supplied URL: DingTalk tokens and Feishu bot IDs are credentials.
func ValidateWebhook(channel, raw string) error {
	if channel != "feishu" && channel != "dingtalk" {
		return fmt.Errorf("不支持的通知渠道")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	valid := err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.User == nil && u.Fragment == "" && u.Port() == ""
	if valid && channel == "feishu" {
		id := strings.TrimPrefix(u.Path, "/open-apis/bot/v2/hook/")
		valid = (u.Host == "open.feishu.cn" || u.Host == "open.larksuite.com") && strings.HasPrefix(u.Path, "/open-apis/bot/v2/hook/") && id != "" && !strings.Contains(id, "/") && u.RawQuery == ""
	} else if valid {
		query, queryErr := url.ParseQuery(u.RawQuery)
		valid = queryErr == nil && u.Host == "oapi.dingtalk.com" && u.Path == "/robot/send" && len(query["access_token"]) == 1 && strings.TrimSpace(query.Get("access_token")) != ""
	}
	if !valid {
		return fmt.Errorf("%s Webhook 必须是官方自定义机器人 HTTPS 地址", channelName(channel))
	}
	return nil
}

func signature(key, text string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(text))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Sender) Send(ctx context.Context, channel string, cfg appsettings.NotificationChannel, message Message) error {
	if err := ValidateWebhook(channel, cfg.Webhook); err != nil {
		return err
	}
	endpoint, _ := url.Parse(strings.TrimSpace(cfg.Webhook))
	title := clip(message.Title, 100)
	text := clip(message.Text, 2500)
	if cfg.Keyword != "" {
		text = cfg.Keyword + "\n" + text
	}
	var payload map[string]any
	if channel == "feishu" {
		payload = map[string]any{"msg_type": "interactive", "card": map[string]any{
			"header":   map[string]any{"title": map[string]string{"tag": "plain_text", "content": title}},
			"elements": []any{map[string]any{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": text}}},
		}}
		if cfg.Secret != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			payload["timestamp"] = timestamp
			payload["sign"] = signature(timestamp+"\n"+cfg.Secret, "")
		}
	} else {
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "\n\n" + text}}
		query := endpoint.Query()
		// The saved URL may include an expired signature. Generate our own.
		query.Del("timestamp")
		query.Del("sign")
		if cfg.Secret != "" {
			timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
			query.Set("timestamp", timestamp)
			query.Set("sign", signature(cfg.Secret, timestamp+"\n"+cfg.Secret))
		}
		endpoint.RawQuery = query.Encode()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("构造通知失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造通知请求失败")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// url.Error embeds the full webhook. Return a stable, secret-free error.
		if ctx.Err() != nil {
			return fmt.Errorf("%s通知发送超时或已取消", channelName(channel))
		}
		return fmt.Errorf("%s通知连接失败，请检查网络和 Webhook", channelName(channel))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s通知发送失败（HTTP %d）", channelName(channel), resp.StatusCode)
	}
	var result struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
		ErrCode    *int `json:"errcode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("%s返回了无效的通知响应", channelName(channel))
	}
	code := result.ErrCode
	if channel == "feishu" {
		code = result.Code
		if code == nil {
			code = result.StatusCode
		}
	}
	if code == nil {
		return fmt.Errorf("%s响应缺少状态码，无法确认发送成功", channelName(channel))
	}
	if *code != 0 {
		return fmt.Errorf("%s拒绝通知（错误码 %d），请检查机器人安全设置、签名和关键词", channelName(channel), *code)
	}
	return nil
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
