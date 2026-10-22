package cockpit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
)

var (
	ErrBusy                  = errors.New("已有巡查正在运行，请稍后再试")
	errAutoPatrolDisabled    = errors.New("自动巡查已关闭")
	errNoDueSlot             = errors.New("暂无到期巡查")
	ErrInvalidPatrolInterval = errors.New("巡查间隔必须为 5、10、15、30 或 60 分钟")
)

// Narrator 由 AI 运行时实现。Available 为 false 时只保留事实，不调用模型。
type Narrator interface {
	Available() bool
	Narrate(ctx context.Context, prompt string) (string, error)
}

type Status struct {
	IntervalMinutes   int        `json:"interval_minutes"`
	AutoPatrolEnabled bool       `json:"auto_patrol_enabled"`
	Phase             Phase      `json:"phase"`
	SessionDate       string     `json:"session_date"`
	TradingDay        bool       `json:"trading_day"`
	NextRunAt         *time.Time `json:"next_run_at,omitempty"`
	NextRunName       string     `json:"next_run_label,omitempty"`
	Running           bool       `json:"running"`
	AIAvailable       bool       `json:"ai_available"`
}

type Service struct {
	settings *appsettings.Store
	store    *Store
	feed     Feed
	narrator Narrator
	logger   *log.Logger
	timeout  time.Duration
	now      func() time.Time

	mu   sync.Mutex
	busy bool
}

func NewService(store *Store, feed Feed, narrator Narrator, logger *log.Logger, settings *appsettings.Store) *Service {
	return &Service{store: store, feed: feed, narrator: narrator, logger: logger, settings: settings, timeout: 6 * time.Minute, now: time.Now}
}

// SetAutoPatrolEnabled 与启动巡查共用锁：保存成功后不再启动新的定时巡查。
// 已经开始的巡查继续完成，手动巡查不受此开关限制。
func (s *Service) SetAutoPatrolEnabled(enabled bool) error {
	return s.UpdateAutoPatrol(&enabled, nil)
}

