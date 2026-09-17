package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

type progressiveThemeProvider interface {
	ProgressiveOverviews(context.Context, func(foundation.ThemeProgress))
}

type themeProgressCache struct {
	mu        sync.Mutex
	value     foundation.ThemeProgress
	startedAt time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool
}

func newThemeProgressCache() *themeProgressCache {
	return &themeProgressCache{value: foundation.ThemeProgress{Data: []foundation.ThemeOverview{}, Steps: map[string]string{}, Errors: map[string]string{}, Stage: "base"}}
}

func (s *Server) progressiveThemeOverview(w http.ResponseWriter, r *http.Request) {
	c := s.themeProgress
	c.mu.Lock()
	// Polling is read-only. A fresh entry/explicit refresh can start one bounded job.
	poll := r.URL.Query().Get("refresh_id") != "" && c.value.RefreshID != ""
	if !c.closed && !poll && !c.value.Refreshing && (r.URL.Query().Get("refresh") == "1" || c.startedAt.IsZero() || time.Since(c.startedAt) > 30*time.Second) {
		c.startedAt = time.Now()
		c.value.RefreshID = fmt.Sprintf("radar-%d", time.Now().UnixNano())
		c.value.Refreshing = true
		c.value.Revision++
		c.value.Steps = map[string]string{"industry": "loading", "kaipanla": "loading", "strength": "loading"}
		c.value.Errors = map[string]string{}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		c.cancel = cancel
		c.done = make(chan struct{})
		go s.refreshThemeProgress(ctx, c.value.RefreshID, c.done)
	}
	value := c.value
	c.mu.Unlock()
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) refreshThemeProgress(ctx context.Context, id string, done chan struct{}) {
	defer close(done)
	started := time.Now()
	c := s.themeProgress
	updates := make(chan foundation.ThemeProgress, 8)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		publish := func(value foundation.ThemeProgress) {
			select {
			case updates <- value:
			case <-ctx.Done():
			}
		}
		if provider, ok := s.themeOverview.(progressiveThemeProvider); ok {
			provider.ProgressiveOverviews(ctx, publish)
			return
		}
		items, meta, err := s.themeOverview.Overviews(ctx)
		value := foundation.ThemeProgress{Data: items, Meta: meta, Stage: "enriched", Steps: map[string]string{"overview": "ready"}, Errors: map[string]string{}}
		if err != nil {
			value.Steps["overview"] = "error"
			value.Errors["overview"] = err.Error()
		}
		publish(value)
	}()
	apply := func(value foundation.ThemeProgress) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed || c.value.RefreshID != id {
			return
		}
		if len(value.Data) == 0 && len(c.value.Data) > 0 {
			value.Data = c.value.Data
			value.Meta = c.value.Meta
			value.Meta.Stale = true
		}
		if value.Data == nil {
			value.Data = []foundation.ThemeOverview{}
		}
		value.Refreshing = true
		value.RefreshID = id
		value.Revision = c.value.Revision + 1
		c.value = value
	}
	complete := false
	for !complete {
		select {
		case value := <-updates:
			apply(value)
		case <-finished:
			for {
				select {
				case value := <-updates:
					apply(value)
				default:
					complete = true
				}
				if complete {
					break
				}
			}
		case <-ctx.Done():
			complete = true
		}
	}
	c.mu.Lock()
	c.value.Revision++
	steps := map[string]string{}
	errs := map[string]string{}
	for key, value := range c.value.Steps {
		steps[key] = value
	}
	for key, value := range c.value.Errors {
		errs[key] = value
	}
	for key, value := range steps {
		if value == "loading" {
			steps[key] = "error"
			errs[key] = "数据更新超时，请重试"
		}
	}
	c.value.Steps = steps
	c.value.Errors = errs
	value := c.value
	value.Refreshing = false
	c.cancel()
	c.mu.Unlock()
	if len(value.Data) > 0 && s.themeRadarStore != nil {
		payload, err := json.Marshal(value)
		if err == nil {
			saveCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = s.themeRadarStore.SaveOverview(saveCtx, payload)
		}
		if err != nil {
			s.logger.Printf("event=theme_cache_save error=%q", err)
		}
	}
	c.mu.Lock()
	c.value.Refreshing = false
	c.value.Revision++
	c.mu.Unlock()
	s.logger.Printf("event=theme_refresh refresh_id=%q duration_ms=%d items=%d errors=%d", id, time.Since(started).Milliseconds(), len(value.Data), len(value.Errors))
}

func (c *themeProgressCache) close() {
	c.mu.Lock()
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}
