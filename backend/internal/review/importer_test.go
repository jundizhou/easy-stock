package review

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestImporterParsesSupportedPublicArticle(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `<html><head><meta property="og:title" content="8月4日复盘"><meta name="author" content="交易者甲"><meta name="description" content="市场高低切观察"><meta property="article:published_time" content="2026-08-04T15:30:00+08:00"></head><body><article><h1>8月4日复盘</h1><p>机器人板块走强。</p><script>alert(1)</script></article></body></html>`
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
	if got := post.PublishedAt.In(shanghaiLocation()).Format("2006-01-02 15:04"); got != "2026-08-04 15:30" {
		t.Fatalf("published_at = %s", got)
	}
}

func TestImporterRejectsPublicArticleWithoutPublishedTime(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `<html><head><meta property="og:title" content="旧文章"><meta name="author" content="作者"></head><body><article>正文内容</article></body></html>`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	_, err := NewImporter(client, "").ImportURL(context.Background(), "https://www.tgb.cn/a/old")
	if err == nil || !strings.Contains(err.Error(), "可靠的发布时间") {
		t.Fatalf("error = %v", err)
	}
}

func TestPublicPagePublishedAtParsesUnixMilliseconds(t *testing.T) {
	publishedAt, ok := publicPagePublishedAt(`<script>var create_time = "1785838200000";</script>`)
	if !ok || publishedAt.Before(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("publishedAt=%v ok=%v", publishedAt, ok)
	}
}

func TestImporterRejectsUnsupportedHost(t *testing.T) {
	_, err := NewImporter(nil, "").ImportURL(context.Background(), "https://example.com/article")
	if err == nil || !strings.Contains(err.Error(), "只支持") {
		t.Fatalf("unexpected error: %v", err)
	}
}
