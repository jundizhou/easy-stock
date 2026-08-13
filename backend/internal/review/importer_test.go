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

func TestNormalizeImportedContentRejectsMinifiedScript(t *testing.T) {
	_, err := normalizeImportedContent(`"}}},{key:"watch",value:function(t,e){this._watcher[t]=e}},Object.defineProperty(e.prototype,"value",{writable:!1}),document.querySelectorAll("*")`)
	if err == nil || !strings.Contains(err.Error(), "网页脚本") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeImportedContentRejectsCSSColorTable(t *testing.T) {
	_, err := normalizeImportedContent(`aliceblue:[240,248,255],magenta:[255,0,255],mediumaquamarine:[102,205,170],mediumslateblue:[123,104,238],navajowhite:[255,222,173]`)
	if err == nil || !strings.Contains(err.Error(), "网页脚本") {
		t.Fatalf("error = %v", err)
	}
}

func TestImporterRejectsPublicPageCSSColorTable(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `<html><head><meta property="og:title" content="错误正文"><meta property="article:published_time" content="2026-08-13T15:30:00+08:00"></head><body><article>aliceblue:[240,248,255],magenta:[255,0,255],mediumaquamarine:[102,205,170],mediumslateblue:[123,104,238]</article></body></html>`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	_, err := NewImporter(client, "").ImportURL(context.Background(), "https://mp.weixin.qq.com/s/test")
	if err == nil || !strings.Contains(err.Error(), "样式数据") {
		t.Fatalf("error = %v", err)
	}
}

func TestImporterRecoversWechatBodyWhenSidecarReturnsPageBundle(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			body := `{"success":true,"data":{"title":"坚守","plain_content":"aliceblue:[240,248,255],magenta:[255,0,255],mediumaquamarine:[102,205,170]","author":"我是股公子","publish_time":1786581780}}`
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}
		body := `<html><head><meta property="og:title" content="坚守"><meta name="author" content="我是股公子"><script>var create_time = "1786581780";var colors={magenta:[255,0,255],mediumaquamarine:[102,205,170]};</script></head><body><div class="rich_media_content" id="js_content"><p>人最怕的就是不知行合一。</p><p>最好的操作就是不操作。</p></div><script>Object.defineProperty(x.prototype,"x",{})</script></body></html>`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	post, err := NewImporter(client, "http://127.0.0.1:30000").ImportURL(context.Background(), "https://mp.weixin.qq.com/s/tjMOgByp7Borq90c8O66EQ")
	if err != nil {
		t.Fatalf("ImportURL failed: %v", err)
	}
	if post.Title != "坚守" || post.AuthorName != "我是股公子" || !strings.Contains(post.ContentText, "不知行合一") {
		t.Fatalf("post = %+v", post)
	}
	if strings.Contains(post.ContentText, "mediumaquamarine") || strings.Contains(post.ContentText, "Object.defineProperty") {
		t.Fatalf("polluted content = %q", post.ContentText)
	}
}

func TestNormalizeImportedContentExtractsWechatBody(t *testing.T) {
	content, err := normalizeImportedContent(`<html><body><script>Object.defineProperty(x.prototype,"x",{})</script><div id="js_content"><p>真正的文章正文。</p><p>市场先分歧后回流。</p></div></body></html>`)
	if err != nil {
		t.Fatalf("normalizeImportedContent failed: %v", err)
	}
	if strings.Contains(content, "Object.defineProperty") || !strings.Contains(content, "真正的文章正文") {
		t.Fatalf("content = %q", content)
	}
}

func TestArticleDocumentExtractsNestedWechatBody(t *testing.T) {
	document := `<html><body><script>var colors={magenta:[255,0,255]};</script><div class="rich_media_content" id="js_content"><section><div><p>嵌套的微信正文。</p></div></section><p>正文结尾。</p></div><script>Object.defineProperty(x.prototype,"x",{})</script></body></html>`
	content := cleanDocument(articleDocument(document))
	if content != "嵌套的微信正文。\n正文结尾。" {
		t.Fatalf("content = %q", content)
	}
}
