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
	mu               sync.Mutex
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
	return &Service{client: client, poolClient: poolClient, store: store, refreshInterval: interval, leaderThemeLimit: leaderLimit, now: now}
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if s.poolClient != nil && poolErr == nil {
		if pool.FetchedAt.IsZero() {
			pool.FetchedAt = now
		}
		if pool.ID == "" {
			pool.ID = fmt.Sprintf("kpl-pool-%s-%d", pool.TradeDate, pool.FetchedAt.UnixMilli())
		}
		if err := s.store.SaveLimitUpSuccess(ctx, pool); err != nil {
			return serviceRefreshResult{}, err
		}
		result.poolRefreshed = true
	}

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
