package review

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestImporterParsesSupportedPublicArticle(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `<html><head><meta property="og:title" content="8月4日复盘"><meta name="author" content="交易者甲"><meta name="description" content="市场高低切观察"></head><body><article><h1>8月4日复盘</h1><p>机器人板块走强。</p><script>alert(1)</script></article></body></html>`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	post, err := NewImporter(client, "").ImportURL(context.Background(), "https://www.tgb.cn/a/123")
	if err != nil {
		t.Fatalf("ImportURL failed: %v", err)
	}
	if post.Source != "taoguba" || post.Title != "8月4日复盘" || post.AuthorName != "交易者甲" {
		t.Fatalf("unexpected post: %+v", post)
	}
	if strings.Contains(post.ContentText, "alert(1)") || !strings.Contains(post.ContentText, "机器人板块走强") {
		t.Fatalf("content was not cleaned: %q", post.ContentText)
	}
}

func TestImporterRejectsUnsupportedHost(t *testing.T) {
	_, err := NewImporter(nil, "").ImportURL(context.Background(), "https://example.com/article")
	if err == nil || !strings.Contains(err.Error(), "只支持") {
		t.Fatalf("unexpected error: %v", err)
	}
}
