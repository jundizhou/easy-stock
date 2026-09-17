package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

// Published values are immutable. Polls only read this snapshot; entering the
// workspace or explicitly refreshing starts at most one bounded background job.
type shortTermProgress[T any] struct {
	Data       *T                `json:"data"`
	RefreshID  string            `json:"refresh_id"`
	Revision   uint64            `json:"revision"`
	Refreshing bool              `json:"refreshing"`
	Stale      bool              `json:"stale"`
	Steps      map[string]string `json:"steps"`
	Errors     map[string]string `json:"errors"`
}

type shortTermCache[T any] struct {
	mu      sync.Mutex
	value   shortTermProgress[T]
	started time.Time
	cancel  context.CancelFunc
	done    chan struct{}
	closed  bool
}

func (c *shortTermCache[T]) read(poll, force bool, budget time.Duration, steps []string, run func(context.Context, func(shortTermProgress[T])), save func(shortTermProgress[T])) shortTermProgress[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed && (!poll || c.value.RefreshID == "") && !c.value.Refreshing && (c.started.IsZero() || time.Since(c.started) >= 30*time.Second || force && time.Since(c.started) >= 2*time.Second) {
		c.started = time.Now()
		c.value.RefreshID = fmt.Sprintf("short-%d", c.started.UnixNano())
		c.value.Revision++
		c.value.Refreshing = true
		c.value.Stale = c.value.Data != nil
		c.value.Steps = make(map[string]string)
		c.value.Errors = make(map[string]string)
		for _, step := range steps {
			c.value.Steps[step] = "loading"
		}
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		c.cancel = cancel
		c.done = make(chan struct{})
		go c.refresh(ctx, c.value.RefreshID, c.done, run, save)
	}
	return c.value
}

func (c *shortTermCache[T]) refresh(ctx context.Context, id string, done chan struct{}, run func(context.Context, func(shortTermProgress[T])), save func(shortTermProgress[T])) {
	defer close(done)
	updates := make(chan shortTermProgress[T])
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		run(ctx, func(value shortTermProgress[T]) {
			select {
			case updates <- value:
			case <-ctx.Done():
			}
		})
	}()
	apply := func(value shortTermProgress[T]) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed || c.value.RefreshID != id {
			return
		}
		if value.Data == nil {
			value.Data = c.value.Data
			value.Stale = value.Data != nil
		}
		value.RefreshID, value.Revision, value.Refreshing = id, c.value.Revision+1, true
		c.value = value
	}
