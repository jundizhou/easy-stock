package xueqiu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestToSnowballSymbol(t *testing.T) {
	cases := map[string]string{
		"600519.SH": "SH600519",
		"000001.SZ": "SZ000001",
		"300750.SZ": "SZ300750",
		"430047.BJ": "BJ430047",
		"600519":    "SH600519",
		"SH600519":  "SH600519",
	}
	for input, want := range cases {
		got, err := toSnowballSymbol(input)
		if err != nil || got != want {
			t.Fatalf("toSnowballSymbol(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := toSnowballSymbol("invalid!"); err == nil {
		t.Fatalf("invalid symbol should error")
	}
}

// seedServer 模拟播种响应（下发 xq_a_token + u），后续按路径返回业务 JSON。
func newTestEnv(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server, *atomic.Int32) {
	var seeds atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/hq", func(w http.ResponseWriter, r *http.Request) {
		seeds.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "xq_a_token", Value: "tok-" + strconv.Itoa(int(seeds.Load()))})
		http.SetCookie(w, &http.Cookie{Name: "u", Value: "123456"})
		w.WriteHeader(200)
	})
	mux.HandleFunc("/", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewClient()
	// 把播种与业务都指向测试服务器：播种 URL 无法注入，改为先手动种 cookie。
	client.mu.Lock()
	client.cookie = "xq_a_token=test; u=123"
	client.mu.Unlock()
	return client, server, &seeds
}

// serverClient 把所有请求（含硬编码的播种域名）重写到测试服务器。
func serverClient(server *httptest.Server) *http.Client {
	target := strings.TrimPrefix(server.URL, "http://")
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = target
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHotStocksParsesList(t *testing.T) {
	client, server, _ := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v5/stock/hot_stock/list.json") {
			w.WriteHeader(404)
			return
		}
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "xq_a_token=test") {
			t.Fatalf("cookie header missing: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[{"symbol":"SH600519","name":"贵州茅台","current":1500.5,"percent":-1.2,"rank_change":3,"increment":12000},{"symbol":"SZ300750","name":"宁德时代","current":380,"percent":2.1}]}}`))
	})
	// 重定向播种与业务到测试服务器
	client.http = serverClient(server)
	hot, err := client.HotStocks(context.Background(), 10)
	if err != nil {
		t.Fatalf("HotStocks: %v", err)
	}
	if len(hot) != 2 || hot[0].Name != "贵州茅台" || hot[0].Rank != 1 || hot[1].Rank != 2 {
		t.Fatalf("unexpected hot list: %+v", hot)
	}
}

func TestCookieExpiryReseedsAndRetries(t *testing.T) {
	var calls atomic.Int32
	client, server, seeds := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/statuses/livenews/list.json") {
			if calls.Add(1) == 1 {
				// 第一次返回 400016：cookie 失效
				_, _ = w.Write([]byte(`{"error_code":400016,"error_description":"Cookie 失效"}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":100,"text":"快讯内容","mark":1,"created_at":1757000000000}]}`))
			return
		}
		w.WriteHeader(404)
	})
	client.http = serverClient(server)
	news, err := client.News(context.Background(), 10)
	if err != nil {
		t.Fatalf("News: %v", err)
	}
	if len(news) != 1 || news[0].Mark != 1 || news[0].Text != "快讯内容" {
		t.Fatalf("unexpected news: %+v", news)
	}
	// 预设 cookie 不经播种（seeds=0）；400016 后必须重播种恰好一次。
	if seeds.Load() != 1 {
		t.Fatalf("cookie should be reseeded exactly once after 400016, seeds=%d", seeds.Load())
	}
}

func TestInflightDedup(t *testing.T) {
	var calls atomic.Int32
	client, server, _ := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(`{"data":{"items":[{"symbol":"SH600519","name":"贵州茅台","current":1}]}}`))
	})
	client.http = serverClient(server)
	ctx := context.Background()
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			if _, err := client.HotStocks(ctx, 5); err != nil {
				t.Errorf("HotStocks: %v", err)
			}
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	if calls.Load() != 1 {
		t.Fatalf("concurrent same-URL calls should dedupe to one upstream call, got %d", calls.Load())
	}
}

// TestCacheKeyStableAcrossMapOrder 防回归：getJSON 拼 URL 前必须对 params 排序。
// map 遍历顺序随机，不排序时同一逻辑请求会生成参数顺序不同的 URL，缓存与
// in-flight 的 key 分裂，去重失效（顺序调用必然出现两种顺序，旧实现必挂）。
func TestCacheKeyStableAcrossMapOrder(t *testing.T) {
	var calls atomic.Int32
	client, server, _ := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"data":{"items":[{"symbol":"SH600519","name":"贵州茅台","current":1}]}}`))
	})
	client.http = serverClient(server)
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		if _, err := client.HotStocks(ctx, 5); err != nil {
			t.Fatalf("HotStocks #%d: %v", i, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("repeated identical calls should be served from one cache entry, got %d upstream calls", calls.Load())
	}
}

func TestHotUsersParsesList(t *testing.T) {
	client, server, _ := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/recommend/user/stock_hot_user.json") {
			w.WriteHeader(404)
			return
		}
		if got := r.URL.Query().Get("symbol"); got != "SH600519" {
			t.Fatalf("symbol param = %q, want SH600519", got)
		}
		_, _ = w.Write([]byte(`[{"id":12345678901234,"screen_name":"雪球大V","description":"价值投资","followers_count":500000,"status_count":3200,"verified":true}]`))
	})
	client.http = serverClient(server)
	users, err := client.HotUsers(context.Background(), "600519.SH", 8)
	if err != nil {
		t.Fatalf("HotUsers: %v", err)
	}
	if len(users) != 1 || users[0].ScreenName != "雪球大V" || !users[0].Verified || users[0].Followers != 500000 {
		t.Fatalf("unexpected users: %+v", users)
	}
}
