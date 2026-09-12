package sector

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"easy-stock/backend/internal/foundation"
)

// radarLimitUpCacheTTL keeps the radar overview from re-reading the limit-up
// pool on every page load; the pool itself only advances a few times a session.
const radarLimitUpCacheTTL = 3 * time.Minute

// radarLimitUpLookbackDays is the window used to derive consecutive active days.
const radarLimitUpLookbackDays = 8

// industryLimitUpStats summarises the limit-up pool for one industry board.
type industryLimitUpStats struct {
	TradeDate     string
	LimitUpCount  int
	MaxStreak     int
	PreviousCount int
	ActiveDays    int
}

// industryLimitUpStatsByIndustry returns per-industry limit-up statistics keyed
// by normalized industry name. Results are cached briefly because the radar
// overview is requested on every page load.
func (p *RadarProvider) industryLimitUpStatsByIndustry(ctx context.Context) map[string]industryLimitUpStats {
	if p.limitUp == nil {
		return nil
	}
	p.limitUpMu.Lock()
	if len(p.limitUpStats) > 0 && p.now().Sub(p.limitUpFetchedAt) < radarLimitUpCacheTTL {
		cached := p.limitUpStats
		p.limitUpMu.Unlock()
		return cached
	}
	p.limitUpMu.Unlock()

	events, err := p.limitUp.RecentLimitUps(ctx, radarLimitUpLookbackDays)
	if err != nil || len(events) == 0 {
		return nil
	}
	stats := aggregateIndustryLimitUps(events)
	if len(stats) == 0 {
		return nil
	}
	p.limitUpMu.Lock()
	p.limitUpStats = stats
	p.limitUpFetchedAt = p.now()
	p.limitUpMu.Unlock()
	return stats
}

// aggregateIndustryLimitUps groups limit-up events by industry and by trade
// date so the radar can report today's limit-up count, the highest streak and
// how many consecutive sessions the industry stayed active.
func aggregateIndustryLimitUps(events []foundation.LimitUpEvent) map[string]industryLimitUpStats {
	type dayStat struct {
		count     int
		maxStreak int
	}
	byIndustry := map[string]map[string]dayStat{}
	dates := map[string]time.Time{}
	var latest time.Time
	for _, event := range events {
		industry := normalizeIndustryKey(event.Industry)
		if industry == "" || event.Date.IsZero() {
			continue
		}
		date := event.Date.Format(radarDateLayout)
		if byIndustry[industry] == nil {
			byIndustry[industry] = map[string]dayStat{}
		}
		current := byIndustry[industry][date]
		current.count++
		current.maxStreak = max(current.maxStreak, event.Streak)
		byIndustry[industry][date] = current
		dates[date] = event.Date
		if event.Date.After(latest) {
			latest = event.Date
		}
	}
	if len(byIndustry) == 0 || latest.IsZero() {
		return nil
	}
	ordered := make([]string, 0, len(dates))
	for date := range dates {
		ordered = append(ordered, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ordered))) // yyyy-mm-dd sorts lexically
	latestDate := latest.Format(radarDateLayout)
	previousDate := ""
	for index, date := range ordered {
		if date == latestDate && index+1 < len(ordered) {
			previousDate = ordered[index+1]
			break
		}
	}

	result := make(map[string]industryLimitUpStats, len(byIndustry))
	for industry, days := range byIndustry {
		today, ok := days[latestDate]
		if !ok {
			// No limit-up in the newest session: the radar would rather show
			// nothing than a stale streak.
			continue
		}
		stats := industryLimitUpStats{
			TradeDate:    latestDate,
			LimitUpCount: today.count,
			MaxStreak:    today.maxStreak,
		}
		if previousDate != "" {
			stats.PreviousCount = days[previousDate].count
		}
		for _, date := range ordered {
			if days[date].count == 0 {
				break
			}
			stats.ActiveDays++
		}
		result[industry] = stats
	}
	return result
}

const radarDateLayout = "2006-01-02"

// normalizeIndustryKey folds the small naming differences between the limit-up
// pool (abbreviated EastMoney board names) and the radar's industry board names.
func normalizeIndustryKey(name string) string {
	key := strings.TrimSpace(name)
	if key == "" {
		return ""
	}
	key = strings.NewReplacer(" ", "", "\u3000", "", "・", "", "·", "").Replace(key)
	for _, suffix := range []string{"板块", "概念", "行业", "Ⅱ", "Ⅲ", "Ⅰ"} {
		key = strings.TrimSuffix(key, suffix)
	}
	return strings.TrimSpace(key)
}

// lookupIndustryLimitUp resolves limit-up statistics for a radar industry row.
func lookupIndustryLimitUp(stats map[string]industryLimitUpStats, names ...string) (industryLimitUpStats, bool) {
	return lookupByIndustryName(stats, names...)
}

// lookupByIndustryName resolves a value keyed by normalized industry name.
// The underlying sources abbreviate some board names (for example 农产品加 for
// 农产品加工), so an exact match is tried first and a truncation-tolerant
// prefix match is used only for names long enough to stay unambiguous.
func lookupByIndustryName[T any](values map[string]T, names ...string) (T, bool) {
	var zero T
	if len(values) == 0 {
		return zero, false
	}
	keys := make([]string, 0, len(names))
	for _, name := range names {
		if key := normalizeIndustryKey(name); key != "" {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	bestKey := ""
	bestLength := 0
	for _, key := range keys {
		if utf8.RuneCountInString(key) < 3 {
			continue
		}
		for candidate := range values {
			if utf8.RuneCountInString(candidate) < 3 {
				continue
			}
			if !strings.HasPrefix(key, candidate) && !strings.HasPrefix(candidate, key) {
				continue
			}
			if length := utf8.RuneCountInString(candidate); length > bestLength {
				bestKey = candidate
				bestLength = length
			}
		}
	}
	if bestKey == "" {
		return zero, false
	}
	return values[bestKey], true
}
