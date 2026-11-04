package sina

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMarketSnapshotSlowPageDoesNotBlockLaterPageRequests(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); inFlight > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, inFlight) {
				break
			}
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 1 {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if page == sinaSnapshotWorkers+1 {
			once.Do(func() { close(release) })
		}
		count := sinaSnapshotPageSize
		if page == 8 {
			count = 1
		} else if page > 8 {
			count = 0
		}
		items := make([]string, count)
		for index := range items {
			items[index] = fmt.Sprintf(`{"symbol":"sz%06d","name":"测试样本","trade":"10","settlement":"9","open":"10","volume":1000}`, 1000+(page-1)*sinaSnapshotPageSize+index)
		}
		fmt.Fprintf(w, "[%s]", joinRaw(items))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, meta, err := NewClient(WithListBaseURL(server.URL)).MarketSnapshot(ctx)
	if err != nil || meta.Stale || len(rows) != 701 {
		t.Fatalf("slow page blocked later requests: rows=%d meta=%+v err=%v", len(rows), meta, err)
	}
	if peak.Load() > sinaSnapshotWorkers {
		t.Fatalf("concurrency grew beyond %d: %d", sinaSnapshotWorkers, peak.Load())
	}
	if rows[0].Symbol != "001000.SZ" || rows[100].Symbol != "001100.SZ" || rows[len(rows)-1].Symbol != "001700.SZ" {
		t.Fatal("out-of-order completion changed page order or dropped rows")
	}
}

func TestMarketSnapshotCancelledPoolDoesNotStartRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := NewClient(WithListBaseURL(server.URL)).MarketSnapshot(ctx)
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("cancelled pool issued requests or lost cancellation: calls=%d err=%v", calls.Load(), err)
	}
}

// 新浪列表按代码升序翻页，每页 100 行；这里造两页数据验证：
// 字段映射（价格/涨跌幅/换手/市值单位）、代码归一化、翻到短页就停。
func TestMarketSnapshotPagesAndFields(t *testing.T) {
	const pageSize = 100
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if got := r.URL.Query().Get("sort"); got != "symbol" {
			t.Errorf("翻页排序必须是代码升序（涨跌幅排序盘中会漏行），got %q", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch page {
		case 1:
			rows := make([]string, 0, pageSize)
			for i := 0; i < pageSize-2; i++ {
				rows = append(rows, fmt.Sprintf(`{"symbol":"sz%06d","name":"样本%d","trade":"10.00","changepercent":1.5,"open":"9.90","high":"10.20","low":"9.80","settlement":"10.00","volume":1000,"amount":100000,"turnoverratio":2.5,"mktcap":120000,"nmc":80000}`, 1000+i, i))
			}
			// 一只深市、一只沪市、一只北交所：覆盖三种代码前缀。
			rows = append(rows,
				`{"symbol":"sz002185","name":"华天科技","trade":"16.19","changepercent":4.12,"open":"15.60","high":"16.30","low":"15.50","settlement":"15.55","volume":123456,"amount":200000000,"turnoverratio":3.21,"mktcap":520000,"nmc":400000}`,
				`{"symbol":"sh600000","name":"浦发银行","trade":"9.10","changepercent":-0.22,"open":"9.12","high":"9.15","low":"9.05","settlement":"9.12","volume":500000,"amount":450000000,"turnoverratio":0.31,"mktcap":3000000,"nmc":2900000}`,
			)
			fmt.Fprintf(w, "[%s]", joinRaw(rows))
		case 2:
			// 短页意味着到最后一页了。
			fmt.Fprint(w, `[{"symbol":"bj920285","name":"N远大","trade":"35.32","changepercent":73.99,"open":"30.21","high":"47.66","low":"30.21","settlement":"20.30","volume":17439270,"amount":610523835,"turnoverratio":96.88,"mktcap":243708,"nmc":63576}]`)
		default:
			fmt.Fprint(w, "[]")
		}
	}))
	defer server.Close()

	client := NewClient(WithListBaseURL(server.URL))
	rows, meta, err := client.MarketSnapshot(context.Background())
	if err != nil {
		t.Fatalf("全市场快照不应报错：%v", err)
	}
	if len(rows) != pageSize+1 {
		t.Fatalf("应取到 %d 行（首页 %d + 次页 1），got %d", pageSize+1, pageSize, len(rows))
	}
	if meta.TradeDate == "" || meta.Source != "sina:market_center" {
		t.Fatalf("meta 不完整：%+v", meta)
	}

	bySymbol := map[string]struct {
		price   float64
		change  float64
		mktcap  float64
		board   string
		name    string
		prevCls float64
	}{}
	for _, row := range rows {
		bySymbol[row.Symbol] = struct {
			price   float64
			change  float64
			mktcap  float64
			board   string
			name    string
			prevCls float64
		}{row.Price, row.ChangePercent, row.MarketCap, row.Board, row.Name, row.PreviousClose}
	}
	huntian, ok := bySymbol["002185.SZ"]
	if !ok {
		t.Fatalf("应该按规范代码登记 002185.SZ，got keys 里有 %d 只", len(bySymbol))
	}
	if huntian.name != "华天科技" || huntian.price != 16.19 || huntian.change != 4.12 || huntian.prevCls != 15.55 {
		t.Fatalf("字段解析不对：%+v", huntian)
	}
	// 新浪 mktcap 是万元，快照统一用元。
	if huntian.mktcap != 520000*10000 {
		t.Fatalf("总市值应换成元，got %v", huntian.mktcap)
	}
	if huntian.board != "main" {
		t.Fatalf("002185 应判主板，got %q", huntian.board)
	}
	if _, ok := bySymbol["600000.SH"]; !ok {
		t.Fatalf("沪市代码应归一化成 .SH")
	}
	if bse, ok := bySymbol["920285.BJ"]; !ok {
		t.Fatalf("北交所代码应归一化成 .BJ")
	} else if bse.board != "bse" {
		t.Fatalf("920285 应判北交所，got %q", bse.board)
	}
}

