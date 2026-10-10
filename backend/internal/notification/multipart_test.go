package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/appsettings"
)

func TestSplitMessagePreservesAllTextWithinWireBudget(t *testing.T) {
	for _, body := range []string{"", "short", strings.Repeat("中文😀<&>\"\\\n", 4000), strings.Repeat("长段落", 10000)} {
		parts := SplitMessage(Message{Title: "巡检报告", Text: body})
		var joined strings.Builder
		for _, part := range parts {
			encoded, _ := json.Marshal(part.Text)
			if len(encoded)-2 > messageTextBudget || !utf8.ValidString(part.Text) {
				t.Fatal("invalid chunk")
			}
			if len(parts) > 1 && !strings.Contains(part.Title, "/") {
				t.Fatal("missing part numbering")
			}
			joined.WriteString(part.Text)
		}
		if joined.String() != body {
			t.Fatal("text lost during split")
		}
	}
}

func TestMultipartWirePayloadsBothPlatforms(t *testing.T) {
	original := strings.Repeat("**逐股判断**\n\n中文😀<&>\"\\：确认条件与退出条件。\n\n", 800)
	for _, channel := range []string{"dingtalk", "feishu"} {
		t.Run(channel, func(t *testing.T) {
			webhook := "https://open.feishu.cn/open-apis/bot/v2/hook/test"
			if channel == "dingtalk" {
				webhook = "https://oapi.dingtalk.com/robot/send?access_token=test"
			}
			cfg := appsettings.NotificationChannel{Webhook: webhook, Secret: "SEC-test", Keyword: strings.Repeat("关", 100)}
			sender := NewSender()
			var got strings.Builder
			sender.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(req.Body)
				if len(raw) > maxPayloadBytes {
					t.Fatalf("oversized payload: %d", len(raw))
				}
				var body map[string]any
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				text := ""
				if channel == "dingtalk" {
					md := body["markdown"].(map[string]any)
					text = strings.TrimPrefix(md["text"].(string), "### "+md["title"].(string)+"\n\n")
				} else {
					card := body["card"].(map[string]any)
					element := card["elements"].([]any)[0].(map[string]any)
					text = element["text"].(map[string]any)["content"].(string)
				}
				got.WriteString(strings.TrimPrefix(text, cfg.Keyword+"\n"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"errcode":0}`))}, nil
			})
			parts := SplitMessage(Message{Title: strings.Repeat("标题", 100), Text: original})
			for _, part := range parts {
				if err := sender.Send(context.Background(), channel, cfg, part); err != nil {
					t.Fatal(err)
				}
			}
			if got.String() != original {
				t.Fatal("wire payload truncated report")
			}
			if err := sender.Send(context.Background(), channel, cfg, Message{Text: original}); err == nil {
				t.Fatal("unsplit oversized body accepted")
			}
		})
	}
}

func TestMultipartDispatcherStopsOnFailureDisableAndClose(t *testing.T) {
	for _, mode := range []string{"success", "failure", "disable", "close"} {
		t.Run(mode, func(t *testing.T) {
			cfg := appsettings.Notifications{Dingtalk: appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}, Events: appsettings.NotificationEvents{PortfolioInspection: true}}
			count := 0
			var d *Dispatcher
			d = NewDispatcher(func(_ context.Context, channel string, _ appsettings.NotificationChannel, _ Message) error {
				count++
				if channel != "dingtalk" {
					t.Fatal("wrong channel")
				}
				switch mode {
				case "failure":
					return errors.New("fixture failure")
				case "disable":
					cfg.Dingtalk.Enabled = false
				case "close":
					d.cancel()
				}
				return nil
			}, func() appsettings.Notifications { return cfg }, nil)
			defer d.Close()
			d.partInterval = 0
			message := Message{Title: "巡检", Text: strings.Repeat("完整结果\n", 3000)}
			d.deliver(delivery{channel: "dingtalk", event: Event{Kind: "portfolio_inspection", Message: message}})
			want := 1
			if mode == "success" {
				want = len(SplitMessage(message))
			}
			if count != want {
				t.Fatalf("deliveries=%d want=%d", count, want)
			}
		})
	}
	// Closing the dispatcher must interrupt the spacing wait immediately.
	cfg := appsettings.Notifications{Dingtalk: appsettings.NotificationChannel{Enabled: true, Webhook: "fixture"}, Events: appsettings.NotificationEvents{PortfolioInspection: true}}
	first := make(chan struct{})
	d := NewDispatcher(func(context.Context, string, appsettings.NotificationChannel, Message) error {
		close(first)
		return nil
	}, func() appsettings.Notifications { return cfg }, nil)
	d.Publish(Event{Kind: "portfolio_inspection", Message: Message{Text: strings.Repeat("长报告", 10000)}})
	<-first
	done := make(chan struct{})
	go func() { d.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close blocked on multipart pacing")
	}
}
