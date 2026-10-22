package cockpit

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type IndexQuote struct {
	Name          string  `json:"name"`
	Code          string  `json:"code"`
	Price         float64 `json:"price"`
	ChangePercent float64 `json:"change_percent"`
	TradeTime     string  `json:"trade_time,omitempty"`
}

type Emotion struct {
	TradeDate      string  `json:"trade_date"`
	Score          float64 `json:"score"`
	Phase          string  `json:"phase"`
	LimitUp        int     `json:"limit_up"`
	LimitDown      int     `json:"limit_down"`
	Broken         int     `json:"broken"`
	FinalBreakRate float64 `json:"final_break_rate"`
	MaxStreak      int     `json:"max_streak"`
	FirstBoard     int     `json:"first_board"`
}

type IntradayView struct {
	Status            string  `json:"status"`
	Breadth           string  `json:"breadth"`
	Summary           string  `json:"summary"`
	RiskScore         float64 `json:"risk_score"`
	LimitUp           int     `json:"limit_up"`
	MaxStreak         int     `json:"max_streak"`
	PreviousMaxStreak int     `json:"previous_max_streak"`
	HighDownRate      float64 `json:"high_down_rate"`
	HighWeakRate      float64 `json:"high_weak_rate"`
}

type LimitUpView struct {
	TradeDate     string `json:"trade_date"`
	SessionStatus string `json:"session_status"`
	LimitUp       int    `json:"limit_up"`
	BoardCount    int    `json:"board_count"`
	FirstBoard    int    `json:"first_board"`
	MaxStreak     int    `json:"max_streak"`
	ReopenedCount int    `json:"reopened_count"`
}

type FlowItem struct {
	Name          string  `json:"name"`
	NetInflow     float64 `json:"net_inflow"`
	ChangePercent float64 `json:"change_percent"`
}

// FlowBoard 只保留资金流入最多与流出最多的若干条目，方向比完整榜单更容易读。
type FlowBoard struct {
	Inflow  []FlowItem `json:"inflow"`
	Outflow []FlowItem `json:"outflow"`
}

// BuildMoveBoard 按即时涨跌幅排序取样：涨幅最大的 n 条与跌幅最大的 n 条。
// 资金榜按净流入排序，尾部板块的涨跌幅未必是当日最差的；题材涨跌这张卡要的是
// 「涨得最多/跌得最多」，所以单独按 ChangePercent 取两端，避免领跌侧样本不足。
func BuildMoveBoard(items []FlowItem, n int) *FlowBoard {
	if len(items) == 0 || n <= 0 {
		return nil
	}
	sorted := append([]FlowItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ChangePercent > sorted[j].ChangePercent })
	board := &FlowBoard{Inflow: []FlowItem{}, Outflow: []FlowItem{}}
	for _, item := range sorted {
		if len(board.Inflow) >= n {
			break
		}
		if item.ChangePercent > 0 {
			board.Inflow = append(board.Inflow, item)
		}
	}
	for i := len(sorted) - 1; i >= 0 && len(board.Outflow) < n; i-- {
		if sorted[i].ChangePercent < 0 {
			board.Outflow = append(board.Outflow, sorted[i])
		}
	}
	return board
}

type NewsLine struct {
	Title       string `json:"title"`
	PublishedAt string `json:"published_at,omitempty"`
}

type SourceResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Facts struct {
	Overseas  []IndexQuote  `json:"overseas,omitempty"`
	CNIndexes []IndexQuote  `json:"cn_indexes,omitempty"`
	Emotion   *Emotion      `json:"emotion,omitempty"`
	Intraday  *IntradayView `json:"intraday,omitempty"`
	LimitUp   *LimitUpView  `json:"limit_up,omitempty"`
	Industry  *FlowBoard    `json:"industry_flows,omitempty"`
	Concepts  *FlowBoard    `json:"concept_flows,omitempty"`
	// ConceptMoves 是题材涨跌卡的取样（按涨跌幅两端各取若干），与资金榜互不影响。
	ConceptMoves *FlowBoard     `json:"concept_moves,omitempty"`
	News         []NewsLine     `json:"news,omitempty"`
	Sources      []SourceResult `json:"sources"`
}

// Feed 是采集器依赖的数据入口。字段为 nil 表示该数据源未接入。
type Feed struct {
	Overseas  func(context.Context) ([]IndexQuote, error)
	CNIndexes func(context.Context) ([]IndexQuote, error)
	Emotion   func(context.Context) (*Emotion, error)
	Intraday  func(context.Context) (*IntradayView, error)
	LimitUp   func(context.Context) (*LimitUpView, error)
	Industry  func(context.Context) ([]FlowItem, error)
	Concepts  func(context.Context) ([]FlowItem, error)
	News      func(context.Context) ([]NewsLine, error)
}

