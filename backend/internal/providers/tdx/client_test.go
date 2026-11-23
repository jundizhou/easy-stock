package tdx

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"github.com/bensema/gotdx/proto"
	"github.com/bensema/gotdx/types"
)

func TestMACDailyBarsUseExchangeAdjustmentAndVolumeUnit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var header proto.ReqHeader
		var req proto.MACSymbolBarsRequest
		for _, v := range []any{&header, &req} {
			if err := binary.Read(conn, binary.LittleEndian, v); err != nil {
				t.Error(err)
				return
			}
		}
		if header.Method != proto.KMSG_MACSYMBOLBARS || req.Market != 1 || string(req.Code[:6]) != "600519" || req.Count != 2 || req.Adjust != types.AdjustQFQ || req.Period != types.KLINE_TYPE_DAILY {
			t.Errorf("unexpected MAC request %+v", req)
		}
		// One preceding close, one requested bar, followed by the instrument trailer.
		body := make([]byte, 33+2*36+120)
		binary.LittleEndian.PutUint16(body, 1)
		copy(body[2:14], req.Code[:])
		body[24] = byte(req.Period)
		binary.LittleEndian.PutUint16(body[27:], 2)
		putFloat := func(at int, value float32) { binary.LittleEndian.PutUint32(body[at:], math.Float32bits(value)) }
		for i, day := range []uint32{20261120, 20261123} {
			start := 33 + i*36
			binary.LittleEndian.PutUint32(body[start:], day)
			for field, value := range []float32{10, 12, 9, 10 + float32(i), 22000, 2000, 100} {
				putFloat(start+8+field*4, value)
			}
		}
		putFloat(33+2*36+47, 100)
		var compressed bytes.Buffer
		z := zlib.NewWriter(&compressed)
		z.Write(body)
		z.Close()
		h := proto.RespHeader{Method: header.Method, ZipSize: uint16(compressed.Len()), UnZipSize: uint16(len(body))}
		if err := binary.Write(conn, binary.LittleEndian, h); err != nil {
			t.Error(err)
			return
		}
		if _, err := io.Copy(conn, &compressed); err != nil {
			t.Error(err)
		}
	}()
	c := NewClient()
	c.hosts = []string{listener.Addr().String()}
	bars, err := c.KLine(context.Background(), "sh600519", "day", 1)
	<-done
	if err != nil || len(bars) != 1 {
		t.Fatalf("bars=%v err=%v", bars, err)
	}
	b := bars[0]
	if b.Meta.Source != "tdx:kline" || b.Symbol != "600519.SH" || b.Volume != 20 || b.Amount != 22000 || b.TurnoverRate != 0.2 || b.PreviousClose != 10 || b.ChangePercent != 10 || b.Time.Format(time.RFC3339) != "2026-11-23T00:00:00+08:00" {
		t.Fatalf("unexpected bar %+v", b)
	}
}

func TestCancellationInterruptsSocketWithoutCoolingHost(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := NewClient()
	c.hosts = []string{listener.Addr().String()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.KLine(ctx, "000001.SZ", "day", 2); done <- err }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !c.retryAfter[0].IsZero() {
			t.Fatalf("cancel poisoned host: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt pending quote")
	}
}

func TestIndexUsesExplicitExchangeAndSkipsUnsupportedMarkets(t *testing.T) {
	for _, tc := range []struct {
		id     string
		market uint16
		code   string
	}{
		{"sse", 1, "000001"}, {"szse", 0, "399001"}, {"sse_a", 1, "000003"},
	} {
		d, ok := foundation.FindMarketIndex(tc.id)
		stock, supported := indexSymbol(d)
		if !ok || !supported || stock.Market != tc.market || string(stock.Code[:6]) != tc.code {
			t.Fatalf("%s routed to %+v", tc.id, stock)
		}
	}
	c := NewClient()
	for _, id := range []string{"hsi", "bse50", "nasdaq"} {
		d, _ := foundation.FindMarketIndex(id)
		if _, ok := indexSymbol(d); ok {
			t.Fatalf("%s routed to mainland MAC", id)
		}
	}
	if c.SupportsKLine("00700.HK", "day") || c.SupportsKLine("920001.BJ", "day") || c.SupportsKLine("600519.SH", "120") {
		t.Fatal("unsupported market or period routed to MAC")
	}
}