loop:
	for {
		select {
		case value := <-updates:
			apply(value)
		case <-finished:
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	c.mu.Lock()
	value := c.value
	value.Steps, value.Errors = copyStrings(value.Steps), copyStrings(value.Errors)
	for step, status := range value.Steps {
		if status == "loading" {
			value.Steps[step] = "error"
			value.Errors[step] = "数据更新超时，请重试"
		}
	}
	value.Refreshing = false
	value.Revision++
	c.cancel()
	// Keep the job active through persistence so an explicit retry cannot race a save.
	c.mu.Unlock()
	if save != nil {
		save(value)
	}
	c.mu.Lock()
	c.value = value
	c.mu.Unlock()
}

func (c *shortTermCache[T]) close() {
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

func copyStrings(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

type progressiveLimitUpProvider interface {
	ProgressiveLimitUps(context.Context, int, func([]foundation.LimitUpEvent, string, error))
}
type cachedLimitUpProvider interface {
	CachedLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error)
}
type cachedConceptProvider interface {
	CachedStockCatalog() []foundation.StockCatalogEntry
}

func (s *Server) progressiveLimitUpLadder(w http.ResponseWriter, r *http.Request) {
	value := s.limitUpProgress.read(r.URL.Query().Get("refresh_id") != "", r.URL.Query().Get("refresh") == "1", 25*time.Second,
		[]string{"primary", "history", "themes", "concepts", "quotes"}, s.refreshLimitUpProgress, s.saveLimitUpProgress)
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) saveLimitUpProgress(value shortTermProgress[limitUpLadderData]) {
	if value.Data == nil || s.themeRadarStore == nil {
		return
	}
	payload, err := json.Marshal(value)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = s.themeRadarStore.SaveLadder(ctx, payload)
	}
	if err != nil {
		s.logger.Printf("event=limit_up_cache_save error=%q", err)
	}
}

func (s *Server) refreshLimitUpProgress(ctx context.Context, publish func(shortTermProgress[limitUpLadderData])) {
	start := time.Now()
	type update struct {
		stage     string
		events    []foundation.LimitUpEvent
		catalog   []foundation.StockCatalogEntry
		previous  *limitUpLadderDay
		quoteDate string
		err       error
	}
	updates := make(chan update, 8)
	send := func(item update) {
		select {
		case updates <- item:
		case <-ctx.Done():
		}
	}
	steps := map[string]string{"primary": "loading", "history": "loading", "themes": "loading", "concepts": "loading", "quotes": "loading"}
	errs := map[string]string{}
	var events []foundation.LimitUpEvent
	var catalog []foundation.StockCatalogEntry
	var data *limitUpLadderData
	var quotePrevious *limitUpLadderDay
	var quoteDate string
	var quoteUpdatedAt time.Time
	var quotesStarted bool
	if provider, ok := s.stockConcepts.(cachedConceptProvider); ok {
		catalog = provider.CachedStockCatalog()
	}
	if provider, ok := s.limitUpProvider.(cachedLimitUpProvider); ok {
		if cached, err := provider.CachedLimitUps(ctx, 8); err == nil && len(cached) > 0 {
			if initial, err := buildLimitUpLadder(cached, catalog, time.Now()); err == nil {
				initial.Meta.Stale = true
				initial.ComparisonReady = false
				initial.Advance = []limitUpAdvanceStep{}
				// Do not replace a richer restored snapshot with the same retained pool.
				s.limitUpProgress.mu.Lock()
				hasSnapshot := s.limitUpProgress.value.Data != nil
				s.limitUpProgress.mu.Unlock()
				if !hasSnapshot {
					publish(shortTermProgress[limitUpLadderData]{Data: &initial, Stale: true, Steps: copyStrings(steps), Errors: copyStrings(errs)})
					s.logger.Printf("event=limit_up_stage stage=cached_publish elapsed_ms=%d", time.Since(start).Milliseconds())
				}
			}
		}
	}
	go func() {
		if provider, ok := s.limitUpProvider.(progressiveLimitUpProvider); ok {
			provider.ProgressiveLimitUps(ctx, 8, func(events []foundation.LimitUpEvent, stage string, err error) {
				send(update{stage: stage, events: events, err: err})
			})
		} else if provider, ok := s.limitUpProvider.(interface {
			ProgressiveRecentLimitUps(context.Context, int, func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error)
		}); ok {
			events, err := provider.ProgressiveRecentLimitUps(ctx, 8, func(items []foundation.LimitUpEvent) { send(update{stage: "history_partial", events: items}) })
			send(update{stage: "pool", events: events, err: err})
		} else {
			events, err := s.limitUpProvider.RecentLimitUps(ctx, 8)
			send(update{stage: "pool", events: events, err: err})
		}
	}()
	go func() {
		var entries []foundation.StockCatalogEntry
		var err error
		if s.stockConcepts != nil {
			entries, err = s.stockConcepts.StockCatalog(ctx)
		}
		send(update{stage: "concepts", catalog: entries, err: err})
	}()
	first := true
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-updates:
			s.logger.Printf("event=limit_up_stage stage=%s elapsed_ms=%d failed=%t", item.stage, time.Since(start).Milliseconds(), item.err != nil)
			status := "ready"
			if item.err != nil {
				status = "error"
				errs[item.stage] = item.err.Error()
			}
			if item.stage != "history_partial" {
				steps[item.stage] = status
			}
			switch item.stage {
			case "concepts":
				if len(item.catalog) > 0 {
					catalog = item.catalog
				}
			case "quotes":
				if item.err == nil {
					quotePrevious, quoteDate = item.previous, item.quoteDate
					quoteUpdatedAt = time.Now()
				}
			case "pool":
				steps["primary"], steps["history"], steps["themes"] = status, status, status
				fallthrough
			default:
				if len(item.events) > 0 {
					events = item.events
				}
			}
			if len(events) > 0 {
				built, err := buildLimitUpLadder(events, catalog, time.Now())
				if err == nil {
					built.ConceptStatus = steps["concepts"]
					if limitUpDayHasRawConcepts(built.Current) {
						built.ConceptStatus = "ready"
					}
					built.ConceptError = errs["concepts"]
					poolsDone := steps["primary"] != "loading" && steps["history"] != "loading"
					built.ComparisonReady = poolsDone && steps["history"] == "ready" && built.Previous.TradeDate != ""
					if !built.ComparisonReady {
						built.Advance = []limitUpAdvanceStep{}
					}
					if quotePrevious != nil && quoteDate == built.Current.TradeDate && quotePrevious.TradeDate == built.Previous.TradeDate {
						copyPreviousChanges(&built.Previous, *quotePrevious)
						intraday := buildMarketEmotionIntraday(built, nil)
						intraday.BaseTradeDate = built.Previous.TradeDate
						intraday.UpdatedAt = quoteUpdatedAt
						intraday.CacheTTLSecond = 30
						intraday.NextRefreshAt = intraday.UpdatedAt.Add(30 * time.Second)
						built.Intraday = &intraday
					}
					// Keep same-session feedback visible while its replacement is loading.
					s.limitUpProgress.mu.Lock()
					previous := s.limitUpProgress.value.Data
					if built.Intraday == nil && previous != nil && previous.Intraday != nil && previous.Current.TradeDate == built.Current.TradeDate && previous.Previous.TradeDate == built.Previous.TradeDate {
						cached := *previous.Intraday
						cached.Stale = true
						built.Intraday = &cached
					}
					data = &built
					if previous != nil && built.Current.TradeDate < previous.Current.TradeDate {
						retained := *previous
						retained.Meta.Stale = true
						data = &retained
					}
					s.limitUpProgress.mu.Unlock()
					if first {
						s.logger.Printf("event=limit_up_stage stage=first_publish elapsed_ms=%d", time.Since(start).Milliseconds())
						first = false
					}
					if poolsDone && !quotesStarted {
						quotesStarted = true
						go func(ladder limitUpLadderData) {
							// Never mutate the slices already published to concurrent readers.
							ladder.Previous = cloneLimitUpDay(ladder.Previous)
							var err error
							if !ladder.ComparisonReady {
								err = fmt.Errorf("缺少可对照的上一交易日梯队")
							} else {
								err = enrichPreviousChangesForDate(ctx, &ladder.Previous, ladder.Current.TradeDate, s.realtimeProvider)
							}
							send(update{stage: "quotes", previous: &ladder.Previous, quoteDate: ladder.Current.TradeDate, err: err})
						}(built)
					}
				}
			}
			if data == nil && steps["primary"] != "loading" && steps["history"] != "loading" {
				steps["quotes"] = "error"
				errs["quotes"] = "没有可用涨停池"
			}
			publish(shortTermProgress[limitUpLadderData]{Data: data, Stale: data != nil && data.Meta.Stale, Steps: copyStrings(steps), Errors: copyStrings(errs)})
			pending := false
			for _, state := range steps {
				if state == "loading" {
					pending = true
				}
			}
			if !pending {
				s.logger.Printf("event=limit_up_stage stage=complete elapsed_ms=%d", time.Since(start).Milliseconds())
				return
			}
		}
	}
}