// BuildFlowBoard 取净流入最多的 n 条与净流出最多的 n 条。
func BuildFlowBoard(items []FlowItem, n int) *FlowBoard {
	if len(items) == 0 || n <= 0 {
		return nil
	}
	sorted := append([]FlowItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].NetInflow > sorted[j].NetInflow })
	board := &FlowBoard{Inflow: []FlowItem{}, Outflow: []FlowItem{}}
	for _, item := range sorted {
		if len(board.Inflow) < n && item.NetInflow > 0 {
			board.Inflow = append(board.Inflow, item)
		}
	}
	for i := len(sorted) - 1; i >= 0 && len(board.Outflow) < n; i-- {
		if sorted[i].NetInflow < 0 {
			board.Outflow = append(board.Outflow, sorted[i])
		}
	}
	return board
}

// CollectFacts 并发采集本阶段需要的数据。单个数据源失败只记录在 Sources 中，
// 其余数据仍然保留，驾驶舱不会因为一个接口故障整体空白。
func CollectFacts(ctx context.Context, phase Phase, feed Feed) Facts {
	c := &collector{facts: Facts{Sources: []SourceResult{}}}
	if feed.Overseas != nil {
		c.run(ctx, "海外指数", func(ctx context.Context) error {
			v, err := feed.Overseas(ctx)
			c.set(func(f *Facts) { f.Overseas = v })
			return err
		})
	}
	if feed.CNIndexes != nil {
		c.run(ctx, "A股指数", func(ctx context.Context) error {
			v, err := feed.CNIndexes(ctx)
			c.set(func(f *Facts) { f.CNIndexes = v })
			return err
		})
	}
	if feed.Emotion != nil {
		c.run(ctx, "情绪周期", func(ctx context.Context) error {
			v, err := feed.Emotion(ctx)
			c.set(func(f *Facts) { f.Emotion = v })
			return err
		})
	}
	// Keep the intraday market-judgment card available after the close as well.
	// The feed contains the latest real snapshot, and the dashboard should not
	// lose the module merely because the scheduler moved to post_close.
	if feed.Intraday != nil && (phase == PhaseIntraday || phase == PhasePostClose) {
		c.run(ctx, "盘中情绪", func(ctx context.Context) error {
			v, err := feed.Intraday(ctx)
			c.set(func(f *Facts) { f.Intraday = v })
			return err
		})
	}
	if feed.LimitUp != nil {
		c.run(ctx, "涨停梯队", func(ctx context.Context) error {
			v, err := feed.LimitUp(ctx)
			c.set(func(f *Facts) { f.LimitUp = v })
			return err
		})
	}
	if feed.Industry != nil {
		c.run(ctx, "行业资金", func(ctx context.Context) error {
			items, err := feed.Industry(ctx)
			c.set(func(f *Facts) { f.Industry = BuildFlowBoard(items, 8) })
			return err
		})
	}
	if feed.Concepts != nil {
		c.run(ctx, "概念资金", func(ctx context.Context) error {
			items, err := feed.Concepts(ctx)
			c.set(func(f *Facts) {
				f.Concepts = BuildFlowBoard(items, 8)
				// 题材卡与资金卡共用同一次上游请求，只是各自按不同口径截断。
				f.ConceptMoves = BuildMoveBoard(items, 5)
			})
			return err
		})
	}
	if feed.News != nil {
		c.run(ctx, "快讯", func(ctx context.Context) error {
			v, err := feed.News(ctx)
			if len(v) > 15 {
				v = v[:15]
			}
			c.set(func(f *Facts) { f.News = v })
			return err
		})
	}
	c.wait()
	sort.SliceStable(c.facts.Sources, func(i, j int) bool { return c.facts.Sources[i].Name < c.facts.Sources[j].Name })
	return c.facts
}

type collector struct {
	mu    sync.Mutex
	wg    sync.WaitGroup
	facts Facts
}

func (c *collector) run(ctx context.Context, name string, fn func(context.Context) error) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("采集异常: %v", r)
				}
			}()
			err = fn(ctx)
		}()
		c.mu.Lock()
		defer c.mu.Unlock()
		result := SourceResult{Name: name, OK: err == nil}
		if err != nil {
			result.Error = err.Error()
		}
		c.facts.Sources = append(c.facts.Sources, result)
	}()
}

func (c *collector) set(fn func(*Facts)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.facts)
}

func (c *collector) wait() {
	c.wg.Wait()
}
