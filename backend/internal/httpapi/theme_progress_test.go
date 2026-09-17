package httpapi

import (
	"context"
	"easy-stock/backend/internal/sector"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type gatedThemeProgress struct {
	release chan struct{}
	calls   atomic.Int32
}

func (p *gatedThemeProgress) Overviews(context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	panic("blocking overview should not be used")
}
func (p *gatedThemeProgress) ProgressiveOverviews(ctx context.Context, publish func(foundation.ThemeProgress)) {
	p.calls.Add(1)
	publish(foundation.ThemeProgress{Data: []foundation.ThemeOverview{{Theme: "fast", Name: "已到齐题材"}}, Stage: "base", Refreshing: true, Steps: map[string]string{"industry": "ready", "strength": "loading"}, Errors: map[string]string{}})
	select {
	case <-p.release:
	case <-ctx.Done():
		return
	}
	publish(foundation.ThemeProgress{Data: []foundation.ThemeOverview{{Theme: "fast", Name: "完整题材"}}, Stage: "enriched", Steps: map[string]string{"industry": "ready", "strength": "ready"}, Errors: map[string]string{}})
}

func readProgress(t *testing.T, s *Server, path string) foundation.ThemeProgress {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result foundation.ThemeProgress
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProgressiveHTTPReturnsBeforeWorkAndPollingDoesNotRestart(t *testing.T) {
	p := &gatedThemeProgress{release: make(chan struct{})}
	s := NewServer(Config{ThemeOverview: p})
	defer s.Close()
	base := "/api/v1/themes/overview?delivery=progressive"
	first := readProgress(t, s, base)
	if !first.Refreshing || first.RefreshID == "" {
		t.Fatalf("missing job: %+v", first)
	}
	poll := base + "&refresh_id=" + first.RefreshID
	deadline := time.Now().Add(time.Second)
	for {
		value := readProgress(t, s, poll)
		if len(value.Data) > 0 {
			if value.Data[0].Name != "已到齐题材" || !value.Refreshing {
				t.Fatalf("bad partial: %+v", value)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partial update not visible")
		}
		time.Sleep(time.Millisecond)
	}
	close(p.release)
	for {
		value := readProgress(t, s, poll)
		if !value.Refreshing {
			if value.Data[0].Name != "完整题材" {
				t.Fatal(value)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		readProgress(t, s, poll)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("polling restarted work %d times", p.calls.Load())
	}
}

func TestProgressiveCacheSurvivesRestart(t *testing.T) {
	db := filepath.Join(t.TempDir(), "radar.db")
	provider := &gatedThemeProgress{release: make(chan struct{})}
	close(provider.release)
	server := NewServer(Config{ThemeOverview: provider, ThemeRadarDBPath: db})
	first := readProgress(t, server, "/api/v1/themes/overview?delivery=progressive")
	deadline := time.Now().Add(time.Second)
	for readProgress(t, server, "/api/v1/themes/overview?delivery=progressive&refresh_id="+first.RefreshID).Refreshing {
		if time.Now().After(deadline) {
			server.Close()
			t.Fatal("job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	slow := &gatedThemeProgress{release: make(chan struct{})}
	restored := NewServer(Config{ThemeOverview: slow, ThemeRadarDBPath: db})
	defer restored.Close()
	value := readProgress(t, restored, "/api/v1/themes/overview?delivery=progressive")
	if len(value.Data) != 1 || value.Data[0].Name != "完整题材" || !value.Meta.Stale {
		t.Fatalf("cache not immediately restored: %+v", value)
	}
}

type expiredThemeMap struct{}

func (expiredThemeMap) Build(context.Context, string) (foundation.SectorMap, error) {
	return foundation.SectorMap{}, sector.ErrSnapshotExpired
}
func (expiredThemeMap) BuildSnapshot(context.Context, string, string) (foundation.SectorMap, error) {
	return foundation.SectorMap{}, sector.ErrSnapshotExpired
}
func TestThemeScreenReportsExpiredSnapshotAsGone(t *testing.T) {
	s := NewServer(Config{SectorMap: expiredThemeMap{}})
	defer s.Close()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/themes/screen?theme=kpl:test&snapshot_id=old", nil))
	var value map[string]string
	json.Unmarshal(w.Body.Bytes(), &value)
	if w.Code != 410 || value["code"] != "SNAPSHOT_EXPIRED" {
		t.Fatalf("response %d %s", w.Code, w.Body.String())
	}
}

func TestProgressiveCancellationLeavesPartialDataAndTerminalError(t *testing.T) {
	p := &gatedThemeProgress{release: make(chan struct{})}
	s := NewServer(Config{ThemeOverview: p})
	defer s.Close()
	first := readProgress(t, s, "/api/v1/themes/overview?delivery=progressive")
	poll := "/api/v1/themes/overview?delivery=progressive&refresh_id=" + first.RefreshID
	deadline := time.Now().Add(time.Second)
	for len(readProgress(t, s, poll).Data) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no partial data")
		}
		time.Sleep(time.Millisecond)
	}
	s.themeProgress.mu.Lock()
	s.themeProgress.cancel()
	s.themeProgress.mu.Unlock()
	for {
		value := readProgress(t, s, poll)
		if !value.Refreshing {
			if len(value.Data) != 1 || value.Steps["strength"] != "error" || value.Errors["strength"] == "" {
				t.Fatalf("invalid terminal state: %+v", value)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled job still loading")
		}
		time.Sleep(time.Millisecond)
	}
}
