package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fakeRealtime struct {
	quotes []foundation.Quote
	err    error
}

func (f *fakeRealtime) Realtime(_ context.Context, _ []string) ([]foundation.Quote, error) {
	return f.quotes, f.err
}

type fakeNews struct {
	items []foundation.NewsItem
	err   error
}

func (f *fakeNews) LatestNews(_ context.Context, _ int) ([]foundation.NewsItem, error) {
	return f.items, f.err
}

func TestRealtimeArchiverWritesOnSuccess(t *testing.T) {
	root := t.TempDir()
	inner := &fakeRealtime{quotes: []foundation.Quote{
		{Symbol: "600519.SH", Price: 1275, Meta: foundation.SourceMeta{Source: "sina"}},
		{Symbol: "000001.SZ", Price: 11.68, Meta: foundation.SourceMeta{Source: "sina"}},
	}}
	ar, err := NewRealtimeArchiver(inner, root, Options{})
	if err != nil {
		t.Fatalf("NewRealtimeArchiver: %v", err)
	}
	out, err := ar.Realtime(context.Background(), []string{"600519.SH"})
	if err != nil || len(out) != 2 {
		t.Fatalf("Realtime passthrough failed: %v, %d", err, len(out))
	}
	daily := filepath.Join(root, "realtime", time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(daily)
	if err != nil {
		t.Fatalf("read archived: %v", err)
	}
	if !strings.Contains(string(data), "600519.SH") || !strings.Contains(string(data), "\"kind\":\"realtime\"") {
		t.Errorf("archive content unexpected: %s", data)
	}
	if strings.Count(strings.TrimSpace(string(data)), "\n")+1 != 2 {
		t.Errorf("expected 2 lines: %s", data)
	}
}

func TestRealtimeArchiverSkipsOnError(t *testing.T) {
	root := t.TempDir()
	inner := &fakeRealtime{err: assertErr("blocked")}
	ar, _ := NewRealtimeArchiver(inner, root, Options{})
	if _, err := ar.Realtime(context.Background(), nil); err == nil {
		t.Fatalf("expected error passthrough")
	}
	// 不应生成当日 jsonl
	daily := filepath.Join(root, "realtime", time.Now().Format("2006-01-02")+".jsonl")
	if _, err := os.Stat(daily); !os.IsNotExist(err) {
		t.Errorf("should not archive on error")
	}
}

func TestNewsArchiverBatchAndDisabled(t *testing.T) {
	root := t.TempDir()
	inner := &fakeNews{items: []foundation.NewsItem{
		{ID: "em-1", Title: "公告", Meta: foundation.SourceMeta{Source: "eastmoney"}},
	}}
	// Disabled：只透传不落盘
	ar, _ := NewNewsArchiver(inner, root, Options{Disabled: true})
	if _, err := ar.LatestNews(context.Background(), 5); err != nil {
		t.Fatalf("err: %v", err)
	}
	daily := filepath.Join(root, "news", time.Now().Format("2006-01-02")+".jsonl")
	if _, err := os.Stat(daily); !os.IsNotExist(err) {
		t.Errorf("disabled archiver should not write")
	}

	// 启用 + Batch
	ar2, _ := NewNewsArchiver(inner, root, Options{Batch: true})
	if _, err := ar2.LatestNews(context.Background(), 5); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(daily); err != nil {
		t.Errorf("enabled archiver should write: %v", err)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
