package eastmoney

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestParseTHSBillboardSeatLabelsScopesToRequestedStock(t *testing.T) {
	document := `<div class="stockcont" stockcode="000001"><td class="tl rel"><a title="错误席位"></a><label class="label lb-red">知名游资</label></td></div>
	<div class="stockcont" stockcode="688137">
	<td class="tl rel"><a title="高盛（中国）证券有限责任公司上海浦东新区世纪大道证券营业部"></a><label class="label lb-red">一线游资</label></td>
	<td class="tl rel"><a title="机构专用"></a></td>
	</div>`
	labels := parseTHSBillboardSeatLabels(document, "688137")
	if len(labels) != 2 || labels[normalizeBillboardSeatName("高盛(中国)证券有限责任公司上海浦东新区世纪大道证券营业部")] != "一线游资" || labels[normalizeBillboardSeatName("机构专用")] != "机构" {
		t.Fatalf("labels=%+v", labels)
	}
}

func TestTHSLabelsEnrichMatchingSeatsAndFailOpen(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<div class="stockcont" stockcode="688137"><td class="tl rel"><a title="高盛（中国）证券有限责任公司上海浦东新区世纪大道证券营业部"></a><label class="label lb-red">一线游资</label></td></div>`))
	}))
	defer upstream.Close()
	client := NewClient(WithTHSBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	input := []foundation.MarketBillboardSeat{{Name: "高盛(中国)证券有限责任公司上海浦东新区世纪大道证券营业部"}}
	buy, _ := client.enrichBillboardSeatLabels(context.Background(), "688137.SH", "2026-08-13", append([]foundation.MarketBillboardSeat(nil), input...), nil)
	if buy[0].SourceLabel != "一线游资" || buy[0].Source != "ths" || buy[0].LabelConfidence != "high" {
		t.Fatalf("seat=%+v", buy[0])
	}
	institutionLabels := `<div class="stockcont" stockcode="688137"><td class="tl rel"><a title="机构专用"></a></td></div>`
	institutionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(institutionLabels)) }))
	defer institutionServer.Close()
	institutionClient := NewClient(WithTHSBaseURL(institutionServer.URL), WithHTTPClient(institutionServer.Client()))
	institutionSeats, _ := institutionClient.enrichBillboardSeatLabels(context.Background(), "688137.SH", "2026-08-13", []foundation.MarketBillboardSeat{{Name: "机构专用"}}, nil)
	if !institutionSeats[0].Institution || institutionSeats[0].SourceLabel != "机构" {
		t.Fatalf("institution label should set institution flag: %+v", institutionSeats[0])
	}

	failing := NewClient(WithTHSBaseURL("://invalid"), WithHTTPClient(&http.Client{}))
	unchanged, _ := failing.enrichBillboardSeatLabels(context.Background(), "688137.SH", "2026-08-13", input, nil)
	if unchanged[0].Name != input[0].Name || unchanged[0].SourceLabel != "" {
		t.Fatalf("failed enrichment must preserve original seat: %+v", unchanged[0])
	}
}