// 修改间隔从下一个时点生效，持久化生效时间以避免重启后补跑旧时点。
func (s *Service) UpdateAutoPatrol(enabled *bool, intervalMinutes *int) error {
	if intervalMinutes != nil && !appsettings.ValidCockpitPatrolIntervalMinutes(*intervalMinutes) {
		return ErrInvalidPatrolInterval
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.settings.Update(func(values *appsettings.Values) error {
		if enabled != nil {
			values.CockpitAutoPatrolEnabled = *enabled
		}
		if intervalMinutes != nil && values.CockpitPatrolIntervalMinutes != *intervalMinutes {
			values.CockpitPatrolIntervalMinutes = *intervalMinutes
			values.CockpitPatrolIntervalChangedAt = s.now()
		}
		return nil
	})
	return err
}

// Tick 由调度器周期调用：到达计划时点且本会话尚未巡查时启动一次。
func (s *Service) Tick() {
	if _, err := s.launch(Slot{}, "scheduled", s.now()); err != nil && !errors.Is(err, ErrBusy) && !errors.Is(err, errAutoPatrolDisabled) && !errors.Is(err, errNoDueSlot) {
		s.logf("level=warn event=cockpit_patrol_start_failed error=%q", err.Error())
	}
}

// RunManual 为用户手动触发的巡查，使用当前会话日，不占用计划去重键。
func (s *Service) RunManual(phase Phase) (Brief, error) {
	now := s.now()
	state := PhaseAt(now)
	if phase == PhaseRest {
		phase = state.Phase
	}
	slot := Slot{
		Key:         fmt.Sprintf("manual-%d", now.UnixNano()),
		Phase:       phase,
		SessionDate: state.SessionDate,
		Label:       "手动巡查 · " + PhaseLabel(phase),
	}
	if phase != state.Phase && state.TradingDay {
		slot.Caveat = fmt.Sprintf("当前为 %s，尚未进入%s时段，本次采集的是当时的快照，结论只能视为临时参考", now.In(foundation.AStockLocation).Format("15:04"), PhaseLabel(phase))
	}
	return s.launch(slot, "manual", now)
}

func (s *Service) Status() Status {
	now := s.now()
	state := PhaseAt(now)
	s.mu.Lock()
	running := s.busy
	settings := s.settings.Snapshot()
	s.mu.Unlock()
	interval := appsettings.NormalizeCockpitPatrolIntervalMinutes(settings.CockpitPatrolIntervalMinutes)
	status := Status{AutoPatrolEnabled: settings.CockpitAutoPatrolEnabled, IntervalMinutes: interval, Phase: state.Phase, SessionDate: state.SessionDate, TradingDay: state.TradingDay, Running: running, AIAvailable: s.narratorAvailable()}
	if at, name := NextRun(now, interval); settings.CockpitAutoPatrolEnabled && !at.IsZero() {
		status.NextRunAt = &at
		status.NextRunName = name
	}
	return status
}

func (s *Service) Store() *Store {
	return s.store
}

func (s *Service) launch(slot Slot, trigger string, now time.Time) (Brief, error) {
	s.mu.Lock()
	if trigger == "scheduled" {
		settings := s.settings.Snapshot()
		if !settings.CockpitAutoPatrolEnabled {
			s.mu.Unlock()
			return Brief{}, errAutoPatrolDisabled
		}
		// 在与配置更新相同的锁内决定计划，避免按刚失效的间隔启动巡查。
		var ok bool
		slot, ok = DueSlot(now, settings.CockpitPatrolIntervalMinutes)
		if !ok || (slot.Phase == PhaseIntraday && !slot.RunAt.After(settings.CockpitPatrolIntervalChangedAt)) || s.store.HasKey(slot.SessionDate+"|"+slot.Key) {
			s.mu.Unlock()
			return Brief{}, errNoDueSlot
		}
	}
	if s.busy {
		s.mu.Unlock()
		return Brief{}, ErrBusy
	}
	s.busy = true
	s.mu.Unlock()

	brief := Brief{
		ID:          fmt.Sprintf("cockpit-%d", now.UnixNano()),
		Key:         slot.SessionDate + "|" + slot.Key,
		Phase:       slot.Phase,
		SessionDate: slot.SessionDate,
		Label:       slot.Label,
		Caveat:      slot.Caveat,
		Trigger:     trigger,
		Status:      "running",
		StartedAt:   now,
	}
	if err := s.store.Put(brief); err != nil {
		s.release()
		return Brief{}, err
	}
	go s.execute(brief)
	return brief, nil
}

func (s *Service) execute(brief Brief) {
	defer s.release()
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	brief.Facts = CollectFacts(ctx, brief.Phase, s.feed)
	var previous *Brief
	if prior, ok := s.store.Previous(brief.Phase, brief.SessionDate, brief.ID); ok && brief.Phase == PhaseIntraday {
		previous = &prior
	}
	prompt := BuildPrompt(brief.Phase, brief.SessionDate, brief.Label, brief.Facts, previous)
	if brief.Caveat != "" {
		prompt = "数据时效说明：" + brief.Caveat + "。请在简报开头写明这一点，不得把盘中快照写成收盘结论。\n\n" + prompt
	}

	switch {
	case !s.narratorAvailable():
		brief.NarrativeStatus = "unavailable"
		brief.Error = "AI 模型未配置，仅保留采集到的事实数据"
	default:
		text, err := s.narrator.Narrate(ctx, prompt)
		text = strings.TrimSpace(text)
		if err != nil || text == "" {
			brief.NarrativeStatus = "failed"
			if err != nil {
				brief.Error = "AI 叙述生成失败：" + err.Error()
			} else {
				brief.Error = "AI 叙述为空"
			}
		} else {
			brief.NarrativeStatus = "ready"
			brief.Narrative = text
			// 上一次巡查的数值同样在提示词中提供，校验时一并纳入，避免把正确环比误报为未核实。
			factSets := []Facts{brief.Facts}
			if previous != nil {
				factSets = append(factSets, previous.Facts)
			}
			brief.UnverifiedNumbers = VerifyNarrativeAgainst(text, factSets...)
		}
	}
	brief.Status = "ready"
	brief.FinishedAt = s.now()
	if err := s.store.Put(brief); err != nil {
		s.logf("level=error event=cockpit_patrol_save_failed id=%s error=%q", brief.ID, err.Error())
	}
	s.logf("level=info event=cockpit_patrol_done phase=%s trigger=%s narrative=%s sources=%d", brief.Phase, brief.Trigger, brief.NarrativeStatus, len(brief.Facts.Sources))
}

func (s *Service) release() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *Service) narratorAvailable() bool {
	return s.narrator != nil && s.narrator.Available()
}

func (s *Service) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}
