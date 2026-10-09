package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"easy-stock/backend/internal/appsettings"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSenderPlatformProtocolsAndSignatures(t *testing.T) {
	for _, channel := range []string{"feishu", "dingtalk"} {
		t.Run(channel, func(t *testing.T) {
			webhook := "https://open.feishu.cn/open-apis/bot/v2/hook/private-bot-id"
			if channel == "dingtalk" {
				webhook = "https://oapi.dingtalk.com/robot/send?access_token=private-token&timestamp=expired&sign=expired"
			}
			sender := NewSender()
			sender.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("invalid request: %s %v", r.Method, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				timestamp, sign, key, data := "", "", "", ""
				if channel == "feishu" {
					timestamp, sign = body["timestamp"].(string), body["sign"].(string)
					key = timestamp + "\nSEC-private"
					if body["msg_type"] != "interactive" || !strings.Contains(mustJSON(body), "安全关键词") || !strings.Contains(mustJSON(body), "个股研究完成") {
						t.Fatalf("invalid Feishu card: %v", body)
					}
				} else {
					query := r.URL.Query()
					timestamp, sign = query.Get("timestamp"), query.Get("sign")
					key, data = "SEC-private", timestamp+"\nSEC-private"
					if query.Get("access_token") != "private-token" || body["msgtype"] != "markdown" || len(timestamp) != 13 {
						t.Fatalf("invalid DingTalk request: %v", body)
					}
				}
				mac := hmac.New(sha256.New, []byte(key))
				mac.Write([]byte(data))
				if timestamp == "" || timestamp == "expired" || sign != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
					t.Fatalf("invalid platform signature")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"errcode":0}`))}, nil
			})
			if err := sender.Send(context.Background(), channel, appsettings.NotificationChannel{Webhook: webhook, Secret: "SEC-private", Keyword: "安全关键词"}, Message{Title: "个股研究完成", Text: "研究摘要"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func mustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

func TestSenderRejectsUnconfirmedResponsesAndRedactsErrors(t *testing.T) {
	for _, tc := range []struct {
		name, channel, body string
		status              int
		want                string
	}{
		{"feishu error", "feishu", `{"code":19021,"msg":"private-bot-id"}`, 200, "19021"},
		{"dingtalk error", "dingtalk", `{"errcode":310000,"errmsg":"private-token"}`, 200, "310000"},
		{"missing code", "feishu", `{}`, 200, "缺少状态码"},
		{"bad json", "feishu", `private-bot-id`, 200, "无效"},
		{"http failure", "feishu", `private-bot-id`, 403, "HTTP 403"},
		{"redirect", "feishu", ``, 302, "HTTP 302"},
		{"legacy success", "feishu", `{"StatusCode":0}`, 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := NewSender()
			sender.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			webhook := "https://open.feishu.cn/open-apis/bot/v2/hook/private-bot-id"
			if tc.channel == "dingtalk" {
				webhook = "https://oapi.dingtalk.com/robot/send?access_token=private-token"
			}
			err := sender.Send(context.Background(), tc.channel, appsettings.NotificationChannel{Webhook: webhook}, Message{})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unexpected response error: %v", err)
			}
		})
	}
	sender := NewSender()
	sender.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("private-bot-id leaked by transport")
	})
	if err := sender.Send(context.Background(), "feishu", appsettings.NotificationChannel{Webhook: "https://open.feishu.cn/open-apis/bot/v2/hook/private-bot-id"}, Message{}); err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatalf("transport error leaked webhook: %v", err)
	}
}

func TestWebhookValidation(t *testing.T) {
	for _, tc := range []struct {
		channel, url string
		valid        bool
	}{
		{"feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/bot-id", true},
		{"feishu", "https://open.larksuite.com/open-apis/bot/v2/hook/bot-id", true},
		{"dingtalk", "https://oapi.dingtalk.com/robot/send?access_token=token", true},
		{"feishu", "http://open.feishu.cn/open-apis/bot/v2/hook/bot-id", false},
		{"feishu", "https://open.feishu.cn.attacker.test/open-apis/bot/v2/hook/bot-id", false},
		{"feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/", false},
		{"feishu", "https://private-bot-id@open.feishu.cn/open-apis/bot/v2/hook/bot-id", false},
		{"dingtalk", "https://oapi.dingtalk.com/robot/send", false},
		{"dingtalk", "https://oapi.dingtalk.com/robot/send?access_token=a&access_token=b", false},
		{"dingtalk", "http://127.0.0.1/robot/send?access_token=token", false},
		{"other", "https://example.com", false},
	} {
		err := ValidateWebhook(tc.channel, tc.url)
		if (err == nil) != tc.valid {
			t.Errorf("validation %s = %v", tc.url, err)
		}
	}
}
