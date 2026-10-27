package sina

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

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
