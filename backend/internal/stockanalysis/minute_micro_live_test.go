package stockanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"easy-stock/backend/internal/providers/stockdb"
)

// TestMinuteKLinesLive 真实拉取 free-stockdb 的 000001 分钟K并跑完整链路。
// 需要本地 7899 引擎在线，且显式设置 STOCKDB_LIVE=1 才执行：
//
//	STOCKDB_LIVE=1 go test ./internal/stockanalysis/ -run TestMinuteKLinesLive -v
func TestMinuteKLinesLive(t *testing.T) {
	if os.Getenv("STOCKDB_LIVE") == "" {
		t.Skip("set STOCKDB_LIVE=1 to run live test against 127.0.0.1:7899")
	}
	client := stockdb.NewClient()
	started := time.Now()
	all, err := client.MinuteKLines(context.Background(), "000001")
	if err != nil {
		t.Fatalf("MinuteKLines 拉取失败: %v", err)
	}
	elapsed := time.Since(started)
	t.Logf("全量分钟K %d 行，耗时 %s", len(all), elapsed)
	if len(all) < 10000 {
		t.Fatalf("全量行数异常偏少: %d", len(all))
	}

	converted := make([]MinuteBar, 0, len(all))
	for _, item := range all {
		converted = append(converted, MinuteBar{
			Stamp: item.Stamp, Open: item.Open, Close: item.Close,
			High: item.High, Low: item.Low, Volume: item.Volume, Amount: item.Amount,
		})
	}
	day := LatestMinuteDay(converted)
	if len(day) == 0 {
		t.Fatal("LatestMinuteDay 返回空")
	}
	t.Logf("最近交易日 %s，样本 %d 根（首根 %s，末根 %s）", day[0].Stamp[:8], len(day), day[0].Stamp, day[len(day)-1].Stamp)

	micro := analyzeMinuteMicro(day)
	if micro == nil {
		t.Fatal("真实数据 analyzeMinuteMicro 返回 nil")
	}
	encoded, _ := json.MarshalIndent(micro, "", "  ")
	fmt.Println(string(encoded))

	evidence := minuteMicroEvidence(micro)
	t.Logf("证据链: [%s] %s\n  %s (source=%s, as_of=%s)",
		evidence[0].Category, evidence[0].Title, evidence[0].Detail, evidence[0].Source, evidence[0].AsOf)
}
