package duanxianxia

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrNoSnapshot = errors.New("no duanxianxia snapshot is available")

type Fetcher interface {
	Fetch(ctx context.Context, leaderThemeLimit int) (Snapshot, error)
}

type LimitUpPoolFetcher interface {
	FetchLimitUpPool(ctx context.Context) (LimitUpPoolSnapshot, error)
}

type Service struct {
	client           Fetcher
	poolClient       LimitUpPoolFetcher
	store            *Store
	refreshInterval  time.Duration
	leaderThemeLimit int
	now              func() time.Time
	gate             chan struct{}
	poolMu           sync.Mutex
	poolUpdated      chan struct{}
}

type ServiceConfig struct {
	RefreshInterval  time.Duration
	LeaderThemeLimit int
	Now              func() time.Time
}

func NewService(client Fetcher, store *Store, config ServiceConfig) *Service {
	interval := config.RefreshInterval
	if interval < 5*time.Minute {
		interval = 5 * time.Minute
	}
	leaderLimit := config.LeaderThemeLimit
	if leaderLimit <= 0 {
		leaderLimit = 3
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	poolClient, _ := client.(LimitUpPoolFetcher)
	return &Service{client: client, poolClient: poolClient, store: store, refreshInterval: interval, leaderThemeLimit: leaderLimit, now: now, gate: make(chan struct{}, 1), poolUpdated: make(chan struct{})}
}

func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, FetchMeta, error) {
	snapshots, meta, err := s.Snapshots(ctx, 1)
	if err != nil {
		return Snapshot{}, meta, err
	}
	if len(snapshots) == 0 {
		return Snapshot{}, meta, ErrNoSnapshot
	}
	return snapshots[0], meta, nil
}

func (s *Service) Snapshots(ctx context.Context, limit int) ([]Snapshot, FetchMeta, error) {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, FetchMeta{}, ctx.Err()
	}
	defer func() { <-s.gate }()
	result, err := s.refreshLocked(ctx)
	if err != nil {
		return nil, FetchMeta{}, err
	}
	snapshots, err := s.store.Recent(ctx, limit)
	if err != nil {
		return nil, FetchMeta{}, err
	}
	meta := fetchMeta(result.state, result.themeRefreshed, len(snapshots) > 0 && !result.themeRefreshed, result.refreshError)
	if len(snapshots) > 0 {
		return snapshots, meta, nil
	}
	if result.themeError != nil {
		return nil, meta, fmt.Errorf("refresh duanxianxia snapshot: %w", result.themeError)
	}
	return nil, meta, ErrNoSnapshot
}

func (s *Service) LimitUpPool(ctx context.Context) (LimitUpPoolSnapshot, FetchMeta, error) {
	pools, meta, err := s.LimitUpPools(ctx, 1)
	if err != nil {
		return LimitUpPoolSnapshot{}, meta, err
	}
	if len(pools) == 0 {
		return LimitUpPoolSnapshot{}, meta, ErrNoSnapshot
	}
	return pools[0], meta, nil
}

func (s *Service) LimitUpPools(ctx context.Context, limit int) ([]LimitUpPoolSnapshot, FetchMeta, error) {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, FetchMeta{}, ctx.Err()
	}
	defer func() { <-s.gate }()
	result, err := s.refreshLocked(ctx)
	if err != nil {
		return nil, FetchMeta{}, err
	}
	pools, err := s.store.RecentLimitUps(ctx, limit)
	if err != nil {
		return nil, FetchMeta{}, err
	}
	meta := fetchMeta(result.state, result.poolRefreshed, len(pools) > 0 && !result.poolRefreshed, result.refreshError)
	if len(pools) > 0 {
		return pools, meta, nil
	}
	if result.poolError != nil {
		return nil, meta, fmt.Errorf("refresh duanxianxia limit-up pool: %w", result.poolError)
	}
	return nil, meta, ErrNoSnapshot
}

type serviceRefreshResult struct {
	state          SyncState
	themeRefreshed bool
	poolRefreshed  bool
	themeError     error
	poolError      error
	refreshError   string
}