func copyPreviousChanges(target *limitUpLadderDay, source limitUpLadderDay) {
	changes := map[string]*float64{}
	for _, level := range source.Levels {
		for _, stock := range level.Stocks {
			changes[stock.Symbol] = stock.CurrentChangePercent
		}
	}
	for i := range target.Levels {
		for j := range target.Levels[i].Stocks {
			stock := &target.Levels[i].Stocks[j]
			stock.CurrentChangePercent = changes[stock.Symbol]
		}
	}
}

func (s *Server) progressiveEmotionHistory(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	history, err := s.marketEmotion.readLocal(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	value := s.emotionProgress.read(r.URL.Query().Get("refresh_id") != "", r.URL.Query().Get("refresh") == "1", 150*time.Second, []string{"history"}, func(ctx context.Context, publish func(shortTermProgress[marketemotion.History])) {
		started := time.Now()
		history, err := s.marketEmotion.load(ctx)
		value := shortTermProgress[marketemotion.History]{Data: &history, Steps: map[string]string{"history": "ready"}, Errors: map[string]string{}}
		if err != nil {
			value.Data = nil
			value.Steps["history"] = "error"
			value.Errors["history"] = err.Error()
		}
		if history.Cache.LastError != "" {
			value.Steps["history"] = "error"
			value.Errors["history"] = history.Cache.LastError
		}
		publish(value)
		s.logger.Printf("event=limit_up_stage stage=emotion_sync duration_ms=%d failed=%t", time.Since(started).Milliseconds(), len(value.Errors) > 0)
	}, nil)
	// The job can finish between the first SQLite read and reading its status.
	// Read again at completion so a terminal response cannot strand an empty chart.
	if !value.Refreshing {
		if latest, readErr := s.marketEmotion.readLocal(r.Context()); readErr == nil {
			history = latest
		}
	}
	value.Data = &history
	value.Stale = value.Refreshing
	s.logger.Printf("event=limit_up_stage stage=emotion_local duration_ms=%d days=%d", time.Since(started).Milliseconds(), len(history.Points))
	writeJSON(w, http.StatusOK, value)
}

func cloneLimitUpDay(day limitUpLadderDay) limitUpLadderDay {
	day.Levels = append([]limitUpLadderLevel(nil), day.Levels...)
	for i := range day.Levels {
		day.Levels[i].Stocks = append([]limitUpLadderStock(nil), day.Levels[i].Stocks...)
	}
	return day
}
