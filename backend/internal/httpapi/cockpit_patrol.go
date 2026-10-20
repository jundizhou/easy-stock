package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/agent"
	"easy-stock/backend/internal/cockpit"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

const cockpitPatrolInterval = 30 * time.Second

// cockpitFactsTTL 是实时事实的短缓存：驾驶舱固定模块自己轮询，多个页面同时打开时
// 只打一次上游，避免把指数、资金、快讯这些接口按客户端数量放大。
const cockpitFactsTTL = 25 * time.Second

// cockpitLiveFacts 缓存最近一次实时采集结果。收紧到 25 秒仍能保证图上数据是「此刻」，
// 阶段变化（盘前→盘中）会立即作废缓存，避免沿用上一阶段少了盘中字段。
type cockpitLiveFacts struct {
	mu    sync.Mutex
	at    time.Time
	phase cockpit.Phase
	facts cockpit.Facts
}

// cockpitNarrator 把 AI 叙述接到统一 Agent 运行时。驾驶舱巡查不需要工具，
// 只读取已采集的事实，因此禁用工具并使用沙箱。
type cockpitNarrator struct {
	gateway agent.Gateway
	usage   agent.Gateway
}

func (n cockpitNarrator) Available() bool {
	if n.gateway == nil {
		return false
	}
	status := n.gateway.Status()
	return status.Available && status.Configured
}