func joinRaw(values []string) string {
	out := ""
	for index, value := range values {
		if index > 0 {
			out += ","
		}
		out += value
	}
	return out
}

func TestMarketSnapshotRetriesOnlyTransientFailedPage(t *testing.T) {
	var mu sync.Mutex
	calls := map[int]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		calls[page]++
		attempt := calls[page]
		mu.Unlock()
		if page == 2 && attempt == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		if page == 1 {
			rows := make([]string, sinaSnapshotPageSize)
			for index := range rows {
				rows[index] = fmt.Sprintf(`{"symbol":"sz%06d","name":"测试样本","trade":"10","settlement":"9","open":"10","volume":1000}`, 1000+index)
			}
			fmt.Fprintf(w, "[%s]", joinRaw(rows))
			return
		}
		if page == 2 {
			fmt.Fprint(w, `[{"symbol":"sh600000","name":"测试样本","trade":"10","settlement":"9","open":"10","volume":1000}]`)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	client := NewClient(WithListBaseURL(server.URL))
	rows, meta, err := client.MarketSnapshot(context.Background())
	if err != nil || meta.Stale || len(rows) != 101 {
		t.Fatalf("successful retry must recover a complete snapshot: rows=%d meta=%+v err=%v", len(rows), meta, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for page, count := range calls {
		want := 1
		if page == 2 {
			want = 2
		}
		if count != want {
			t.Fatalf("page %d requested %d times, want %d", page, count, want)
		}
	}
}

func TestMarketSnapshotPersistentPageFailureRemainsStale(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			mu.Lock()
			calls++
			mu.Unlock()
			http.Error(w, "still unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `[{"symbol":"sz001000","name":"测试样本","trade":"10","settlement":"9","open":"10","volume":1000}]`)
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	rows, meta, err := NewClient(WithListBaseURL(server.URL)).MarketSnapshot(context.Background())
	if err != nil || len(rows) != 1 || !meta.Stale || !strings.Contains(meta.FallbackReason, "503") {
		t.Fatalf("failed retry must retain incomplete flag and error: rows=%d meta=%+v err=%v", len(rows), meta, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("failed page must be attempted exactly twice, got %d", calls)
	}
}

func TestSnapshotPageDoesNotRetryPermanentErrorOrCancellation(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "permanent failure", status)
			}))
			defer server.Close()
			client := NewClient(WithListBaseURL(server.URL))
			if _, err := client.snapshotPage(context.Background(), 1); err == nil || calls.Load() != 1 {
				t.Fatalf("permanent error must not retry: calls=%d err=%v", calls.Load(), err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := client.snapshotPage(ctx, 1); !errors.Is(err, context.Canceled) || calls.Load() != 1 {
				t.Fatalf("cancelled page must not issue a request: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestSnapshotPageCancellationPreventsSecondAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := NewClient(WithListBaseURL(server.URL))
	// 首次响应后取消，即使原错误可重试，也不应再发送第二次请求。
	client.httpClient.Transport = cancelSnapshotTransport{base: http.DefaultTransport, cancel: cancel}
	if _, err := client.snapshotPage(ctx, 1); err == nil {
		t.Fatal("cancelled retry must retain the source error or cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("cancelled request retried: %d attempts", calls.Load())
	}
}

type cancelSnapshotTransport struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func (transport cancelSnapshotTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	transport.cancel()
	return response, err
}
