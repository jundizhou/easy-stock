package cockpit

import (
	"fmt"
	"time"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/foundation"
)

type Phase string

const (
	PhasePreOpen   Phase = "pre_open"
	PhaseIntraday  Phase = "intraday"
	PhasePostClose Phase = "post_close"
	PhaseRest      Phase = "rest"
)

const (
	preOpenRunMinutes   = 8*60 + 40
	sessionOpenMinutes  = 9*60 + 25
	sessionCloseMinutes = 15*60 + 5
	postCloseMinutes    = 15*60 + 35
	restEveningMinutes  = 20 * 60
)

// 上午和下午各在开盘 10 分钟后开始，收盘前按所选间隔安排；午休不安排时点。
func intradaySlots(intervalMinutes int) []int {
	intervalMinutes = appsettings.NormalizeCockpitPatrolIntervalMinutes(intervalMinutes)
	var slots []int
	for _, session := range [][2]int{{9*60 + 40, 11*60 + 30}, {13*60 + 10, 15 * 60}} {
		for minute := session[0]; minute < session[1]; minute += intervalMinutes {
			slots = append(slots, minute)
		}
	}
	return slots
}

type State struct {
	Phase       Phase
	SessionDate string
	TradingDay  bool
}

type Slot struct {
	RunAt       time.Time
	Key         string
	Phase       Phase
	SessionDate string
	Label       string
	Caveat      string
}

func PhaseLabel(phase Phase) string {
	switch phase {
	case PhasePreOpen:
		return "盘前"
	case PhaseIntraday:
		return "盘中"
	case PhasePostClose:
		return "盘后"
	default:
		return "休市"
	}
}

// PhaseAt 按北京时间与 A 股交易日历判定当前所处阶段。休市日的 SessionDate
// 指向下一交易日，使周末的外围消息归属于下一个开盘会话。
func PhaseAt(now time.Time) State {
	local := now.In(foundation.AStockLocation)
	today := local.Format("2006-01-02")
	if !foundation.IsAStockTradingDay(local) {
		return State{Phase: PhaseRest, SessionDate: nextTradingDay(local), TradingDay: false}
	}
	minutes := minuteOfDay(local)
	switch {
	case minutes < sessionOpenMinutes:
		return State{Phase: PhasePreOpen, SessionDate: today, TradingDay: true}
	case minutes < sessionCloseMinutes:
		return State{Phase: PhaseIntraday, SessionDate: today, TradingDay: true}
	default:
		return State{Phase: PhasePostClose, SessionDate: today, TradingDay: true}
	}
}

// DueSlot 返回现在应当执行的一次巡查。盘中只返回最近一个已到期的计划时点：
// 服务中途启动时只补跑一次，不会把错过的时点逐个重放。
func DueSlot(now time.Time, intervalMinutes int) (Slot, bool) {
	local := now.In(foundation.AStockLocation)
	state := PhaseAt(local)
	minutes := minuteOfDay(local)
	switch state.Phase {
	case PhasePreOpen:
		if minutes >= preOpenRunMinutes {
			return Slot{Key: "pre", Phase: PhasePreOpen, SessionDate: state.SessionDate, Label: "盘前巡查"}, true
		}
	case PhaseIntraday:
		due := -1
		for _, slot := range intradaySlots(intervalMinutes) {
			if minutes >= slot {
				due = slot
			}
		}
		if due >= 0 {
			key := clockText(due)
			return Slot{Key: key, Phase: PhaseIntraday, SessionDate: state.SessionDate, Label: "盘中巡查 " + key,
				RunAt: time.Date(local.Year(), local.Month(), local.Day(), due/60, due%60, 0, 0, foundation.AStockLocation)}, true
		}
	case PhasePostClose:
		if minutes >= postCloseMinutes {
			return Slot{Key: "post", Phase: PhasePostClose, SessionDate: state.SessionDate, Label: "盘后复盘"}, true
		}
	case PhaseRest:
		if minutes >= restEveningMinutes {
			return Slot{Key: "weekend", Phase: PhasePreOpen, SessionDate: state.SessionDate, Label: "周末外围巡查"}, true
		}
	}
	return Slot{}, false
}

// NextRun 返回下一次计划巡查的时间与名称，用于状态条展示。
func NextRun(now time.Time, intervalMinutes int) (time.Time, string) {
	local := now.In(foundation.AStockLocation)
	for offset := 0; offset < 10; offset++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+offset, 0, 0, 0, 0, foundation.AStockLocation)
		type candidate struct {
			minutes int
			label   string
		}
		var candidates []candidate
		if foundation.IsAStockTradingDay(day) {
			candidates = append(candidates, candidate{preOpenRunMinutes, "盘前巡查"})
			for _, slot := range intradaySlots(intervalMinutes) {
				candidates = append(candidates, candidate{slot, "盘中巡查 " + clockText(slot)})
			}
			candidates = append(candidates, candidate{postCloseMinutes, "盘后复盘"})
		} else {
			candidates = append(candidates, candidate{restEveningMinutes, "周末外围巡查"})
		}
		for _, item := range candidates {
			at := time.Date(day.Year(), day.Month(), day.Day(), item.minutes/60, item.minutes%60, 0, 0, foundation.AStockLocation)
			if at.After(local) {
				return at, item.label
			}
		}
	}
	return time.Time{}, ""
}

func nextTradingDay(value time.Time) string {
	day := value
	for i := 0; i < 14; i++ {
		day = day.AddDate(0, 0, 1)
		if foundation.IsAStockTradingDay(day) {
			break
		}
	}
	return day.Format("2006-01-02")
}

func minuteOfDay(value time.Time) int {
	return value.Hour()*60 + value.Minute()
}

func clockText(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