func (n cockpitNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	gateway := n.gateway
	if n.usage != nil {
		gateway = n.usage
	}
	result, err := agent.PromptUsingOptions(agent.WithUsageModule(ctx, "cockpit-patrol"), gateway, prompt, agent.PromptOptions{Sandbox: true, DisableTools: true})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (s *Server) newCockpitPatrolService(settingsPath string, logger *log.Logger) *cockpit.Service {
	path := ""
	if settingsPath != "" {
		path = filepath.Join(filepath.Dir(settingsPath), "cockpit-briefs.json")
	}
	store, err := cockpit.OpenStore(path)
	if err != nil {
		// 巡查记录损坏不应阻止整个应用启动：驾驶舱降级为不可用，其余模块照常工作。
		if logger != nil {
			logger.Printf("level=error event=cockpit_store_unavailable error=%q", err.Error())
		}
		return nil
	}
	return cockpit.NewService(store, s.cockpitFeed(), cockpitNarrator{gateway: s.agentGateway, usage: s.usageGateway}, logger)
}

func (s *Server) cockpitFeed() cockpit.Feed {
	var feed cockpit.Feed
	if s.marketOverview != nil {
		feed.Overseas = func(ctx context.Context) ([]cockpit.IndexQuote, error) {
			items, _, err := s.marketOverview.MarketIndexes(ctx, "global")
			if err != nil {
				return nil, err
			}
			return cockpitIndexQuotes(items, false), nil
		}
		feed.CNIndexes = func(ctx context.Context) ([]cockpit.IndexQuote, error) {
			items, _, err := s.marketOverview.MarketIndexes(ctx, "core")
			if err != nil {
				return nil, err
			}
			return cockpitIndexQuotes(items, true), nil
		}
		feed.Industry = func(ctx context.Context) ([]cockpit.FlowItem, error) {
			rows, _, err := s.marketOverview.MarketFundFlows(ctx, "industry", "net", 60)
			if err != nil {
				return nil, err
			}
			items := make([]cockpit.FlowItem, 0, len(rows))
			for _, row := range rows {
				items = append(items, cockpit.FlowItem{Name: row.Name, NetInflow: row.NetInflow, ChangePercent: row.ChangePercent})
			}
			return items, nil
		}
		if provider, ok := s.marketOverview.(SectorFlowProvider); ok {
			feed.Concepts = func(ctx context.Context) ([]cockpit.FlowItem, error) {
				rows, _, err := provider.SectorFlowRanking(ctx, "concept", 60)
				if err != nil {
					return nil, err
				}
				items := make([]cockpit.FlowItem, 0, len(rows))
				for _, row := range rows {
					items = append(items, cockpit.FlowItem{Name: row.Name, NetInflow: row.MainNetInflow, ChangePercent: row.ChangePercent})
				}
				return items, nil
			}
		}
	}
	if s.newsProvider != nil {
		feed.News = func(ctx context.Context) ([]cockpit.NewsLine, error) {
			items, err := s.newsProvider.LatestNews(ctx, 20)
			if err != nil {
				return nil, err
			}
			lines := make([]cockpit.NewsLine, 0, len(items))
			for _, item := range items {
				line := cockpit.NewsLine{Title: item.Title}
				if !item.PublishedAt.IsZero() {
					line.PublishedAt = item.PublishedAt.In(foundation.AStockLocation).Format("15:04")
				}
				lines = append(lines, line)
			}
			return lines, nil
		}
	}
	if s.marketEmotion != nil {
		feed.Emotion = func(ctx context.Context) (*cockpit.Emotion, error) {
			history, err := s.marketEmotion.load(ctx)
			if err != nil {
				return nil, err
			}
			if history.Latest == nil {
				return nil, errors.New("暂无情绪历史")
			}
			return cockpitEmotion(history.Latest), nil
		}
	}
	if s.marketEmotion != nil && s.limitUpSnapshots != nil && s.limitUpProvider != nil {
		feed.LimitUp = func(ctx context.Context) (*cockpit.LimitUpView, error) {
			ladder, err := s.limitUpSnapshots.load(ctx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
			if err != nil {
				return nil, err
			}
			return cockpitLimitUp(ladder), nil
		}
		feed.Intraday = func(ctx context.Context) (*cockpit.IntradayView, error) {
			ladder, err := s.limitUpSnapshots.load(ctx, s.limitUpProvider, s.stockConcepts, s.realtimeProvider)
			if err != nil {
				return nil, err
			}
			history, err := s.marketEmotion.load(ctx)
			if err != nil {
				return nil, err
			}
			return cockpitIntraday(buildMarketEmotionIntraday(ladder, history.Latest)), nil
		}
	}
	return feed
}

// omittedOverseasCodes / Names 是产品上不进入巡查的欧洲指数（英国富时100、德国DAX30、法国CAC40）。
// 三者收盘时间与 A 股交易时段错开，盘中看到的是隔夜旧值，留在海外板块只会稀释真正有效的指数，
// 因此采集端就不返回，AI 叙述也不会引用。不同上游给的代码不一致（FTSE/UKX），用名称兜底。
var omittedOverseasCodes = map[string]bool{"FTSE": true, "UKX": true, "GDAXI": true, "DAX": true, "FCHI": true, "CAC": true}

var omittedOverseasNames = map[string]bool{"英国富时100": true, "富时100": true, "德国DAX30": true, "德国DAX": true, "法国CAC40": true, "法国CAC": true}

// cockpitIndexQuotes 按市场拆分：A 股指数只保留 CN，海外指数排除 CN。
// 上游的 global/core 范围会同时返回两类指数，不拆分就会把 A 股混进海外板块。
func cockpitIndexQuotes(items []foundation.MarketIndexSnapshot, mainland bool) []cockpit.IndexQuote {
	quotes := make([]cockpit.IndexQuote, 0, len(items))
	for _, item := range items {
		if (item.Market == "CN") != mainland {
			continue
		}
		if !mainland && (omittedOverseasCodes[strings.ToUpper(strings.TrimSpace(item.Code))] || omittedOverseasNames[strings.TrimSpace(item.Name)]) {
			continue
		}
		quote := cockpit.IndexQuote{Name: item.Name, Code: item.Code, Price: item.Price, ChangePercent: item.ChangePercent}
		if !item.TradeTime.IsZero() {
			quote.TradeTime = item.TradeTime.In(foundation.AStockLocation).Format("2006-01-02 15:04")
		}
		quotes = append(quotes, quote)
	}
	return quotes
}

func cockpitEmotion(point *marketemotion.Snapshot) *cockpit.Emotion {
	return &cockpit.Emotion{
		TradeDate:      point.TradeDate,
		Score:          point.EmotionScore,
		Phase:          point.Phase,
		LimitUp:        point.Raw.LimitUpCount,
		LimitDown:      point.Raw.LimitDownCount,
		Broken:         point.Raw.BrokenCount,
		FinalBreakRate: point.Raw.FinalBreakRate,
		MaxStreak:      point.Raw.MaxStreak,
		FirstBoard:     point.Raw.FirstBoardCount,
	}
}

func cockpitIntraday(snapshot marketemotion.IntradaySnapshot) *cockpit.IntradayView {
	return &cockpit.IntradayView{
		Status:            snapshot.Status,
		Breadth:           snapshot.Breadth,
		Summary:           snapshot.Summary,
		RiskScore:         snapshot.RiskScore,
		LimitUp:           snapshot.Metrics.LimitUpCount,
		MaxStreak:         snapshot.Metrics.CurrentMaxStreak,
		PreviousMaxStreak: snapshot.Metrics.PreviousMaxStreak,
		HighDownRate:      snapshot.Metrics.HighDownRate,
		HighWeakRate:      snapshot.Metrics.HighWeakRate,
	}
}

func cockpitLimitUp(ladder limitUpLadderData) *cockpit.LimitUpView {
	return &cockpit.LimitUpView{
		TradeDate:     ladder.Current.TradeDate,
		SessionStatus: ladder.SessionStatus,
		LimitUp:       ladder.Current.LimitUpCount,
		BoardCount:    ladder.Current.BoardCount,
		FirstBoard:    ladder.Current.FirstBoardCount,
		MaxStreak:     ladder.Current.MaxStreak,
		ReopenedCount: ladder.Current.ReopenedCount,
	}
}

func (s *Server) cockpitStatus(w http.ResponseWriter, r *http.Request) {
	if s.cockpit == nil {
		writeError(w, http.StatusServiceUnavailable, "驾驶舱巡查服务不可用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.cockpit.Status()})
}

func (s *Server) cockpitBriefs(w http.ResponseWriter, r *http.Request) {
	if s.cockpit == nil {
		writeError(w, http.StatusServiceUnavailable, "驾驶舱巡查服务不可用")
		return
	}
	phase := cockpit.Phase(strings.TrimSpace(r.URL.Query().Get("phase")))
	if phase != "" && phase != cockpit.PhasePreOpen && phase != cockpit.PhaseIntraday && phase != cockpit.PhasePostClose {
		writeError(w, http.StatusBadRequest, "phase 必须是 pre_open、intraday 或 post_close")
		return
	}
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit 必须在 1 到 100 之间")
			return
		}
		limit = parsed
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.cockpit.Store().List(phase, limit)})
}

// cockpitFacts 返回此刻采集到的事实，供驾驶舱固定模块实时刷新。
// 与巡查共用同一套 Feed：单个数据源失败只写进 sources，其余模块照常返回可查数据。
func (s *Server) cockpitFacts(w http.ResponseWriter, r *http.Request) {
	if s.cockpit == nil {
		writeError(w, http.StatusServiceUnavailable, "驾驶舱巡查服务不可用")
		return
	}
	state := cockpit.PhaseAt(time.Now())
	live := &s.cockpitLive
	live.mu.Lock()
	defer live.mu.Unlock()
	if !live.at.IsZero() && live.phase == state.Phase && time.Since(live.at) < cockpitFactsTTL {
		writeJSON(w, http.StatusOK, cockpitFactsPayload(state, live.at, live.facts))
		return
	}
	// 用后台上下文采集：客户端中途断开不应把半份结果写进缓存。
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	facts := cockpit.CollectFacts(ctx, state.Phase, s.cockpitFeed())
	live.at, live.phase, live.facts = time.Now(), state.Phase, facts
	writeJSON(w, http.StatusOK, cockpitFactsPayload(state, live.at, facts))
}

func cockpitFactsPayload(state cockpit.State, collectedAt time.Time, facts cockpit.Facts) map[string]any {
	return map[string]any{"data": map[string]any{
		"phase":        state.Phase,
		"session_date": state.SessionDate,
		"trading_day":  state.TradingDay,
		"collected_at": collectedAt.In(foundation.AStockLocation).Format(time.RFC3339),
		"facts":        facts,
	}}
}

func (s *Server) cockpitBrief(w http.ResponseWriter, r *http.Request) {
	if s.cockpit == nil {
		writeError(w, http.StatusServiceUnavailable, "驾驶舱巡查服务不可用")
		return
	}
	brief, ok := s.cockpit.Store().Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "巡查记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": brief})
}

func (s *Server) cockpitRun(w http.ResponseWriter, r *http.Request) {
	if s.cockpit == nil {
		writeError(w, http.StatusServiceUnavailable, "驾驶舱巡查服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	var request struct {
		Phase string `json:"phase"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的巡查请求")
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	phase := cockpit.Phase(strings.TrimSpace(request.Phase))
	if phase != cockpit.PhasePreOpen && phase != cockpit.PhaseIntraday && phase != cockpit.PhasePostClose {
		writeError(w, http.StatusBadRequest, "phase 必须是 pre_open、intraday 或 post_close")
		return
	}
	brief, err := s.cockpit.RunManual(phase)
	if errors.Is(err, cockpit.ErrBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "启动巡查失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": brief})
}

// RunCockpitPatrol 按 30 秒周期检查计划时点；巡查本身在后台执行，调度循环不会被阻塞。
func (s *Server) RunCockpitPatrol(ctx context.Context) {
	if s.cockpit == nil {
		return
	}
	s.logSchedulerLifecycle(ctx, "cockpit", "patrol", func() {
		ticker := time.NewTicker(cockpitPatrolInterval)
		defer ticker.Stop()
		s.cockpit.Tick()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cockpit.Tick()
			}
		}
	})
}