func (s *Service) refreshLocked(ctx context.Context) (serviceRefreshResult, error) {
	now := s.now()
	allowed, state, err := s.store.TryBegin(ctx, now, s.refreshInterval)
	if err != nil {
		return serviceRefreshResult{}, err
	}
	if !allowed {
		return serviceRefreshResult{state: state, refreshError: state.LastError}, nil
	}

	var theme Snapshot
	var pool LimitUpPoolSnapshot
	var themeErr error
	var poolErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		theme, themeErr = s.client.Fetch(ctx, s.leaderThemeLimit)
	}()
	if s.poolClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool, poolErr = s.poolClient.FetchLimitUpPool(ctx)
			if poolErr == nil {
				if pool.FetchedAt.IsZero() {
					pool.FetchedAt = now
				}
				if pool.ID == "" {
					pool.ID = fmt.Sprintf("kpl-pool-%s-%d", pool.TradeDate, pool.FetchedAt.UnixMilli())
				}
				poolErr = s.store.SaveLimitUpSuccess(ctx, pool)
				if poolErr == nil {
					s.poolMu.Lock()
					close(s.poolUpdated)
					s.poolUpdated = make(chan struct{})
					s.poolMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	result := serviceRefreshResult{themeError: themeErr, poolError: poolErr}
	if themeErr == nil {
		if theme.FetchedAt.IsZero() {
			theme.FetchedAt = now
		}
		if theme.ID == "" {
			theme.ID = fmt.Sprintf("kpl-%s-%d", theme.TradeDate, theme.FetchedAt.UnixMilli())
		}
		if err := s.store.SaveSuccess(ctx, theme); err != nil {
			return serviceRefreshResult{}, err
		}
		result.themeRefreshed = true
	}
	result.poolRefreshed = s.poolClient != nil && poolErr == nil

	errors := []string{}
	if themeErr != nil {
		errors = append(errors, "板块轮动: "+themeErr.Error())
	}
	if poolErr != nil {
		errors = append(errors, "涨停池: "+poolErr.Error())
	}
	result.refreshError = strings.Join(errors, "; ")
	if result.refreshError != "" {
		_ = s.store.MarkError(ctx, result.refreshError)
	}
	result.state, _ = s.store.State(ctx)
	return result, nil
}

func (s *Service) SnapshotByID(ctx context.Context, id string) (Snapshot, bool, error) {
	if id == "" {
		snapshot, _, err := s.Snapshot(ctx)
		return snapshot, err == nil, err
	}
	return s.store.Get(ctx, id)
}

func fetchMeta(state SyncState, refreshed bool, fromCache bool, refreshError string) FetchMeta {
	return FetchMeta{
		LastAttemptAt: state.LastAttemptAt,
		NextAllowedAt: state.NextAllowedAt,
		LastSuccessAt: state.LastSuccessAt,
		RefreshError:  refreshError,
		Refreshed:     refreshed,
		FromCache:     fromCache,
	}
}

// CachedSnapshot reads local membership without waiting for the remote refresh lock.
func (s *Service) CachedSnapshot(ctx context.Context) (Snapshot, bool, error) {
	return s.store.Latest(ctx)
}

// CachedLimitUpPools only reads persisted pools; it never acquires the remote refresh gate.
func (s *Service) CachedLimitUpPools(ctx context.Context, limit int) ([]LimitUpPoolSnapshot, error) {
	return s.store.RecentLimitUps(ctx, limit)
}

// EarlyLimitUpPools observes the pool publication in the shared refresh batch.
// Theme leaders may still be loading; the shared five-minute gate remains in effect.
func (s *Service) EarlyLimitUpPools(ctx context.Context, limit int) ([]LimitUpPoolSnapshot, error) {
	s.poolMu.Lock()
	updated := s.poolUpdated
	s.poolMu.Unlock()
	// A pool may have been published before this subscriber joined while theme
	// leaders are still loading. Do not wait for a second pool notification.
	if pools, err := s.CachedLimitUpPools(ctx, limit); err == nil && len(pools) > 0 && s.now().Sub(pools[0].FetchedAt) < s.refreshInterval {
		return pools, nil
	}
	type result struct {
		pools []LimitUpPoolSnapshot
		err   error
	}
	done := make(chan result, 1)
	go func() { pools, _, err := s.LimitUpPools(ctx, limit); done <- result{pools, err} }()
	select {
	case <-updated:
		return s.CachedLimitUpPools(ctx, limit)
	case value := <-done:
		return value.pools, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
